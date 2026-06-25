//go:build forknative

package native

import (
	"errors"
	stdfs "io/fs"
	"os"

	corefs "github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// nativeForkEngine serves the resource fork and Finder info from the HOST file's native
// facilities: on macOS/HFS+ the resource fork is the "<file>/..namedfork/rsrc" stream
// and the Finder info is the com.apple.FinderInfo xattr. The data fork is the plain host
// file, reached through the base FileSystem like the AppleDouble engine. Finder-info
// access is per-OS (finderinfo_*.go); the resource-fork stream path is opened directly
// on the host path, which simply does not exist on a host without native forks (treated
// as "no resource fork").
type nativeForkEngine struct {
	base corefs.FileSystem
	host corefs.HostPather
}

func newNativeForkEngine(base corefs.FileSystem, host corefs.HostPather) *nativeForkEngine {
	return &nativeForkEngine{base: base, host: host}
}

// rsrcStreamPath is the host path of the native resource-fork stream for a store path,
// or ok=false when the store path cannot be resolved to a host path.
func (e *nativeForkEngine) rsrcStreamPath(storePath string) (string, bool) {
	hp, ok := e.host.HostPath(storePath)
	if !ok {
		return "", false
	}
	return hp + "/..namedfork/rsrc", true
}

func (e *nativeForkEngine) OpenFork(path string, fork corefs.ForkType, flag int) (corefs.File, error) {
	if fork == corefs.DataFork {
		// The data fork is the plain host file; defer to the base FileSystem.
		return e.base.OpenFile(path, flag)
	}
	sp, ok := e.rsrcStreamPath(path)
	if !ok {
		return nil, stdfs.ErrNotExist
	}
	f, err := os.OpenFile(sp, flag, 0o644)
	if err != nil {
		if errors.Is(err, stdfs.ErrNotExist) && flag&os.O_CREATE == 0 {
			return nil, stdfs.ErrNotExist
		}
		return nil, err
	}
	return f, nil // *os.File satisfies fs.File (ReadAt/WriteAt/Truncate/Stat/Sync/Close)
}

func (e *nativeForkEngine) ForkLen(path string, fork corefs.ForkType) (int64, error) {
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

// ReadFinderInfo / WriteFinderInfo are per-OS (finderinfo_darwin.go uses the
// com.apple.FinderInfo xattr; finderinfo_other.go reports absent / no-op).

func (e *nativeForkEngine) ReadComment(path string) ([]byte, bool) { _ = path; return nil, false }
func (e *nativeForkEngine) WriteComment(path string, c []byte) error {
	_ = path
	_ = c
	return nil
}

// MoveMetadata / DeleteMetadata are no-ops: the resource fork and Finder info are host
// attributes of the file itself, so the base FileSystem's Rename/Remove of the data path
// carries them automatically.
func (e *nativeForkEngine) MoveMetadata(old, new string) error { _ = old; _ = new; return nil }
func (e *nativeForkEngine) DeleteMetadata(path string) error   { _ = path; return nil }

// MetadataPaths returns nil: native forks ride with the host file, so there is no
// separate container to coordinate on a rename/delete.
func (e *nativeForkEngine) MetadataPaths(storePath string) []string { _ = storePath; return nil }

// hostPathOf resolves the host path for a store path (used by the per-OS Finder-info
// code), or ok=false.
func (e *nativeForkEngine) hostPathOf(storePath string) (string, bool) {
	return e.host.HostPath(storePath)
}
