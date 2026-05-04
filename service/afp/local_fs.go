//go:build afp || all

package afp

import (
	"io/fs"
	"os"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/pkg/vfs"
)

// LocalFileSystem is AFP's local-FS adapter. It composes the shared
// pkg/vfs local backend (consumed through the vfs.FileSystem
// interface, not a concrete type, so AFP stays implementation-agnostic)
// and adds the AFP-specific methods that the cross-service VFS
// contract does not carry — ChildCount, CatSearch, IsReadOnly, etc.
//
// Callers must hand it already-translated UTF-8 host paths; path
// translation (MacRoman, decomposed-name escapes) lives in AFP's
// path codec, not here.
//
// The zero value is usable: the backing vfs.FileSystem is created
// lazily on first call. Production code should construct via the
// FSTypeLocalFS factory so the backend is selected from a Params,
// but tests that build &LocalFileSystem{} directly still work.
type LocalFileSystem struct {
	backendOnce sync.Once
	backend     vfs.FileSystem
}

func (l *LocalFileSystem) fs() vfs.FileSystem {
	l.backendOnce.Do(func() {
		if l.backend != nil {
			return
		}
		// The default-constructed backend ignores Params and is
		// stateless, so any error here would indicate a missing
		// registration; panic so the cause is obvious.
		b, err := vfs.New(vfs.LocalFSName, vfs.Params{})
		if err != nil {
			panic("afp: vfs.local_fs not registered: " + err.Error())
		}
		l.backend = b
	})
	return l.backend
}

func init() {
	RegisterFS(FSTypeLocalFS, func(cfg VolumeConfig) (FileSystem, error) {
		base, err := vfs.New(vfs.LocalFSName, vfs.Params{
			Name:     cfg.Name,
			Path:     cfg.Path,
			ReadOnly: cfg.ReadOnly,
		})
		if err != nil {
			return nil, err
		}
		l := &LocalFileSystem{}
		// Pre-populate the backend so fs() never overwrites it.
		l.backendOnce.Do(func() { l.backend = base })
		return l, nil
	})
}

// ReadDir delegates to the backing vfs.FileSystem.
func (l *LocalFileSystem) ReadDir(path string) ([]fs.DirEntry, error) {
	return l.fs().ReadDir(path)
}

// Stat delegates to the backing vfs.FileSystem.
func (l *LocalFileSystem) Stat(path string) (fs.FileInfo, error) {
	return l.fs().Stat(path)
}

// DiskUsage delegates to the backing vfs.FileSystem.
func (l *LocalFileSystem) DiskUsage(path string) (totalBytes uint64, freeBytes uint64, err error) {
	return l.fs().DiskUsage(path)
}

// CreateDir delegates to the backing vfs.FileSystem.
func (l *LocalFileSystem) CreateDir(path string) error {
	return l.fs().CreateDir(path)
}

// CreateFile delegates to the backing vfs.FileSystem.
func (l *LocalFileSystem) CreateFile(path string) (File, error) {
	return l.fs().CreateFile(path)
}

// OpenFile delegates to the backing vfs.FileSystem.
func (l *LocalFileSystem) OpenFile(path string, flag int) (File, error) {
	return l.fs().OpenFile(path, flag)
}

// Remove delegates to the backing vfs.FileSystem.
func (l *LocalFileSystem) Remove(path string) error {
	return l.fs().Remove(path)
}

// Rename delegates to the backing vfs.FileSystem.
func (l *LocalFileSystem) Rename(oldpath, newpath string) error {
	return l.fs().Rename(oldpath, newpath)
}

// Capabilities adds AFP-specific extensions on top of whatever the
// underlying backend already advertises.
func (l *LocalFileSystem) Capabilities() FileSystemCapabilities {
	base := l.fs().Capabilities()
	return FileSystemCapabilities{
		CatSearch:     base.CatSearch,
		ChildCount:    true,
		ReadDirRange:  base.ReadDirRange,
		DirAttributes: true,
		ReadOnlyState: true,
	}
}

// CatSearch is AFP-specific and not provided by the cross-service
// backend; the local adapter declines it.
func (l *LocalFileSystem) CatSearch(_ string, _ string, _ int32, cursor [16]byte) ([]string, [16]byte, int32) {
	return nil, cursor, ErrCallNotSupported
}

// ChildCount counts entries in a directory, capped at 0xFFFF for the
// AFP wire format.
func (l *LocalFileSystem) ChildCount(path string) (uint16, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return 0, err
	}
	if len(entries) > 0xffff {
		return 0xffff, nil
	}
	return uint16(len(entries)), nil
}

// ReadDirRange is unsupported on the local backend; the AFP service
// falls back to ReadDir + paginate.
func (l *LocalFileSystem) ReadDirRange(_ string, _ uint16, _ uint16) ([]fs.DirEntry, uint16, error) {
	return nil, 0, newNotSupported("ReadDirRange")
}

// DirAttributes returns 0 — the local backend has no AFP-native
// directory attribute storage; volumes that need them use AppleDouble.
func (l *LocalFileSystem) DirAttributes(_ string) (uint16, error) {
	return 0, nil
}

// IsReadOnly returns false — the local backend does not enforce a
// read-only state at the path level. AFP volumes that need it set
// the per-volume flag in VolumeConfig.
func (l *LocalFileSystem) IsReadOnly(_ string) (bool, error) {
	return false, nil
}

// SupportsCatSearch matches Capabilities() — false for the local
// backend.
func (l *LocalFileSystem) SupportsCatSearch(_ string) (bool, error) {
	return false, nil
}
