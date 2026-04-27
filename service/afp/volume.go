//go:build afp

package afp

import (
	"bytes"
	"log"
	"math"
	"path/filepath"
	"time"

	"github.com/pgodw/omnitalk/pkg/binutil"
)

const (
	defaultAFPBytesFree  = uint64(0x10000000)
	defaultAFPBytesTotal = uint64(0x20000000)
)

func constrainAFPVolumeType(volType uint16) uint16 {
	switch volType {
	case AFPVolumeTypeFlat, AFPVolumeTypeFixedDirID, AFPVolumeTypeVariableDirID:
		return volType
	default:
		return AFPVolumeTypeFixedDirID
	}
}

func (s *Service) volumeType(_ *Volume) uint16 {
	// OmniTalk exposes hierarchical volumes with CNID-based directory IDs,
	// so we advertise Variable Directory ID semantics.
	return constrainAFPVolumeType(AFPVolumeTypeFixedDirID)
}

func capAFPBytes32(v uint64) uint32 {
	if v > uint64(math.MaxInt32) {
		return math.MaxInt32
	}
	return uint32(v)
}

func (s *Service) volumeAttributes(vol *Volume) uint16 {
	if vol == nil {
		return 0
	}
	attrs := uint16(0)
	if s.volumeIsReadOnly(vol.ID) {
		attrs |= VolAttrReadOnly
	}
	volFS := s.fsForVolume(vol.ID)
	if volFS != nil {
		volumeRoot := filepath.Clean(vol.Config.Path)
		if volFS.Capabilities().CatSearch {
			if supported, err := volFS.SupportsCatSearch(volumeRoot); err == nil && supported {
				attrs |= VolAttrSupportsCatSearch
			}
		}
	}
	return attrs
}

func (s *Service) handleCloseVol(req *FPCloseVolReq) (*FPCloseVolRes, int32) {
	log.Printf("[AFP] FPCloseVol for Volume ID %d", req.VolumeID)
	return &FPCloseVolRes{}, NoErr
}

func (s *Service) handleOpenVol(req *FPOpenVolReq) (*FPOpenVolRes, int32) {
	// handleOpenVol implements the FPOpenVol operation.
	//
	// Algorithm (summary): Ensure the requested volume exists and the
	// client provided a non-null Bitmap that includes the Volume ID bit.
	// If the volume is password-protected, compare the provided password
	// (up to 8 bytes, padded with NULs) in a case-sensitive manner and
	// reject with ErrAccessDenied on mismatch or absence. On success,
	// prepare the requested volume parameters and return them with a
	// copy of the request Bitmap. This call must be made by the client
	// before any file/directory operations on the volume.

	var targetVol *Volume
	for i := range s.Volumes {
		if s.Volumes[i].Config.Name == req.VolName {
			targetVol = &s.Volumes[i]
			break
		}
	}

	if targetVol == nil {
		return &FPOpenVolRes{}, ErrObjectNotFound
	}

	if req.Bitmap&VolBitmapVolID == 0 {
		return &FPOpenVolRes{}, ErrBitmapErr
	}
	if unsupported := req.Bitmap &^ SupportedVolBitmap; unsupported != 0 {
		return &FPOpenVolRes{}, ErrBitmapErr
	}

	if targetVol.Config.Password != "" {
		expected := targetVol.Config.Password
		if len(expected) > 8 {
			expected = expected[:8]
		}
		if req.Password != expected {
			return &FPOpenVolRes{}, ErrAccessDenied
		}
	}

	cleanRoot := filepath.Clean(targetVol.Config.Path)
	if store, ok := s.cnidStore(targetVol.ID); ok {
		store.EnsureReserved(cleanRoot, CNIDRoot)
	}
	volDate := s.volumeDate(targetVol)
	bytesFree, bytesTotal := s.volumeCapacity(targetVol)

	fixedSize := calcVolParamsSize(req.Bitmap)
	fixed := new(bytes.Buffer)
	var varBuf bytes.Buffer

	s.mu.RLock()
	backupDate := s.volumeBackupDate[targetVol.ID]
	s.mu.RUnlock()

	if req.Bitmap&VolBitmapAttributes != 0 {
		binutil.WriteU16(fixed, s.volumeAttributes(targetVol))
	}
	if req.Bitmap&VolBitmapSignature != 0 {
		binutil.WriteU16(fixed, s.volumeType(targetVol))
	}
	if req.Bitmap&VolBitmapCreateDate != 0 {
		binutil.WriteU32(fixed, volDate)
	}
	if req.Bitmap&VolBitmapModDate != 0 {
		binutil.WriteU32(fixed, volDate)
	}
	if req.Bitmap&VolBitmapBackupDate != 0 {
		binutil.WriteU32(fixed, backupDate)
	}
	if req.Bitmap&VolBitmapVolID != 0 {
		binutil.WriteU16(fixed, targetVol.ID)
	}
	if req.Bitmap&VolBitmapBytesFree != 0 {
		binutil.WriteU32(fixed, capAFPBytes32(bytesFree))
	}
	if req.Bitmap&VolBitmapBytesTotal != 0 {
		binutil.WriteU32(fixed, capAFPBytes32(bytesTotal))
	}
	if req.Bitmap&VolBitmapName != 0 {
		binutil.WriteU16(fixed, uint16(fixedSize+varBuf.Len()))
		s.writeAFPName(&varBuf, targetVol.Config.Name, targetVol.ID)
	}
	if req.Bitmap&VolBitmapExtBytesFree != 0 {
		binutil.WriteU64(fixed, bytesFree)
	}
	if req.Bitmap&VolBitmapExtBytesTotal != 0 {
		binutil.WriteU64(fixed, bytesTotal)
	}
	if req.Bitmap&VolBitmapBlockSize != 0 {
		binutil.WriteU32(fixed, 4096)
	}

	res := &FPOpenVolRes{
		Bitmap: req.Bitmap,
		Data:   append(fixed.Bytes(), varBuf.Bytes()...),
	}

	return res, NoErr
}

func (s *Service) volumeRootByID(volumeID uint16) (string, bool) {
	for i := range s.Volumes {
		if s.Volumes[i].ID == volumeID {
			return filepath.Clean(s.Volumes[i].Config.Path), true
		}
	}
	return "", false
}

func (s *Service) volumeByID(volumeID uint16) (Volume, bool) {
	for i := range s.Volumes {
		if s.Volumes[i].ID == volumeID {
			return s.Volumes[i], true
		}
	}
	return Volume{}, false
}

func (s *Service) volumeIsReadOnly(volumeID uint16) bool {
	for i := range s.Volumes {
		if s.Volumes[i].ID == volumeID {
			if s.Volumes[i].Config.ReadOnly {
				return true
			}
			volFS := s.fsForVolume(volumeID)
			if volFS != nil {
				if volFS.Capabilities().ReadOnlyState {
					if readonly, err := volFS.IsReadOnly(filepath.Clean(s.Volumes[i].Config.Path)); err == nil {
						return readonly
					}
				}
			}
			return false
		}
	}
	return false
}

func (s *Service) volumeDate(vol *Volume) uint32 {
	if vol == nil {
		return toAFPTime(time.Now())
	}
	if volFS := s.fsForVolume(vol.ID); volFS != nil {
		if info, err := volFS.Stat(filepath.Clean(vol.Config.Path)); err == nil && info != nil {
			return toAFPTime(info.ModTime())
		}
	}
	return toAFPTime(time.Now())
}

func (s *Service) resolveVolumePath(volumeID uint16, dirID uint32, relPath string, pathType uint8) (string, int32) {
	basePath, ok := s.getDIDPath(volumeID, dirID)
	if !ok {
		if dirID == 0 {
			basePath, ok = s.getDIDPath(volumeID, CNIDRoot)
			if !ok {
				root, vok := s.volumeRootByID(volumeID)
				if !vok {
					return "", ErrParamErr
				}
				basePath = root
			}
		} else {
			return "", ErrObjectNotFound
		}
	}
	if relPath == "" {
		return basePath, NoErr
	}
	full, errCode := s.resolvePath(basePath, relPath, pathType)
	if errCode != NoErr {
		return "", errCode
	}
	return full, NoErr
}

func (s *Service) handleGetVolParms(req *FPGetVolParmsReq) (*FPGetVolParmsRes, int32) {
	// handleGetVolParms implements the FPGetVolParms operation.
	//
	// Algorithm (summary): Verify the volume exists and that the
	// Bitmap is supported. The server returns a copy of the Bitmap
	// followed by the requested parameters packed in bitmap order.
	// Variable-length parameters (for example, the Volume Name) are
	// represented in the fixed section as offsets (measured from the
	// start of the parameters block) and their contents appended after
	// the fixed fields. The client must previously have opened the
	// volume with FPOpenVol.

	var targetVol *Volume
	for i := range s.Volumes {
		if s.Volumes[i].ID == req.VolumeID {
			targetVol = &s.Volumes[i]
			break
		}
	}
	if targetVol == nil {
		return &FPGetVolParmsRes{}, ErrObjectNotFound
	}

	if unsupported := req.Bitmap &^ SupportedVolBitmap; unsupported != 0 {
		return &FPGetVolParmsRes{}, ErrBitmapErr
	}

	fixedSize := calcVolParamsSize(req.Bitmap)
	fixed := new(bytes.Buffer)
	var varBuf bytes.Buffer
	volDate := s.volumeDate(targetVol)
	bytesFree, bytesTotal := s.volumeCapacity(targetVol)

	s.mu.RLock()
	backupDate := s.volumeBackupDate[req.VolumeID]
	s.mu.RUnlock()

	if req.Bitmap&VolBitmapAttributes != 0 {
		binutil.WriteU16(fixed, s.volumeAttributes(targetVol))
	}
	if req.Bitmap&VolBitmapSignature != 0 {
		binutil.WriteU16(fixed, s.volumeType(targetVol))
	}
	if req.Bitmap&VolBitmapCreateDate != 0 {
		binutil.WriteU32(fixed, volDate)
	}
	if req.Bitmap&VolBitmapModDate != 0 {
		binutil.WriteU32(fixed, volDate)
	}
	if req.Bitmap&VolBitmapBackupDate != 0 {
		binutil.WriteU32(fixed, backupDate)
	}
	if req.Bitmap&VolBitmapVolID != 0 {
		binutil.WriteU16(fixed, targetVol.ID)
	}
	if req.Bitmap&VolBitmapBytesFree != 0 {
		binutil.WriteU32(fixed, capAFPBytes32(bytesFree))
	}
	if req.Bitmap&VolBitmapBytesTotal != 0 {
		binutil.WriteU32(fixed, capAFPBytes32(bytesTotal))
	}
	if req.Bitmap&VolBitmapName != 0 {
		binutil.WriteU16(fixed, uint16(fixedSize+varBuf.Len()))
		s.writeAFPName(&varBuf, targetVol.Config.Name, targetVol.ID)
	}
	if req.Bitmap&VolBitmapExtBytesFree != 0 {
		binutil.WriteU64(fixed, bytesFree)
	}
	if req.Bitmap&VolBitmapExtBytesTotal != 0 {
		binutil.WriteU64(fixed, bytesTotal)
	}
	if req.Bitmap&VolBitmapBlockSize != 0 {
		binutil.WriteU32(fixed, 4096)
	}

	res := &FPGetVolParmsRes{
		Bitmap: req.Bitmap,
		Data:   append(fixed.Bytes(), varBuf.Bytes()...),
	}
	return res, NoErr
}

func (s *Service) handleSetVolParms(req *FPSetVolParmsReq) (*FPSetVolParmsRes, int32) {
	if s.volumeIsReadOnly(req.VolumeID) {
		return &FPSetVolParmsRes{}, ErrVolLocked
	}
	if req.Bitmap != VolBitmapBackupDate {
		return &FPSetVolParmsRes{}, ErrBitmapErr
	}

	var ok bool
	for i := range s.Volumes {
		if s.Volumes[i].ID == req.VolumeID {
			ok = true
			break
		}
	}
	if !ok {
		return &FPSetVolParmsRes{}, ErrParamErr
	}

	s.mu.Lock()
	s.volumeBackupDate[req.VolumeID] = req.BackupDate
	s.mu.Unlock()

	return &FPSetVolParmsRes{}, NoErr
}

func (s *Service) volumeCapacity(vol *Volume) (bytesFree uint64, bytesTotal uint64) {
	bytesFree = defaultAFPBytesFree
	bytesTotal = defaultAFPBytesTotal
	if vol == nil {
		return bytesFree, bytesTotal
	}
	volFS := s.fsForVolume(vol.ID)
	if volFS == nil {
		return bytesFree, bytesTotal
	}

	total, free, err := volFS.DiskUsage(filepath.Clean(vol.Config.Path))
	if err != nil {
		return bytesFree, bytesTotal
	}
	return free, total
}

// calcVolParamsSize returns the total byte size of all fixed fields
// (including the variable-name offset pointer) in a volume parameter
// block for the given bitmap. The variable-length name itself is
// emitted into a separate buffer and concatenated by the caller.
func calcVolParamsSize(bitmap uint16) int {
	size := 0
	if bitmap&VolBitmapAttributes != 0 {
		size += 2
	}
	if bitmap&VolBitmapSignature != 0 {
		size += 2
	}
	if bitmap&VolBitmapCreateDate != 0 {
		size += 4
	}
	if bitmap&VolBitmapModDate != 0 {
		size += 4
	}
	if bitmap&VolBitmapBackupDate != 0 {
		size += 4
	}
	if bitmap&VolBitmapVolID != 0 {
		size += 2
	}
	if bitmap&VolBitmapBytesFree != 0 {
		size += 4
	}
	if bitmap&VolBitmapBytesTotal != 0 {
		size += 4
	}
	if bitmap&VolBitmapName != 0 {
		size += 2 // offset pointer
	}
	if bitmap&VolBitmapExtBytesFree != 0 {
		size += 8
	}
	if bitmap&VolBitmapExtBytesTotal != 0 {
		size += 8
	}
	if bitmap&VolBitmapBlockSize != 0 {
		size += 4
	}
	return size
}
