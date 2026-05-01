//go:build afp || all

package afp

import (
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"sync"
)

// FileSystemFactory constructs a FileSystem from a normalized
// VolumeConfig. Backends register themselves with RegisterFS during
// package init().
type FileSystemFactory func(VolumeConfig) (FileSystem, error)

var (
	fsRegistryMu sync.RWMutex
	fsRegistry   = map[string]FileSystemFactory{}
)

// RegisterFS associates an FSType name with its factory. It is safe to
// call from package init() blocks; a duplicate name panics so missing
// build tags surface immediately rather than silently overriding the
// default backend.
func RegisterFS(name string, f FileSystemFactory) {
	fsRegistryMu.Lock()
	defer fsRegistryMu.Unlock()
	if _, exists := fsRegistry[name]; exists {
		panic(fmt.Sprintf("afp: FileSystem %q already registered", name))
	}
	fsRegistry[name] = f
}

// NewFS dispatches to the factory registered for cfg.FSType. The
// returned error includes the list of registered names when no
// factory matches.
func NewFS(cfg VolumeConfig) (FileSystem, error) {
	fsRegistryMu.RLock()
	f, ok := fsRegistry[cfg.FSType]
	fsRegistryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("afp: no FileSystem registered for fs_type %q (registered: %v)", cfg.FSType, registeredFSNames())
	}
	return f(cfg)
}

func registeredFSNames() []string {
	fsRegistryMu.RLock()
	defer fsRegistryMu.RUnlock()
	return slices.Sorted(maps.Keys(fsRegistry))
}

type FileSystem interface {
	ReadDir(path string) ([]fs.DirEntry, error)
	Stat(path string) (fs.FileInfo, error)
	DiskUsage(path string) (totalBytes uint64, freeBytes uint64, err error)
	CreateDir(path string) error
	CreateFile(path string) (File, error)
	OpenFile(path string, flag int) (File, error)
	Remove(path string) error
	Rename(oldpath, newpath string) error
	Capabilities() FileSystemCapabilities
	CatSearch(volumeRoot string, query string, reqMatches int32, cursor [16]byte) ([]string, [16]byte, int32)
	ChildCount(path string) (uint16, error)
	ReadDirRange(path string, startIndex uint16, reqCount uint16) ([]fs.DirEntry, uint16, error)
	DirAttributes(path string) (uint16, error)
	IsReadOnly(path string) (bool, error)
	SupportsCatSearch(path string) (bool, error)
}

// FileSystemCapabilities describes optional AFP behaviors a FileSystem
// implementation supports.
type FileSystemCapabilities struct {
	CatSearch     bool
	ChildCount    bool
	ReadDirRange  bool
	DirAttributes bool
	ReadOnlyState bool
}

type File interface {
	ReadAt(p []byte, off int64) (n int, err error)
	WriteAt(p []byte, off int64) (n int, err error)
	Truncate(size int64) error
	Close() error
	Stat() (fs.FileInfo, error)
	Sync() error
}
