package afp

import (
	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
)

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
