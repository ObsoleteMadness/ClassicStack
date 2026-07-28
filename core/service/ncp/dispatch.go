package ncp

// dispatch.go is the transport-independent NCP command engine: it decodes one NCP
// request (already stripped of its IPX framing by the transport), demuxes on the
// request type and function code, acts over the bound Volume's §9 storage seam,
// and returns the reply body (the transport prepends the reply header and IPX
// framing). The spine holds no transport knowledge, so it is unit-tested directly
// over raw NCP frames (dispatch_test.go).
//
// NetWare function multiplexing: a few function codes (0x16 "directory services",
// 0x17 "connection/bindery services", 0x22 "file/dir services") are themselves
// multiplexed — the body begins with a 2-byte subfunction-length then a
// subfunction byte. The plain file functions (0x42 close, 0x47 get size, 0x48
// read, 0x49 write, …) take their arguments directly.
//
// Reference: Novell NCP function codes; mars_nwe / ncpfs (CLAUDE.md #7).

import (
	"errors"
	"os"
	"time"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	ncpproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ncp"
)

// NCP function codes the engine recognises (named from mars_nwe nwconn.c's
// dispatch switch). Functions not listed here answer CompletionFuncNotSupp.
const (
	fnFileSearchInit     uint8 = 0x3E // begin a directory scan
	fnFileSearchContinue uint8 = 0x3F // continue a directory scan
	fnSearchForFile      uint8 = 0x40 // Search for a File (FCB-era search, one call per entry)
	fnOpenForRead        uint8 = 0x41 // open file for reading
	fnCloseFile          uint8 = 0x42 // close file
	fnCreateFile         uint8 = 0x43 // create file, overwrite if exists
	fnEraseFile          uint8 = 0x44 // erase/delete file
	fnRenameFile         uint8 = 0x45 // rename file
	fnSetFileAttributes  uint8 = 0x46 // set file attributes
	fnGetFileSize        uint8 = 0x47 // seek to end, return file size
	fnReadFile           uint8 = 0x48 // read file
	fnWriteFile          uint8 = 0x49 // write file
	fnOpenFile           uint8 = 0x4C // open file
	fnCreateNewFile      uint8 = 0x4D // create new file
	fnDirServices        uint8 = 0x16 // multiplexed dir-handle / volume services
	fnConnBindery        uint8 = 0x17 // multiplexed connection/bindery services
	fnNameSpace          uint8 = 0x57 // name-space family (OS/2 & Mac long names); subfn at body[0]
	fnGetVolInfoNumber   uint8 = 0x12 // Get Volume Info with Number
	fnGetStationNumber   uint8 = 0x13 // Get Station Number (connection number)
	fnGetServerDateTime  uint8 = 0x14 // get file-server date/time
	fnEndOfJob           uint8 = 0x18 // end of job
	fnLogout             uint8 = 0x19 // logout
	fnNegotiateBuffer    uint8 = 0x21 // Negotiate Buffer Size (max read/write packet)
	fnTTS                uint8 = 0x22 // Transaction Tracking System family; subfn at body[0]
	fnAFP                uint8 = 0x23 // AFP-namespace family (answered CompletionBadNameSpace)
	fnCommitFile         uint8 = 0x3B // commit file to disk
	fnCommitFile2        uint8 = 0x3D // commit file (older form)
)

// Synchronization (log/lock/release/clear) function codes — mars_nwe nwconn.c
// cases 0x3..0xe and the physical-record calls. ClassicStack keeps no
// cross-connection lock manager: the whole family is acknowledged as granted
// (grantLock), the same practical posture the SMB service takes. 0x04 (Lock File
// Set) and 0x0C (Release Logical Record) are NOT accepted — mars_nwe leaves both
// to its unsupported default and clients tolerate 0xFB there.
const (
	fnLogFile             uint8 = 0x03 // log (and optionally lock) a file
	fnReleaseFile         uint8 = 0x05 // release a file lock (keep it logged)
	fnReleaseFileSet      uint8 = 0x06 // release every file lock in the set
	fnClearFile           uint8 = 0x07 // clear a file from the log set
	fnClearFileSet        uint8 = 0x08 // clear the whole file log set
	fnLogLogicalRecord    uint8 = 0x09 // log (and optionally lock) a logical record
	fnLogLogicalRecordSet uint8 = 0x0A // lock the logged logical-record set
	fnClearLogicalRecord  uint8 = 0x0B // clear a logical record
	fnReleaseLogRecordSet uint8 = 0x0D // release the logical-record set
	fnClearLogRecordSet   uint8 = 0x0E // clear the logical-record set
	fnLogPhysicalRecord   uint8 = 0x1A // log/lock a physical byte range of an open file
	fnClearPhysicalRecord uint8 = 0x1E // clear a physical byte-range lock
	fnClearPhysRecordSet  uint8 = 0x1F // clear the physical-record set (mars_nwe: dummy)
)

// Subfunctions of fnConnBindery (0x17). Get-server-info / bindery-access / login
// are handled by the bindery layer in mars_nwe (nwbind.c); we handle them inline.
const (
	sf17GetServerInfo    uint8 = 0x11 // Get File Server Information
	sf17GetBinderyAccess uint8 = 0x46 // Get Bindery Access Level
	sf17GetInetAddrOld   uint8 = 0x13 // Get Connection Internet Address (old, 1-byte conn)
	sf17LoginUnencrypted uint8 = 0x14 // Login To File Server (cleartext)
	sf17GetObjConnList   uint8 = 0x15 // Get Object Connection List (old, 1-byte conn numbers)
	sf17GetConnInfoOld   uint8 = 0x16 // Get Connection Information (old; "Get Station's Logged Info")
	sf17GetLoginKey      uint8 = 0x17 // Get login encryption key (challenge)
	sf17LoginEncrypted   uint8 = 0x18 // Keyed login (challenge-response)
	sf17GetInetAddr      uint8 = 0x1A // Get Connection Internet Address (new, +conn-type byte)
	sf17GetObjConnList2  uint8 = 0x1B // Get Object Connection List (new, 2-byte conn numbers)
	sf17GetConnInfo      uint8 = 0x1C // Get Connection Information (new, 4-byte conn)
	sf17GetObjectID      uint8 = 0x35 // Get Bindery Object ID (by type+name)
	sf17GetObjectName    uint8 = 0x36 // Get Bindery Object Name (by id)
	sf17ScanObject       uint8 = 0x37 // Scan Bindery Object (wildcard scan)
)

// Subfunctions of fnDirServices (0x16) — mars_nwe nwconn.c case 0x16. Allocate has
// three flavours (permanent/temp/special-temp); all build a dir handle.
const (
	sf16SetDirHandle    uint8 = 0x00 // Set Directory Handle (retarget an existing handle)
	sf16GetDirPath      uint8 = 0x01 // Get Directory Path ("VOL:path" of a handle)
	sf16ScanDirInfo     uint8 = 0x02 // Scan Directory Information (Nth subdirectory)
	sf16GetEffDirRights uint8 = 0x03 // Get Effective Directory Rights
	sf16GetVolumeNumber uint8 = 0x05 // Get Volume Number (by name)
	sf16GetVolumeName   uint8 = 0x06 // Get Volume Name (number 0..31)
	sf16CreateDir       uint8 = 0x0A // Create Directory
	sf16DeleteDir       uint8 = 0x0B // Delete Directory
	sf16RenameDir       uint8 = 0x0F // Rename Directory (in place)
	sf16AllocPermDir    uint8 = 0x12 // Allocate Permanent Directory Handle
	sf16AllocTempDir    uint8 = 0x13 // Allocate Temporary Directory Handle
	sf16AllocSpecialDir uint8 = 0x16 // Allocate Special Temporary Directory Handle
	sf16DeallocDirHdl   uint8 = 0x14 // Deallocate Directory Handle
	sf16GetVolumeInfo   uint8 = 0x15 // Get Volume Info with Handle
	sf16SetDirInfo      uint8 = 0x19 // Set Directory Information (dates/owner/rights)
	sf16ScanVolRestrict uint8 = 0x20 // Scan volume user disk restrictions
	sf16GetVolPurgeInfo uint8 = 0x2C // Get Volume and Purge Information (NW 3.11+; ncpfs)
	sf16GetDirInfo      uint8 = 0x2D // Get Directory Information (usage for a handle's volume)
)

// errFuncNotSupported is the engine's sentinel for an unrecognised function/
// subfunction; the dispatch maps it to CompletionFuncNotSupp and bumps the
// unsupported counter.
var errFuncNotSupported = errors.New("ncp: function not supported")

// Conn is one client's NCP circuit over a transport, bound to its service
// connection. The transport creates one per remote endpoint via Service.NewConn
// and feeds it whole NCP request bodies; ServeRequest returns the reply body. This
// mirrors the smb.Conn seam.
type Conn struct {
	svc *Service
	c   *connection
}

// NewConn binds a transport circuit to an existing service connection.
func (s *Service) NewConn(c *connection) *Conn { return &Conn{svc: s, c: c} }

// ServeRequest dispatches one decoded NCP request and returns (completionCode,
// replyBody). The transport has already matched the request to this circuit's
// connection. A request type other than TypeRequest is a framing error the
// transport handles before calling here.
func (cn *Conn) ServeRequest(req *ncpproto.RequestHeader) (uint8, []byte) {
	cn.c.touch()
	cn.svc.logging.Log2(log.Trace, "NCP request",
		log.Str("fn", fnString(req)), log.Int("conn", int64(cn.c.number)))
	body, err := cn.handle(req)
	if err != nil {
		code := cn.svc.completionFor(err)
		// Every non-success completion is narrated at Debug with the function (and
		// subfunction, for the multiplexed families) so an unsupported or failing
		// verb is visible from the log without a capture.
		cn.svc.logging.Log(log.Debug, "NCP request failed",
			log.Str("fn", fnString(req)),
			log.Str("completion", hex8(code)),
			log.Int("conn", int64(cn.c.number)))
		return code, nil
	}
	return ncpproto.CompletionSuccess, body
}

// handle demuxes on the function code. It returns the reply body or an error the
// caller maps to a completion code.
func (cn *Conn) handle(req *ncpproto.RequestHeader) ([]byte, error) {
	switch req.Function {
	case fnGetServerDateTime:
		return cn.getServerDateTime()
	case fnGetVolInfoNumber:
		return cn.getVolumeInfoWithNumber(req.Body)
	case fnGetStationNumber:
		// Per mars_nwe (nwconn.c case 0x13): the reply is the 1-byte connection number.
		return []byte{byte(cn.c.number)}, nil
	case fnLogFile, fnReleaseFile, fnReleaseFileSet, fnClearFile, fnClearFileSet,
		fnLogLogicalRecord, fnLogLogicalRecordSet, fnClearLogicalRecord,
		fnReleaseLogRecordSet, fnClearLogRecordSet,
		fnLogPhysicalRecord, fnClearPhysicalRecord, fnClearPhysRecordSet:
		return cn.grantLock()
	case fnTTS:
		return cn.ttsCall(req.Body)
	case fnAFP:
		// Per mars_nwe (nwconn.c case 0x23): the AFP-namespace family is answered
		// "invalid name space" — the client falls back to the DOS calls.
		return nil, errBadNameSpace
	case fnCommitFile, fnCommitFile2:
		return cn.commitFile(req.Body)
	case fnSetFileAttributes:
		return cn.setFileAttributes(req.Body)
	case fnSearchForFile:
		return cn.searchForFile(req.Body)
	case fnEndOfJob, fnLogout:
		// End-of-job / logout: clear the connection's login identity but keep the
		// connection (the client may log in again). No reply body.
		cn.c.mu.Lock()
		cn.c.loggedIn = false
		cn.c.user = ""
		cn.c.objectID = 0
		cn.c.objectType = 0
		cn.c.loginTime = time.Time{}
		cn.c.mu.Unlock()
		cn.svc.pushStats()
		return nil, nil
	case fnNegotiateBuffer:
		return cn.negotiateBufferSize(req.Body)
	case fnConnBindery:
		return cn.connBindery(req.Body)
	case fnDirServices:
		return cn.dirServices(req.Body)
	case fnNameSpace:
		return cn.nameSpace(req.Body)
	case fnOpenFile, fnOpenForRead:
		return cn.openFile(req.Body, false)
	case fnCreateFile, fnCreateNewFile:
		return cn.openFile(req.Body, true)
	case fnCloseFile:
		return cn.closeFile(req.Body)
	case fnReadFile:
		return cn.readFile(req.Body)
	case fnWriteFile:
		return cn.writeFile(req.Body)
	case fnGetFileSize:
		return cn.getFileSize(req.Body)
	case fnEraseFile:
		return cn.eraseFile(req.Body)
	case fnRenameFile:
		return cn.renameFile(req.Body)
	case fnFileSearchInit:
		return cn.searchInit(req.Body)
	case fnFileSearchContinue:
		return cn.searchContinue(req.Body)
	default:
		return nil, errFuncNotSupported
	}
}

// subfunction splits a multiplexed-function body into (subfunction, args). The
// body begins with a 2-byte big-endian subfunction-length covering the
// subfunction byte and its args; we read the subfunction byte that follows.
func subfunction(body []byte) (uint8, []byte, bool) {
	if len(body) < 3 {
		return 0, nil, false
	}
	// body[0:2] = subfunction length (BE); body[2] = subfunction; rest = args.
	return body[2], body[3:], true
}

// connBindery handles the multiplexed connection/bindery services (0x17).
func (cn *Conn) connBindery(body []byte) ([]byte, error) {
	sf, args, ok := subfunction(body)
	if !ok {
		return nil, errFuncNotSupported
	}
	switch sf {
	case sf17GetServerInfo:
		return cn.getServerInfo()
	case sf17GetBinderyAccess:
		// Per mars_nwe (nwbind.c): reply is access_level(1) + object_id[4 BE]
		// (0xFFFFFFFF when not logged in). 0x33 = supervisor, 0x22 = user; we report
		// supervisor-equivalent for a logged-in connection, anonymous otherwise.
		cn.c.mu.Lock()
		id := cn.c.objectID
		cn.c.mu.Unlock()
		if id != 0 {
			return appendU32([]byte{0x33}, id), nil
		}
		return appendU32([]byte{0x00}, 0xFFFFFFFF), nil
	case sf17LoginUnencrypted:
		return cn.loginUnencrypted(args)
	case sf17GetLoginKey:
		return cn.getLoginKey()
	case sf17LoginEncrypted:
		return cn.loginEncrypted(args)
	case sf17GetConnInfoOld:
		return cn.getConnectionInfo(args, true)
	case sf17GetConnInfo:
		return cn.getConnectionInfo(args, false)
	case sf17GetInetAddrOld:
		return cn.getConnInternetAddress(args, true)
	case sf17GetInetAddr:
		return cn.getConnInternetAddress(args, false)
	case sf17GetObjConnList:
		return cn.getObjectConnList(args, true)
	case sf17GetObjConnList2:
		return cn.getObjectConnList(args, false)
	case sf17GetObjectID:
		return cn.getBinderyObjectID(args)
	case sf17GetObjectName:
		return cn.getBinderyObjectName(args)
	case sf17ScanObject:
		return cn.scanBinderyObject(args)
	default:
		return nil, errFuncNotSupported
	}
}

// dirServices handles the multiplexed dir-handle / volume services (0x16). The
// three allocate flavours (permanent/temp/special-temp) all build a directory
// handle; get-volume-info reports disk usage for the handle's volume.
func (cn *Conn) dirServices(body []byte) ([]byte, error) {
	sf, args, ok := subfunction(body)
	if !ok {
		return nil, errFuncNotSupported
	}
	switch sf {
	case sf16SetDirHandle:
		return cn.setDirHandle(args)
	case sf16GetDirPath:
		return cn.getDirPath(args)
	case sf16ScanDirInfo:
		return cn.scanDirInfo(args)
	case sf16GetEffDirRights:
		return cn.getEffDirRights(args)
	case sf16GetVolumeNumber:
		return cn.getVolumeNumber(args)
	case sf16GetVolumeName:
		return cn.getVolumeName(args)
	case sf16CreateDir:
		return cn.createDir(args)
	case sf16DeleteDir:
		return cn.deleteDir(args)
	case sf16RenameDir:
		return cn.renameDir(args)
	case sf16AllocPermDir, sf16AllocTempDir, sf16AllocSpecialDir:
		return cn.allocDirHandle(args)
	case sf16DeallocDirHdl:
		return cn.deallocDirHandle(args)
	case sf16GetVolumeInfo:
		return cn.getVolumeInfo(args)
	case sf16SetDirInfo:
		return cn.setDirInfo(args)
	case sf16ScanVolRestrict:
		return cn.scanVolRestrictions(args)
	case sf16GetVolPurgeInfo:
		return cn.getVolPurgeInfo(args)
	case sf16GetDirInfo:
		return cn.getDirInfo(args)
	default:
		return nil, errFuncNotSupported
	}
}

// completionFor maps an engine error to an NCP completion code and bumps the
// relevant counter.
func (s *Service) completionFor(err error) uint8 {
	switch {
	case errors.Is(err, errFuncNotSupported):
		s.counters.unsupportedFn.Add(1)
		return ncpproto.CompletionFuncNotSupp
	case errors.Is(err, os.ErrNotExist), errors.Is(err, fs.ErrUnrepresentable):
		return ncpproto.CompletionNoSuchFile
	case errors.Is(err, os.ErrPermission), errors.Is(err, errAccessDenied):
		return ncpproto.CompletionAccessDenied
	case errors.Is(err, errNoMoreFiles):
		return ncpproto.CompletionNoFiles
	case errors.Is(err, errBadHandle):
		return ncpproto.CompletionInvalidConn
	case errors.Is(err, errBadStation):
		return ncpproto.CompletionBadStation
	case errors.Is(err, errNoSuchObject):
		return ncpproto.CompletionNoSuchObject
	case errors.Is(err, errNoSuchVolume):
		return ncpproto.CompletionNoSuchVolume
	case errors.Is(err, errBadNameSpace):
		return ncpproto.CompletionBadNameSpace
	default:
		return ncpproto.CompletionNoSuchFile
	}
}

// errNoMoreFiles ends a directory scan; errBadHandle marks an unknown dir/file
// handle; errAccessDenied marks a login/permission failure; errNoSuchObject
// marks a bindery lookup/scan miss; errNoSuchVolume marks a bad volume
// name/number; errBadNameSpace answers the AFP-namespace family.
var (
	errNoMoreFiles  = errors.New("ncp: no more files")
	errBadHandle    = errors.New("ncp: invalid handle")
	errBadStation   = errors.New("ncp: bad station number")
	errAccessDenied = errors.New("ncp: access denied")
	errNoSuchObject = errors.New("ncp: no such bindery object")
	errNoSuchVolume = errors.New("ncp: no such volume")
	errBadNameSpace = errors.New("ncp: invalid name space")
)

// --- small helpers for building reply bodies (big-endian on the wire) ---

func appendU16(dst []byte, v uint16) []byte { return bp.AppendBE16(dst, v) }
func appendU32(dst []byte, v uint32) []byte { return bp.AppendBE32(dst, v) }
