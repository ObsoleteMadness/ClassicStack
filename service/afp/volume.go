//go:build afp || all

package afp

import (
	"bytes"
	"fmt"
	"hash/crc32"
	"math"
	"path/filepath"
	"strings"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/netlog"
	"github.com/ObsoleteMadness/ClassicStack/pkg/binutil"
)

const (
	defaultAFPBytesFree  = uint64(0x10000000)
	defaultAFPBytesTotal = uint64(0x20000000)
)

// installVolumes builds per-volume state from VolumeConfigs: assigns the
// volume ID, opens the CNID store, resolves the FileSystem backend, and
// wires the AppleDouble metadata backend. fallbackFS, when non-nil, wins
// over the per-volume registry lookup (used by tests that inject a single
// shared FileSystem).
func (s *Service) installVolumes(configs []VolumeConfig, fallbackFS FileSystem) {
	cnidBackend := resolveCNIDBackend(s.options)
	usedVolumeIDs := make(map[uint16]struct{}, len(configs))

	for i, cfg := range configs {
		volume := Volume{
			Config: cfg,
			ID:     s.assignVolumeID(cfg, i, usedVolumeIDs),
		}
		s.Volumes = append(s.Volumes, volume)

		store := cnidBackend.Open(volume)
		store.EnsureReserved(filepath.Clean(cfg.Path), CNIDRoot)
		s.cnidStores[volume.ID] = store

		s.volumeFS[volume.ID] = resolveVolumeFS(cfg, fallbackFS)
		s.installAppleDoubleBackend(volume.ID, cfg, fallbackFS)
	}
}

func (s *Service) assignVolumeID(cfg VolumeConfig, i int, used map[uint16]struct{}) uint16 {
	if s.options.PersistentVolumeIDs {
		return persistentVolumeIDForConfig(cfg, used)
	}
	id := uint16(i + 1)
	used[id] = struct{}{}
	return id
}

func resolveVolumeFS(cfg VolumeConfig, fallbackFS FileSystem) FileSystem {
	if fallbackFS != nil {
		return fallbackFS
	}
	if backend, err := newBackendForVolumeConfig(cfg); err == nil {
		return backend
	}
	return nil
}

func (s *Service) installAppleDoubleBackend(volID uint16, cfg VolumeConfig, fallbackFS FileSystem) {
	if s.metas == nil {
		return
	}
	metaFS := s.volumeFS[volID]
	if metaFS == nil {
		metaFS = fallbackFS
	}
	if metaFS == nil {
		return
	}
	mode := cfg.AppleDoubleMode
	if mode == "" {
		mode = s.options.AppleDoubleMode
	}
	s.metas[volID] = NewAppleDoubleBackend(metaFS, mode, s.options.DecomposedFilenames)
}

func constrainAFPVolumeType(volType uint16) uint16 {
	switch volType {
	case AFPVolumeTypeFlat, AFPVolumeTypeFixedDirID, AFPVolumeTypeVariableDirID:
		return volType
	default:
		return AFPVolumeTypeFixedDirID
	}
}

func (s *Service) volumeType(_ *Volume) uint16 {
	// ClassicStack exposes hierarchical volumes with CNID-based directory IDs,
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
	netlog.Debug("[AFP] FPCloseVol for Volume ID %d", req.VolumeID)
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

	res := &FPOpenVolRes{
		Bitmap: req.Bitmap,
		Data:   s.packVolumeParams(targetVol, req.Bitmap),
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

	res := &FPGetVolParmsRes{
		Bitmap: req.Bitmap,
		Data:   s.packVolumeParams(targetVol, req.Bitmap),
	}
	return res, NoErr
}

// packVolumeParams emits the AFP "volume parameters block" for vol per the
// caller-supplied bitmap (AFP 2.x §5.1.30). Variable-length fields (the
// volume name) are appended after the fixed section and referenced by an
// offset relative to the start of the parameters block.
func (s *Service) packVolumeParams(vol *Volume, bitmap uint16) []byte {
	fixedSize := calcVolParamsSize(bitmap)
	fixed := new(bytes.Buffer)
	var varBuf bytes.Buffer

	volDate := s.volumeDate(vol)
	bytesFree, bytesTotal := s.volumeCapacity(vol)

	backupDate := s.backupDates.get(vol.ID)

	if bitmap&VolBitmapAttributes != 0 {
		binutil.WriteU16(fixed, s.volumeAttributes(vol))
	}
	if bitmap&VolBitmapSignature != 0 {
		binutil.WriteU16(fixed, s.volumeType(vol))
	}
	if bitmap&VolBitmapCreateDate != 0 {
		binutil.WriteU32(fixed, volDate)
	}
	if bitmap&VolBitmapModDate != 0 {
		binutil.WriteU32(fixed, volDate)
	}
	if bitmap&VolBitmapBackupDate != 0 {
		binutil.WriteU32(fixed, backupDate)
	}
	if bitmap&VolBitmapVolID != 0 {
		binutil.WriteU16(fixed, vol.ID)
	}
	if bitmap&VolBitmapBytesFree != 0 {
		binutil.WriteU32(fixed, capAFPBytes32(bytesFree))
	}
	if bitmap&VolBitmapBytesTotal != 0 {
		binutil.WriteU32(fixed, capAFPBytes32(bytesTotal))
	}
	if bitmap&VolBitmapName != 0 {
		binutil.WriteU16(fixed, uint16(fixedSize+varBuf.Len()))
		s.writeAFPName(&varBuf, vol.Config.Name, vol.ID)
	}
	if bitmap&VolBitmapExtBytesFree != 0 {
		binutil.WriteU64(fixed, bytesFree)
	}
	if bitmap&VolBitmapExtBytesTotal != 0 {
		binutil.WriteU64(fixed, bytesTotal)
	}
	if bitmap&VolBitmapBlockSize != 0 {
		binutil.WriteU32(fixed, 4096)
	}

	return append(fixed.Bytes(), varBuf.Bytes()...)
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

	s.backupDates.set(req.VolumeID, req.BackupDate)

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

// catalogNameForPath returns the configured volume name when fullPath
// is the volume root, otherwise fallbackName. AFP clients see the
// configured volume name (which may differ from the host directory
// basename) for the root entry in catalog listings.
func (s *Service) catalogNameForPath(volumeID uint16, fullPath, fallbackName string) string {
	cleanPath := filepath.Clean(fullPath)
	for i := range s.Volumes {
		vol := s.Volumes[i]
		if vol.ID != volumeID {
			continue
		}
		if cleanPath == filepath.Clean(vol.Config.Path) && vol.Config.Name != "" {
			return vol.Config.Name
		}
		break
	}
	return fallbackName
}

// persistentVolumeIDForConfig derives a stable 16-bit volume ID
// from the volume's configured name and path so that clients see the
// same VolumeID across server restarts. Collisions within a single
// run are resolved by salting the CRC input.
func persistentVolumeIDForConfig(cfg VolumeConfig, used map[uint16]struct{}) uint16 {
	nameKey := strings.ToLower(strings.TrimSpace(cfg.Name))
	pathKey := filepath.Clean(strings.TrimSpace(cfg.Path))

	candidates := []string{
		nameKey,
		nameKey + "|" + pathKey,
	}
	for _, key := range candidates {
		id := crcVolumeID(key)
		if _, exists := used[id]; exists {
			continue
		}
		used[id] = struct{}{}
		return id
	}

	for salt := 1; ; salt++ {
		id := crcVolumeID(fmt.Sprintf("%s|%s|%d", nameKey, pathKey, salt))
		if _, exists := used[id]; exists {
			continue
		}
		used[id] = struct{}{}
		return id
	}
}

func crcVolumeID(key string) uint16 {
	id := uint16(crc32.ChecksumIEEE([]byte(key)) & 0xffff)
	if id == 0 {
		return 1
	}
	return id
}

// metaFor returns the ForkMetadataBackend for the given volume ID.
// If a per-volume backend is registered it is returned; otherwise the global
// injected backend (s.meta) is used. Returns nil when neither is available.
func (s *Service) metaFor(volID uint16) ForkMetadataBackend {
	if s.metas != nil {
		if m, ok := s.metas[volID]; ok {
			return m
		}
	}
	return s.meta
}

// metaForPath returns the ForkMetadataBackend for the volume whose root path
// is a prefix of path. Falls back to the global injected backend when no
// matching volume is found.
func (s *Service) metaForPath(path string) ForkMetadataBackend {
	clean := filepath.Clean(path)
	for _, vol := range s.Volumes {
		rel, err := filepath.Rel(vol.Config.Path, clean)
		if err == nil && !strings.HasPrefix(rel, "..") {
			return s.metaFor(vol.ID)
		}
	}
	return s.meta
}

func (s *Service) fsForVolume(volID uint16) FileSystem {
	if fs, ok := s.volumeFS[volID]; ok && fs != nil {
		return fs
	}
	return s.fs
}

func (s *Service) fsForPath(path string) FileSystem {
	clean := filepath.Clean(path)
	for _, vol := range s.Volumes {
		rel, err := filepath.Rel(filepath.Clean(vol.Config.Path), clean)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			if fs := s.fsForVolume(vol.ID); fs != nil {
				return fs
			}
		}
	}
	return s.fs
}

func newBackendForVolumeConfig(cfg VolumeConfig) (FileSystem, error) {
	fsType, err := NormalizeFSType(cfg.FSType)
	if err != nil {
		return nil, err
	}
	cfg.FSType = fsType
	cfg.Path = filepath.Clean(cfg.Path)
	return NewFS(cfg)
}
