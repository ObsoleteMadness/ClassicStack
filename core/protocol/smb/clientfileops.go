// clientfileops.go is the client-direction codec for the SMB1 file, path, and
// directory-enumeration commands: OPEN_ANDX / READ_ANDX / WRITE_ANDX / CLOSE for the
// data fork; DELETE / RENAME / CREATE_DIRECTORY / DELETE_DIRECTORY for path ops; and
// TRANS2 FIND_FIRST2 / FIND_NEXT2 with the SMB_FIND_FILE_BOTH_DIRECTORY_INFO record
// parser for directory listing. Each request builder mirrors the exact word/byte
// layout the service handlers in core/service/smb parse; each response parser mirrors
// what those handlers build.
//
// Ring: CORE.
//
// Reference: [MS-CIFS] §2.2.4.41 (OPEN_ANDX), §2.2.4.42 (READ_ANDX),
// §2.2.4.43 (WRITE_ANDX), §2.2.4.5 (CLOSE), §2.2.6.2 (FIND_FIRST2).
package smb

import (
	"strings"
	"time"
	"unicode/utf16"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
)

// SMB file attribute bits ([MS-CIFS] §2.2.1.2.4) the client uses in FIND filters and
// OPEN.
const (
	AttrReadOnly  uint16 = 0x0001
	AttrHidden    uint16 = 0x0002
	AttrSystem    uint16 = 0x0004
	AttrDirectory uint16 = 0x0010
	AttrArchive   uint16 = 0x0020
)

// OpenFunction nibbles for OPEN_ANDX ([MS-CIFS] §2.2.4.41.1): low nibble = action if
// the file exists, high nibble = action if it is missing.
const (
	openFuncOpen         uint16 = 0x0001 // open existing
	openFuncTruncate     uint16 = 0x0002 // truncate existing to zero
	openFuncCreate       uint16 = 0x0010 // create if missing
	openFuncFailIfExists uint16 = 0x0000
)

// AccessMode low-3-bit values for OPEN_ANDX DesiredAccess ([MS-CIFS] §2.2.4.41.1).
const (
	accessRead      uint16 = 0x0000
	accessWrite     uint16 = 0x0001
	accessReadWrite uint16 = 0x0002
)

// OpenParams selects the open behaviour for BuildOpenAndX. ReadWrite requests r/w
// access; Create makes the file if missing; Truncate zeroes an existing file.
type OpenParams struct {
	ReadWrite bool
	Create    bool
	Truncate  bool
}

// accessMode maps the params to the SMB DesiredAccess low bits.
func (p OpenParams) accessMode() uint16 {
	if p.ReadWrite || p.Create || p.Truncate {
		return accessReadWrite
	}
	return accessRead
}

// openFunction maps the params to the SMB OpenFunction word.
func (p OpenParams) openFunction() uint16 {
	f := openFuncOpen
	if p.Truncate {
		f = openFuncTruncate
	}
	if p.Create {
		f |= openFuncCreate
	}
	return f
}

// BuildOpenAndX builds an SMB_COM_OPEN_ANDX request (WCT=15, [MS-CIFS] §2.2.4.41.1).
// The byte area carries the wire path (a leading pad byte precedes a Unicode path so
// the UTF-16LE name is 2-byte aligned after the odd WCT*2+... offset; the service's
// extractWirePath strips a buffer-format byte but OPEN_ANDX carries none, so the path
// begins directly — matching resolvePath's tolerance).
func (b *Builder) BuildOpenAndX(path string, p OpenParams) []byte {
	words := make([]byte, 30) // WCT=15
	words[0] = CommandNoAndXCommand
	words[1] = 0x00
	bp.PutLE16(words[2:4], 0)              // AndXOffset
	bp.PutLE16(words[4:6], 0)              // Flags
	bp.PutLE16(words[6:8], p.accessMode()) // AccessMode (DesiredAccess)
	// SearchAttributes is the set of attributes a file may carry and still be opened. A
	// classic server (observed: Win98) treats a 0 here as a filter that EXCLUDES hidden/
	// system/read-only files, so OPEN_ANDX on MSDOS.SYS (hidden+system) fails "file not
	// found" even though QUERY_INFORMATION found it. Include hidden/system/read-only/
	// archive so ordinary DOS system files open. Matches the other path commands above.
	bp.PutLE16(words[8:10], AttrReadOnly|AttrHidden|AttrSystem|AttrArchive) // SearchAttrs
	bp.PutLE16(words[10:12], 0)                                             // FileAttrs
	bp.PutLE32(words[12:16], 0)                                             // CreationTime
	bp.PutLE16(words[16:18], p.openFunction())                              // OpenFunction
	bp.PutLE32(words[18:22], 0)                                             // AllocationSize
	bp.PutLE32(words[22:26], 0)                                             // Timeout
	bp.PutLE32(words[26:30], 0)                                             // Reserved

	area := b.pathArea(path)
	return b.frame(CommandOpenAndX, words, area)
}

// pathArea builds a request byte area holding one wire path for the path-bearing
// commands whose area is "just the filename" (OPEN_ANDX / DELETE / CREATE_DIRECTORY /
// …). A CORE-form 0x04 buffer-format byte is NOT emitted (the NT commands this client
// uses do not carry it); a Unicode path gets no leading pad because the area starts on
// an even boundary relative to the header. The path is NUL-terminated in its charset.
func (b *Builder) pathArea(path string) []byte {
	out := encodePathBytes(path, b.Unicode)
	if b.Unicode {
		return append(out, 0, 0)
	}
	return append(out, 0)
}

// OpenResult is the parsed OPEN_ANDX response: the granted FID and the file size the
// server reported at open time.
type OpenResult struct {
	FID  uint16
	Size uint32
}

// ParseOpenAndX parses an SMB_COM_OPEN_ANDX response (WCT=15, [MS-CIFS] §2.2.4.41.2):
// FID at words[4:6], FileDataSize at words[12:16].
func ParseOpenAndX(resp []byte) (OpenResult, error) {
	_, words, _, err := respBody(CommandOpenAndX, resp)
	if err != nil {
		return OpenResult{}, err
	}
	if len(words) < 16 {
		return OpenResult{}, ErrShortResponse
	}
	return OpenResult{FID: bp.LE16(words[4:6]), Size: bp.LE32(words[12:16])}, nil
}

// --- READ_ANDX ---

// BuildReadAndX builds an SMB_COM_READ_ANDX request (WCT=12, [MS-CIFS] §2.2.4.42.1)
// reading up to maxCount bytes from fid at offset. The 64-bit offset high word is sent
// (WCT=12 form) so files above 4 GiB read correctly.
func (b *Builder) BuildReadAndX(fid uint16, offset int64, maxCount uint16) []byte {
	words := make([]byte, 24) // WCT=12
	words[0] = CommandNoAndXCommand
	words[1] = 0x00
	bp.PutLE16(words[2:4], 0)                    // AndXOffset
	bp.PutLE16(words[4:6], fid)                  // FID
	bp.PutLE32(words[6:10], uint32(offset))      // Offset (low)
	bp.PutLE16(words[10:12], maxCount)           // MaxCountOfBytesToReturn
	bp.PutLE16(words[12:14], maxCount)           // MinCountOfBytesToReturn
	bp.PutLE32(words[14:18], 0)                  // Timeout / MaxCountHigh
	bp.PutLE16(words[18:20], 0)                  // Remaining
	bp.PutLE32(words[20:24], uint32(offset>>32)) // OffsetHigh
	return b.frame(CommandReadAndX, words, nil)
}

// ParseReadAndX parses an SMB_COM_READ_ANDX response (WCT=12, [MS-CIFS] §2.2.4.42.2):
// DataLength at words[10:12], DataOffset (header-relative) at words[12:14]. The data
// bytes are returned as a fresh copy (the caller owns them past the response buffer's
// lifetime). A DataLength shorter than the requested MaxCount signals EOF, the SMB
// convention.
func ParseReadAndX(resp []byte) ([]byte, error) {
	_, words, _, err := respBody(CommandReadAndX, resp)
	if err != nil {
		return nil, err
	}
	if len(words) < 14 {
		return nil, ErrShortResponse
	}
	dataLen := int(bp.LE16(words[10:12]))
	dataOff := int(bp.LE16(words[12:14]))
	if dataLen == 0 {
		return nil, nil
	}
	if dataOff < 0 || dataOff+dataLen > len(resp) {
		return nil, ErrShortResponse
	}
	out := make([]byte, dataLen)
	copy(out, resp[dataOff:dataOff+dataLen])
	return out, nil
}

// --- WRITE_ANDX ---

// BuildWriteAndX builds an SMB_COM_WRITE_ANDX request (WCT=14, [MS-CIFS] §2.2.4.43.1)
// writing data to fid at offset. The data rides the byte area at a header-relative
// DataOffset the builder computes; a zero-length data write truncates the file to
// offset (the SMB convention the service honours). The 64-bit offset high word is
// sent (WCT=14 form).
func (b *Builder) BuildWriteAndX(fid uint16, offset int64, data []byte) []byte {
	const wct = 14
	words := make([]byte, 2*wct)
	// DataOffset is header-relative: header(32) + WCT(1) + words(28) + BCC(2). No pad
	// is needed because that sum is even, and the service reads data by this offset.
	dataOffset := HeaderLen + 1 + 2*wct + 2

	words[0] = CommandNoAndXCommand
	words[1] = 0x00
	bp.PutLE16(words[2:4], 0)                    // AndXOffset
	bp.PutLE16(words[4:6], fid)                  // FID
	bp.PutLE32(words[6:10], uint32(offset))      // Offset (low)
	bp.PutLE32(words[10:14], 0)                  // Timeout
	bp.PutLE16(words[14:16], 0)                  // WriteMode
	bp.PutLE16(words[16:18], 0)                  // Remaining
	bp.PutLE16(words[18:20], 0)                  // DataLengthHigh
	bp.PutLE16(words[20:22], uint16(len(data)))  // DataLength
	bp.PutLE16(words[22:24], uint16(dataOffset)) // DataOffset
	bp.PutLE32(words[24:28], uint32(offset>>32)) // OffsetHigh
	return b.frame(CommandWriteAndX, words, data)
}

// ParseWriteAndX parses an SMB_COM_WRITE_ANDX response (WCT=6, [MS-CIFS] §2.2.4.43.2):
// Count at words[4:6] — the number of bytes actually written.
func ParseWriteAndX(resp []byte) (count int, err error) {
	_, words, _, err := respBody(CommandWriteAndX, resp)
	if err != nil {
		return 0, err
	}
	if len(words) < 6 {
		return 0, ErrShortResponse
	}
	return int(bp.LE16(words[4:6])), nil
}

// --- CLOSE / FLUSH ---

// BuildClose builds an SMB_COM_CLOSE request (WCT=3, [MS-CIFS] §2.2.4.5.1) releasing
// fid. LastWriteTime is 0 (leave the server's mtime untouched).
func (b *Builder) BuildClose(fid uint16) []byte {
	words := make([]byte, 6)
	bp.PutLE16(words[0:2], fid)
	bp.PutLE32(words[2:6], 0) // LastWriteTime
	return b.frame(CommandClose, words, nil)
}

// ParseClose parses an SMB_COM_CLOSE response (header-only success).
func ParseClose(resp []byte) error {
	_, _, _, err := respBody(CommandClose, resp)
	return err
}

// --- path operations ---

// BuildDelete builds an SMB_COM_DELETE request (WCT=1, [MS-CIFS] §2.2.4.7.1). The byte
// area is a 0x04 SMB_FORMAT_ASCII buffer-format byte then the path — the CORE path-op
// form the service's extractWirePath strips.
func (b *Builder) BuildDelete(path string) []byte {
	words := make([]byte, 2)
	bp.PutLE16(words[0:2], AttrHidden|AttrSystem) // SearchAttributes: match hidden/system too
	return b.frame(CommandDelete, words, b.bufferFormatPathArea(path))
}

// BuildCreateDirectory builds an SMB_COM_CREATE_DIRECTORY request (WCT=0, [MS-CIFS]
// §2.2.4.1.1); the byte area is the buffer-format path.
func (b *Builder) BuildCreateDirectory(path string) []byte {
	return b.frame(CommandCreateDirectory, nil, b.bufferFormatPathArea(path))
}

// BuildDeleteDirectory builds an SMB_COM_DELETE_DIRECTORY request (WCT=0); the byte
// area is the buffer-format path.
func (b *Builder) BuildDeleteDirectory(path string) []byte {
	return b.frame(CommandDeleteDirectory, nil, b.bufferFormatPathArea(path))
}

// BuildRename builds an SMB_COM_RENAME request (WCT=1, [MS-CIFS] §2.2.4.8.1) moving
// oldPath to newPath. The byte area carries two buffer-format paths back to back.
func (b *Builder) BuildRename(oldPath, newPath string) []byte {
	words := make([]byte, 2)
	bp.PutLE16(words[0:2], AttrHidden|AttrSystem) // SearchAttributes
	area := b.bufferFormatPathArea(oldPath)
	area = append(area, b.bufferFormatPathArea(newPath)...)
	return b.frame(CommandRename, words, area)
}

// bufferFormatPathArea builds a byte area holding one path prefixed by the 0x04
// SMB_FORMAT_ASCII buffer-format byte the CORE path ops carry. For a Unicode session
// the service's extractWirePath expects an alignment pad byte after the 0x04 before
// the UTF-16LE name; this builder emits it so the round trip matches.
func (b *Builder) bufferFormatPathArea(path string) []byte {
	out := []byte{0x04} // SMB_FORMAT_ASCII
	if b.Unicode {
		out = append(out, 0x00) // alignment pad, consumed by extractWirePath
		out = append(out, encodePathBytes(path, true)...)
		return append(out, 0, 0)
	}
	out = append(out, encodePathBytes(path, false)...)
	return append(out, 0)
}

// success-only parsers for the path ops (all return a header-only success reply).

// ParseDelete parses an SMB_COM_DELETE response.
func ParseDelete(resp []byte) error { _, _, _, err := respBody(CommandDelete, resp); return err }

// ParseRename parses an SMB_COM_RENAME response.
func ParseRename(resp []byte) error { _, _, _, err := respBody(CommandRename, resp); return err }

// ParseCreateDirectory parses an SMB_COM_CREATE_DIRECTORY response.
func ParseCreateDirectory(resp []byte) error {
	_, _, _, err := respBody(CommandCreateDirectory, resp)
	return err
}

// ParseDeleteDirectory parses an SMB_COM_DELETE_DIRECTORY response.
func ParseDeleteDirectory(resp []byte) error {
	_, _, _, err := respBody(CommandDeleteDirectory, resp)
	return err
}

// --- QUERY_INFORMATION (stat by path) ---

// FileInfo is a parsed stat result: DOS attributes and size. The client uses the CORE
// SMB_COM_QUERY_INFORMATION which every dialect answers, so a single Stat needs no
// TRANS2 round trip.
type FileInfo struct {
	Attrs   uint16
	Size    uint32
	ModTime time.Time // LastWriteTime (UTIME) from the response; zero if unset
}

// IsDir reports whether the DOS attribute word marks a directory.
func (fi FileInfo) IsDir() bool { return fi.Attrs&AttrDirectory != 0 }

// BuildQueryInformation builds an SMB_COM_QUERY_INFORMATION request (WCT=0, [MS-CIFS]
// §2.2.4.9.1); the byte area is the buffer-format path.
func (b *Builder) BuildQueryInformation(path string) []byte {
	return b.frame(CommandQueryInformation, nil, b.bufferFormatPathArea(path))
}

// ParseQueryInformation parses an SMB_COM_QUERY_INFORMATION response (WCT=10,
// [MS-CIFS] §2.2.4.9.2): FileAttributes(2) LastWriteTime(4) FileSize(4) Reserved[10].
func ParseQueryInformation(resp []byte) (FileInfo, error) {
	_, words, _, err := respBody(CommandQueryInformation, resp)
	if err != nil {
		return FileInfo{}, err
	}
	if len(words) < 10 {
		return FileInfo{}, ErrShortResponse
	}
	return FileInfo{
		Attrs:   bp.LE16(words[0:2]),
		ModTime: utimeToTime(bp.LE32(words[2:6])), // LastWriteTime (UTIME, secs since 1970)
		Size:    bp.LE32(words[6:10]),
	}, nil
}

// filetimeEpochDelta100ns is the 100-ns tick count between the FILETIME epoch
// (1601-01-01) and the Unix epoch (1970-01-01).
const filetimeEpochDelta100ns = 116444736000000000

// utimeToTime converts an SMB UTIME (seconds since 1970-01-01 UTC) to a time.Time. Zero
// (unset / unknown) maps to the zero time so callers can test IsZero.
func utimeToTime(secs uint32) time.Time {
	if secs == 0 {
		return time.Time{}
	}
	return time.Unix(int64(secs), 0).UTC()
}

// filetimeToTime converts a Windows FILETIME (100-ns ticks since 1601-01-01 UTC, as
// carried in TRANS2 FIND records) to a time.Time. Zero maps to the zero time.
func filetimeToTime(ft uint64) time.Time {
	if ft == 0 {
		return time.Time{}
	}
	return time.Unix(0, (int64(ft)-filetimeEpochDelta100ns)*100).UTC()
}

// --- TRANS2 FIND_FIRST2 / FIND_NEXT2 (directory listing) ---

// FindEntry is one directory entry decoded from an SMB_FIND_FILE_BOTH_DIRECTORY_INFO
// record: the long file name, its DOS attributes and size, and the 8.3 short name
// (empty when the server reports no distinct short name). ShortName is decoded but not
// yet surfaced by the client fs adapter (multi-name listing is deferred).
type FindEntry struct {
	Name       string
	ShortName  string
	Attrs      uint16
	Size       uint64
	ModTime    time.Time // LastWriteTime (FILETIME) from the FIND record; zero if unset
	CreateTime time.Time // CreationTime (FILETIME) from the FIND record; zero if unset
}

// IsDir reports whether the entry's attribute word marks a directory.
func (e FindEntry) IsDir() bool { return e.Attrs&AttrDirectory != 0 }

// FindResult is the parsed result of a FIND_FIRST2 (or FIND_NEXT2): the entries in
// this batch, the search id to continue under, and whether the search is complete.
type FindResult struct {
	SID         uint16
	EndOfSearch bool
	Entries     []FindEntry
}

// findFileBothDirInfo is the FIND information level this client requests — full long
// names plus the 8.3 short name ([MS-CIFS] §2.2.8.1.7). Value mirrors the service's
// infoFileBothDirInfo.
const findFileBothDirInfo = 0x0104

// BuildFindFirst2 builds an SMB_COM_TRANSACTION2 / TRANS2_FIND_FIRST2 request listing
// dir (a share-relative '/'-path; "" = root) with a trailing "*" wildcard, at the
// SMB_FIND_FILE_BOTH_DIRECTORY_INFO level. maxCount bounds the batch size. It packs
// the TRANS2 wrapper (WCT=15: totals, offsets, one setup word = the subcommand) with
// the find parameter block in the byte area.
func (b *Builder) BuildFindFirst2(dir string, maxCount uint16) []byte {
	// FIND_FIRST2 params: SearchAttributes(2) SearchCount(2) Flags(2)
	// InformationLevel(2) SearchStorageType(4) FileName(SMB_STRING).
	pattern := findPattern(dir)
	params := make([]byte, 12)
	bp.PutLE16(params[0:2], AttrHidden|AttrSystem|AttrDirectory) // SearchAttributes
	bp.PutLE16(params[2:4], maxCount)                            // SearchCount
	bp.PutLE16(params[4:6], findCloseAtEOSFlag)                  // Flags: close at end-of-search
	bp.PutLE16(params[6:8], findFileBothDirInfo)                 // InformationLevel
	bp.PutLE32(params[8:12], 0)                                  // SearchStorageType
	params = append(params, encodePathBytes(pattern, b.Unicode)...)
	if b.Unicode {
		params = append(params, 0, 0)
	} else {
		params = append(params, 0)
	}
	return b.buildTrans2(trans2FindFirst2Sub, params, nil)
}

// BuildFindNext2 builds a TRANS2_FIND_NEXT2 continuation for search sid. Params:
// SID(2) SearchCount(2) InformationLevel(2) ResumeKey(4) Flags(2) FileName(SMB_STRING,
// empty). The empty filename ("" → "\") continues the snapshot the server holds.
func (b *Builder) BuildFindNext2(sid, maxCount uint16) []byte {
	params := make([]byte, 12)
	bp.PutLE16(params[0:2], sid)                 // SID
	bp.PutLE16(params[2:4], maxCount)            // SearchCount
	bp.PutLE16(params[4:6], findFileBothDirInfo) // InformationLevel
	bp.PutLE32(params[6:10], 0)                  // ResumeKey
	bp.PutLE16(params[10:12], 0)                 // Flags
	// Empty filename in the wire charset (just a terminator).
	if b.Unicode {
		params = append(params, 0, 0)
	} else {
		params = append(params, 0)
	}
	return b.buildTrans2(trans2FindNext2Sub, params, nil)
}

// --- TRANS2 QUERY_PATH_INFORMATION (single-file stat with reliable timestamps) ---

// BuildQueryPathInfo builds a TRANS2_QUERY_PATH_INFORMATION request for path at info level
// SMB_QUERY_FILE_BASIC_INFO ([MS-CIFS] §2.2.6.6.1). Params: InformationLevel(2) Reserved(4)
// FileName(SMB_STRING). Preferred over the legacy SMB_COM_QUERY_INFORMATION because it
// returns the four FILETIMEs (a Win9x server's legacy query returns a poor LastWriteTime).
func (b *Builder) BuildQueryPathInfo(path string) []byte {
	params := make([]byte, 6)
	bp.PutLE16(params[0:2], queryFileBasicInfo) // InformationLevel
	bp.PutLE32(params[2:6], 0)                  // Reserved
	params = append(params, encodePathBytes(path, b.Unicode)...)
	if b.Unicode {
		params = append(params, 0, 0)
	} else {
		params = append(params, 0)
	}
	return b.buildTrans2(trans2QueryPathInfoSub, params, nil)
}

// BasicInfo is the parsed SMB_QUERY_FILE_BASIC_INFO data block: the file's timestamps and
// DOS attributes. A zero time means the server did not report that timestamp.
type BasicInfo struct {
	CreateTime time.Time
	AccessTime time.Time
	WriteTime  time.Time
	ChangeTime time.Time
	Attrs      uint16
}

// IsDir reports whether the attribute word marks a directory.
func (bi BasicInfo) IsDir() bool { return bi.Attrs&AttrDirectory != 0 }

// ParseQueryPathInfo parses a TRANS2_QUERY_PATH_INFORMATION (BASIC_INFO) response. The
// data block is CreationTime(8) LastAccessTime(8) LastWriteTime(8) ChangeTime(8)
// ExtFileAttributes(4) [Reserved(4)] — all FILETIME/LE.
func ParseQueryPathInfo(resp []byte) (BasicInfo, error) {
	if _, _, _, err := respBody(CommandTransaction2, resp); err != nil {
		return BasicInfo{}, err
	}
	_, _, dOff, dLen, ok := trans2ResponseBlocks(resp)
	if !ok || dLen < 36 {
		return BasicInfo{}, ErrShortResponse
	}
	d := resp[dOff : dOff+dLen]
	return BasicInfo{
		CreateTime: filetimeToTime(bp.LE64(d[0:8])),
		AccessTime: filetimeToTime(bp.LE64(d[8:16])),
		WriteTime:  filetimeToTime(bp.LE64(d[16:24])),
		ChangeTime: filetimeToTime(bp.LE64(d[24:32])),
		Attrs:      uint16(bp.LE32(d[32:36]) & 0xFFFF),
	}, nil
}

// findPattern builds the wildcard search path for a directory: the '/'-path with a
// trailing "*" so the server lists the directory's entries (resolveSearchPath treats a
// trailing wildcard element as the pattern).
func findPattern(dir string) string {
	d := strings.Trim(dir, "/")
	if d == "" {
		return "*"
	}
	return d + "/*"
}

// TRANS2 subcommand codes (client copy; mirror the service trans2FindFirst2/Next2).
const (
	trans2FindFirst2Sub    uint16 = 0x0001
	trans2FindNext2Sub     uint16 = 0x0002
	trans2QueryPathInfoSub uint16 = 0x0005
)

// queryFileBasicInfo is the TRANS2 information level SMB_QUERY_FILE_BASIC_INFO
// ([MS-CIFS] §2.2.8.3.1): the four FILETIMEs plus the extended attributes — the compact
// stat every NT-dialect server answers, and (unlike the legacy QUERY_INFORMATION) with
// reliable timestamps on a Win9x server.
const queryFileBasicInfo uint16 = 0x0101

// findCloseAtEOSFlag asks the server to release the search when it reaches end-of-
// search (SMB_FIND_CLOSE_AT_EOS), so a fully-listed directory needs no FIND_CLOSE2.
const findCloseAtEOSFlag uint16 = 0x0002

// buildTrans2 assembles an SMB_COM_TRANSACTION2 request (WCT=15, [MS-CIFS]
// §2.2.4.46.1) carrying one setup word (the subcommand) and the given parameter and
// data blocks at their header-relative offsets. This client always sends the whole
// transaction in one message (its find params are tiny), so there are no secondaries.
func (b *Builder) buildTrans2(sub uint16, params, data []byte) []byte {
	const setupCount = 1
	const wct = 14 + setupCount // 14 words + SetupCount setup words
	words := make([]byte, 2*wct)

	// Header-relative offsets: header(32) + WCT(1) + words(2*wct) + BCC(2), then a pad
	// to align the parameter block to an even boundary. The name field is placed with
	// a leading pad byte in the byte area when needed.
	base := HeaderLen + 1 + 2*wct + 2
	namePad := (base + 3) &^ 3 // 4-align the params start (matches typical clients)
	paramOffset := namePad
	dataOffset := paramOffset + len(params)
	dataOffset = (dataOffset + 1) &^ 1 // 2-align the data block

	// MaxDataCount: the largest reply the server may return in one transaction. A
	// connectionless transport (SMB over IPX) has no reassembly, so the caller sets
	// b.MaxTransactBytes to a datagram-safe cap; otherwise (TCP/NBT) accept a large reply.
	maxData := uint16(0xFFFF)
	if b.MaxTransactBytes != 0 {
		maxData = b.MaxTransactBytes
	}
	bp.PutLE16(words[0:2], uint16(len(params))) // TotalParameterCount
	bp.PutLE16(words[2:4], uint16(len(data)))   // TotalDataCount
	bp.PutLE16(words[4:6], 0)                   // MaxParameterCount
	bp.PutLE16(words[6:8], maxData)             // MaxDataCount
	words[8] = 0                                // MaxSetupCount
	// words[9] Reserved
	bp.PutLE16(words[10:12], 0) // Flags
	bp.PutLE32(words[12:16], 0) // Timeout
	// words[16:18] Reserved2
	bp.PutLE16(words[18:20], uint16(len(params))) // ParameterCount
	bp.PutLE16(words[20:22], uint16(paramOffset)) // ParameterOffset
	bp.PutLE16(words[22:24], uint16(len(data)))   // DataCount
	bp.PutLE16(words[24:26], uint16(dataOffset))  // DataOffset
	words[26] = setupCount                        // SetupCount
	// words[27] Reserved3
	bp.PutLE16(words[28:30], sub) // Setup[0] = subcommand

	// Byte area: pad to ParameterOffset, params, pad to DataOffset, data.
	area := make([]byte, 0, dataOffset-base+len(params)+len(data))
	for len(area)+base < paramOffset {
		area = append(area, 0)
	}
	area = append(area, params...)
	for len(area)+base < dataOffset {
		area = append(area, 0)
	}
	area = append(area, data...)

	return b.frame(CommandTransaction2, words, area)
}

// ParseFind parses a TRANS2 FIND_FIRST2 or FIND_NEXT2 response, decoding the
// SMB_FIND_FILE_BOTH_DIRECTORY_INFO records in the data block. first selects whether a
// leading 2-byte SID is present in the parameter block (FIND_FIRST2 has it, FIND_NEXT2
// does not). unicode selects the filename charset the records were packed in (the same
// bit the request carried).
func ParseFind(resp []byte, first, unicode bool) (FindResult, error) {
	_, params, _, err := respBody(CommandTransaction2, resp)
	if err != nil {
		return FindResult{}, err
	}
	// The service packs params + data at their own offsets in the byte area; re-read
	// them from the TRANS2 response words rather than the reqBody area slice, because
	// buildTrans2Response places them by ParameterOffset/DataOffset.
	pOff, pLen, dOff, dLen, ok := trans2ResponseBlocks(resp)
	if !ok {
		return FindResult{}, ErrShortResponse
	}
	_ = params
	pblock := resp[pOff : pOff+pLen]
	dblock := resp[dOff : dOff+dLen]

	var res FindResult
	off := 0
	if first {
		if len(pblock) < 8 {
			return FindResult{}, ErrShortResponse
		}
		res.SID = bp.LE16(pblock[0:2])
		off = 2
	}
	if len(pblock) < off+4 {
		return FindResult{}, ErrShortResponse
	}
	// SearchCount(2) EndOfSearch(2) [EaErrorOffset(2) LastNameOffset(2)].
	res.EndOfSearch = bp.LE16(pblock[off+2:off+4]) != 0

	res.Entries = parseBothDirInfo(dblock, unicode)
	return res, nil
}

// trans2ResponseBlocks returns the parameter and data block offsets/lengths from an
// SMB_COM_TRANSACTION2 response's words (ParameterOffset/Count, DataOffset/Count are
// header-relative, [MS-CIFS] §2.2.4.46.2). ok is false when the frame is malformed.
func trans2ResponseBlocks(resp []byte) (pOff, pLen, dOff, dLen int, ok bool) {
	if len(resp) < HeaderLen+1 {
		return 0, 0, 0, 0, false
	}
	wct := int(resp[HeaderLen])
	wStart := HeaderLen + 1
	if wct < 10 || len(resp) < wStart+2*wct {
		return 0, 0, 0, 0, false
	}
	w := resp[wStart : wStart+2*wct]
	pLen = int(bp.LE16(w[6:8]))
	pOff = int(bp.LE16(w[8:10]))
	dLen = int(bp.LE16(w[12:14]))
	dOff = int(bp.LE16(w[14:16]))
	if pOff+pLen > len(resp) || dOff+dLen > len(resp) {
		return 0, 0, 0, 0, false
	}
	return pOff, pLen, dOff, dLen, true
}

// parseBothDirInfo decodes a chain of SMB_FIND_FILE_BOTH_DIRECTORY_INFO records
// ([MS-CIFS] §2.2.8.1.7): each is a 94-byte fixed area (NextEntryOffset(4) at 0, times,
// EndOfFile(8) at 40, FileAttributes(4) at 56, FileNameLength(4) at 60, ShortNameLength
// (1) at 68, ShortName[24] at 70) followed by the long FileName at offset 94. Records
// chain by NextEntryOffset (0 terminates). Names decode from UTF-16LE (unicode) or
// OEM/ANSI.
func parseBothDirInfo(data []byte, unicode bool) []FindEntry {
	var out []FindEntry
	pos := 0
	for pos+94 <= len(data) {
		rec := data[pos:]
		next := int(bp.LE32(rec[0:4]))
		// Fixed area: NextEntryOffset(0) FileIndex(4) CreationTime(8) LastAccessTime(16)
		// LastWriteTime(24) ChangeTime(32) EndOfFile(40) AllocationSize(48)
		// ExtFileAttributes(56) FileNameLength(60) …
		createTime := filetimeToTime(bp.LE64(rec[8:16]))
		writeTime := filetimeToTime(bp.LE64(rec[24:32]))
		size := bp.LE64(rec[40:48])
		attrs := uint16(bp.LE32(rec[56:60]) & 0xFFFF)
		nameLen := int(bp.LE32(rec[60:64]))
		shortLen := int(rec[68])

		name := ""
		if 94+nameLen <= len(rec) {
			name = decodeWireName(rec[94:94+nameLen], unicode)
		}
		short := ""
		if shortLen > 0 && 70+shortLen <= len(rec) {
			short = decodeWireName(rec[70:70+shortLen], true) // ShortName is always UTF-16LE
		}
		name = strings.TrimRight(name, "\x00")
		if name != "" && name != "." && name != ".." {
			out = append(out, FindEntry{
				Name:       name,
				ShortName:  strings.TrimRight(short, "\x00"),
				Attrs:      attrs,
				Size:       size,
				ModTime:    writeTime,
				CreateTime: createTime,
			})
		}
		if next <= 0 {
			break
		}
		pos += next
	}
	return out
}

// decodeWireName decodes a filename from the wire charset: UTF-16LE when unicode, else
// OEM/ANSI bytes taken verbatim (ASCII). It stops at the first NUL unit.
func decodeWireName(b []byte, unicode bool) string {
	if unicode {
		units := make([]uint16, 0, len(b)/2)
		for i := 0; i+1 < len(b); i += 2 {
			u := bp.LE16(b[i : i+2])
			if u == 0 {
				break
			}
			units = append(units, u)
		}
		return string(utf16.Decode(units))
	}
	if i := indexByteClient(b, 0); i >= 0 {
		return string(b[:i])
	}
	return string(b)
}

// indexByteClient returns the index of c in b, or -1 (a local helper so the client
// codec does not import bytes in the core ring).
func indexByteClient(b []byte, c byte) int {
	for i := range b {
		if b[i] == c {
			return i
		}
	}
	return -1
}
