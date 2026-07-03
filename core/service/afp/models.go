package afp

import (
	"errors"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
)

// errShortRequest is returned by a request DTO's Unmarshal when the block is too
// short to hold the fixed header the command requires; the handler maps it to
// kFPParamErr.
var errShortRequest = errors.New("afp: request block too short")

// Self-serialising AFP reply DTOs (CLAUDE.md rule #10). These carry the exact
// wire layout ported from the known-good main-branch service/afp *_models.go so
// handlers assemble a typed value and call Marshal() rather than hand-rolling
// reply bytes — the class of drift that produced the M7 wire regressions. The
// variable-length parameter/entry payloads are still packed by the Volume
// packers (parms.go); these DTOs own only the fixed reply headers and the
// even-length framing around each entry.

// The directory type byte (isDirFlag, 0x80) is defined in handlers.go.

// FPGetFileDirParmsRes is the FPGetFileDirParms reply:
//
//	FileBitmap(2) DirBitmap(2) typeByte(1) pad(1) <packed params>
//
// typeByte is 0x80 for a directory, 0x00 for a file. The refactor originally
// emitted a single combined bitmap here (2 bytes short) — this DTO pins both.
type FPGetFileDirParmsRes struct {
	FileBitmap uint16
	DirBitmap  uint16
	IsDir      bool
	Params     []byte
}

func (r *FPGetFileDirParmsRes) Marshal() []byte {
	out := make([]byte, 0, 6+len(r.Params))
	out = bp.AppendBE16(out, r.FileBitmap)
	out = bp.AppendBE16(out, r.DirBitmap)
	if r.IsDir {
		out = append(out, isDirFlag, 0)
	} else {
		out = append(out, 0, 0)
	}
	return append(out, r.Params...)
}

// FPEnumerateRes is the FPEnumerate reply header:
//
//	FileBitmap(2) DirBitmap(2) ActualCount(2) <entries...>
//
// Each entry is framed by enumEntry: a length byte, the type byte, then the
// packed params, padded to an even total length. The name-offset words inside
// the params are measured from the start of the params (the byte after the type
// byte) — so an entry is exactly [len][type][params], TWO bytes before params,
// with any even-length pad applied at the entry's tail (never between the type
// byte and the params).
type FPEnumerateRes struct {
	FileBitmap uint16
	DirBitmap  uint16
	ActCount   uint16
	Entries    []byte
}

func (r *FPEnumerateRes) Marshal() []byte {
	out := make([]byte, 0, 6+len(r.Entries))
	out = bp.AppendBE16(out, r.FileBitmap)
	out = bp.AppendBE16(out, r.DirBitmap)
	out = bp.AppendBE16(out, r.ActCount)
	return append(out, r.Entries...)
}

// enumEntry frames one FPEnumerate result entry from its packed params: a
// leading length byte (covering the whole entry incl. the length byte), the type
// byte (0x80 dir / 0x00 file), then the params, padded with a trailing zero to
// an even total length. This mirrors main's packEnumerateEntry — critically the
// params begin at offset 2 (len+type) with NO pad byte between the type byte and
// the params, which is where the name offsets are anchored.
func enumEntry(isDir bool, params []byte) []byte {
	entry := make([]byte, 0, 2+len(params)+1)
	entry = append(entry, 0) // length placeholder, patched below
	if isDir {
		entry = append(entry, isDirFlag)
	} else {
		entry = append(entry, 0)
	}
	entry = append(entry, params...)
	if len(entry)%2 != 0 {
		entry = append(entry, 0)
	}
	entry[0] = byte(len(entry))
	return entry
}

// --- request DTOs for the ported catalog file/dir commands (CLAUDE.md rule #10) ---
// Ported verbatim from the known-good main branch service/afp/{filedir,file}_models.go
// so each request decodes itself (Unmarshal) rather than being picked apart in the
// handler body. Offsets count from the command byte, matching Inside Macintosh's
// request-block tables. Names are Pascal strings whose interior \x00 bytes are the
// path separators ResolvePath expects, so they are kept as raw strings here.

// FPMoveAndRameReq: cmd(0) pad(1) volID(2:4) srcDirID(4:8) dstDirID(8:12)
// srcPathType(12) srcName(pascal) dstPathType dstDirName(pascal) newPathType
// newName(pascal).
type FPMoveAndRenameReq struct {
	VolumeID    uint16
	SrcDirID    uint32
	DstDirID    uint32
	SrcPathType uint8
	SrcName     string
	DstPathType uint8
	DstDirName  string
	NewPathType uint8
	NewName     string
}

func (req *FPMoveAndRenameReq) Unmarshal(data []byte) error {
	if len(data) < 14 {
		return errShortRequest
	}
	req.VolumeID = bp.BE16(data[2:4])
	req.SrcDirID = bp.BE32(data[4:8])
	req.DstDirID = bp.BE32(data[8:12])
	req.SrcPathType = data[12]
	srcLen := int(data[13])
	if len(data) < 14+srcLen {
		return errShortRequest
	}
	req.SrcName = string(data[14 : 14+srcLen])
	idx := 14 + srcLen
	if idx+2 > len(data) {
		return nil
	}
	req.DstPathType = data[idx]
	dstLen := int(data[idx+1])
	if idx+2+dstLen > len(data) {
		return nil
	}
	req.DstDirName = string(data[idx+2 : idx+2+dstLen])
	idx += 2 + dstLen
	if idx+2 > len(data) {
		return nil
	}
	req.NewPathType = data[idx]
	newLen := int(data[idx+1])
	if idx+2+newLen > len(data) {
		return nil
	}
	req.NewName = string(data[idx+2 : idx+2+newLen])
	return nil
}

// FPExchangeFilesReq: cmd(0) pad(1) volID(2:4) srcDirID(4:8) dstDirID(8:12)
// srcPathType(12) srcName(pascal) [pad to even] dstPathType dstName(pascal).
type FPExchangeFilesReq struct {
	VolumeID    uint16
	SrcDirID    uint32
	DstDirID    uint32
	SrcPathType uint8
	SrcName     string
	DstPathType uint8
	DstName     string
}

func (req *FPExchangeFilesReq) Unmarshal(data []byte) error {
	if len(data) < 14 {
		return errShortRequest
	}
	req.VolumeID = bp.BE16(data[2:4])
	req.SrcDirID = bp.BE32(data[4:8])
	req.DstDirID = bp.BE32(data[8:12])
	req.SrcPathType = data[12]
	srcLen := int(data[13])
	if len(data) < 14+srcLen {
		return errShortRequest
	}
	req.SrcName = string(data[14 : 14+srcLen])
	idx := 14 + srcLen
	if srcLen%2 != 0 {
		idx++ // the second path type is word-aligned
	}
	if idx+2 > len(data) {
		return nil
	}
	req.DstPathType = data[idx]
	dstLen := int(data[idx+1])
	if idx+2+dstLen > len(data) {
		return nil
	}
	req.DstName = string(data[idx+2 : idx+2+dstLen])
	return nil
}

// FPCopyFileReq: cmd(0) pad(1) srcVolID(2:4) srcDirID(4:8) dstVolID(8:10)
// dstDirID(10:14) srcPathType(14) srcName(pascal) [pad to even] dstPathType
// dstDirName(pascal) newPathType newName(pascal).
type FPCopyFileReq struct {
	SrcVolumeID uint16
	SrcDirID    uint32
	DstVolumeID uint16
	DstDirID    uint32
	SrcPathType uint8
	SrcName     string
	DstPathType uint8
	DstDirName  string
	NewPathType uint8
	NewName     string
}

func (req *FPCopyFileReq) Unmarshal(data []byte) error {
	if len(data) < 16 {
		return errShortRequest
	}
	req.SrcVolumeID = bp.BE16(data[2:4])
	req.SrcDirID = bp.BE32(data[4:8])
	req.DstVolumeID = bp.BE16(data[8:10])
	req.DstDirID = bp.BE32(data[10:14])
	req.SrcPathType = data[14]
	srcLen := int(data[15])
	if len(data) < 16+srcLen {
		return errShortRequest
	}
	req.SrcName = string(data[16 : 16+srcLen])
	idx := 16 + srcLen
	if srcLen%2 != 0 {
		idx++
	}
	if idx+2 > len(data) {
		return nil
	}
	req.DstPathType = data[idx]
	dstLen := int(data[idx+1])
	if idx+2+dstLen > len(data) {
		return nil
	}
	req.DstDirName = string(data[idx+2 : idx+2+dstLen])
	idx += 2 + dstLen
	if idx+2 > len(data) {
		return nil
	}
	req.NewPathType = data[idx]
	newLen := int(data[idx+1])
	if idx+2+newLen > len(data) {
		return nil
	}
	req.NewName = string(data[idx+2 : idx+2+newLen])
	return nil
}
