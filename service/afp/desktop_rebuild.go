//go:build afp

package afp

// Desktop database rebuild / ingest support. Populates the in-memory and
// on-disk .desktop.db for a volume by walking the filesystem and pulling
// icons out of AppleDouble resource forks — useful for volumes imported
// from netatalk where a desktop database may never have been generated
// through our own FPAddIcon path.

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/pgodw/omnitalk/netlog"
	"github.com/pgodw/omnitalk/pkg/appledouble"
)

// EnableAppleDoubleIconFallback controls whether FPGetIcon misses trigger a
// best-effort rebuild of the Desktop database from AppleDouble resource forks.
// A rebuild walks the entire volume and parses BNDL/FREF/ICN# chains, so
// enabling it costs a one-time O(N) scan per volume on first icon miss.
const EnableAppleDoubleIconFallback = true

// volumeRootByIDLocked is the lock-free helper used from ingest paths that
// already hold s.mu.
func (s *Service) desktopDBForVolumeLocked(volID uint16) DesktopDB {
	if db, ok := s.desktopDBs[volID]; ok {
		return db
	}
	volume, ok := s.volumeByID(volID)
	if !ok {
		return nil
	}
	db := s.desktopDB.Open(volume)
	s.desktopDBs[volID] = db
	return db
}

// appleDoubleOwnerPath normalizes a host file path or AppleDouble sidecar path
// to the logical host file path the metadata backend expects.
func (s *Service) appleDoubleOwnerPath(filePath string) string {
	m := s.metaForPath(filePath)
	if backend, ok := m.(*AppleDoubleBackend); ok {
		return backend.ownerPath(filePath)
	}
	return filePath
}

// appleDoubleMetadataPath returns the sidecar path for filePath using the
// MetadataPath method on the metadata backend. Returns "" if no backend is configured.
func (s *Service) appleDoubleMetadataPath(filePath string) string {
	filePath = s.appleDoubleOwnerPath(filePath)
	m := s.metaForPath(filePath)
	if m == nil {
		return ""
	}
	return m.MetadataPath(filePath)
}

// IngestAppleDoubleIcons parses the AppleDouble sidecar for filePath (if any)
// and adds any icons it finds to the Desktop database for volID. Three sources
// are consumed:
//
//  1. Icons embedded directly in the AppleDouble as entry ID 5 (classic B&W
//     icon per netatalk adouble.h / AppleSingle spec). These are keyed by the
//     file's own (type, creator) pulled from FinderInfo.
//  2. ICN# icons reachable via BNDL/FREF chains inside the resource fork
//     (entry ID 2), which typically covers APPL files that ship icons for
//     every document type they own.
//  3. Custom folder icons from Icon\r files: ICN#/icl4/icl8 resources at
//     the well-known resource ID -16455 (kCustomIconResource).
//
// Returns the number of icons added.
func (s *Service) IngestAppleDoubleIcons(volID uint16, filePath string) int {
	filePath = s.appleDoubleOwnerPath(filePath)
	adPath := s.appleDoubleMetadataPath(filePath)
	if adPath == "" {
		return 0
	}
	raw, err := os.ReadFile(adPath)
	if err != nil {
		return 0
	}
	ad, err := appledouble.Parse(raw)
	if err != nil {
		return 0
	}

	isAPPL := ad.HasFinder && ad.FinderInfo[0] == 'A' && ad.FinderInfo[1] == 'P' && ad.FinderInfo[2] == 'P' && ad.FinderInfo[3] == 'L'
	isIconFile := isIconFile(filepath.Base(filePath))

	var icons []extractedIcon
	// For APPL files, the AppleDouble embedded icon entry is ignored — the
	// authoritative app icon lives in the resource fork's ID-128 icon family.
	if !isAPPL && !isIconFile && ad.HasIconBW && len(ad.IconBW) > 0 && ad.HasFinder {
		if icon, ok := iconFromAppleDoubleEntry(ad.FinderInfo, ad.IconBW); ok {
			icons = append(icons, icon)
		}
	}
	if ad.HasResource && len(ad.Resource) > 0 {
		icons = append(icons, extractIconsFromResourceFork(ad.Resource)...)
		if isAPPL {
			var creator [4]byte
			copy(creator[:], ad.FinderInfo[4:8])
			icons = append(icons, extractAppIconFromResourceFork(ad.Resource, creator)...)
		}
		if isIconFile {
			// Icon\r files store custom folder icons at resource ID -16455.
			// AFP convention: these are always keyed as creator="MACS" type="fldr"
			// regardless of what the Icon file's own FinderInfo says.
			var creator, fileType [4]byte
			copy(creator[:], "MACS")
			copy(fileType[:], "fldr")
			icons = append(icons, extractCustomIconFromResourceFork(ad.Resource, creator, fileType)...)
		}
	}
	if len(icons) == 0 {
		return 0
	}

	s.mu.Lock()
	db := s.desktopDBForVolumeLocked(volID)
	s.mu.Unlock()
	if db == nil {
		return 0
	}

	added := 0
	for _, icon := range icons {
		if _, found := db.GetIcon(icon.creator, icon.fileType, icon.iconType); found {
			netlog.Debug("[AFP][Desktop] ingest skip existing icon creator=%q type=%q itype=%d path=%q", string(icon.creator[:]), string(icon.fileType[:]), icon.iconType, filePath)
			continue
		}
		if err := db.SetIcon(icon.creator, icon.fileType, icon.iconType, 0, icon.bitmap); err != nil {
			netlog.Debug("[AFP][Desktop] ingest icon error creator=%q type=%q itype=%d path=%q: %v", string(icon.creator[:]), string(icon.fileType[:]), icon.iconType, filePath, err)
			continue
		}
		netlog.Debug("[AFP][Desktop] ingest added icon creator=%q type=%q itype=%d size=%d path=%q", string(icon.creator[:]), string(icon.fileType[:]), icon.iconType, len(icon.bitmap), filePath)
		added++
	}
	return added
}

// ingestAppleDoubleIconsForCreator resolves every APPL mapping registered
// for creator on volID and feeds each app file through IngestAppleDoubleIcons.
// This is the per-file fallback used by FPGetIcon on a cache miss — it never
// walks the volume.
func (s *Service) ingestAppleDoubleIconsForCreator(volID uint16, db DesktopDB, creator [4]byte) {
	entries := db.ListAPPL(creator)
	for _, e := range entries {
		path, errCode := s.resolveVolumePath(volID, e.dirID, e.pathname, 2 /* long names */)
		if errCode != NoErr {
			continue
		}
		s.IngestAppleDoubleIcons(volID, path)
	}
}

// RebuildDesktopDBFromVolume walks the volume's filesystem and ingests
// AppleDouble-resident icons into the Desktop database. It skips our own
// metadata artifacts (._*, .AppleDouble, .AppleDesktop, .desktop.db).
// It also probes each directory for an Icon\r file (using the canonical
// host name from the metadata backend) and ingests custom folder icons.
// Returns (filesScanned, iconsAdded).
func (s *Service) RebuildDesktopDBFromVolume(volID uint16) (filesScanned, iconsAdded int) {
	root, ok := s.volumeRootByID(volID)
	if !ok {
		return 0, 0
	}
	iconName := s.iconFileNameFor(volID)
	netlog.Info("[AFP][Desktop] rebuild starting volID=%d root=%q iconName=%q", volID, root, iconName)
	filepath.Walk(root, func(path string, info fs.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		base := filepath.Base(path)
		if info.IsDir() {
			if base == ".AppleDouble" || base == ".AppleDesktop" {
				return filepath.SkipDir
			}
			// Probe for an Icon\r file inside this directory.
			iconPath := filepath.Join(path, iconName)
			backend := s.fsForPath(iconPath)
			if backend != nil {
				if _, iconErr := backend.Stat(iconPath); iconErr == nil {
					netlog.Debug("[AFP][Desktop] rebuild scanning icon file=%q", iconPath)
					filesScanned++
					iconsAdded += s.IngestAppleDoubleIcons(volID, iconPath)
				}
			}
			return nil
		}
		if strings.HasPrefix(base, "._") || base == desktopDBFilename {
			return nil
		}
		netlog.Debug("[AFP][Desktop] rebuild scanning file=%q", path)
		filesScanned++
		iconsAdded += s.IngestAppleDoubleIcons(volID, path)
		return nil
	})
	netlog.Info("[AFP][Desktop] rebuild finished volID=%d scanned=%d iconsAdded=%d", volID, filesScanned, iconsAdded)
	return
}

// rebuildDesktopDBsIfConfigured triggers a rebuild for each volume that has
// RebuildDesktopDB set in its VolumeConfig. Safe to call once at service start.
func (s *Service) rebuildDesktopDBsIfConfigured() {
	for i := range s.Volumes {
		if s.Volumes[i].Config.RebuildDesktopDB {
			s.RebuildDesktopDBFromVolume(s.Volumes[i].ID)
		}
	}
}
