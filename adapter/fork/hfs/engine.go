//go:build darwin

package hfs

import (
	"errors"
	stdfs "io/fs"
	"os"

	corefs "github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// hfsForkEngine serves the resource fork and Finder info from the HOST file's macOS
// facilities: the resource fork is the "<file>/..namedfork/rsrc" stream and the Finder
// info is the com.apple.FinderInfo xattr (finderinfo_darwin.go). The data fork is the
// plain host file, reached through the base FileSystem like the AppleDouble engine.
type hfsForkEngine struct {
	base corefs.FileSystem
	host corefs.HostPather
}

func newHFSForkEngine(base corefs.FileSystem, host corefs.HostPather) *hfsForkEngine {
	return &hfsForkEngine{base: base, host: host}
}

// rsrcStreamPath is the host path of the HFS+ resource-fork stream for a store path,
// or ok=false when the store path cannot be resolved to a host path.
func (e *hfsForkEngine) rsrcStreamPath(storePath string) (string, bool) {
	hp, ok := e.host.HostPath(storePath)
	if !ok {
		return "", false
	}
	return hp + "/..namedfork/rsrc", true
}

func (e *hfsForkEngine) OpenFork(path string, fork corefs.ForkType, flag int) (corefs.File, error) {
	if fork == corefs.DataFork {
		// The data fork is the plain host file; defer to the base FileSystem.
		return e.base.OpenFile(path, flag)
	}
	sp, ok := e.rsrcStreamPath(path)
	if !ok {
		return nil, stdfs.ErrNotExist
	}
	// 0644 (if O_CREATE): the resource-fork stream is a companion of a
	// shared-volume user file and shares its permission model (see core/fs).
	// sp is derived from a share-relative path already validated by the base FS,
	// not an attacker-controlled absolute path.
	f, err := os.OpenFile(sp, flag, 0o644) // #nosec G302,G304 -- shared-volume fork stream, path validated by base FS
	if err != nil {
		if errors.Is(err, stdfs.ErrNotExist) && flag&os.O_CREATE == 0 {
			return nil, stdfs.ErrNotExist
		}
		return nil, err
	}
	return f, nil // *os.File satisfies fs.File (ReadAt/WriteAt/Truncate/Stat/Sync/Close)
}

func (e *hfsForkEngine) ForkLen(path string, fork corefs.ForkType) (int64, error) {
	if fork == corefs.DataFork {
		info, err := e.base.Stat(path)
		if err != nil {
			return 0, err
		}
		return info.Size(), nil
	}
	sp, ok := e.rsrcStreamPath(path)
	if !ok {
		return 0, nil
	}
	info, err := os.Stat(sp)
	if err != nil {
		if errors.Is(err, stdfs.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	return info.Size(), nil
}

// ReadFinderInfo / WriteFinderInfo live in finderinfo_darwin.go (com.apple.FinderInfo
// xattr).

// ReadComment / WriteComment: HFS+ has no per-file comment stream (the Finder comment is
// an AFP desktop-DB concern), so they are not persisted here.
func (e *hfsForkEngine) ReadComment(path string) ([]byte, bool) { _ = path; return nil, false }
func (e *hfsForkEngine) WriteComment(path string, c []byte) error {
	_ = path
	_ = c
	return nil
}

// MoveMetadata / DeleteMetadata are no-ops: the resource fork and Finder info are host
// attributes of the file itself, so the base FileSystem's Rename/Remove of the data path
// carries them automatically.
func (e *hfsForkEngine) MoveMetadata(old, new string) error { _ = old; _ = new; return nil }
func (e *hfsForkEngine) DeleteMetadata(path string) error   { _ = path; return nil }

// MetadataPaths returns nil: HFS+ forks ride with the host file, so there is no separate
// container to coordinate on a rename/delete.
func (e *hfsForkEngine) MetadataPaths(storePath string) []string { _ = storePath; return nil }

// hostPathOf resolves the host path for a store path (used by the Finder-info code),
// or ok=false.
func (e *hfsForkEngine) hostPathOf(storePath string) (string, bool) {
	return e.host.HostPath(storePath)
}
