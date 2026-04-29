//go:build afp || all

package afp

import (
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
)

func (s *Service) handleSetFileParms(req *FPSetFileParmsReq) (*FPSetFileParmsRes, int32) {
	if s.volumeIsReadOnly(req.VolumeID) {
		return &FPSetFileParmsRes{}, ErrVolLocked
	}
	targetPath, errCode := s.resolveSetPath(req.VolumeID, req.DirID, req.Path, req.PathType)
	if errCode != NoErr {
		return &FPSetFileParmsRes{}, errCode
	}
	s.applyFinderInfo(req.Bitmap, req.FinderInfo, targetPath, req.VolumeID)
	return &FPSetFileParmsRes{}, NoErr
}

func (s *Service) handleCreateFile(req *FPCreateFileReq) (*FPCreateFileRes, int32) {
	if s.fs == nil {
		return &FPCreateFileRes{}, ErrAccessDenied
	}
	if s.volumeIsReadOnly(req.VolumeID) {
		return &FPCreateFileRes{}, ErrVolLocked
	}
	targetPath, errCode := s.resolveSetPath(req.VolumeID, req.DirID, req.Path, req.PathType)
	if errCode != NoErr {
		return &FPCreateFileRes{}, errCode
	}
	backend := s.fsForPath(targetPath)
	if backend == nil {
		return &FPCreateFileRes{}, ErrAccessDenied
	}
	if req.HasFlag(FPCreateFileFlagHardCreate) {
		f, err := backend.CreateFile(targetPath)
		if err != nil {
			return &FPCreateFileRes{}, ErrAccessDenied
		}
		f.Close()
	} else {
		f, err := backend.OpenFile(targetPath, os.O_CREATE|os.O_EXCL)
		if err != nil {
			if os.IsExist(err) {
				return &FPCreateFileRes{}, ErrObjectExists
			}
			return &FPCreateFileRes{}, ErrAccessDenied
		}
		f.Close()
	}
	return &FPCreateFileRes{}, NoErr
}

func (s *Service) handleCopyFile(req *FPCopyFileReq) (*FPCopyFileRes, int32) {
	srcParent, ok := s.resolveDIDPath(req.SrcVolumeID, req.SrcDirID)
	if !ok {
		return &FPCopyFileRes{}, ErrObjectNotFound
	}
	srcPath, errCode := s.resolvePath(srcParent, req.SrcName, req.SrcPathType)
	if errCode != NoErr {
		return &FPCopyFileRes{}, errCode
	}

	dstParent, ok := s.resolveDIDPath(req.DstVolumeID, req.DstDirID)
	if !ok {
		return &FPCopyFileRes{}, ErrObjectNotFound
	}
	if s.volumeIsReadOnly(req.DstVolumeID) {
		return &FPCopyFileRes{}, ErrVolLocked
	}
	// Some clients send a control-marker payload in DstDirName when DstPathType=0.
	// Treat pathType 0 as "no destination subpath" and use DstDirID directly.
	if req.DstPathType != 0 && req.DstDirName != "" {
		dstParent, errCode = s.resolvePath(dstParent, req.DstDirName, req.DstPathType)
		if errCode != NoErr {
			return &FPCopyFileRes{}, errCode
		}
	}

	copyName := req.NewName
	if copyName != "" {
		if req.NewPathType == 1 {
			return &FPCopyFileRes{}, ErrObjectNotFound
		}
		copyName = s.afpPathElementToHost(copyName)
		if copyName == ".." {
			return &FPCopyFileRes{}, ErrAccessDenied
		}
		if !s.options.DecomposedFilenames && hasHostReservedChar(copyName) {
			return &FPCopyFileRes{}, ErrAccessDenied
		}
	} else {
		copyName = filepath.Base(srcPath)
	}
	dstPath := s.canonicalizePath(filepath.Join(dstParent, copyName))
	srcBackend := s.fsForPath(srcPath)
	dstBackend := s.fsForPath(dstPath)
	if srcBackend == nil || dstBackend == nil {
		return &FPCopyFileRes{}, ErrAccessDenied
	}

	if _, err := dstBackend.Stat(dstPath); err == nil {
		return &FPCopyFileRes{}, ErrObjectExists
	}

	srcFile, err := srcBackend.OpenFile(srcPath, os.O_RDONLY)
	if err != nil {
		return &FPCopyFileRes{}, ErrObjectNotFound
	}
	defer srcFile.Close()

	dstFile, err := dstBackend.CreateFile(dstPath)
	if err != nil {
		return &FPCopyFileRes{}, ErrAccessDenied
	}
	defer dstFile.Close()

	buf := make([]byte, 32768)
	var offset int64
	for {
		n, readErr := srcFile.ReadAt(buf, offset)
		if n > 0 {
			if _, writeErr := dstFile.WriteAt(buf[:n], offset); writeErr != nil {
				return &FPCopyFileRes{}, ErrDFull
			}
			offset += int64(n)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			if errors.Is(readErr, ErrCopySourceReadEOF) {
				return &FPCopyFileRes{}, ErrEOFErr
			}
			return &FPCopyFileRes{}, ErrMiscErr
		}
	}

	srcMeta := s.metaFor(req.SrcVolumeID)
	dstMeta := s.metaFor(req.DstVolumeID)
	if srcMeta != nil && dstMeta != nil {
		if err := dstMeta.CopyMetadataFrom(srcMeta, srcPath, dstPath); err != nil {
			log.Printf("[AFP] warning: metadata copy failed %q -> %q: %v", srcPath, dstPath, err)
		}
	}

	return &FPCopyFileRes{}, NoErr
}
