package ncp

// clientfileops.go holds the typed CLIENT-direction request builders and reply
// parsers for the file-service functions the NCP file client drives: negotiate buffer
// size, cleartext login, volume-number lookup, directory-handle allocation, open/
// create/close/read/write/getsize, erase/rename, and the FCB-era directory search
// (Search for a File, 0x40). Each builder produces the exact function body
// core/service/ncp parses (fileio.go / handlers.go), so a request round-trips against
// the ClassicStack server and a real NetWare 3.x server. The reply parsers read the
// bodies those handlers emit.
//
// Path form: the client addresses files by a NetWare wire path — an uppercase 8.3
// name, optionally "VOL:" volume-qualified, backslash- or slash-separated — resolved
// server-side by Volume.ResolvePath. The client speaks the DOS name space (8.3); long
// names are the name-space family (0x57), deferred here as the AppleDouble sidecars the
// client reads are themselves 8.3-representable ("._NAME").
//
// Reference: mars_nwe nwconn.c request layouts; Linux ncpfs (CLAUDE.md #7).

import (
	"errors"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
)

var (
	// ErrShortBody is returned by a reply parser when the reply body is shorter than
	// the fixed fields the function's reply carries.
	ErrShortBody = errors.New("ncp: reply body shorter than expected")
)

// --- Negotiate Buffer Size (0x21) ---

// BuildNegotiateBuffer builds fnNegotiateBuffer (0x21): the request body is the
// client's proposed buffer size (2 BE). The reply is the accepted size (2 BE) =
// min(server max, proposed).
func (r *Requester) BuildNegotiateBuffer(proposed uint16) []byte {
	return r.marshalRequest(fnNegotiateBuffer, bp.AppendBE16(nil, proposed))
}

// ParseNegotiateBuffer reads the accepted buffer size (2 BE) from a Negotiate Buffer
// Size reply body.
func ParseNegotiateBuffer(body []byte) (uint16, error) {
	if len(body) < 2 {
		return 0, ErrShortBody
	}
	return uint16(body[0])<<8 | uint16(body[1]), nil
}

// --- Login (cleartext, 0x17/0x14) ---

// objTypeUser is the NetWare bindery object type for a user (OT_USER = 1); the login
// request carries the object type of the name being logged in.
const objTypeUser uint16 = 0x0001

// BuildLogin builds the cleartext Login To File Server (0x17/0x14) request. The
// multiplexed body is: subfunction-length(2 BE, covering subfunction + args),
// subfunction(0x14), object-type(2 BE), length-prefixed user name, length-prefixed
// password — the layout handlers.go parseLoginArgs expects.
func (r *Requester) BuildLogin(user, password string) []byte {
	args := bp.AppendBE16(nil, objTypeUser)
	args = appendByteString(args, user)
	args = appendByteString(args, password)
	return r.marshalRequest(fnConnBindery, wrapSubfunction(sf17LoginUnencrypted, args))
}

// --- Encrypted bindery login (NetWare 3.x): GetLoginKey / GetBinderyObjectID / Login ---
//
// A default-configured real NetWare server refuses the cleartext login and requires the
// challenge-response bindery login: draw an 8-byte login key, resolve the user name to a
// 4-byte object ID, then send a password digest keyed by both. This is CLIENT-side only;
// the ClassicStack server stays on the cleartext NW-3.1 path. See nwcrypt.go for the
// shuffle/nw_encrypt algorithm (ncpfs / DDJ 11/93 attribution).

// BuildGetLoginKey builds Get Login Key (0x17/0x17): no args; the reply body is the
// 8-byte challenge key.
func (r *Requester) BuildGetLoginKey() []byte {
	return r.marshalRequest(fnConnBindery, wrapSubfunction(sf17GetLoginKey, nil))
}

// ParseLoginKey reads the 8-byte login key from a Get Login Key reply body.
func ParseLoginKey(body []byte) ([8]byte, error) {
	var key [8]byte
	if len(body) < 8 {
		return key, ErrShortBody
	}
	copy(key[:], body[:8])
	return key, nil
}

// BuildGetBinderyObjectID builds Get Bindery Object ID (0x17/0x35): object-type(2 BE) +
// length-prefixed name. The reply body is object-id(4 BE), object-type(2 BE), then a
// 48-byte NUL-padded name.
func (r *Requester) BuildGetBinderyObjectID(objType uint16, name string) []byte {
	args := bp.AppendBE16(nil, objType)
	args = appendByteString(args, name)
	return r.marshalRequest(fnConnBindery, wrapSubfunction(sf17GetBinderyObjectID, args))
}

// ParseBinderyObjectID reads the 4-byte object ID (big-endian) from a Get Bindery Object
// ID reply body.
func ParseBinderyObjectID(body []byte) (uint32, error) {
	if len(body) < 4 {
		return 0, ErrShortBody
	}
	return uint32(body[0])<<24 | uint32(body[1])<<16 | uint32(body[2])<<8 | uint32(body[3]), nil
}

// BuildLoginEncrypted builds Login Object Encrypted (0x17/0x18): the 8-byte
// challenge-response (nwEncrypt of the shuffled password), object-type(2 BE), and the
// length-prefixed user name. objectID is the user's bindery object ID from
// BuildGetBinderyObjectID; key is the server's login key from BuildGetLoginKey.
func (r *Requester) BuildLoginEncrypted(objType uint16, name, password string, objectID uint32, key [8]byte) []byte {
	// shuffle the password keyed by the object ID in NETWORK byte order (big-endian),
	// matching ncpfs (htonl(object_id)); then fold in the challenge key.
	lon := [4]byte{byte(objectID >> 24), byte(objectID >> 16), byte(objectID >> 8), byte(objectID)}
	var digest [16]byte
	shuffle(lon, []byte(password), &digest)
	var resp [8]byte
	nwEncrypt(key, digest, &resp)

	args := append([]byte(nil), resp[:]...)
	args = bp.AppendBE16(args, objType)
	args = appendByteString(args, name)
	return r.marshalRequest(fnConnBindery, wrapSubfunction(sf17LoginEncrypted, args))
}

// --- Get Volume Number (0x16/0x05) ---

// BuildGetVolumeNumber builds Get Volume Number (0x16/0x05): a length-prefixed volume
// name; the reply is a 1-byte volume number.
func (r *Requester) BuildGetVolumeNumber(volume string) []byte {
	args := appendByteString(nil, volume)
	return r.marshalRequest(fnDirServices, wrapSubfunction(sf16GetVolumeNumber, args))
}

// MaxVolumeSlots is the number of volume-number slots a browse iterates (0..63).
const MaxVolumeSlots = maxVolumeSlots

// BuildGetVolumeName builds Get Volume Name (0x16/0x06): a single volume-number byte. The
// reply body is a 1-byte length followed by the volume name. Iterating the volume number
// 0..MaxVolumeSlots-1 enumerates a server's mounted volumes (a not-OK completion or empty
// name means no volume in that slot) — the NetWare 3.x way to browse a server's volumes.
func (r *Requester) BuildGetVolumeName(volumeNumber uint8) []byte {
	return r.marshalRequest(fnDirServices, wrapSubfunction(sf16GetVolumeName, []byte{volumeNumber}))
}

// ParseVolumeName reads the volume name (1-byte length + bytes) from a Get Volume Name
// reply body. An empty name is returned as "".
func ParseVolumeName(body []byte) (string, error) {
	if len(body) < 1 {
		return "", ErrShortBody
	}
	n := int(body[0])
	if len(body) < 1+n {
		return "", ErrShortBody
	}
	return string(body[1 : 1+n]), nil
}

// ParseVolumeNumber reads the 1-byte volume number from a Get Volume Number reply.
func ParseVolumeNumber(body []byte) (uint8, error) {
	if len(body) < 1 {
		return 0, ErrShortBody
	}
	return body[0], nil
}

// --- Allocate Directory Handle (0x16/0x12 permanent) ---

// BuildAllocDirHandle builds Allocate Permanent Directory Handle (0x16/0x12). Per
// fileio.go allocDirHandle the args are: source dir-handle(1), drive letter(1), then
// the length-prefixed path ("VOL:dir" absolute). The reply is the new dir-handle byte
// and an 8-bit effective-rights mask. srcHandle 0 + an absolute VOL: path allocates a
// fresh handle at the volume path.
func (r *Requester) BuildAllocDirHandle(srcHandle uint8, drive uint8, path string) []byte {
	args := []byte{srcHandle, drive}
	args = appendByteString(args, path)
	return r.marshalRequest(fnDirServices, wrapSubfunction(sf16AllocPermDir, args))
}

// DirHandleReply is the parsed Allocate Directory Handle reply: the new handle byte
// and the effective-rights mask.
type DirHandleReply struct {
	Handle uint8
	Rights uint8
}

// ParseDirHandle reads the Allocate Directory Handle reply (handle, rights).
func ParseDirHandle(body []byte) (DirHandleReply, error) {
	if len(body) < 2 {
		return DirHandleReply{}, ErrShortBody
	}
	return DirHandleReply{Handle: body[0], Rights: body[1]}, nil
}

// BuildDeallocDirHandle builds Deallocate Directory Handle (0x16/0x14): the handle
// byte. No reply body.
func (r *Requester) BuildDeallocDirHandle(handle uint8) []byte {
	return r.marshalRequest(fnDirServices, wrapSubfunction(sf16DeallocDirHdl, []byte{handle}))
}

// --- Get Volume Info with Handle (0x16/0x15) ---

// BuildGetVolumeInfo builds Get Volume Info with Handle (0x16/0x15): a dir-handle byte
// whose volume is reported.
func (r *Requester) BuildGetVolumeInfo(handle uint8) []byte {
	return r.marshalRequest(fnDirServices, wrapSubfunction(sf16GetVolumeInfo, []byte{handle}))
}

// VolumeInfo is the parsed Get Volume Info reply (fileio.go volumeInfoReply): the block
// scaling and counts let the client compute total/free bytes.
type VolumeInfo struct {
	SectorsPerBlock uint16
	TotalBlocks     uint16
	AvailBlocks     uint16
	Name            string
}

// blockSize is the byte size of one NetWare volume "block" the server reports counts
// in (matching core/service/ncp's fixed 4096-byte block); the client multiplies
// block counts × SectorsPerBlock × blockSize for total/free bytes.
const blockSize = 4096

// ParseVolumeInfo reads a Get Volume Info reply: sectors-per-block(2 BE),
// total_blocks(2 BE), avail_blocks(2 BE), total_dirs(2), avail_dirs(2), name[16],
// removable(2).
func ParseVolumeInfo(body []byte) (VolumeInfo, error) {
	if len(body) < 6+4+16+2 {
		return VolumeInfo{}, ErrShortBody
	}
	vi := VolumeInfo{
		SectorsPerBlock: bp.BE16(body[0:]),
		TotalBlocks:     bp.BE16(body[2:]),
		AvailBlocks:     bp.BE16(body[4:]),
	}
	name := body[10:26]
	vi.Name = trimNUL(name)
	return vi, nil
}

// TotalBytes / FreeBytes convert the block counts to bytes.
func (v VolumeInfo) TotalBytes() uint64 {
	return uint64(v.SectorsPerBlock) * uint64(v.TotalBlocks) * blockSize
}
func (v VolumeInfo) FreeBytes() uint64 {
	return uint64(v.SectorsPerBlock) * uint64(v.AvailBlocks) * blockSize
}

// --- Open / Create (0x4C open, 0x43 create) ---

// nwAttrNormal is the attribute byte a plain open/create sends (no read-only/hidden).
const nwAttrNormal uint8 = 0x00

// openAccessReadWrite is the access-rights byte for an open (0x4C): read (0x01) +
// write (0x02) — the DOS shell's r/w open. (Create carries no access byte.)
const openAccessReadWrite uint8 = 0x03

// BuildOpenFile builds Open File (0x4C). Per fileio.go openFile the open args are:
// dir-handle(1), attribute(1), access(1), name-length(1), name — the server reads a
// dir-handle byte at args[0], skips 2 bytes (attribute+access), then the
// length-prefixed relative path.
func (r *Requester) BuildOpenFile(handle uint8, path string) []byte {
	body := []byte{handle, nwAttrNormal, openAccessReadWrite}
	body = appendByteString(body, path)
	return r.marshalRequest(fnOpenFile, body)
}

// BuildCreateFile builds Create File (0x43). Per fileio.go openFile the create args
// are: dir-handle(1), attribute(1), name-length(1), name — the server skips 1 byte
// (attribute) after the handle.
func (r *Requester) BuildCreateFile(handle uint8, path string) []byte {
	body := []byte{handle, nwAttrNormal}
	body = appendByteString(body, path)
	return r.marshalRequest(fnCreateFile, body)
}

// OpenReply is the parsed open/create reply (fileio.go openFile): the 6-byte file
// handle the client echoes on read/write/close, and the file size. FileHandle is the
// ext_fhandle[2]+fhandle[4] prefix; Size is the trailing 4-byte length.
type OpenReply struct {
	FileHandle [6]byte
	Name       string
	Size       uint32
}

// ParseOpenReply reads the open/create reply: file-handle[6], reserved[2], name[14],
// size[4 BE].
func ParseOpenReply(body []byte) (OpenReply, error) {
	const fixed = 6 + 2 + 14 + 4
	if len(body) < fixed {
		return OpenReply{}, ErrShortBody
	}
	var rep OpenReply
	copy(rep.FileHandle[:], body[0:6])
	rep.Name = trimNUL(body[8:22])
	rep.Size = bp.BE32(body[22:])
	return rep, nil
}

// --- Close (0x42) ---

// BuildCloseFile builds Close File (0x42). Per fileio.go closeFile the args are
// reserve(1), then the 6-byte file handle (ext_fhandle[2]+fhandle[4]); the server
// reads the slot id from the fhandle. No reply body.
func (r *Requester) BuildCloseFile(handle [6]byte) []byte {
	body := append([]byte{0x00}, handle[:]...)
	return r.marshalRequest(fnCloseFile, body)
}

// --- Read (0x48) / Write (0x49) / Get File Size (0x47) ---

// BuildReadFile builds Read File (0x48). Per fileio.go readFile the args are
// filler(1), file-handle[6], offset[4 BE], max_size[2 BE].
func (r *Requester) BuildReadFile(handle [6]byte, off uint32, want uint16) []byte {
	body := append([]byte{0x00}, handle[:]...)
	body = bp.AppendBE32(body, off)
	body = bp.AppendBE16(body, want)
	return r.marshalRequest(fnReadFile, body)
}

// ParseReadReply reads a Read File reply: size[2 BE], then a leading pad byte when the
// read offset was odd (NetWare aligns data to an even offset — fileio.go's `zusatz`),
// then the data. off is the offset the read was issued at, needed to know whether the
// pad byte is present.
func ParseReadReply(body []byte, off uint32) ([]byte, error) {
	if len(body) < 2 {
		return nil, ErrShortBody
	}
	n := int(uint16(body[0])<<8 | uint16(body[1]))
	p := 2
	if off&1 == 1 {
		p++ // skip the alignment pad byte
	}
	if p+n > len(body) {
		// Truncated datagram: return what arrived (the caller loops on short reads).
		n = len(body) - p
		if n < 0 {
			n = 0
		}
	}
	return body[p : p+n], nil
}

// BuildWriteFile builds Write File (0x49). Per fileio.go writeFile the args are
// filler(1), file-handle[6], offset[4 BE], size[2 BE], data. No reply body.
func (r *Requester) BuildWriteFile(handle [6]byte, off uint32, data []byte) []byte {
	body := append([]byte{0x00}, handle[:]...)
	body = bp.AppendBE32(body, off)
	body = bp.AppendBE16(body, uint16(len(data)))
	body = append(body, data...)
	return r.marshalRequest(fnWriteFile, body)
}

// BuildGetFileSize builds Get File Size (0x47): filler(1), file-handle[6]; reply is a
// 4-byte BE size.
func (r *Requester) BuildGetFileSize(handle [6]byte) []byte {
	body := append([]byte{0x00}, handle[:]...)
	return r.marshalRequest(fnGetFileSize, body)
}

// ParseFileSize reads the 4-byte BE size from a Get File Size reply.
func ParseFileSize(body []byte) (uint32, error) {
	if len(body) < 4 {
		return 0, ErrShortBody
	}
	return bp.BE32(body), nil
}

// --- Erase (0x44) / Rename (0x45) ---

// BuildEraseFile builds Erase File (0x44): dir-handle(1), attribute(1), then the
// length-prefixed path (fileio.go eraseFile skips 1 byte after the handle).
func (r *Requester) BuildEraseFile(handle uint8, path string) []byte {
	body := []byte{handle, nwAttrNormal}
	body = appendByteString(body, path)
	return r.marshalRequest(fnEraseFile, body)
}

// BuildRenameFile builds Rename File (0x45): a source dir-handle(1) + length-prefixed
// path, then a destination dir-handle(1) + length-prefixed path (fileio.go renameFile
// reads two handle+path pairs back to back).
func (r *Requester) BuildRenameFile(srcHandle uint8, srcPath string, dstHandle uint8, dstPath string) []byte {
	body := append([]byte{srcHandle}, byteString(srcPath)...)
	body = append(body, dstHandle)
	body = append(body, byteString(dstPath)...)
	return r.marshalRequest(fnRenameFile, body)
}

// --- Create Directory (0x16/0x0A) / Delete Directory (0x16/0x0B) ---

// BuildCreateDir builds Create Directory (0x16/0x0A): dir-handle(1), then the
// length-prefixed relative path, then an access-rights-mask byte (0xFF = full).
func (r *Requester) BuildCreateDir(handle uint8, path string) []byte {
	args := append([]byte{handle}, byteString(path)...)
	args = append(args, 0xFF) // inherited-rights mask (full)
	return r.marshalRequest(fnDirServices, wrapSubfunction(sf16CreateDir, args))
}

// BuildDeleteDir builds Delete Directory (0x16/0x0B): dir-handle(1), a reserved byte,
// then the length-prefixed relative path.
func (r *Requester) BuildDeleteDir(handle uint8, path string) []byte {
	args := append([]byte{handle, 0x00}, byteString(path)...)
	return r.marshalRequest(fnDirServices, wrapSubfunction(sf16DeleteDir, args))
}

// --- Search for a File (0x40, FCB-era one-call-per-entry DIR) ---

// SearchBefore is the sequence value that starts a directory scan ("before the first
// match"); the reply carries the next sequence to pass, until the scan ends.
const SearchBefore uint16 = 0xFFFF

// Search-attribute values for Search for a File (0x40). NetWare's search-attribute
// selects EITHER files OR directories per pass via its directory bit (0x10): a DOS DIR
// shell issues one pass with the bit clear (files) and one set (directories). The
// hidden (0x02) + system (0x04) bits are set in both so hidden/system entries are also
// returned (mars_nwe's fn_dos_match honours them).
const (
	// NwSearchAttrFiles matches files (hidden + system, directory bit clear).
	NwSearchAttrFiles uint8 = 0x06
	// NwSearchAttrDirs matches subdirectories (hidden + system + directory bit 0x10).
	NwSearchAttrDirs uint8 = 0x16
	// NwSearchAttrAll is retained for a caller wanting the directory-inclusive attribute
	// in a single pass; the server treats a set directory bit as "directories only", so
	// most callers use the two NwSearchAttr{Files,Dirs} passes instead.
	NwSearchAttrAll uint8 = 0x16
)

// BuildSearchForFile builds Search for a File (0x40). Per fileio.go searchForFile the
// args are sequence[2 BE] (0xFFFF = first), dir-handle(1), search-attrib(1),
// length-prefixed path (whose final component is the wildcard pattern). searchAttr
// selects files vs directories via its directory bit (0x10).
func (r *Requester) BuildSearchForFile(seq uint16, handle uint8, searchAttr uint8, path string) []byte {
	body := bp.AppendBE16(nil, seq)
	body = append(body, handle, searchAttr)
	body = appendByteString(body, path)
	return r.marshalRequest(fnSearchForFile, body)
}

// SearchEntry is one parsed directory-search reply entry: the next sequence to pass on
// the following call, the 8.3 name, whether it is a directory, and the size (files
// only). The dates are the NetWare DOS date/time words (not decoded here — the client
// fs layer surfaces a zero time, matching the SMB client).
type SearchEntry struct {
	NextSeq uint16
	Name    string
	IsDir   bool
	Size    uint32
}

// ParseSearchReply reads a Search for a File reply (0x40). The reply is sequence[2 BE],
// reserved[2], then NW_DIR_INFO or NW_FILE_INFO. Both info records start with a
// 14-byte name and a 2-byte attribute word (LO-HI); the directory bit (0x10) in the LO
// byte distinguishes them, and a file record carries a 4-byte BE size after the
// attribute word (fileio.go appendFileEntryInfo / appendDirEntryInfo).
func ParseSearchReply(body []byte) (SearchEntry, error) {
	const fixed = 2 + 2 + 14 + 2 // sequence, reserved, name, attrib
	if len(body) < fixed {
		return SearchEntry{}, ErrShortBody
	}
	var e SearchEntry
	e.NextSeq = bp.BE16(body[0:])
	name := body[4:18]
	e.Name = trimNUL(name)
	attrLo := body[18] // attribute LO byte
	e.IsDir = attrLo&nwAttrDirectory != 0
	if !e.IsDir {
		if len(body) < fixed+4 {
			return SearchEntry{}, ErrShortBody
		}
		e.Size = bp.BE32(body[20:])
	}
	return e, nil
}

// nwAttrDirectory is the NetWare DOS directory attribute bit (fileio.go).
const nwAttrDirectory uint8 = 0x10

// --- File Search Initialize / Continue (0x3E / 0x3F): the NetWare 3.x directory scan ---
//
// A real NetWare 3.x/4.x server enumerates a directory with the two-call File Search
// Initialize (62/0x3E) + File Search Continue (63/0x3F) pair, NOT the FCB-era Search for a
// File (0x40) above (which a real server answers 0xFF/no-files). Initialize takes a dir
// handle + subpath and returns a search context (volume, directory id, sequence); Continue
// pages that context with a wildcard pattern, one entry per call, until completion 0xFF
// (end of scan). Layout ported from ncpfs lib/filemgmt.c ncp_file_search_init /
// ncp_file_search_continue (CLAUDE.md #7).

// searchAllPattern is the NetWare "match every 8.3 name" wildcard for File Search
// Continue: "*.*" with each character's high bit set (0x2A→0xAA '*', 0x2E→0xAE '.'), the
// server's marker for a wildcard match-any (observed on the wire from a real NW 4.1
// client: bytes AA AE AA). Sent as a length-prefixed string.
var searchAllPattern = string([]byte{0xAA, 0xAE, 0xAA})

// FileSearchContext is the search state File Search Initialize returns and File Search
// Continue pages: the volume number, directory id, and the running sequence.
type FileSearchContext struct {
	VolumeNumber uint8
	DirectoryID  uint16
	Sequence     uint16
}

// BuildFileSearchInit builds File Search Initialize (0x3E): dir-handle(1) + pstring path
// (the directory to scan, relative to the handle; "" scans the handle's own directory).
func (r *Requester) BuildFileSearchInit(handle uint8, path string) []byte {
	body := []byte{handle}
	body = appendByteString(body, path)
	return r.marshalRequest(fnFileSearchInit, body)
}

// ParseFileSearchInit reads the search context from a File Search Initialize reply:
// volume-number(1), directory-id(2 HL/BE), sequence(2 HL/BE), access-rights(1).
func ParseFileSearchInit(body []byte) (FileSearchContext, error) {
	if len(body) < 6 {
		return FileSearchContext{}, ErrShortBody
	}
	return FileSearchContext{
		VolumeNumber: body[0],
		DirectoryID:  uint16(body[1])<<8 | uint16(body[2]),
		Sequence:     uint16(body[3])<<8 | uint16(body[4]),
	}, nil
}

// BuildFileSearchContinue builds File Search Continue (0x3F): volume-number(1),
// directory-id(2 HL), sequence(2 HL), search-attributes(1), pstring pattern. Pass
// searchAllPattern to match every entry. attr selects files vs directories via the
// directory bit (0x10), like the 0x40 scan.
func (r *Requester) BuildFileSearchContinue(ctx FileSearchContext, attr uint8, pattern string) []byte {
	body := []byte{ctx.VolumeNumber}
	body = bp.AppendBE16(body, ctx.DirectoryID)
	body = bp.AppendBE16(body, ctx.Sequence)
	body = append(body, attr)
	body = appendByteString(body, pattern)
	return r.marshalRequest(fnFileSearchCont, body)
}

// SearchAllPattern is the exported match-every-entry wildcard for File Search Continue.
func SearchAllPattern() string { return searchAllPattern }

// ParseFileSearchContinue reads one entry from a File Search Continue reply: sequence(2
// HL) + reserved(2), then the entry record — a 14-byte name at offset 4, the attribute
// byte at offset 18, and (for a file) a 4-byte length at offset 20 (HL/BE). It updates
// ctx.Sequence to page the next call.
func ParseFileSearchContinue(body []byte, ctx *FileSearchContext) (SearchEntry, error) {
	const fixed = 2 + 2 + 14 + 1 // sequence, reserved, name, attribute
	if len(body) < fixed {
		return SearchEntry{}, ErrShortBody
	}
	ctx.Sequence = uint16(body[0])<<8 | uint16(body[1])
	var e SearchEntry
	e.NextSeq = ctx.Sequence
	e.Name = trimNUL(body[4:18])
	attr := body[18]
	e.IsDir = attr&nwAttrDirectory != 0
	if !e.IsDir {
		// File length is a 4-byte HL/BE word at offset 20 (ncpfs file_length,
		// ncp_reply_dword_hl(conn, 20)).
		if len(body) >= 24 {
			e.Size = bp.BE32(body[20:])
		}
	}
	return e, nil
}

// --- little wire helpers (big-endian body fields) ---

// byteString renders a 1-byte-length-prefixed string (Pascal form) the NCP file calls
// use for names and paths; a name longer than 255 bytes is truncated (NetWare names
// never approach that).
func byteString(s string) []byte {
	if len(s) > 0xFF {
		s = s[:0xFF]
	}
	return append([]byte{byte(len(s))}, s...)
}

// appendByteString appends a length-prefixed string to dst.
func appendByteString(dst []byte, s string) []byte { return append(dst, byteString(s)...) }

// wrapSubfunction wraps a multiplexed-function (0x16/0x17) body: a 2-byte big-endian
// subfunction-length covering the subfunction byte and its args, then the subfunction
// byte, then the args — the layout dispatch.go's subfunction() reads.
func wrapSubfunction(sf uint8, args []byte) []byte {
	sflen := uint16(1 + len(args)) // subfunction byte + args
	out := bp.AppendBE16(nil, sflen)
	out = append(out, sf)
	return append(out, args...)
}

// trimNUL returns b up to the first NUL as a string (NetWare fixed name fields are
// NUL-padded).
func trimNUL(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}
