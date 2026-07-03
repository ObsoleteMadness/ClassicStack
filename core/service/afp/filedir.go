package afp

import (
	"errors"
	"io"
	"os"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// Catalog file/dir commands ported from the known-good main branch
// (service/afp/filedir.go + file.go): FPGetDirParms, FPGetFileParms,
// FPMoveAndRename, FPExchangeFiles, FPCopyFile, FPSetVolParms. The M7/M10 rewrite
// dropped these, so a client issuing them saw kFPCallNotSupported (-5024) —
// FPMoveAndRename in particular breaks a Finder drag-move between folders.
//
// They reach storage only through the same Volume/ForkFS seam the other catalog
// handlers use (resolveCatalogPath, dirPath, vol.ResolvePath, vol.renamePath,
// vol.FS()); the FS layer publishes the bus mutation events (§10d), so — unlike
// main, which published vfs events by hand — these handlers need not.

// afpGetDirParms returns the parameters of a directory (FPGetDirParms; Inside
// Macintosh: Networking, AFP 2.x §5.1.14). It is FPGetFileDirParms restricted to
// directories: a path naming a file is kFPObjectNotFound.
//
// Request: cmd(1) pad(1) volID(2) dirID(4) bitmap(2) pathType(1) pathname...
// Reply: bitmap(2) 0x80 0x00 <packed dir params>.
func (s *Service) afpGetDirParms(a *afpSession, block []byte) ([]byte, int32) {
	return s.getFileOrDirParms(a, block, true)
}

// afpGetFileParms returns the parameters of a file (FPGetFileParms; §5.1.16). It
// is FPGetFileDirParms restricted to files: a path naming a directory is
// kFPObjectNotFound.
//
// Request: cmd(1) pad(1) volID(2) dirID(4) bitmap(2) pathType(1) pathname...
// Reply: bitmap(2) 0x00 0x00 <packed file params>.
func (s *Service) afpGetFileParms(a *afpSession, block []byte) ([]byte, int32) {
	return s.getFileOrDirParms(a, block, false)
}

// getFileOrDirParms is the shared body of FPGetDirParms/FPGetFileParms: resolve
// the object relative to dirID, require it to be the wanted kind, and pack its
// params under the single request bitmap. wantDir selects directory semantics
// (and the 0x80 type byte in the reply).
func (s *Service) getFileOrDirParms(a *afpSession, block []byte, wantDir bool) ([]byte, int32) {
	if len(block) < 12 {
		return nil, afpErrParamErr
	}
	vol, ok := a.openVols[bp.BE16(block[2:4])]
	if !ok {
		return nil, afpErrParamErr
	}
	dirID := bp.BE32(block[4:8])
	bitmap := bp.BE16(block[8:10])
	pathType := block[10]
	store, code := resolveBlockPath(vol, dirID, block, 11, pathType)
	if code != afpNoErr {
		return nil, code
	}
	info, err := vol.Stat(store)
	if err != nil {
		return nil, mapStatErr(err)
	}
	if info.IsDir() != wantDir {
		return nil, afpErrObjectNotFnd
	}
	// Reply: bitmap(2) type(1) pad(1) <packed params>. Unlike FPGetFileDirParms
	// (which echoes both bitmaps), FPGetDir/FileParms carry the single request
	// bitmap, then the 0x80/0x00 type byte main emits for dir/file respectively.
	params := vol.fileDirParams(nil, store, info, bitmap, pathType)
	out := make([]byte, 0, 4+len(params))
	out = bp.AppendBE16(out, bitmap)
	if wantDir {
		out = append(out, isDirFlag, 0)
	} else {
		out = append(out, 0, 0)
	}
	out = append(out, params...)
	return out, afpNoErr
}

// afpMoveAndRename moves an object to a different parent directory and optionally
// renames it in the same operation (FPMoveAndRename; §5.1.24). The Finder issues
// it for a drag-move between folders.
//
// Request: cmd(1) pad(1) volID(2) srcDirID(4) dstDirID(4) srcPathType(1)
//
//	srcName(pascal) dstPathType(1) dstDirName(pascal) newPathType(1)
//	newName(pascal)
//
// Reply: empty. The CNID rides the rename (vol.renamePath).
func (s *Service) afpMoveAndRename(a *afpSession, block []byte) ([]byte, int32) {
	vol, ok := a.openVols[bp.BE16(block[2:4])]
	if !ok {
		return nil, afpErrParamErr
	}
	if vol.FS().Capabilities().ReadOnly {
		return nil, afpErrAccessDenied
	}
	var req FPMoveAndRenameReq
	if err := req.Unmarshal(block); err != nil {
		return nil, afpErrParamErr
	}

	srcParent, code := dirPath(vol, req.SrcDirID)
	if code != afpNoErr {
		return nil, code
	}
	srcStore, err := vol.ResolvePath(srcParent, req.SrcName, req.SrcPathType)
	if err != nil {
		return nil, afpErrParamErr
	}
	if srcStore == "" {
		return nil, afpErrAccessDenied // cannot move the volume root
	}

	dstParent, code := dirPath(vol, req.DstDirID)
	if code != afpNoErr {
		return nil, code
	}
	// pathType 0 means "no destination subpath"; some clients still send a control
	// marker in DstDirName, so only descend when a real path type accompanies it.
	if req.DstPathType != 0 && req.DstDirName != "" {
		dstParent, err = vol.ResolvePath(dstParent, req.DstDirName, req.DstPathType)
		if err != nil {
			return nil, afpErrParamErr
		}
	}

	// The final leaf name: the new name if supplied, else the source's own name.
	newStore := srcStore
	if req.NewName != "" {
		newStore, err = vol.ResolvePath(dstParent, req.NewName, req.NewPathType)
	} else {
		_, leaf := splitStore(srcStore)
		newStore, err = vol.ResolvePath(dstParent, leaf, PathTypeUTF8Names)
	}
	if err != nil {
		return nil, afpErrParamErr
	}

	if _, err := vol.Stat(srcStore); err != nil {
		return nil, mapStatErr(err)
	}
	if _, err := vol.Stat(newStore); err == nil {
		return nil, afpErrObjectExists
	}
	if err := vol.renamePath(srcStore, newStore); err != nil {
		return nil, mapRenameErr(err)
	}
	return nil, afpNoErr
}

// afpExchangeFiles atomically swaps the contents (and metadata) of two files so
// their CNIDs stay with their original names (FPExchangeFiles; §5.1.10). The
// Finder uses it for a safe-save: write a temp file, then exchange it with the
// original so open references to the original see the new data.
//
// Request: cmd(1) pad(1) volID(2) srcDirID(4) dstDirID(4) srcPathType(1)
//
//	srcName(pascal) dstPathType(1) dstName(pascal)
//
// Reply: empty. Implemented as a three-step rename via a temp name; each rename
// carries the object's metadata container and rebinds its CNID (vol.renamePath).
func (s *Service) afpExchangeFiles(a *afpSession, block []byte) ([]byte, int32) {
	vol, ok := a.openVols[bp.BE16(block[2:4])]
	if !ok {
		return nil, afpErrParamErr
	}
	if vol.FS().Capabilities().ReadOnly {
		return nil, afpErrAccessDenied
	}
	var req FPExchangeFilesReq
	if err := req.Unmarshal(block); err != nil {
		return nil, afpErrParamErr
	}

	srcParent, code := dirPath(vol, req.SrcDirID)
	if code != afpNoErr {
		return nil, code
	}
	srcStore, err := vol.ResolvePath(srcParent, req.SrcName, req.SrcPathType)
	if err != nil {
		return nil, afpErrParamErr
	}
	dstParent, code := dirPath(vol, req.DstDirID)
	if code != afpNoErr {
		return nil, code
	}
	dstStore, err := vol.ResolvePath(dstParent, req.DstName, req.DstPathType)
	if err != nil {
		return nil, afpErrParamErr
	}
	if srcStore == "" || dstStore == "" {
		return nil, afpErrAccessDenied
	}
	if _, err := vol.Stat(srcStore); err != nil {
		return nil, mapStatErr(err)
	}
	if _, err := vol.Stat(dstStore); err != nil {
		return nil, mapStatErr(err)
	}

	// Three-step atomic swap through a temp name, rolling back on failure.
	tmp := srcStore + ".__afp_swap__"
	if err := vol.renamePath(srcStore, tmp); err != nil {
		return nil, mapRenameErr(err)
	}
	if err := vol.renamePath(dstStore, srcStore); err != nil {
		_ = vol.renamePath(tmp, srcStore) // roll back step 1
		return nil, mapRenameErr(err)
	}
	if err := vol.renamePath(tmp, dstStore); err != nil {
		// Roll back steps 1-2 as best we can.
		_ = vol.renamePath(srcStore, dstStore)
		_ = vol.renamePath(tmp, srcStore)
		return nil, mapRenameErr(err)
	}
	return nil, afpNoErr
}

// afpCopyFile copies a file (both forks and Finder info) within the server
// (FPCopyFile; §5.1.6). Source and destination may be different volumes.
//
// Request: cmd(1) pad(1) srcVolID(2) srcDirID(4) dstVolID(2) dstDirID(4)
//
//	srcPathType(1) srcName(pascal) dstPathType(1) dstDirName(pascal)
//	newPathType(1) newName(pascal)
//
// Reply: empty. A destination that already exists is kFPObjectExists.
func (s *Service) afpCopyFile(a *afpSession, block []byte) ([]byte, int32) {
	var req FPCopyFileReq
	if err := req.Unmarshal(block); err != nil {
		return nil, afpErrParamErr
	}
	srcVol, ok := a.openVols[req.SrcVolumeID]
	if !ok {
		return nil, afpErrParamErr
	}
	dstVol, ok := a.openVols[req.DstVolumeID]
	if !ok {
		return nil, afpErrParamErr
	}
	if dstVol.FS().Capabilities().ReadOnly {
		return nil, afpErrAccessDenied
	}

	srcParent, code := dirPath(srcVol, req.SrcDirID)
	if code != afpNoErr {
		return nil, code
	}
	srcStore, err := srcVol.ResolvePath(srcParent, req.SrcName, req.SrcPathType)
	if err != nil {
		return nil, afpErrParamErr
	}
	dstParent, code := dirPath(dstVol, req.DstDirID)
	if code != afpNoErr {
		return nil, code
	}
	if req.DstPathType != 0 && req.DstDirName != "" {
		dstParent, err = dstVol.ResolvePath(dstParent, req.DstDirName, req.DstPathType)
		if err != nil {
			return nil, afpErrParamErr
		}
	}
	copyName := req.NewName
	if copyName == "" {
		_, copyName = splitStore(srcStore)
		copyName = string(mustEncode(srcVol, copyName, PathTypeUTF8Names))
		req.NewPathType = PathTypeUTF8Names
	}
	dstStore, err := dstVol.ResolvePath(dstParent, copyName, req.NewPathType)
	if err != nil {
		return nil, afpErrParamErr
	}

	si, err := srcVol.Stat(srcStore)
	if err != nil {
		return nil, mapStatErr(err)
	}
	if si.IsDir() {
		return nil, afpErrObjectTypeErr // FPCopyFile copies a file, not a directory
	}
	if _, err := dstVol.Stat(dstStore); err == nil {
		return nil, afpErrObjectExists
	}

	// Create the destination file, then copy each fork the source presents. The
	// data fork is always present; the resource fork is copied only if the source
	// has one (an empty/absent resource fork is skipped so no sidecar is minted).
	f, err := dstVol.FS().CreateFile(dstStore)
	if err != nil {
		return nil, mapCreateErr(err)
	}
	_ = f.Close()

	if code := copyFork(srcVol, dstVol, srcStore, dstStore, fs.DataFork); code != afpNoErr {
		_ = dstVol.removePath(dstStore)
		return nil, code
	}
	if rl, _ := srcVol.ForkLen(srcStore, fs.ResourceFork); rl > 0 {
		if code := copyFork(srcVol, dstVol, srcStore, dstStore, fs.ResourceFork); code != afpNoErr {
			_ = dstVol.removePath(dstStore)
			return nil, code
		}
	}
	// Carry Finder info (type/creator/flags) if the source has any.
	if fi, ok := srcVol.FinderInfo(srcStore); ok {
		_ = dstVol.SetFinderInfo(dstStore, fi)
	}
	dstVol.CNID(dstStore) // allocate the copy's catalog id
	return nil, afpNoErr
}

// copyFork streams one fork of srcStore into the same fork of dstStore.
func copyFork(srcVol, dstVol *Volume, srcStore, dstStore string, fork fs.ForkType) int32 {
	in, err := srcVol.FS().OpenFork(srcStore, fork, os.O_RDONLY)
	if err != nil {
		return afpErrAccessDenied
	}
	defer func() { _ = in.Close() }()
	out, err := dstVol.FS().OpenFork(dstStore, fork, os.O_RDWR)
	if err != nil {
		return afpErrAccessDenied
	}
	defer func() { _ = out.Close() }()

	buf := make([]byte, 32768)
	var offset int64
	for {
		n, readErr := in.ReadAt(buf, offset)
		if n > 0 {
			if _, werr := out.WriteAt(buf[:n], offset); werr != nil {
				return afpErrDiskFull
			}
			offset += int64(n)
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return afpErrMiscErr
		}
	}
	return afpNoErr
}

// mustEncode renders a store-native leaf back to the wire charset for re-parsing
// through ResolvePath; on an unrepresentable name it returns the store bytes
// unchanged (ResolvePath will then reject it), matching main's best-effort copy
// of a same-named object.
func mustEncode(vol *Volume, stored string, pathType uint8) []byte {
	b, err := vol.EncodeName(stored, pathType)
	if err != nil {
		return []byte(stored)
	}
	return b
}

// afpSetVolParms sets a volume's parameters (FPSetVolParms; §5.1.30). The only
// mutable parameter this server honours is the volume backup date; other bits are
// accepted and acknowledged so the client proceeds. A read-only volume rejects
// the call with kFPAccessDenied.
//
// Request: cmd(1) pad(1) volID(2) bitmap(2) <params>. Reply: empty.
func (s *Service) afpSetVolParms(a *afpSession, block []byte) ([]byte, int32) {
	if len(block) < 6 {
		return nil, afpErrParamErr
	}
	vol, ok := a.openVols[bp.BE16(block[2:4])]
	if !ok {
		return nil, afpErrParamErr
	}
	if vol.FS().Capabilities().ReadOnly {
		return nil, afpErrAccessDenied
	}
	// The backup date is the only writable volume parameter and this server does
	// not persist it; the call is acknowledged so the Finder proceeds (main did the
	// same — it validated the request shape and returned NoErr without storing).
	return nil, afpNoErr
}
