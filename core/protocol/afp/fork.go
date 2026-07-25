package afp

import (
	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
)

// fork.go holds the client-direction fork-I/O and file/dir mutation command DTOs,
// mirroring core/service/afp/forkio.go and the create/delete/rename handlers.

// --- FPOpenFork (cmd 26) — forkio.go:afpOpenFork ---
// Request: cmd(1) flag(1) volID(2) dirID(4) bitmap(2) accessMode(2) pathType(1)
//          pathname(pascal). (flag bit 0x80 → resource fork.)
// Reply: bitmap(2) forkRefNum(2) <file params>.

// OpenForkRequest builds an FPOpenFork block.
type OpenForkRequest struct {
	Resource   bool // false → data fork, true → resource fork
	VolID      uint16
	DirID      uint32
	Bitmap     uint16
	AccessMode uint16
	PathType   uint8
	Path       []byte
}

// Marshal encodes the FPOpenFork command block.
func (r OpenForkRequest) Marshal() []byte {
	flag := ForkFlagData
	if r.Resource {
		flag = ForkFlagResource
	}
	out := []byte{CmdOpenFork, flag}
	out = bp.AppendBE16(out, r.VolID)
	out = bp.AppendBE32(out, r.DirID)
	out = bp.AppendBE16(out, r.Bitmap)
	out = bp.AppendBE16(out, r.AccessMode)
	out = append(out, r.PathType)
	out = PutPString(out, r.Path)
	return out
}

// OpenForkReply is the parsed FPOpenFork reply: the echoed bitmap, the fork ref, and
// the packed file params (parsed as a file — a fork is only opened on a file).
type OpenForkReply struct {
	Bitmap     uint16
	ForkRefNum uint16
	Params     FileDirParams
}

// ParseOpenForkReply decodes an FPOpenFork reply body.
func ParseOpenForkReply(b []byte) (OpenForkReply, bool) {
	if len(b) < 4 {
		return OpenForkReply{}, false
	}
	r := OpenForkReply{
		Bitmap:     bp.BE16(b[0:2]),
		ForkRefNum: bp.BE16(b[2:4]),
	}
	r.Params = ParseFileDirParams(b[4:], r.Bitmap, false)
	return r, true
}

// --- FPRead (cmd 27) — forkio.go:afpRead ---
// Request: cmd(1) pad(1) forkRefNum(2) offset(4) reqCount(4) [newLineMask(1)
//          newLineChar(1)]. Reply: the fork bytes, raw.

// ReadRequest builds an FPRead block.
type ReadRequest struct {
	ForkRefNum uint16
	Offset     uint32
	ReqCount   uint32
}

// Marshal encodes the FPRead command block (no newline substitution).
func (r ReadRequest) Marshal() []byte {
	out := []byte{CmdRead, 0}
	out = bp.AppendBE16(out, r.ForkRefNum)
	out = bp.AppendBE32(out, r.Offset)
	out = bp.AppendBE32(out, r.ReqCount)
	// newLineMask + newLineChar. 0/0 disables newline substitution. Emitted explicitly
	// rather than omitted: a strict real server (observed: System 7.5 Personal File
	// Sharing) rejects the short 12-byte block with kFPParamErr (-5019), expecting the
	// full fixed 14-byte FPRead command block. See spec/errata.md.
	out = append(out, 0, 0)
	return out
}

// --- FPWrite (cmd 33) — forkio.go:afpWrite ---
// Request: cmd(1) flag(1) forkRefNum(2) offset(4) reqCount(4) data...
// (flag bit 0x80 → offset from end of fork.) Reply: lastWritten(4).
//
// FPWrite rides ASP's two-phase Write: the client sends the 12-byte header via ASPWrite
// and the server pulls the data with ASPWriteContinue. WriteHeader marshals that header;
// the data is sent separately. The single-block Marshal (header+data) is provided for
// tests and the in-memory transport.

// WriteRequest builds an FPWrite header (and, via Marshal, the full block for tests).
type WriteRequest struct {
	FromEnd    bool
	ForkRefNum uint16
	Offset     uint32
	Data       []byte
}

// Header marshals the 12-byte FPWrite header (no data) for the ASP two-phase path.
func (r WriteRequest) Header() []byte {
	flag := uint8(0)
	if r.FromEnd {
		flag = FromEndFlag
	}
	out := []byte{CmdWrite, flag}
	out = bp.AppendBE16(out, r.ForkRefNum)
	out = bp.AppendBE32(out, r.Offset)
	out = bp.AppendBE32(out, uint32(len(r.Data)))
	return out
}

// Marshal encodes the full FPWrite block (header + data), for the in-memory transport
// and round-trip tests that reconstitute the single-block form.
func (r WriteRequest) Marshal() []byte {
	return append(r.Header(), r.Data...)
}

// ParseWriteReply decodes the FPWrite reply: the fork offset one past the last byte
// written.
func ParseWriteReply(b []byte) (lastWritten uint32, ok bool) {
	if len(b) < 4 {
		return 0, false
	}
	return bp.BE32(b[0:4]), true
}

// --- FPCloseFork (cmd 4) ---
// Request: cmd(1) pad(1) forkRefNum(2). Reply: empty.

// CloseForkRequest builds an FPCloseFork block.
type CloseForkRequest struct{ ForkRefNum uint16 }

// Marshal encodes the FPCloseFork command block.
func (r CloseForkRequest) Marshal() []byte {
	return append([]byte{CmdCloseFork, 0}, be16(r.ForkRefNum)...)
}

// --- FPGetForkParms (cmd 14) — forkio.go:afpGetForkParms ---
// Request: cmd(1) pad(1) forkRefNum(2) bitmap(2). Reply: bitmap(2) <file params>.

// GetForkParmsRequest builds an FPGetForkParms block.
type GetForkParmsRequest struct {
	ForkRefNum uint16
	Bitmap     uint16
}

// Marshal encodes the FPGetForkParms command block.
func (r GetForkParmsRequest) Marshal() []byte {
	out := []byte{CmdGetForkParms, 0}
	out = bp.AppendBE16(out, r.ForkRefNum)
	out = bp.AppendBE16(out, r.Bitmap)
	return out
}

// ParseGetForkParmsReply decodes an FPGetForkParms reply: bitmap(2) then file params.
func ParseGetForkParmsReply(b []byte) (bitmap uint16, params FileDirParams, ok bool) {
	if len(b) < 2 {
		return 0, FileDirParams{}, false
	}
	bitmap = bp.BE16(b[0:2])
	return bitmap, ParseFileDirParams(b[2:], bitmap, false), true
}

// --- FPSetForkParms (cmd 31) — forkio.go:afpSetForkParms ---
// Request: cmd(1) pad(1) forkRefNum(2) bitmap(2) forkLen(4). Reply: empty.

// SetForkParmsRequest builds an FPSetForkParms block. Bitmap carries FileBitmapDataForkLen
// or FileBitmapRsrcForkLen; ForkLen is the new fork length.
type SetForkParmsRequest struct {
	ForkRefNum uint16
	Bitmap     uint16
	ForkLen    uint32
}

// Marshal encodes the FPSetForkParms command block.
func (r SetForkParmsRequest) Marshal() []byte {
	out := []byte{CmdSetForkParms, 0}
	out = bp.AppendBE16(out, r.ForkRefNum)
	out = bp.AppendBE16(out, r.Bitmap)
	out = bp.AppendBE32(out, r.ForkLen)
	return out
}

// --- FPCreateFile (cmd 7) ---
// Request: cmd(1) flag(1) volID(2) dirID(4) pathType(1) pathname(pascal).
// flag bit 0x80 = hard create (overwrite). Reply: empty.

// CreateFileRequest builds an FPCreateFile block.
type CreateFileRequest struct {
	Hard     bool
	VolID    uint16
	DirID    uint32
	PathType uint8
	Path     []byte
}

// Marshal encodes the FPCreateFile command block.
func (r CreateFileRequest) Marshal() []byte {
	flag := uint8(0)
	if r.Hard {
		flag = 0x80
	}
	out := []byte{CmdCreateFile, flag}
	out = bp.AppendBE16(out, r.VolID)
	out = bp.AppendBE32(out, r.DirID)
	out = append(out, r.PathType)
	out = PutPString(out, r.Path)
	return out
}

// --- FPCreateDir (cmd 6) ---
// Request: cmd(1) pad(1) volID(2) dirID(4) pathType(1) pathname(pascal).
// Reply: newDirID(4).

// CreateDirRequest builds an FPCreateDir block.
type CreateDirRequest struct {
	VolID    uint16
	DirID    uint32
	PathType uint8
	Path     []byte
}

// Marshal encodes the FPCreateDir command block.
func (r CreateDirRequest) Marshal() []byte {
	out := []byte{CmdCreateDir, 0}
	out = bp.AppendBE16(out, r.VolID)
	out = bp.AppendBE32(out, r.DirID)
	out = append(out, r.PathType)
	out = PutPString(out, r.Path)
	return out
}

// ParseCreateDirReply decodes the FPCreateDir reply (the new directory's CNID).
func ParseCreateDirReply(b []byte) (newDirID uint32, ok bool) {
	if len(b) < 4 {
		return 0, false
	}
	return bp.BE32(b[0:4]), true
}

// --- FPDelete (cmd 8) ---
// Request: cmd(1) pad(1) volID(2) dirID(4) pathType(1) pathname(pascal). Reply: empty.

// DeleteRequest builds an FPDelete block.
type DeleteRequest struct {
	VolID    uint16
	DirID    uint32
	PathType uint8
	Path     []byte
}

// Marshal encodes the FPDelete command block.
func (r DeleteRequest) Marshal() []byte {
	out := []byte{CmdDelete, 0}
	out = bp.AppendBE16(out, r.VolID)
	out = bp.AppendBE32(out, r.DirID)
	out = append(out, r.PathType)
	out = PutPString(out, r.Path)
	return out
}

// --- FPRename (cmd 28) ---
// Request: cmd(1) pad(1) volID(2) dirID(4) pathType(1) oldName(pascal)
//          pathType(1) newName(pascal). Reply: empty.

// RenameRequest builds an FPRename block (rename within one directory).
type RenameRequest struct {
	VolID    uint16
	DirID    uint32
	PathType uint8
	OldName  []byte
	NewName  []byte
}

// Marshal encodes the FPRename command block.
func (r RenameRequest) Marshal() []byte {
	out := []byte{CmdRename, 0}
	out = bp.AppendBE16(out, r.VolID)
	out = bp.AppendBE32(out, r.DirID)
	out = append(out, r.PathType)
	out = PutPString(out, r.OldName)
	out = append(out, r.PathType)
	out = PutPString(out, r.NewName)
	return out
}

// --- FPMoveAndRename (cmd 23) — handlers.go:afpMoveAndRename ---
// Request: cmd(1) pad(1) volID(2) srcDirID(4) dstDirID(4) srcPathType(1)
//          srcPath(pascal) dstPathType(1) dstPath(pascal) newType(1) newName(pascal).
// Reply: empty.

// MoveAndRenameRequest builds an FPMoveAndRename block. It moves the source object into
// the destination directory, optionally renaming it (NewName empty → keep the name).
type MoveAndRenameRequest struct {
	VolID    uint16
	SrcDirID uint32
	DstDirID uint32
	PathType uint8
	SrcPath  []byte
	DstPath  []byte // path of the destination DIRECTORY (often empty → dstDirID root)
	NewName  []byte // new leaf name (empty → unchanged)
}

// Marshal encodes the FPMoveAndRename command block.
func (r MoveAndRenameRequest) Marshal() []byte {
	out := []byte{CmdMoveAndRename, 0}
	out = bp.AppendBE16(out, r.VolID)
	out = bp.AppendBE32(out, r.SrcDirID)
	out = bp.AppendBE32(out, r.DstDirID)
	out = append(out, r.PathType)
	out = PutPString(out, r.SrcPath)
	out = append(out, r.PathType)
	out = PutPString(out, r.DstPath)
	out = append(out, r.PathType)
	out = PutPString(out, r.NewName)
	return out
}

// --- FPSetFileDirParms (cmd 35) — handlers.go:afpSetFileDirParms ---
// Request: cmd(1) pad(1) volID(2) dirID(4) bitmap(2) pathType(1) pathname(pascal)
//          [pad to even] <params in bitmap order>. Reply: empty.
//
// The client uses it to stamp Finder info (type/creator). SetFinderInfoRequest marshals
// the FinderInfo-only form.

// SetFinderInfoRequest builds an FPSetFileDirParms block that sets only the 32-byte
// Finder info (bitmap = FDBitmapFinderInfo).
type SetFinderInfoRequest struct {
	VolID      uint16
	DirID      uint32
	PathType   uint8
	Path       []byte
	FinderInfo [32]byte
}

// Marshal encodes the FPSetFileDirParms command block (FinderInfo only). The parameter
// block is word-aligned to an even offset from the start of the command block, matching
// the server's setParamsFinderInfo walk.
func (r SetFinderInfoRequest) Marshal() []byte {
	out := []byte{CmdSetFileDirParms, 0}
	out = bp.AppendBE16(out, r.VolID)
	out = bp.AppendBE32(out, r.DirID)
	out = bp.AppendBE16(out, FDBitmapFinderInfo)
	out = append(out, r.PathType)
	out = PutPString(out, r.Path)
	out = even(out) // pad to an even offset before the parameter block
	out = append(out, r.FinderInfo[:]...)
	return out
}
