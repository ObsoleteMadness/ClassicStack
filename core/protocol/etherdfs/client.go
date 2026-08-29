package etherdfs

import (
	"errors"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
)

// client.go holds the CLIENT-direction EtherDFS wire DTOs: request-body encoders (the
// mirror of the server-direction Decode*Request in requests.go) and reply-body decoders
// (the mirror of the server-direction *Reply.Encode in replies.go). The frame header
// itself is already bidirectional (Frame.Encode emits a request when IsReply is false;
// ParseFrame decodes a reply), so this file only adds the per-opcode body serialisers a
// DOS-redirector-style client needs and the parsers for the answers the server returns.
//
// EtherDFS is little-endian on the wire (a real-mode x86 TSR), so all multi-byte body
// fields use core/binaryprimitives (encoding/binary transitively imports reflect, which
// the CORE ring forbids). Paths are DOS wire paths — backslash-separated, optionally
// drive-qualified; the server's NormalizePath strips the drive and leading separator.
//
// Reference: the EtherDFS protocol description (etherdfs.txt) and the reference client
// ETHERDFS.C (Mateusz Viste) — the request layouts here match what that TSR sends and
// the server-direction Decode*Request already parses (CLAUDE.md #7).

var (
	// ErrShortReply is returned by a reply decoder when the reply body is shorter than
	// the opcode's fixed fields.
	ErrShortReply = errors.New("etherdfs: reply body too short")
)

// --- request body encoders (client → server) ---

// EncodePathRequest encodes a bare-path body (AL_MKDIR/AL_RMDIR/AL_CHDIR/AL_DELETE/
// AL_GETATTR): the whole body is the DOS wire path.
func EncodePathRequest(path string) []byte { return []byte(path) }

// EncodeOpenRequest encodes an open-family body (AL_OPEN/AL_CREATE/AL_SPOPNFIL): the
// fixed 6-byte SS/CC/MM prefix (Attr, Action, OpenMode — all LE) then the path. Per the
// reference client and DecodeOpenRequest, all three words are ALWAYS present even for
// plain AL_OPEN/AL_CREATE (Action/OpenMode zero there).
func EncodeOpenRequest(r OpenRequest) []byte {
	out := make([]byte, 6, 6+len(r.Path))
	bp.PutLE16(out[0:2], r.Attr)
	bp.PutLE16(out[2:4], r.Action)
	bp.PutLE16(out[4:6], r.OpenMode)
	return append(out, r.Path...)
}

// EncodeReadRequest encodes an AL_READFIL body: offset[4], file ID[2], length[2] (LE).
func EncodeReadRequest(r ReadRequest) []byte {
	out := make([]byte, 8)
	bp.PutLE32(out[0:4], r.Offset)
	bp.PutLE16(out[4:6], r.FileID)
	bp.PutLE16(out[6:8], r.Length)
	return out
}

// EncodeWriteRequest encodes an AL_WRITEFIL body: offset[4], file ID[2], then the data.
// A zero-length Data is a truncate-at-offset request (the DOS zero-byte write).
func EncodeWriteRequest(r WriteRequest) []byte {
	out := make([]byte, 6, 6+len(r.Data))
	bp.PutLE32(out[0:4], r.Offset)
	bp.PutLE16(out[4:6], r.FileID)
	return append(out, r.Data...)
}

// EncodeSeekFromEndRequest encodes an AL_SKFMEND body: signed offset[4], file ID[2] (LE).
func EncodeSeekFromEndRequest(r SeekFromEndRequest) []byte {
	out := make([]byte, 6)
	bp.PutLE32(out[0:4], uint32(r.Offset))
	bp.PutLE16(out[4:6], r.FileID)
	return out
}

// EncodeFindFirstRequest encodes an AL_FINDFIRST body: attribute filter[1] then the
// search path (whose final element may carry DOS wildcards).
func EncodeFindFirstRequest(r FindFirstRequest) []byte {
	return append([]byte{r.Attr}, r.Path...)
}

// EncodeFindNextRequest encodes an AL_FINDNEXT body: dir ID[2], position[2], attribute
// filter[1], then the 11-byte FCB search mask (all LE).
func EncodeFindNextRequest(r FindNextRequest) []byte {
	out := make([]byte, 5+FCBNameLen)
	bp.PutLE16(out[0:2], r.DirID)
	bp.PutLE16(out[2:4], r.Position)
	out[4] = r.Attr
	copy(out[5:5+FCBNameLen], r.Mask[:])
	return out
}

// EncodeSetAttrRequest encodes an AL_SETATTR body: attribute[1] then the path.
func EncodeSetAttrRequest(r SetAttrRequest) []byte {
	return append([]byte{r.Attr}, r.Path...)
}

// EncodeRenameRequest encodes an AL_RENAME body: source length[1], the source path,
// then the destination path (the remainder).
func EncodeRenameRequest(r RenameRequest) []byte {
	out := make([]byte, 0, 1+len(r.Src)+len(r.Dst))
	out = append(out, byte(len(r.Src)))
	out = append(out, r.Src...)
	return append(out, r.Dst...)
}

// EncodeFileIDBody encodes the bare 2-byte file-ID body AL_CLSFIL / AL_CMMTFIL carry
// (fileIDFromBody on the server reads it).
func EncodeFileIDBody(fileID uint16) []byte {
	out := make([]byte, 2)
	bp.PutLE16(out, fileID)
	return out
}

// --- reply body decoders (server → client) ---

// DecodeOpenReply parses an open-family success reply (25 bytes): attribute, 11-byte
// FCB name, DOS date/time[4], size[4], file ID[2], action[2], mode[1].
func DecodeOpenReply(b []byte) (OpenReply, error) {
	const fixed = 1 + FCBNameLen + 13
	if len(b) < fixed {
		return OpenReply{}, ErrShortReply
	}
	var r OpenReply
	r.Attr = b[0]
	copy(r.FCB[:], b[1:1+FCBNameLen])
	o := 1 + FCBNameLen
	r.Time = bp.LE32(b[o : o+4])
	r.Size = bp.LE32(b[o+4 : o+8])
	r.FileID = bp.LE16(b[o+8 : o+10])
	r.Action = bp.LE16(b[o+10 : o+12])
	r.Mode = b[o+12]
	return r, nil
}

// DecodeGetAttrReply parses an AL_GETATTR success reply: DOS date/time[4], size[4],
// attribute[1].
func DecodeGetAttrReply(b []byte) (GetAttrReply, error) {
	if len(b) < 9 {
		return GetAttrReply{}, ErrShortReply
	}
	return GetAttrReply{
		Time: bp.LE32(b[0:4]),
		Size: bp.LE32(b[4:8]),
		Attr: b[8],
	}, nil
}

// DecodeFindReply parses an AL_FINDFIRST / AL_FINDNEXT success reply: attribute,
// 11-byte FCB name, DOS date/time[4], size[4], dir ID[2], position[2].
func DecodeFindReply(b []byte) (FindReply, error) {
	const fixed = 1 + FCBNameLen + 12
	if len(b) < fixed {
		return FindReply{}, ErrShortReply
	}
	var r FindReply
	r.Attr = b[0]
	copy(r.FCB[:], b[1:1+FCBNameLen])
	o := 1 + FCBNameLen
	r.Time = bp.LE32(b[o : o+4])
	r.Size = bp.LE32(b[o+4 : o+8])
	r.DirID = bp.LE16(b[o+8 : o+10])
	r.Position = bp.LE16(b[o+10 : o+12])
	return r, nil
}

// DecodeDiskSpaceReply parses an AL_DISKSPACE reply: total clusters[2], bytes per
// sector[2], free clusters[2] (LE). The AX status word (DiskSpaceStatus) carries the
// media-id/sectors-per-cluster in the frame header, not here.
func DecodeDiskSpaceReply(b []byte) (total, bytesPerSector, free uint16, err error) {
	if len(b) < 6 {
		return 0, 0, 0, ErrShortReply
	}
	return bp.LE16(b[0:2]), bp.LE16(b[2:4]), bp.LE16(b[4:6]), nil
}

// DecodeWriteReply parses an AL_WRITEFIL reply: the 2-byte count of bytes written.
func DecodeWriteReply(b []byte) (uint16, error) {
	if len(b) < 2 {
		return 0, ErrShortReply
	}
	return bp.LE16(b[0:2]), nil
}

// DecodeSeekReply parses an AL_SKFMEND reply: the 4-byte resulting absolute offset.
func DecodeSeekReply(b []byte) (uint32, error) {
	if len(b) < 4 {
		return 0, ErrShortReply
	}
	return bp.LE32(b[0:4]), nil
}
