//go:build afp || all

package afp

import (
	"bytes"
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
)

func (s *Service) handleOpenDir(req *FPOpenDirReq) (*FPOpenDirRes, int32) {
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

// enumerateReplyHeaderLen is the fixed header size of an FPEnumerate reply
// (FileBitmap+DirBitmap+ActCount); each entry is appended after it.
const enumerateReplyHeaderLen = 6

func (s *Service) handleEnumerate(req *FPEnumerateReq) (*FPEnumerateRes, int32) {
	log.Printf("[AFP] FPEnumerate: DirID=%d Path=%q StartIndex=%d ReqCount=%d", req.DirID, req.Path, req.StartIndex, req.ReqCount)

	if errCode := validateEnumerateRequest(req); errCode != NoErr {
		return &FPEnumerateRes{}, errCode
	}
	volFS := s.fsForVolume(req.VolumeID)
	if volFS == nil {
		return &FPEnumerateRes{}, ErrParamErr
	}

	targetPath, errCode := s.resolveEnumerateTarget(req, volFS)
	if errCode != NoErr {
		return &FPEnumerateRes{}, errCode
	}

	entries, visibleCount, usedRangeFS, errCode := s.readEnumerateEntries(volFS, targetPath, req)
	if errCode != NoErr {
		return &FPEnumerateRes{}, errCode
	}

	resData, actCount, totalVisible := s.packEnumerateEntries(req, targetPath, entries, visibleCount, usedRangeFS)

	res := &FPEnumerateRes{
		FileBitmap: req.FileBitmap,
		DirBitmap:  req.DirBitmap,
		ActCount:   actCount,
		Data:       resData,
	}

	errCode = NoErr
	if actCount == 0 && usedRangeFS && len(entries) == 0 {
		// Range-capable backends signal end-of-directory by returning an empty
		// page for the requested start index.
		errCode = ErrObjectNotFound
	}
	if actCount == 0 && req.StartIndex > uint16(totalVisible) {
		errCode = ErrObjectNotFound
	}

	return res, errCode
}

// validateEnumerateRequest checks the caller-supplied bitmaps, path type, and
// MaxReply budget. It does not touch the filesystem.
func validateEnumerateRequest(req *FPEnumerateReq) int32 {
	if req.FileBitmap == 0 && req.DirBitmap == 0 {
		return ErrBitmapErr
	}
	if req.FileBitmap&^enumerateFileBitmapMask != 0 || req.DirBitmap&^enumerateDirBitmapMask != 0 {
		return ErrBitmapErr
	}
	if req.Path != "" && req.PathType != 1 && req.PathType != 2 {
		return ErrParamErr
	}
	if req.MaxReply < uint32(enumerateReplyHeaderLen+minEnumerateEntryLen(req.FileBitmap, req.DirBitmap)) {
		return ErrParamErr
	}
	return NoErr
}

// resolveEnumerateTarget walks DirID + Path to the directory whose contents
// will be enumerated. Returns the on-disk target path or an AFP error.
func (s *Service) resolveEnumerateTarget(req *FPEnumerateReq, volFS FileSystem) (string, int32) {
	if _, ok := s.volumeRootByID(req.VolumeID); !ok {
		return "", ErrParamErr
	}
	parentPath, ok := s.getDIDPath(req.VolumeID, req.DirID)
	if !ok {
		return "", ErrDirNotFound
	}
	targetPath := parentPath
	if req.Path != "" {
		resolved, errCode := s.resolvePath(parentPath, req.Path, req.PathType)
		if errCode != NoErr {
			return "", ErrParamErr
		}
		targetPath = resolved
	}

	info, err := volFS.Stat(targetPath)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return "", ErrAccessDenied
		}
		return "", ErrDirNotFound
	}
	if !info.IsDir() {
		return "", ErrObjectTypeErr
	}
	return targetPath, NoErr
}

// readEnumerateEntries lists targetPath, preferring a range-aware backend when
// available so paging stays cheap on virtual volumes. visibleCount is the
// total entry count when the backend is range-aware (zero otherwise — the
// pager increments it as it walks).
func (s *Service) readEnumerateEntries(volFS FileSystem, targetPath string, req *FPEnumerateReq) ([]fs.DirEntry, int, bool, int32) {
	if volFS.Capabilities().ReadDirRange {
		entries, reqVisibleCount, err := volFS.ReadDirRange(targetPath, req.StartIndex, req.ReqCount)
		if err == nil {
			return entries, int(reqVisibleCount), true, NoErr
		}
		if !isNotSupported(err) {
			return nil, 0, false, ErrDirNotFound
		}
	}
	entries, err := volFS.ReadDir(targetPath)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return nil, 0, false, ErrAccessDenied
		}
		return nil, 0, false, ErrDirNotFound
	}
	return entries, 0, false, NoErr
}

// packEnumerateEntries pages, filters, and serialises directory entries into
// the FPEnumerate reply payload. Returns the wire bytes, the actual entry
// count emitted, and the total visible entry count (which the caller uses to
// detect "start index past end").
func (s *Service) packEnumerateEntries(req *FPEnumerateReq, targetPath string, entries []fs.DirEntry, visibleCount int, usedRangeFS bool) ([]byte, uint16, int) {
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

		entryBytes, ok := s.packEnumerateEntry(req.VolumeID, targetPath, entry, req.FileBitmap, req.DirBitmap)
		if !ok {
			continue
		}
		if uint32(enumerateReplyHeaderLen+resData.Len()+len(entryBytes)) > req.MaxReply {
			break
		}
		resData.Write(entryBytes)
		actCount++
		idx++
	}
	return resData.Bytes(), actCount, visibleCount
}

// packEnumerateEntry serialises a single FPEnumerate result entry. It
// returns the entry's wire bytes (with the leading length byte populated
// and any trailing pad applied) and ok=false if the entry should be
// skipped (Stat failure). The volFS lookup is repeated here rather than
// threaded in so the helper stays self-contained.
func (s *Service) packEnumerateEntry(volumeID uint16, parentPath string, entry fs.DirEntry, fileBitmap, dirBitmap uint16) ([]byte, bool) {
	volFS := s.fsForVolume(volumeID)
	if volFS == nil {
		return nil, false
	}
	fullPath := filepath.Join(parentPath, entry.Name())
	info, err := volFS.Stat(fullPath)
	if err != nil {
		return nil, false
	}

	isDir := entry.IsDir()
	if EnableAppleDoubleIconFallback && !isDir {
		s.IngestAppleDoubleIcons(volumeID, fullPath)
	}

	entryBuf := new(bytes.Buffer)
	entryBuf.WriteByte(0)
	if isDir {
		entryBuf.WriteByte(0x80)
	} else {
		entryBuf.WriteByte(0x00)
	}

	bitmap := fileBitmap
	if isDir {
		bitmap = dirBitmap
	}
	s.packFileInfo(entryBuf, volumeID, bitmap, parentPath, entry.Name(), info, isDir)

	entryBytes := entryBuf.Bytes()
	if len(entryBytes)%2 != 0 {
		entryBuf.WriteByte(0)
		entryBytes = entryBuf.Bytes()
	}
	entryBytes[0] = byte(len(entryBytes))
	return entryBytes, true
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

func (s *Service) handleCloseDir(req *FPCloseDirReq) (*FPCloseDirRes, int32) {
	log.Printf("[AFP] FPCloseDir called for DirID %d on Vol %d", req.DirID, req.VolumeID)
	return &FPCloseDirRes{}, NoErr
}

func (s *Service) handleSetDirParms(req *FPSetDirParmsReq) (*FPSetDirParmsRes, int32) {
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

func (s *Service) handleCreateDir(req *FPCreateDirReq) (*FPCreateDirRes, int32) {
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
