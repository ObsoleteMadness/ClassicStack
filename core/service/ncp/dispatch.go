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

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	ncpproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ncp"
)

// NCP function codes the engine recognises (named from mars_nwe nwconn.c's
// dispatch switch). Functions not listed here answer CompletionFuncNotSupp.
const (
	fnFileSearchInit     uint8 = 0x3E // begin a directory scan
	fnFileSearchContinue uint8 = 0x3F // continue a directory scan
	fnOpenForRead        uint8 = 0x41 // open file for reading
	fnCloseFile          uint8 = 0x42 // close file
	fnCreateFile         uint8 = 0x43 // create file, overwrite if exists
	fnEraseFile          uint8 = 0x44 // erase/delete file
	fnRenameFile         uint8 = 0x45 // rename file (0x46 is set-attributes, NOT rename)
	fnGetFileSize        uint8 = 0x47 // seek to end, return file size
	fnReadFile           uint8 = 0x48 // read file
	fnWriteFile          uint8 = 0x49 // write file
	fnOpenFile           uint8 = 0x4C // open file
	fnCreateNewFile      uint8 = 0x4D // create new file
	fnDirServices        uint8 = 0x16 // multiplexed dir-handle / volume services
	fnConnBindery        uint8 = 0x17 // multiplexed connection/bindery services
	fnNameSpace          uint8 = 0x57 // name-space family (OS/2 & Mac long names); subfn at body[0]
	fnGetServerDateTime  uint8 = 0x14 // get file-server date/time
	fnEndOfJob           uint8 = 0x18 // end of job
	fnLogout             uint8 = 0x19 // logout
)

// Subfunctions of fnConnBindery (0x17). Get-server-info / bindery-access / login
// are handled by the bindery layer in mars_nwe (nwbind.c); we handle them inline.
const (
	sf17GetServerInfo    uint8 = 0x11 // Get File Server Information
	sf17GetBinderyAccess uint8 = 0x46 // Get Bindery Access Level
	sf17LoginUnencrypted uint8 = 0x14 // Login To File Server (cleartext)
	sf17GetLoginKey      uint8 = 0x17 // Get login encryption key (challenge)
	sf17LoginEncrypted   uint8 = 0x18 // Keyed login (challenge-response)
	sf17GetLoginUserID   uint8 = 0x15 // Get connection's logged identity
)

// Subfunctions of fnDirServices (0x16) — mars_nwe nwconn.c case 0x16. Allocate has
// three flavours (permanent/temp/special-temp); all build a dir handle.
const (
	sf16AllocPermDir    uint8 = 0x12 // Allocate Permanent Directory Handle
	sf16AllocTempDir    uint8 = 0x13 // Allocate Temporary Directory Handle
	sf16AllocSpecialDir uint8 = 0x16 // Allocate Special Temporary Directory Handle
	sf16DeallocDirHdl   uint8 = 0x14 // Deallocate Directory Handle
	sf16GetVolumeInfo   uint8 = 0x15 // Get Volume Info with Handle
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
	body, err := cn.handle(req)
	if err != nil {
		return cn.svc.completionFor(err), nil
	}
	return ncpproto.CompletionSuccess, body
}

// handle demuxes on the function code. It returns the reply body or an error the
// caller maps to a completion code.
func (cn *Conn) handle(req *ncpproto.RequestHeader) ([]byte, error) {
	switch req.Function {
	case fnGetServerDateTime:
		return cn.getServerDateTime()
	case fnEndOfJob, fnLogout:
		// End-of-job / logout: clear the connection's login identity but keep the
		// connection (the client may log in again). No reply body.
		cn.c.mu.Lock()
		cn.c.loggedIn = false
		cn.c.user = ""
		cn.c.mu.Unlock()
		cn.svc.pushStats()
		return nil, nil
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
		logged := cn.c.loggedIn
		cn.c.mu.Unlock()
		if logged {
			return appendU32([]byte{0x33}, 1), nil
		}
		return appendU32([]byte{0x00}, 0xFFFFFFFF), nil
	case sf17LoginUnencrypted:
		return cn.loginUnencrypted(args)
	case sf17GetLoginKey:
		return cn.getLoginKey()
	case sf17LoginEncrypted:
		return cn.loginEncrypted(args)
	case sf17GetLoginUserID:
		return cn.getLoginUserID()
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
	case sf16AllocPermDir, sf16AllocTempDir, sf16AllocSpecialDir:
		return cn.allocDirHandle(args)
	case sf16DeallocDirHdl:
		return cn.deallocDirHandle(args)
	case sf16GetVolumeInfo:
		return cn.getVolumeInfo(args)
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
	default:
		return ncpproto.CompletionNoSuchFile
	}
}

// errNoMoreFiles ends a directory scan; errBadHandle marks an unknown dir/file
// handle; errAccessDenied marks a login/permission failure.
var (
	errNoMoreFiles  = errors.New("ncp: no more files")
	errBadHandle    = errors.New("ncp: invalid handle")
	errAccessDenied = errors.New("ncp: access denied")
)

// --- small helpers for building reply bodies (big-endian on the wire) ---

func appendU16(dst []byte, v uint16) []byte { return bp.AppendBE16(dst, v) }
func appendU32(dst []byte, v uint32) []byte { return bp.AppendBE32(dst, v) }
