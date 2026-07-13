package etherdfs

import (
	"errors"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
)

// ErrBadRequest is returned by a request DTO's Decode when the body is shorter
// than the opcode's fixed fields.
var ErrBadRequest = errors.New("etherdfs: request body too short")

// ReadRequest is the AL_READFIL body: a 4-byte file offset, a 2-byte file ID,
// and a 2-byte requested length (all little-endian).
type ReadRequest struct {
	Offset uint32
	FileID uint16
	Length uint16
}

// DecodeReadRequest parses an AL_READFIL body.
func DecodeReadRequest(b []byte) (ReadRequest, error) {
	if len(b) < 8 {
		return ReadRequest{}, ErrBadRequest
	}
	return ReadRequest{
		Offset: bp.LE32(b[0:4]),
		FileID: bp.LE16(b[4:6]),
		Length: bp.LE16(b[6:8]),
	}, nil
}

// WriteRequest is the AL_WRITEFIL body: a 4-byte file offset, a 2-byte file ID,
// then the data to write (the remainder of the body). A zero-length Data is a
// truncate-at-offset request, matching the DOS redirector's zero-byte write.
type WriteRequest struct {
	Offset uint32
	FileID uint16
	Data   []byte
}

// DecodeWriteRequest parses an AL_WRITEFIL body. Data aliases b.
func DecodeWriteRequest(b []byte) (WriteRequest, error) {
	if len(b) < 6 {
		return WriteRequest{}, ErrBadRequest
	}
	return WriteRequest{
		Offset: bp.LE32(b[0:4]),
		FileID: bp.LE16(b[4:6]),
		Data:   b[6:],
	}, nil
}

// SeekFromEndRequest is the AL_SKFMEND body: a signed 4-byte offset (usually
// negative, measured back from end-of-file) and a 2-byte file ID.
type SeekFromEndRequest struct {
	Offset int32
	FileID uint16
}

// DecodeSeekFromEndRequest parses an AL_SKFMEND body.
func DecodeSeekFromEndRequest(b []byte) (SeekFromEndRequest, error) {
	if len(b) < 6 {
		return SeekFromEndRequest{}, ErrBadRequest
	}
	return SeekFromEndRequest{
		Offset: int32(bp.LE32(b[0:4])),
		FileID: bp.LE16(b[4:6]),
	}, nil
}

// OpenRequest is the AL_OPEN / AL_CREATE / AL_SPOPNFIL body: three fixed 2-byte
// words — Attr (SS, the stack attribute word), Action (CC, the action code —
// only meaningful for AL_SPOPNFIL), OpenMode (MM, the open mode — only
// meaningful for AL_SPOPNFIL) — then the path (the remainder). Per
// spec/etherdfs.txt ("Request: SSCCMMfff...") and the reference server (which
// reads the path starting at a fixed body offset 6 for ALL THREE opcodes,
// `reqbuff + 6`), all three words are ALWAYS present on the wire for AL_OPEN and
// AL_CREATE too, even though only SS is meaningful there — Action/OpenMode are
// sent as zero and ignored. Decoding fewer than 3 words for AL_OPEN/AL_CREATE
// misparses the path (it starts 4 bytes early, with garbage CC/MM bytes
// prepended) — see spec/errata.md.
type OpenRequest struct {
	Attr     uint16
	Action   uint16
	OpenMode uint16
	Path     string
}

// DecodeOpenRequest parses an open-family body: always a fixed 6-byte SS/CC/MM
// prefix before the path, for AL_OPEN/AL_CREATE/AL_SPOPNFIL alike.
func DecodeOpenRequest(b []byte) (OpenRequest, error) {
	if len(b) < 6 {
		return OpenRequest{}, ErrBadRequest
	}
	return OpenRequest{
		Attr:     bp.LE16(b[0:2]),
		Action:   bp.LE16(b[2:4]),
		OpenMode: bp.LE16(b[4:6]),
		Path:     string(b[6:]),
	}, nil
}

// FindFirstRequest is the AL_FINDFIRST body: a 1-byte attribute filter and the
// search path (which may contain DOS wildcards in its final element).
type FindFirstRequest struct {
	Attr uint8
	Path string
}

// DecodeFindFirstRequest parses an AL_FINDFIRST body.
func DecodeFindFirstRequest(b []byte) (FindFirstRequest, error) {
	if len(b) < 1 {
		return FindFirstRequest{}, ErrBadRequest
	}
	return FindFirstRequest{Attr: b[0], Path: string(b[1:])}, nil
}

// FindNextRequest is the AL_FINDNEXT body: the 2-byte directory ID and 2-byte
// position returned by the matching FINDFIRST, a 1-byte attribute filter, and
// the 11-byte FCB search mask.
type FindNextRequest struct {
	DirID    uint16
	Position uint16
	Attr     uint8
	Mask     [FCBNameLen]byte
}

// DecodeFindNextRequest parses an AL_FINDNEXT body.
func DecodeFindNextRequest(b []byte) (FindNextRequest, error) {
	if len(b) < 5+FCBNameLen {
		return FindNextRequest{}, ErrBadRequest
	}
	var r FindNextRequest
	r.DirID = bp.LE16(b[0:2])
	r.Position = bp.LE16(b[2:4])
	r.Attr = b[4]
	copy(r.Mask[:], b[5:5+FCBNameLen])
	return r, nil
}

// SetAttrRequest is the AL_SETATTR body: a 1-byte attribute value and the path.
type SetAttrRequest struct {
	Attr uint8
	Path string
}

// DecodeSetAttrRequest parses an AL_SETATTR body.
func DecodeSetAttrRequest(b []byte) (SetAttrRequest, error) {
	if len(b) < 1 {
		return SetAttrRequest{}, ErrBadRequest
	}
	return SetAttrRequest{Attr: b[0], Path: string(b[1:])}, nil
}

// RenameRequest is the AL_RENAME body: a 1-byte source length, the source path
// of that length, then the destination path (the remainder).
type RenameRequest struct {
	Src string
	Dst string
}

// DecodeRenameRequest parses an AL_RENAME body.
func DecodeRenameRequest(b []byte) (RenameRequest, error) {
	if len(b) < 1 {
		return RenameRequest{}, ErrBadRequest
	}
	srcLen := int(b[0])
	if len(b) < 1+srcLen {
		return RenameRequest{}, ErrBadRequest
	}
	return RenameRequest{
		Src: string(b[1 : 1+srcLen]),
		Dst: string(b[1+srcLen:]),
	}, nil
}

// PathRequest is a bare path body shared by AL_MKDIR/AL_RMDIR/AL_CHDIR/AL_DELETE/
// AL_GETATTR: the whole body is the path. Provided for symmetry so the dispatch
// reads every request through a DTO (rule #10).
type PathRequest struct {
	Path string
}

// DecodePathRequest parses a bare-path body.
func DecodePathRequest(b []byte) PathRequest {
	return PathRequest{Path: string(b)}
}
