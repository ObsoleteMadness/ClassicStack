package etherdfs

import bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"

// StatusReply is a bare AX status reply (the common shape for AL_MKDIR/AL_RMDIR/
// AL_CHDIR/AL_CLSFIL/AL_CMMTFIL/AL_LOCKFIL/AL_UNLOCKFIL/AL_DELETE/AL_SETATTR/
// AL_RENAME and any error reply): the 2-byte DOS status word and nothing else.
func StatusReply(status uint16) []byte {
	out := make([]byte, 2)
	bp.PutLE16(out, status)
	return out
}

// DiskSpaceReply is the AL_DISKSPACE body: media descriptor, sectors-per-cluster,
// bytes-per-sector, total clusters, and available clusters. The client multiplies
// these to present a drive's free/total size to DOS. Fields are packed
// little-endian; clusters are clamped to the 16-bit DOS range by the caller.
type DiskSpaceReply struct {
	Status            uint16
	SectorsPerCluster uint16
	BytesPerSector    uint16
	TotalClusters     uint16
	FreeClusters      uint16
}

// Encode appends the AL_DISKSPACE reply body to dst.
func (r DiskSpaceReply) Encode(dst []byte) []byte {
	var b [10]byte
	bp.PutLE16(b[0:2], r.Status)
	bp.PutLE16(b[2:4], r.SectorsPerCluster)
	bp.PutLE16(b[4:6], r.BytesPerSector)
	bp.PutLE16(b[6:8], r.TotalClusters)
	bp.PutLE16(b[8:10], r.FreeClusters)
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
// the client uses for subsequent READ/WRITE/SEEK/CLOSE, and the open mode. For
// AL_SPOPNFIL an extra 2-byte action-result precedes the mode (HasAction).
type OpenReply struct {
	Attr      uint8
	FCB       [FCBNameLen]byte
	Time      uint32
	Size      uint32
	FileID    uint16
	Action    uint16 // AL_SPOPNFIL action result (1=opened, 2=created, 3=truncated)
	HasAction bool
	Mode      uint8
}

// Encode appends the open-family reply body to dst.
func (r OpenReply) Encode(dst []byte) []byte {
	out := make([]byte, 0, 1+FCBNameLen+12)
	out = append(out, r.Attr)
	out = append(out, r.FCB[:]...)
	var tmp [10]byte
	bp.PutLE32(tmp[0:4], r.Time)
	bp.PutLE32(tmp[4:8], r.Size)
	bp.PutLE16(tmp[8:10], r.FileID)
	out = append(out, tmp[:]...)
	if r.HasAction {
		var a [2]byte
		bp.PutLE16(a[:], r.Action)
		out = append(out, a[:]...)
	}
	out = append(out, r.Mode)
	return append(dst, out...)
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
