package etherdfs

import bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"

// DiskSpaceStatus is the fixed AX value AL_DISKSPACE returns on success: a
// packed media-id byte (low) + sectors-per-cluster byte (high), per the
// reference server's "*ax = 1" (media id 1, ONE 32KB sector per cluster — the
// reference server's comment notes MS-DOS tolerates only 1 here). This is a
// DATA word, not a generic success/failure status: DISKSPACE is the one AL_*
// call whose AX the client reads as content (glob_intregs.w.ax = *ax, used
// directly as the sectors-per-cluster DOS reports) rather than as an error code.
const DiskSpaceStatus uint16 = 1

// diskSpaceBytesPerSector is the fixed sector size AL_DISKSPACE reports (CX),
// matching the reference server. Combined with the single sector-per-cluster in
// DiskSpaceStatus's high byte, one "cluster" as reported to DOS is 32 KiB.
const diskSpaceBytesPerSector uint16 = 32768

// DiskSpaceReply is the AL_DISKSPACE body: BX (total 32KB clusters), CX (bytes
// per sector, fixed), DX (available 32KB clusters) — exactly 3 words per
// spec/etherdfs.txt ("Answer: BBCCDD"). The AX status word is DiskSpaceStatus,
// carried in the frame header (see Frame.Reply), not in this payload — the spec
// notes AX is "already handled in the protocol's header, no need to transmit it
// a second time here."
type DiskSpaceReply struct {
	TotalClusters uint16 // BX: total 32KB clusters (input bytes >> 15, clamped to 16 bits)
	FreeClusters  uint16 // DX: available 32KB clusters
}

// Encode appends the AL_DISKSPACE reply body to dst (BX, CX, DX — 6 bytes).
func (r DiskSpaceReply) Encode(dst []byte) []byte {
	var b [6]byte
	bp.PutLE16(b[0:2], r.TotalClusters)
	bp.PutLE16(b[2:4], diskSpaceBytesPerSector)
	bp.PutLE16(b[4:6], r.FreeClusters)
	return append(dst, b[:]...)
}

// GetAttrReply is the AL_GETATTR success body: the DOS packed date/time, the
// 4-byte file size, and the 1-byte FAT attribute. On failure the dispatch sends
// a StatusReply(ErrFileNotFound) instead.
type GetAttrReply struct {
	Time uint32 // DOS packed date+time (low word time, high word date)
	Size uint32
	Attr uint8
}

// Encode appends the AL_GETATTR reply body to dst.
func (r GetAttrReply) Encode(dst []byte) []byte {
	var b [9]byte
	bp.PutLE32(b[0:4], r.Time)
	bp.PutLE32(b[4:8], r.Size)
	b[8] = r.Attr
	return append(dst, b[:]...)
}

// FindReply is the AL_FINDFIRST / AL_FINDNEXT success body: the matched entry's
// attribute, 11-byte FCB name, DOS date/time, size, and the directory ID +
// position cursor the client echoes back in the next AL_FINDNEXT. On exhaustion
// the dispatch sends StatusReply(ErrNoMoreFiles) instead.
type FindReply struct {
	Attr     uint8
	FCB      [FCBNameLen]byte
	Time     uint32
	Size     uint32
	DirID    uint16
	Position uint16
}

// Encode appends the find reply body to dst.
func (r FindReply) Encode(dst []byte) []byte {
	var b [1 + FCBNameLen + 12]byte
	b[0] = r.Attr
	copy(b[1:1+FCBNameLen], r.FCB[:])
	o := 1 + FCBNameLen
	bp.PutLE32(b[o:o+4], r.Time)
	bp.PutLE32(b[o+4:o+8], r.Size)
	bp.PutLE16(b[o+8:o+10], r.DirID)
	bp.PutLE16(b[o+10:o+12], r.Position)
	return append(dst, b[:]...)
}

// OpenReply is the AL_OPEN / AL_CREATE / AL_SPOPNFIL success body: the opened
// entry's attribute, 11-byte FCB name, DOS date/time, size, the server file ID
// the client uses for subsequent READ/WRITE/SEEK/CLOSE, the CX action-result
// word, and the open mode — always 25 bytes per spec/etherdfs.txt ("Answer:
// AfffffffffffttddssssCCRRo (25 bytes)"), for every one of OPEN/CREATE/SPOPNFIL.
// The reference server writes the same 25-byte shape unconditionally (its CX
// result, spopres, is simply 0 for plain OPEN/CREATE); Action is meaningful only
// for AL_SPOPNFIL (1=opened, 2=created, 3=truncated) but is always transmitted.
type OpenReply struct {
	Attr   uint8
	FCB    [FCBNameLen]byte
	Time   uint32
	Size   uint32
	FileID uint16
	Action uint16 // AL_SPOPNFIL action result (1=opened, 2=created, 3=truncated); 0 otherwise
	Mode   uint8
}

// Encode appends the open-family reply body to dst (always 25 bytes).
func (r OpenReply) Encode(dst []byte) []byte {
	var b [1 + FCBNameLen + 13]byte
	b[0] = r.Attr
	copy(b[1:1+FCBNameLen], r.FCB[:])
	o := 1 + FCBNameLen
	bp.PutLE32(b[o:o+4], r.Time)
	bp.PutLE32(b[o+4:o+8], r.Size)
	bp.PutLE16(b[o+8:o+10], r.FileID)
	bp.PutLE16(b[o+10:o+12], r.Action)
	b[o+12] = r.Mode
	return append(dst, b[:]...)
}

// ReadReply is the AL_READFIL success body: the raw file data (up to the
// requested length). It is just the bytes; the dispatch sends a
// StatusReply(ErrReadFault) on error instead.
func ReadReply(data []byte) []byte { return data }

// WriteReply is the AL_WRITEFIL success body: the 2-byte count of bytes written.
func WriteReply(written uint16) []byte {
	out := make([]byte, 2)
	bp.PutLE16(out, written)
	return out
}

// SeekReply is the AL_SKFMEND success body: the resulting 4-byte absolute file
// offset.
func SeekReply(offset uint32) []byte {
	out := make([]byte, 4)
	bp.PutLE32(out, offset)
	return out
}
