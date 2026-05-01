//go:build afp || all

package afp

import (
	"bytes"
	"io/fs"
	"path/filepath"

	"github.com/ObsoleteMadness/ClassicStack/netlog"
)

func (s *Service) handleGetFileDirParms(req *FPGetFileDirParmsReq) (*FPGetFileDirParmsRes, int32) {
	if req.FileBitmap == 0 && req.DirBitmap == 0 {
		return &FPGetFileDirParmsRes{}, ErrBitmapErr
	}
	if req.FileBitmap&^enumerateFileBitmapMask != 0 || req.DirBitmap&^enumerateDirBitmapMask != 0 {
		return &FPGetFileDirParmsRes{}, ErrBitmapErr
	}
	if req.Path != "" && req.PathType != PathTypeShortNames && req.PathType != PathTypeLongNames {
		return &FPGetFileDirParmsRes{}, ErrParamErr
	}

	parentPath, ok := s.resolveDIDPath(req.VolumeID, req.DirID)
	if !ok && req.DirID != 0 {
		return emptyGetFileDirParmsRes(req), ErrObjectNotFound
	} else if !ok && req.DirID == 0 {
		parentPath, _ = s.resolveDIDPath(req.VolumeID, CNIDRoot)
	}

	targetPath := parentPath
	if req.Path != "" {
		resolvedPath, errCode := s.resolvePath(parentPath, req.Path, req.PathType)
		if errCode != NoErr {
			if errCode == ErrObjectNotFound {
				return emptyGetFileDirParmsRes(req), ErrObjectNotFound
			}
			return &FPGetFileDirParmsRes{}, errCode
		}
		targetPath = resolvedPath
	}

	infoPath := targetPath
	var info fs.FileInfo
	var err error
	if req.Path != "" {
		infoPath, info, err = s.statPathWithAppleDoubleFallback(targetPath)
	} else {
		backend := s.fsForPath(targetPath)
		if backend == nil {
			return emptyGetFileDirParmsRes(req), ErrObjectNotFound
		}
		info, err = backend.Stat(targetPath)
	}
	if err != nil {
		return emptyGetFileDirParmsRes(req), ErrObjectNotFound
	}
	targetPath = infoPath

	isDir := info.IsDir()
	bitmap := req.FileBitmap
	if isDir {
		bitmap = req.DirBitmap
	}

	resData := new(bytes.Buffer)
	s.packFileInfo(resData, req.VolumeID, bitmap, filepath.Dir(targetPath), filepath.Base(targetPath), info, isDir)

	res := &FPGetFileDirParmsRes{
		FileBitmap: req.FileBitmap,
		DirBitmap:  req.DirBitmap,
		IsFile:     !isDir,
		Data:       resData.Bytes(),
	}

	return res, NoErr
}

func emptyGetFileDirParmsRes(req *FPGetFileDirParmsReq) *FPGetFileDirParmsRes {
	// Preserve a valid reply layout (bitmaps + File/DirFlag + pad) even on
	// ObjectNotFound so clients can parse the envelope deterministically.
	isFile := true
	if req.FileBitmap == 0 && req.DirBitmap != 0 {
		isFile = false
	}
	return &FPGetFileDirParmsRes{
		FileBitmap: req.FileBitmap,
		DirBitmap:  req.DirBitmap,
		IsFile:     isFile,
		Data:       nil,
	}
}

func (s *Service) handleRename(req *FPRenameReq) (*FPRenameRes, int32) {
	if s.volumeIsReadOnly(req.VolumeID) {
		return &FPRenameRes{}, ErrVolLocked
	}
	parentPath, ok := s.resolveDIDPath(req.VolumeID, req.DirID)
	if !ok {
		return &FPRenameRes{}, ErrObjectNotFound
	}

	oldPath, errCode := s.resolvePath(parentPath, req.Name, req.PathType)
	if errCode != NoErr {
		return &FPRenameRes{}, errCode
	}
	newPath, errCode := s.resolvePath(parentPath, req.NewName, req.NewPathType)
	if errCode != NoErr {
		return &FPRenameRes{}, errCode
	}
	backend := s.fsForPath(oldPath)
	if backend == nil {
		return &FPRenameRes{}, ErrObjectNotFound
	}
	_, err := backend.Stat(oldPath)
	if err != nil {
		return &FPRenameRes{}, ErrObjectNotFound
	}

	err = backend.Rename(oldPath, newPath)
	if err != nil {
		return &FPRenameRes{}, ErrAccessDenied
	}
	s.moveAppleDoubleSidecar(oldPath, newPath)
	s.rebindDIDSubtree(req.VolumeID, oldPath, newPath)
	return &FPRenameRes{}, NoErr
}

func (s *Service) handleGetDirParms(req *FPGetDirParmsReq) (*FPGetDirParmsRes, int32) {
	parentPath, ok := s.getDIDPath(req.VolumeID, req.DirID)
	if !ok && req.DirID != 0 {
		return &FPGetDirParmsRes{}, ErrObjectNotFound
	} else if !ok {
		parentPath, _ = s.getDIDPath(req.VolumeID, CNIDRoot)
	}
	targetPath := parentPath
	if req.Path != "" {
		resolvedPath, errCode := s.resolvePath(parentPath, req.Path, req.PathType)
		if errCode != NoErr {
			return &FPGetDirParmsRes{}, errCode
		}
		targetPath = resolvedPath
	}
	backend := s.fsForPath(targetPath)
	if backend == nil {
		return &FPGetDirParmsRes{}, ErrObjectNotFound
	}
	info, err := backend.Stat(targetPath)
	if err != nil || !info.IsDir() {
		return &FPGetDirParmsRes{}, ErrObjectNotFound
	}
	resData := new(bytes.Buffer)
	s.packFileInfo(resData, req.VolumeID, req.Bitmap, filepath.Dir(targetPath), filepath.Base(targetPath), info, true)
	return &FPGetDirParmsRes{Bitmap: req.Bitmap, Data: resData.Bytes()}, NoErr
}

func (s *Service) handleGetFileParms(req *FPGetFileParmsReq) (*FPGetFileParmsRes, int32) {
	parentPath, ok := s.getDIDPath(req.VolumeID, req.DirID)
	if !ok && req.DirID != 0 {
		return &FPGetFileParmsRes{}, ErrObjectNotFound
	} else if !ok {
		parentPath, _ = s.getDIDPath(req.VolumeID, CNIDRoot)
	}
	targetPath := parentPath
	if req.Path != "" {
		resolvedPath, errCode := s.resolvePath(parentPath, req.Path, req.PathType)
		if errCode != NoErr {
			return &FPGetFileParmsRes{}, errCode
		}
		targetPath = resolvedPath
	}
	backend := s.fsForPath(targetPath)
	if backend == nil {
		return &FPGetFileParmsRes{}, ErrObjectNotFound
	}
	info, err := backend.Stat(targetPath)
	if err != nil || info.IsDir() {
		return &FPGetFileParmsRes{}, ErrObjectNotFound
	}
	resData := new(bytes.Buffer)
	s.packFileInfo(resData, req.VolumeID, req.Bitmap, filepath.Dir(targetPath), filepath.Base(targetPath), info, false)
	return &FPGetFileParmsRes{Bitmap: req.Bitmap, Data: resData.Bytes()}, NoErr
}

func (s *Service) handleSetFileDirParms(req *FPSetFileDirParmsReq) (*FPSetFileDirParmsRes, int32) {
	if s.volumeIsReadOnly(req.VolumeID) {
		return &FPSetFileDirParmsRes{}, ErrVolLocked
	}
	targetPath, errCode := s.resolveSetPath(req.VolumeID, req.DirID, req.Path, req.PathType)
	if errCode != NoErr {
		return &FPSetFileDirParmsRes{}, errCode
	}
	s.applyFinderInfo(req.Bitmap, req.FinderInfo, targetPath, req.VolumeID)
	return &FPSetFileDirParmsRes{}, NoErr
}

func (s *Service) handleDelete(req *FPDeleteReq) (*FPDeleteRes, int32) {
	if s.fs == nil {
		return &FPDeleteRes{}, ErrAccessDenied
	}
	if s.volumeIsReadOnly(req.VolumeID) {
		return &FPDeleteRes{}, ErrVolLocked
	}
	targetPath, errCode := s.resolveSetPath(req.VolumeID, req.DirID, req.Path, req.PathType)
	if errCode != NoErr {
		return &FPDeleteRes{}, errCode
	}
	backend := s.fsForPath(targetPath)
	if backend == nil {
		return &FPDeleteRes{}, ErrObjectNotFound
	}
	_, err := backend.Stat(targetPath)
	if err != nil {
		return &FPDeleteRes{}, ErrObjectNotFound
	}
	if err := backend.Remove(targetPath); err != nil {
		return &FPDeleteRes{}, ErrAccessDenied
	}
	s.deleteAppleDoubleSidecar(targetPath)
	s.removeDIDSubtree(req.VolumeID, targetPath)
	return &FPDeleteRes{}, NoErr
}

func (s *Service) handleMoveAndRename(req *FPMoveAndRenameReq) (*FPMoveAndRenameRes, int32) {
	if s.volumeIsReadOnly(req.VolumeID) {
		return &FPMoveAndRenameRes{}, ErrVolLocked
	}
	srcParent, ok := s.resolveDIDPath(req.VolumeID, req.SrcDirID)
	if !ok {
		return &FPMoveAndRenameRes{}, ErrObjectNotFound
	}
	srcPath, errCode := s.resolvePath(srcParent, req.SrcName, req.SrcPathType)
	if errCode != NoErr {
		return &FPMoveAndRenameRes{}, errCode
	}

	dstParent, ok := s.resolveDIDPath(req.VolumeID, req.DstDirID)
	if !ok {
		return &FPMoveAndRenameRes{}, ErrObjectNotFound
	}
	// Some clients send a control-marker payload in DstDirName when DstPathType=0.
	// Treat pathType 0 as "no destination subpath" and use DstDirID directly.
	if req.DstPathType != 0 && req.DstDirName != "" {
		dstParent, errCode = s.resolvePath(dstParent, req.DstDirName, req.DstPathType)
		if errCode != NoErr {
			return &FPMoveAndRenameRes{}, errCode
		}
	}

	finalName := req.NewName
	if finalName != "" {
		if req.NewPathType == 1 {
			return &FPMoveAndRenameRes{}, ErrObjectNotFound
		}
		finalName = s.afpPathElementToHost(finalName)
		if finalName == ".." {
			return &FPMoveAndRenameRes{}, ErrAccessDenied
		}
		if !s.options.DecomposedFilenames && hasHostReservedChar(finalName) {
			return &FPMoveAndRenameRes{}, ErrAccessDenied
		}
	} else {
		finalName = filepath.Base(srcPath)
	}
	dstPath := s.canonicalizePath(filepath.Join(dstParent, finalName))
	backend := s.fsForPath(srcPath)
	if backend == nil {
		return &FPMoveAndRenameRes{}, ErrObjectNotFound
	}
	_, err := backend.Stat(srcPath)
	if err != nil {
		return &FPMoveAndRenameRes{}, ErrObjectNotFound
	}

	if err := backend.Rename(srcPath, dstPath); err != nil {
		return &FPMoveAndRenameRes{}, ErrAccessDenied
	}
	s.moveAppleDoubleSidecar(srcPath, dstPath)
	s.rebindDIDSubtree(req.VolumeID, srcPath, dstPath)
	return &FPMoveAndRenameRes{}, NoErr
}

func (s *Service) handleExchangeFiles(req *FPExchangeFilesReq) (*FPExchangeFilesRes, int32) {
	if s.volumeIsReadOnly(req.VolumeID) {
		return &FPExchangeFilesRes{}, ErrVolLocked
	}
	srcParent, ok := s.resolveDIDPath(req.VolumeID, req.SrcDirID)
	if !ok {
		return &FPExchangeFilesRes{}, ErrObjectNotFound
	}
	srcPath, errCode := s.resolvePath(srcParent, req.SrcName, req.SrcPathType)
	if errCode != NoErr {
		return &FPExchangeFilesRes{}, errCode
	}

	dstParent, ok := s.resolveDIDPath(req.VolumeID, req.DstDirID)
	if !ok {
		return &FPExchangeFilesRes{}, ErrObjectNotFound
	}
	dstPath, errCode := s.resolvePath(dstParent, req.DstName, req.DstPathType)
	if errCode != NoErr {
		return &FPExchangeFilesRes{}, errCode
	}

	// Three-step atomic swap via temp name.
	tmpPath := srcPath + ".__afp_swap__"
	backend := s.fsForPath(srcPath)
	if backend == nil {
		return &FPExchangeFilesRes{}, ErrObjectNotFound
	}
	if err := backend.Rename(srcPath, tmpPath); err != nil {
		return &FPExchangeFilesRes{}, ErrAccessDenied
	}
	s.rebindDIDSubtree(req.VolumeID, srcPath, tmpPath)
	if err := backend.Rename(dstPath, srcPath); err != nil {
		s.rebindDIDSubtree(req.VolumeID, tmpPath, srcPath)
		backend.Rename(tmpPath, srcPath) // attempt rollback
		return &FPExchangeFilesRes{}, ErrAccessDenied
	}
	s.rebindDIDSubtree(req.VolumeID, dstPath, srcPath)
	if err := backend.Rename(tmpPath, dstPath); err != nil {
		return &FPExchangeFilesRes{}, ErrAccessDenied
	}
	s.rebindDIDSubtree(req.VolumeID, tmpPath, dstPath)
	if m := s.metaFor(req.VolumeID); m != nil {
		if err := m.ExchangeMetadata(srcPath, dstPath); err != nil {
			netlog.Debug("[AFP] warning: metadata exchange failed %q <-> %q: %v", srcPath, dstPath, err)
		}
	}
	return &FPExchangeFilesRes{}, NoErr
}
