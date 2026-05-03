//go:build afp || all

package afp

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/netlog"
	"github.com/ObsoleteMadness/ClassicStack/pkg/cnid"
)

// AppleDouble sidecar / hidden-name / icon canonicalisation helpers.
// These bridge the AFP-visible filesystem (which never sees ._sidecar
// files, .AppleDouble folders, or per-volume CNID databases) and the
// host filesystem where those artefacts physically live.

func (s *Service) statPathWithAppleDoubleFallback(path string) (string, fs.FileInfo, error) {
	m := s.metaForPath(path)
	if m == nil {
		return path, nil, os.ErrNotExist
	}
	return m.StatWithMetadataFallback(path)
}

// iconFileNameFor returns the host filesystem name for the Mac "Icon\r" file
// for the given volume, respecting its AppleDouble mode and decomposed filename settings.
func (s *Service) iconFileNameFor(volID uint16) string {
	if m := s.metaFor(volID); m != nil {
		return m.IconFileName()
	}
	if s.options.DecomposedFilenames {
		return "Icon0x0D"
	}
	return "Icon\r"
}

// canonicalizePath remaps any Icon\r variant in path to the canonical host
// name for the configured backend (e.g. Icon0x0D→Icon_ in legacy mode).
// This is applied during path resolution so both reads and writes use the
// correct on-disk name without duplicating the alias logic in every handler.
func (s *Service) canonicalizePath(path string) string {
	m := s.metaForPath(path)
	if m == nil {
		return path
	}
	base := filepath.Base(path)
	canonical := m.IconFileName()
	if isIconFile(base) && base != canonical {
		return filepath.Join(filepath.Dir(path), canonical)
	}
	return path
}

// alwaysHiddenNames lists directory and file names that are always hidden from
// AFP clients regardless of volume backend or AppleDouble mode. Names are
// matched case-insensitively.
var alwaysHiddenNames = []string{
	".appledesktop",
	".appledouble",
}

func (s *Service) isMetadataArtifact(name string, isDir bool, volID uint16) bool {
	if !isDir && strings.EqualFold(name, cnid.SQLiteFilename) {
		return true
	}
	for _, hidden := range alwaysHiddenNames {
		if strings.EqualFold(name, hidden) {
			return true
		}
	}
	if m := s.metaFor(volID); m != nil {
		return m.IsMetadataArtifact(name, isDir)
	}
	return strings.HasPrefix(name, "._")
}

// moveAppleDoubleSidecar renames an AppleDouble sidecar (._name) alongside a
// primary file rename/move. This is best-effort: missing sidecars are silently
// ignored, and unexpected errors are logged but not returned to the caller so
// that a sidecar failure never causes the already-completed primary operation
// to report an error to the client.
func (s *Service) moveAppleDoubleSidecar(oldPath, newPath string) error {
	m := s.metaForPath(oldPath)
	if m == nil {
		return nil
	}
	if err := m.MoveMetadata(oldPath, newPath); err != nil {
		netlog.Debug("[AFP] warning: could not move metadata %s → %s: %v", oldPath, newPath, err)
	}
	return nil
}

// deleteAppleDoubleSidecar removes a file's AppleDouble sidecar. This is
// best-effort: missing sidecars are silently ignored, and unexpected errors
// are logged but not returned to the caller.
func (s *Service) deleteAppleDoubleSidecar(path string) error {
	m := s.metaForPath(path)
	if m == nil {
		return nil
	}
	if err := m.DeleteMetadata(path); err != nil {
		netlog.Debug("[AFP] warning: could not delete metadata for %s: %v", path, err)
	}
	return nil
}
