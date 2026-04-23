package afp

import (
	"bytes"
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
)

func (s *AFPService) handleOpenDir(req *FPOpenDirReq) (*FPOpenDirRes, int32) {
	parentPath, ok := s.getDIDPath(req.VolumeID, req.DirID)
	if !ok && req.DirID != 0 {
		return &FPOpenDirRes{}, ErrObjectNotFound
	} else if !ok && req.DirID == 0 {
		parentPath, _ = s.getDIDPath(req.VolumeID, CNIDRoot)
	}

	targetPath := parentPath
	if req.Path != "" {
		resolvedPath, errCode := s.resolvePath(parentPath, req.Path, req.PathType)
		if errCode != NoErr {
			return &FPOpenDirRes{}, errCode
		}
		targetPath = resolvedPath
	}

	newDID := s.getPathDID(req.VolumeID, targetPath)

	res := &FPOpenDirRes{DirID: newDID}
	return res, NoErr
}

func (s *AFPService) handleEnumerate(req *FPEnumerateReq) (*FPEnumerateRes, int32) {
	log.Printf("[AFP] FPEnumerate: DirID=%d Path=%q StartIndex=%d ReqCount=%d", req.DirID, req.Path, req.StartIndex, req.ReqCount)

	if req.FileBitmap == 0 && req.DirBitmap == 0 {
		return &FPEnumerateRes{}, ErrBitmapErr
	}
	if req.FileBitmap&^enumerateFileBitmapMask != 0 || req.DirBitmap&^enumerateDirBitmapMask != 0 {
		return &FPEnumerateRes{}, ErrBitmapErr
	}

	if _, ok := s.volumeRootByID(req.VolumeID); !ok {
		return &FPEnumerateRes{}, ErrParamErr
	}
	volFS := s.fsForVolume(req.VolumeID)
	if volFS == nil {
		return &FPEnumerateRes{}, ErrParamErr
	}
	if req.Path != "" && req.PathType != 1 && req.PathType != 2 {
		return &FPEnumerateRes{}, ErrParamErr
	}
	const enumerateReplyHeaderLen = 6
	if req.MaxReply < uint32(enumerateReplyHeaderLen+minEnumerateEntryLen(req.FileBitmap, req.DirBitmap)) {
		return &FPEnumerateRes{}, ErrParamErr
	}

	parentPath, ok := s.getDIDPath(req.VolumeID, req.DirID)
	if !ok {
		return &FPEnumerateRes{}, ErrDirNotFound
	}

	targetPath := parentPath
	if req.Path != "" {
		resolved, errCode := s.resolvePath(parentPath, req.Path, req.PathType)
		if errCode != NoErr {
			return &FPEnumerateRes{}, ErrParamErr
		}
		targetPath = resolved
	}

	info, err := volFS.Stat(targetPath)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return &FPEnumerateRes{}, ErrAccessDenied
		}
		return &FPEnumerateRes{}, ErrDirNotFound
	}
	if !info.IsDir() {
		return &FPEnumerateRes{}, ErrObjectTypeErr
	}

	var (
		entries      []fs.DirEntry
		visibleCount int
		usedRangeFS  bool
	)
	if volFS.Capabilities().ReadDirRange {
		var reqVisibleCount uint16
		entries, reqVisibleCount, err = volFS.ReadDirRange(targetPath, req.StartIndex, req.ReqCount)
		if err == nil {
			visibleCount = int(reqVisibleCount)
			usedRangeFS = true
		} else if !isNotSupported(err) {
			return &FPEnumerateRes{}, ErrDirNotFound
		}
	}
	if !usedRangeFS {
		entries, err = volFS.ReadDir(targetPath)
		if err != nil {
			if errors.Is(err, fs.ErrPermission) {
				return &FPEnumerateRes{}, ErrAccessDenied
			}
			return &FPEnumerateRes{}, ErrDirNotFound
		}
	}

	resData := new(bytes.Buffer)
	actCount := uint16(0)
	idx := uint16(1)

	for _, entry := range entries {
		if s.isMetadataArtifact(entry.Name(), entry.IsDir(), req.VolumeID) {
			continue
		}

		if entry.IsDir() && req.DirBitmap == 0 {
			continue
		}
		if !entry.IsDir() && req.FileBitmap == 0 {
			continue
		}
		if !usedRangeFS {
			visibleCount++
		}

		if !usedRangeFS && idx < req.StartIndex {
			idx++
			continue
		}
		if actCount >= req.ReqCount {
			break
		}

		fullPath := filepath.Join(targetPath, entry.Name())
		info, err := volFS.Stat(fullPath)
		if err != nil {
			continue
		}

		isDir := entry.IsDir()
		if EnableAppleDoubleIconFallback && !isDir {
			s.IngestAppleDoubleIcons(req.VolumeID, fullPath)
		}

		entryBuf := new(bytes.Buffer)
		entryBuf.WriteByte(0)

		if isDir {
			entryBuf.WriteByte(0x80)
		} else {
			entryBuf.WriteByte(0x00)
		}

		bitmap := req.FileBitmap
		if isDir {
			bitmap = req.DirBitmap
		}

		s.packFileInfo(entryBuf, req.VolumeID, bitmap, targetPath, entry.Name(), info, isDir)

		entryBytes := entryBuf.Bytes()
		entryLength := len(entryBytes)

		if entryLength%2 != 0 {
			entryBuf.WriteByte(0)
			entryBytes = entryBuf.Bytes()
			entryLength++
		}

		entryBytes[0] = byte(entryLength)

		if uint32(enumerateReplyHeaderLen+resData.Len()+len(entryBytes)) > req.MaxReply {
			break
		}

		resData.Write(entryBytes)
		actCount++
		idx++
	}

	res := &FPEnumerateRes{
		FileBitmap: req.FileBitmap,
		DirBitmap:  req.DirBitmap,
		ActCount:   actCount,
		Data:       resData.Bytes(),
	}

	errCode := NoErr
	if actCount == 0 && usedRangeFS && len(entries) == 0 {
		// Range-capable backends signal end-of-directory by returning an empty
		// page for the requested start index.
		errCode = ErrObjectNotFound
	}
	if actCount == 0 && req.StartIndex > uint16(visibleCount) {
		errCode = ErrObjectNotFound
	}

	return res, errCode
}

func minEnumerateEntryLen(fileBitmap, dirBitmap uint16) int {
	if fileBitmap == 0 {
		return minEnumerateEntryLenForBitmap(calcDirParamsSize(dirBitmap), dirBitmap&DirBitmapLongName != 0, dirBitmap&DirBitmapShortName != 0)
	}
	if dirBitmap == 0 {
		return minEnumerateEntryLenForBitmap(calcFileParamsSize(fileBitmap), fileBitmap&FileBitmapLongName != 0, fileBitmap&FileBitmapShortName != 0)
	}

	minFile := minEnumerateEntryLenForBitmap(calcFileParamsSize(fileBitmap), fileBitmap&FileBitmapLongName != 0, fileBitmap&FileBitmapShortName != 0)
	minDir := minEnumerateEntryLenForBitmap(calcDirParamsSize(dirBitmap), dirBitmap&DirBitmapLongName != 0, dirBitmap&DirBitmapShortName != 0)
	if minFile < minDir {
		return minFile
	}
	return minDir
}

func minEnumerateEntryLenForBitmap(fixedSize int, hasLongName, hasShortName bool) int {
	entryLen := 2 + fixedSize
	if hasLongName {
		entryLen++
	}
	if hasShortName {
		entryLen++
	}
	if entryLen%2 != 0 {
		entryLen++
	}
	return entryLen
}

const (
	enumerateFileBitmapMask = FileBitmapAttributes |
		FileBitmapParentDID |
		FileBitmapCreateDate |
		FileBitmapModDate |
		FileBitmapBackupDate |
		FileBitmapFinderInfo |
		FileBitmapLongName |
		FileBitmapShortName |
		FileBitmapFileNum |
		FileBitmapDataForkLen |
		FileBitmapRsrcForkLen |
		FileBitmapProDOSInfo

	enumerateDirBitmapMask = DirBitmapAttributes |
		DirBitmapParentDID |
		DirBitmapCreateDate |
		DirBitmapModDate |
		DirBitmapBackupDate |
		DirBitmapFinderInfo |
		DirBitmapLongName |
		DirBitmapShortName |
		DirBitmapDirID |
		DirBitmapOffspringCount |
		DirBitmapOwnerID |
		DirBitmapGroupID |
		DirBitmapAccessRights |
		DirBitmapProDOSInfo
)

func (s *AFPService) handleCloseDir(req *FPCloseDirReq) (*FPCloseDirRes, int32) {
	log.Printf("[AFP] FPCloseDir called for DirID %d on Vol %d", req.DirID, req.VolumeID)
	return &FPCloseDirRes{}, NoErr
}

func (s *AFPService) handleSetDirParms(req *FPSetDirParmsReq) (*FPSetDirParmsRes, int32) {
	if s.volumeIsReadOnly(req.VolumeID) {
		return &FPSetDirParmsRes{}, ErrVolLocked
	}
	targetPath, errCode := s.resolveSetPath(req.VolumeID, req.DirID, req.Path, req.PathType)
	if errCode != NoErr {
		return &FPSetDirParmsRes{}, errCode
	}
	s.applyFinderInfo(req.Bitmap, req.FinderInfo, targetPath, req.VolumeID)
	return &FPSetDirParmsRes{}, NoErr
}

func (s *AFPService) handleCreateDir(req *FPCreateDirReq) (*FPCreateDirRes, int32) {
	if s.fs == nil {
		return &FPCreateDirRes{}, ErrAccessDenied
	}
	if s.volumeIsReadOnly(req.VolumeID) {
		return &FPCreateDirRes{}, ErrVolLocked
	}
	targetPath, errCode := s.resolveSetPath(req.VolumeID, req.DirID, req.Path, req.PathType)
	if errCode != NoErr {
		return &FPCreateDirRes{}, errCode
	}
	backend := s.fsForPath(targetPath)
	if backend == nil {
		return &FPCreateDirRes{}, ErrAccessDenied
	}
	if err := backend.CreateDir(targetPath); err != nil {
		if os.IsExist(err) {
			return &FPCreateDirRes{}, ErrObjectExists
		}
		return &FPCreateDirRes{}, ErrAccessDenied
	}
	newDID := s.getPathDID(req.VolumeID, targetPath)
	return &FPCreateDirRes{DirID: newDID}, NoErr
}
