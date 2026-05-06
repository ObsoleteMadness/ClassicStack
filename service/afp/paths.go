//go:build afp || all

package afp

import (
	"path/filepath"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/netlog"
	"github.com/ObsoleteMadness/ClassicStack/pkg/shortname"
)

func (s *Service) volIDForPath(path string) (uint16, bool) {
	clean := filepath.Clean(path)
	for _, vol := range s.Volumes {
		rel, err := filepath.Rel(vol.Config.Path, clean)
		if err == nil && !strings.HasPrefix(rel, "..") {
			return vol.ID, true
		}
	}
	return 0, false
}

// CNID-backed path/DID resolution and AFP path-string parsing. The
// helpers here translate between AFP pathnames (null-separated, with
// consecutive nulls ascending the tree) and host filesystem paths,
// and between Catalog Node IDs and the path strings they index.

func (s *Service) cnidStore(volumeID uint16) (CNIDStore, bool) {
	store, ok := s.cnidStores[volumeID]
	return store, ok
}

func (s *Service) getPathDID(volumeID uint16, path string) uint32 {
	store, ok := s.cnidStore(volumeID)
	if !ok {
		return CNIDInvalid
	}
	return store.Ensure(path)
}

func (s *Service) getDIDPath(volumeID uint16, did uint32) (string, bool) {
	store, ok := s.cnidStore(volumeID)
	if !ok {
		return "", false
	}
	return store.Path(did)
}

func (s *Service) resolveDIDPath(volumeID uint16, did uint32) (string, bool) {
	if did == CNIDInvalid {
		return "", false
	}
	return s.getDIDPath(volumeID, did)
}

func (s *Service) rebindDIDSubtree(volumeID uint16, oldPath, newPath string) {
	store, ok := s.cnidStore(volumeID)
	if !ok {
		return
	}
	store.Rebind(oldPath, newPath)
}

func (s *Service) removeDIDSubtree(volumeID uint16, path string) {
	store, ok := s.cnidStore(volumeID)
	if !ok {
		return
	}
	store.Remove(path)
}

func (s *Service) resolvePath(parentPath, name string, pathType uint8) (string, int32) {
	if pathType == 1 && !s.options.UseShortnames {
		// Short names are not supported.
		return "", ErrObjectNotFound
	}

	// AFP pathnames are separated by null bytes (\x00).
	// A single leading null byte is ignored.
	if len(name) > 0 && name[0] == '\x00' {
		name = name[1:]
	}

	// A pathname string is composed of CNode names separated by null bytes.
	// Consecutive null bytes ascend the directory tree:
	// Two consecutive null bytes ascend one level.
	// Three consecutive null bytes ascend two levels, etc.
	elements := strings.Split(name, "\x00")
	currentPath := parentPath

	for i := 0; i < len(elements); i++ {
		el := elements[i]
		if el == "" {
			// Empty element means a null byte following another null byte (or a leading/trailing one).
			// If it's the last element, it represents a trailing null byte which we can ignore.
			if i == len(elements)-1 {
				continue
			}
			// Each consecutive null byte (after the first separator) means ascending one level.
			// "To ascend one level... two consecutive null bytes should follow the offspring CNode name."
			// If we see an empty string here, it corresponds to ascending.
			currentPath = filepath.Dir(currentPath)
		} else {
			if pathType == 1 && s.options.UseShortnames {
				// Convert shortname to longname if possible
				volID, ok := s.volIDForPath(currentPath)
				if !ok {
					return "", ErrObjectNotFound
				}
				store, _ := s.cnidStore(volID)
				mapper := shortname.NewMapper(store)
				if long, ok := mapper.ShortToLong(el); ok {
					el = long
				}
				// If not found, `el` remains the short name string directly,
				// which is perfectly valid as per AFP spec (short name = long name if new).
			}

			hostEl := s.afpPathElementToHost(el)
			if hostEl == ".." {
				return "", ErrAccessDenied
			}
			if !s.options.DecomposedFilenames && hasHostReservedChar(hostEl) {
				return "", ErrAccessDenied
			}
			currentPath = s.canonicalizePath(filepath.Join(currentPath, hostEl))
		}
	}

	fullPath := filepath.Clean(currentPath)

	for _, vol := range s.Volumes {
		rel, err := filepath.Rel(vol.Config.Path, fullPath)
		if err == nil && !strings.HasPrefix(rel, "..") {
			return fullPath, NoErr
		}
	}
	return "", ErrAccessDenied
}

func (s *Service) resolveSetPath(volumeID uint16, dirID uint32, path string, pathType uint8) (string, int32) {
	parentPath, ok := s.resolveDIDPath(volumeID, dirID)
	if !ok && dirID != 0 {
		return "", ErrObjectNotFound
	} else if !ok {
		parentPath, _ = s.resolveDIDPath(volumeID, CNIDRoot)
	}
	if path == "" {
		return parentPath, NoErr
	}
	return s.resolvePath(parentPath, path, pathType)
}

func (s *Service) applyFinderInfo(bitmap uint16, finderInfo [32]byte, targetPath string, volID uint16) {
	if bitmap&FileBitmapFinderInfo != 0 {
		m := s.metaFor(volID)
		if m == nil {
			return
		}
		if err := m.WriteFinderInfo(targetPath, finderInfo); err != nil {
			netlog.Debug("[AFP] writeFinderInfo %q: %v", targetPath, err)
		}
	}
}
