package afp

import (
	"bytes"
	"errors"
	"io/fs"
	"path/filepath"

	"github.com/pgodw/omnitalk/netlog"
)

// getDesktopDB looks up the DesktopDB associated with a DTRefNum.
// Must be called with s.mu held (at least RLock).
func (s *Service) getDesktopDB(dtRefNum uint16) (DesktopDB, bool) {
	volID, ok := s.dtRefs[dtRefNum]
	if !ok {
		return nil, false
	}
	db, ok := s.desktopDBs[volID]
	return db, ok
}

// volRelPath returns the path of absPath relative to volumeRoot, using forward slashes.
func volRelPath(volumeRoot, absPath string) string {
	rel, err := filepath.Rel(volumeRoot, absPath)
	if err != nil {
		return absPath
	}
	return filepath.ToSlash(rel)
}

// handleOpenDT opens the Desktop database for a volume.
// It creates the .AppleDesktop directory (for SMB client compatibility) and
// opens or initialises the .desktop.db cache for AFP desktop operations.
func (s *Service) handleOpenDT(req *FPOpenDTReq) (*FPOpenDTRes, int32) {
	root, ok := s.volumeRootByID(req.VolID)
	if !ok {
		return &FPOpenDTRes{}, ErrParamErr
	}

	// Keep .AppleDesktop directory for SMB client compatibility — macOS writes
	// its own Desktop DB / Desktop DF files into this directory.
	dtDir := filepath.Join(root, ".AppleDesktop")
	backend := s.fsForVolume(req.VolID)
	if backend == nil {
		return &FPOpenDTRes{}, ErrParamErr
	}
	if _, err := backend.Stat(dtDir); err != nil {
		if err2 := backend.CreateDir(dtDir); err2 != nil {
			if errors.Is(err2, fs.ErrPermission) || isNotSupported(err2) || s.volumeIsReadOnly(req.VolID) {
				netlog.Debug("[AFP][Desktop] skipping .AppleDesktop creation for volume=%d dir=%q: %v", req.VolID, dtDir, err2)
			} else {
				if _, err3 := backend.Stat(dtDir); err3 != nil {
					return &FPOpenDTRes{}, ErrMiscErr
				}
			}
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Lazily open the .desktop.db for this volume.
	if _, loaded := s.desktopDBs[req.VolID]; !loaded {
		volume, vok := s.volumeByID(req.VolID)
		if !vok {
			return &FPOpenDTRes{}, ErrParamErr
		}
		s.desktopDBs[req.VolID] = s.desktopDB.Open(volume)
	}

	dtRef := s.nextDTRef
	s.nextDTRef++
	s.dtRefs[dtRef] = req.VolID

	return &FPOpenDTRes{DTRefNum: dtRef}, NoErr
}

// handleCloseDT invalidates a Desktop database reference number.
func (s *Service) handleCloseDT(req *FPCloseDTReq) (*FPCloseDTRes, int32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.dtRefs[req.DTRefNum]; !ok {
		return &FPCloseDTRes{}, ErrParamErr
	}
	delete(s.dtRefs, req.DTRefNum)
	return &FPCloseDTRes{}, NoErr
}

// handleAddIcon stores an icon bitmap in the Desktop database.
func (s *Service) handleAddIcon(req *FPAddIconReq) (*FPAddIconRes, int32) {
	s.mu.RLock()
	db, ok := s.getDesktopDB(req.DTRefNum)
	volID, _ := s.dtRefs[req.DTRefNum]
	s.mu.RUnlock()
	if !ok {
		netlog.Debug("[AFP][Desktop] FPAddIcon dtRef=%d creator=%q type=%q itype=%d tag=%d size=%d -> ErrParamErr (no desktop db)", req.DTRefNum, string(req.Creator[:]), string(req.Type[:]), req.IType, req.Tag, req.Size)
		return &FPAddIconRes{}, ErrParamErr
	}
	if s.volumeIsReadOnly(volID) {
		return &FPAddIconRes{}, ErrAccessDenied
	}
	netlog.Debug("[AFP][Desktop] FPAddIcon dtRef=%d creator=%q type=%q itype=%d tag=%d size=%d", req.DTRefNum, string(req.Creator[:]), string(req.Type[:]), req.IType, req.Tag, req.Size)
	err := db.SetIcon(req.Creator, req.Type, req.IType, req.Tag, req.Data)
	if err == ErrIconSizeMismatch {
		netlog.Debug("[AFP][Desktop] FPAddIcon creator=%q type=%q itype=%d -> ErrIconTypeError (size mismatch)", string(req.Creator[:]), string(req.Type[:]), req.IType)
		return &FPAddIconRes{}, ErrIconTypeError
	}
	if err != nil {
		netlog.Debug("[AFP][Desktop] FPAddIcon creator=%q type=%q itype=%d -> ErrMiscErr: %v", string(req.Creator[:]), string(req.Type[:]), req.IType, err)
		return &FPAddIconRes{}, ErrMiscErr
	}
	netlog.Debug("[AFP][Desktop] FPAddIcon creator=%q type=%q itype=%d stored %d bytes", string(req.Creator[:]), string(req.Type[:]), req.IType, len(req.Data))
	return &FPAddIconRes{}, NoErr
}

// handleGetIcon retrieves an icon bitmap from the Desktop database.
func (s *Service) handleGetIcon(req *FPGetIconReq) (*FPGetIconRes, int32) {
	s.mu.RLock()
	db, ok := s.getDesktopDB(req.DTRefNum)
	s.mu.RUnlock()
	if !ok {
		netlog.Debug("[AFP][Desktop] FPGetIcon dtRef=%d creator=%q type=%q itype=%d size=%d -> ErrParamErr (no desktop db)", req.DTRefNum, string(req.Creator[:]), string(req.Type[:]), req.IType, req.Size)
		return &FPGetIconRes{}, ErrParamErr
	}
	entry, found := db.GetIcon(req.Creator, req.Type, req.IType)
	if !found && EnableAppleDoubleIconFallback {
		// Per-file fallback: walk the APPL mappings registered for this
		// creator and ingest icons from each app's AppleDouble resource fork.
		// Bounded by the number of registered apps for the creator — never
		// rebuilds the whole volume.
		volID, vok := s.dtRefs[req.DTRefNum]
		if vok {
			s.ingestAppleDoubleIconsForCreator(volID, db, req.Creator)
			entry, found = db.GetIcon(req.Creator, req.Type, req.IType)
		}
	}
	if !found {
		creatorKeyCount, totalIconCount := db.IconCount(req.Creator)
		netlog.Debug("[AFP][Desktop] FPGetIcon dtRef=%d creator=%q type=%q itype=%d size=%d -> ErrItemNotFound (desktop db miss; creatorKeys=%d totalIcons=%d)", req.DTRefNum, string(req.Creator[:]), string(req.Type[:]), req.IType, req.Size, creatorKeyCount, totalIconCount)
		return &FPGetIconRes{}, ErrItemNotFound
	}
	// Size==0 tests for icon presence; return empty data with success.
	if req.Size == 0 {
		netlog.Debug("[AFP][Desktop] FPGetIcon dtRef=%d creator=%q type=%q itype=%d size=0 -> present (stored=%d)", req.DTRefNum, string(req.Creator[:]), string(req.Type[:]), req.IType, len(entry.bitmap))
		return &FPGetIconRes{Data: nil}, NoErr
	}
	data := entry.bitmap
	if int(req.Size) < len(data) {
		data = data[:req.Size]
	}
	netlog.Debug("[AFP][Desktop] FPGetIcon dtRef=%d creator=%q type=%q itype=%d requested=%d stored=%d returned=%d", req.DTRefNum, string(req.Creator[:]), string(req.Type[:]), req.IType, req.Size, len(entry.bitmap), len(data))
	return &FPGetIconRes{Data: data}, NoErr
}

// handleGetIconInfo retrieves icon metadata by 1-based index for a given creator.
func (s *Service) handleGetIconInfo(req *FPGetIconInfoReq) (*FPGetIconInfoRes, int32) {
	s.mu.RLock()
	db, ok := s.getDesktopDB(req.DTRefNum)
	s.mu.RUnlock()
	if !ok {
		netlog.Debug("[AFP][Desktop] FPGetIconInfo dtRef=%d creator=%q index=%d -> ErrParamErr (no desktop db)", req.DTRefNum, string(req.Creator[:]), req.IconIndex)
		return &FPGetIconInfoRes{}, ErrParamErr
	}
	entry, fileType, iconType, found := db.GetIconInfo(req.Creator, req.IconIndex)
	if !found {
		netlog.Debug("[AFP][Desktop] FPGetIconInfo dtRef=%d creator=%q index=%d -> ErrObjectNotFound (desktop db miss)", req.DTRefNum, string(req.Creator[:]), req.IconIndex)
		return &FPGetIconInfoRes{}, ErrObjectNotFound
	}
	// Reply: Tag(4) + FileType(4) + IconType(1) + pad(1) + Size(2) = 12 bytes
	var hdr [12]byte
	hdr[0] = byte(entry.tag >> 24)
	hdr[1] = byte(entry.tag >> 16)
	hdr[2] = byte(entry.tag >> 8)
	hdr[3] = byte(entry.tag)
	copy(hdr[4:8], fileType[:])
	hdr[8] = iconType
	// hdr[9] = 0 (pad)
	size := uint16(len(entry.bitmap))
	hdr[10] = byte(size >> 8)
	hdr[11] = byte(size)
	netlog.Debug("[AFP][Desktop] FPGetIconInfo dtRef=%d creator=%q index=%d -> type=%q itype=%d tag=%d size=%d", req.DTRefNum, string(req.Creator[:]), req.IconIndex, string(fileType[:]), hdr[8], entry.tag, len(entry.bitmap))
	return &FPGetIconInfoRes{Header: hdr}, NoErr
}

// handleAddAPPL registers an APPL mapping in the Desktop database.
func (s *Service) handleAddAPPL(req *FPAddAPPLReq) (*FPAddAPPLRes, int32) {
	s.mu.RLock()
	db, ok := s.getDesktopDB(req.DTRefNum)
	volID, _ := s.dtRefs[req.DTRefNum]
	s.mu.RUnlock()
	if !ok {
		netlog.Debug("[AFP][Desktop] FPAddAPPL dtRef=%d creator=%q dirID=%d tag=%d path=%q -> ErrParamErr (no desktop db)", req.DTRefNum, string(req.Creator[:]), req.DirID, req.Tag, req.Path)
		return &FPAddAPPLRes{}, ErrParamErr
	}
	if s.volumeIsReadOnly(volID) {
		return &FPAddAPPLRes{}, ErrAccessDenied
	}

	// Verify the application file exists.
	targetPath, errCode := s.resolveVolumePath(volID, req.DirID, req.Path, req.PathType)
	if errCode != NoErr {
		netlog.Debug("[AFP][Desktop] FPAddAPPL dtRef=%d creator=%q dirID=%d tag=%d path=%q -> resolve err=%d", req.DTRefNum, string(req.Creator[:]), req.DirID, req.Tag, req.Path, errCode)
		return &FPAddAPPLRes{}, errCode
	}
	resolvedPath, info, err := s.statPathWithAppleDoubleFallback(targetPath)
	if err != nil {
		fallbackPath, _, fallbackErr := s.statPathWithAppleDoubleFallback(targetPath)
		if fallbackErr == nil {
			netlog.Debug("[AFP][Desktop] FPAddAPPL creator=%q path=%q resolved=%q -> direct stat miss, metadata fallback found %q", string(req.Creator[:]), req.Path, targetPath, fallbackPath)
		} else {
			netlog.Debug("[AFP][Desktop] FPAddAPPL creator=%q path=%q resolved=%q -> ErrObjectNotFound: %v", string(req.Creator[:]), req.Path, targetPath, err)
		}
		return &FPAddAPPLRes{}, ErrObjectNotFound
	}
	targetPath = resolvedPath
	if info.IsDir() {
		netlog.Debug("[AFP][Desktop] FPAddAPPL creator=%q path=%q resolved=%q -> ErrObjectTypeErr (directory)", string(req.Creator[:]), req.Path, targetPath)
		return &FPAddAPPLRes{}, ErrObjectTypeErr
	}

	if err := db.AddAPPL(req.Creator, req.Tag, req.DirID, req.Path); err != nil {
		netlog.Debug("[AFP][Desktop] FPAddAPPL creator=%q path=%q resolved=%q -> ErrMiscErr: %v", string(req.Creator[:]), req.Path, targetPath, err)
		return &FPAddAPPLRes{}, ErrMiscErr
	}
	netlog.Debug("[AFP][Desktop] FPAddAPPL dtRef=%d creator=%q dirID=%d tag=%d path=%q resolved=%q", req.DTRefNum, string(req.Creator[:]), req.DirID, req.Tag, req.Path, targetPath)
	return &FPAddAPPLRes{}, NoErr
}

// handleRemoveAPPL removes an APPL mapping from the Desktop database.
func (s *Service) handleRemoveAPPL(req *FPRemoveAPPLReq) (*FPRemoveAPPLRes, int32) {
	s.mu.RLock()
	db, ok := s.getDesktopDB(req.DTRefNum)
	volID, _ := s.dtRefs[req.DTRefNum]
	s.mu.RUnlock()
	if !ok {
		return &FPRemoveAPPLRes{}, ErrParamErr
	}
	if s.volumeIsReadOnly(volID) {
		return &FPRemoveAPPLRes{}, ErrAccessDenied
	}
	if err := db.RemoveAPPL(req.Creator, req.DirID, req.Path); err != nil {
		return &FPRemoveAPPLRes{}, ErrMiscErr
	}
	return &FPRemoveAPPLRes{}, NoErr
}

// handleGetAPPL retrieves an APPL mapping by 0-based index and returns file parameters.
func (s *Service) handleGetAPPL(req *FPGetAPPLReq) (*FPGetAPPLRes, int32) {
	s.mu.RLock()
	db, ok := s.getDesktopDB(req.DTRefNum)
	volID, _ := s.dtRefs[req.DTRefNum]
	s.mu.RUnlock()
	if !ok {
		netlog.Debug("[AFP][Desktop] FPGetAPPL dtRef=%d creator=%q index=%d bitmap=0x%04x -> ErrParamErr (no desktop db)", req.DTRefNum, string(req.Creator[:]), req.APPLIndex, req.Bitmap)
		return emptyGetAPPLRes(req), ErrParamErr
	}

	entry, found := db.GetAPPL(req.Creator, req.APPLIndex)
	if !found {
		netlog.Debug("[AFP][Desktop] FPGetAPPL dtRef=%d creator=%q index=%d -> ErrObjectNotFound (desktop db miss)", req.DTRefNum, string(req.Creator[:]), req.APPLIndex)
		return emptyGetAPPLRes(req), ErrObjectNotFound
	}

	// Resolve the application's filesystem path so we can return file parameters.
	targetPath, errCode := s.resolveVolumePath(volID, entry.dirID, entry.pathname, 2 /* long names */)
	if errCode != NoErr {
		netlog.Debug("[AFP][Desktop] FPGetAPPL creator=%q index=%d storedDirID=%d storedPath=%q -> resolve err=%d", string(req.Creator[:]), req.APPLIndex, entry.dirID, entry.pathname, errCode)
		return emptyGetAPPLRes(req), ErrObjectNotFound
	}
	resolvedPath, info, err := s.statPathWithAppleDoubleFallback(targetPath)
	if err != nil {
		fallbackPath, _, fallbackErr := s.statPathWithAppleDoubleFallback(targetPath)
		if fallbackErr == nil {
			netlog.Debug("[AFP][Desktop] FPGetAPPL creator=%q index=%d storedPath=%q resolved=%q -> direct stat miss, metadata fallback found %q", string(req.Creator[:]), req.APPLIndex, entry.pathname, targetPath, fallbackPath)
		} else {
			netlog.Debug("[AFP][Desktop] FPGetAPPL creator=%q index=%d storedPath=%q resolved=%q -> ErrObjectNotFound: %v", string(req.Creator[:]), req.APPLIndex, entry.pathname, targetPath, err)
		}
		return emptyGetAPPLRes(req), ErrObjectNotFound
	}
	targetPath = resolvedPath

	// Pack file parameters according to the client's bitmap.
	// We support the same subset as SupportedFileBitmap.
	bitmap := req.Bitmap & SupportedFileBitmap
	resData := new(bytes.Buffer)
	s.packFileInfo(resData, volID, bitmap, filepath.Dir(targetPath), filepath.Base(targetPath), info, false)

	return &FPGetAPPLRes{
		Bitmap:  bitmap,
		APPLTag: entry.tag,
		Data:    resData.Bytes(),
	}, NoErr
}

// emptyGetAPPLRes returns a valid empty FPGetAPPLRes envelope echoing the
// requested bitmap so clients can still parse the reply on error paths.
func emptyGetAPPLRes(req *FPGetAPPLReq) *FPGetAPPLRes {
	return &FPGetAPPLRes{Bitmap: req.Bitmap & SupportedFileBitmap}
}

// handleAddComment stores a Finder comment in the AppleDouble sidecar (preferred)
// or in the Desktop database (fallback when no CommentBackend is available).
func (s *Service) handleAddComment(req *FPAddCommentReq) (*FPAddCommentRes, int32) {
	s.mu.RLock()
	volID, volOK := s.dtRefs[req.DTRefNum]
	db, _ := s.getDesktopDB(req.DTRefNum)
	s.mu.RUnlock()
	if !volOK {
		return &FPAddCommentRes{}, ErrParamErr
	}
	if s.volumeIsReadOnly(volID) {
		return &FPAddCommentRes{}, ErrAccessDenied
	}

	targetPath, errCode := s.resolveVolumePath(volID, req.DirID, req.Path, req.PathType)
	if errCode != NoErr {
		return &FPAddCommentRes{}, errCode
	}
	resolvedPath, _, err := s.statPathWithAppleDoubleFallback(targetPath)
	if err != nil {
		return &FPAddCommentRes{}, ErrObjectNotFound
	}
	targetPath = resolvedPath

	if cb, ok := s.metaFor(volID).(CommentBackend); ok {
		if err := cb.WriteComment(targetPath, req.Comment); err != nil {
			return &FPAddCommentRes{}, ErrMiscErr
		}
		return &FPAddCommentRes{}, NoErr
	}

	if db == nil {
		return &FPAddCommentRes{}, ErrMiscErr
	}
	root, _ := s.volumeRootByID(volID)
	relPath := volRelPath(root, targetPath)
	if err := db.SetComment(relPath, string(req.Comment)); err != nil {
		return &FPAddCommentRes{}, ErrMiscErr
	}
	return &FPAddCommentRes{}, NoErr
}

// handleRemoveComment removes a Finder comment from the AppleDouble sidecar (preferred)
// or from the Desktop database (fallback).
func (s *Service) handleRemoveComment(req *FPRemoveCommentReq) (*FPRemoveCommentRes, int32) {
	s.mu.RLock()
	volID, volOK := s.dtRefs[req.DTRefNum]
	db, _ := s.getDesktopDB(req.DTRefNum)
	s.mu.RUnlock()
	if !volOK {
		return &FPRemoveCommentRes{}, ErrParamErr
	}
	if s.volumeIsReadOnly(volID) {
		return &FPRemoveCommentRes{}, ErrAccessDenied
	}

	targetPath, errCode := s.resolveVolumePath(volID, req.DirID, req.Path, req.PathType)
	if errCode != NoErr {
		return &FPRemoveCommentRes{}, errCode
	}
	resolvedPath, _, err := s.statPathWithAppleDoubleFallback(targetPath)
	if err != nil {
		return &FPRemoveCommentRes{}, ErrObjectNotFound
	}
	targetPath = resolvedPath

	if cb, ok := s.metaFor(volID).(CommentBackend); ok {
		if err := cb.RemoveComment(targetPath); err != nil {
			return &FPRemoveCommentRes{}, ErrMiscErr
		}
		return &FPRemoveCommentRes{}, NoErr
	}

	if db == nil {
		return &FPRemoveCommentRes{}, ErrMiscErr
	}
	root, _ := s.volumeRootByID(volID)
	relPath := volRelPath(root, targetPath)
	if err := db.RemoveComment(relPath); err != nil {
		return &FPRemoveCommentRes{}, ErrMiscErr
	}
	return &FPRemoveCommentRes{}, NoErr
}

// handleGetComment retrieves a Finder comment from the AppleDouble sidecar (preferred)
// or from the Desktop database (fallback).
func (s *Service) handleGetComment(req *FPGetCommentReq) (*FPGetCommentRes, int32) {
	s.mu.RLock()
	volID, volOK := s.dtRefs[req.DTRefNum]
	db, _ := s.getDesktopDB(req.DTRefNum)
	s.mu.RUnlock()
	if !volOK {
		return &FPGetCommentRes{}, ErrParamErr
	}

	targetPath, errCode := s.resolveVolumePath(volID, req.DirID, req.Path, req.PathType)
	if errCode != NoErr {
		return &FPGetCommentRes{}, errCode
	}
	resolvedPath, _, err := s.statPathWithAppleDoubleFallback(targetPath)
	if err != nil {
		return &FPGetCommentRes{}, ErrObjectNotFound
	}
	targetPath = resolvedPath

	if cb, ok := s.metaFor(volID).(CommentBackend); ok {
		comment, found := cb.ReadComment(targetPath)
		if !found {
			return &FPGetCommentRes{}, ErrObjectNotFound
		}
		return &FPGetCommentRes{Comment: comment}, NoErr
	}

	if db == nil {
		return &FPGetCommentRes{}, ErrObjectNotFound
	}
	root, _ := s.volumeRootByID(volID)
	relPath := volRelPath(root, targetPath)
	comment, found := db.GetComment(relPath)
	if !found {
		return &FPGetCommentRes{}, ErrObjectNotFound
	}
	return &FPGetCommentRes{Comment: []byte(comment)}, NoErr
}
