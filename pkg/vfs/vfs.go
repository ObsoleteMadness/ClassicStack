// Package vfs is a backend-neutral filesystem abstraction shared
// across ClassicStack file-server services (AFP, SMB, ...).
//
// Today AFP carries its own copy of this surface in service/afp/fs.go;
// the long-term direction is for both AFP and SMB to consume the
// FileSystem interface and FS-factory registry from this package, with
// service-specific extensions (AppleDouble, fork metadata, NTFS streams)
// living in service-local interfaces composed on top.
//
// This package currently provides only the factory registry and the
// minimal FileSystem/File contract needed by stubs of new services.
// The AFP surface is intentionally not collapsed into this package
// yet; that move is mechanical and lands in a follow-up commit so
// it can be reviewed in isolation from the new-protocol work.
package vfs

import (
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"sync"
)

// ErrNotImplemented is returned by stub backends and stub operations
// that have not yet been filled in.
var ErrNotImplemented = errors.New("vfs: not implemented")

// Params is a backend-supplied bag of normalized configuration. Each
// FileSystem factory documents the keys it consumes; the registry is
// agnostic to the schema. Callers (AFP, SMB) translate their own
// service-specific config types to a Params before calling NewFS.
type ShortnameMapper interface {
	Bind(dir, long string) string
	ShortToLong(short string) (string, bool)
}

type Params struct {
	// Name is the human-visible volume / share name.
	Name string
	// Path is the host filesystem root for path-backed backends. May
	// be empty for synthetic backends (e.g. MacGarden).
	Path string
	// ReadOnly hints that writes should be rejected. Backends that
	// cannot enforce this should return an error from RegisterFS-time.
	ReadOnly bool
	// Extra holds backend-specific keys; backends document their schema.
	Extra map[string]any
	// ShortnameMapper is an optional global mapping engine used by
	// backends (like local_fs) to produce deterministic DOS 8.3 short names.
	ShortnameMapper ShortnameMapper
}

// File is the per-open-handle contract any backend must satisfy.
type File interface {
	ReadAt(p []byte, off int64) (n int, err error)
	WriteAt(p []byte, off int64) (n int, err error)
	Truncate(size int64) error
	Close() error
	Stat() (fs.FileInfo, error)
	Sync() error
}

// FileSystem is the minimal cross-service backend contract. Service-
// specific extensions (AFP fork metadata, SMB ADS streams) compose
// this interface in their own packages rather than appearing here.
type FileSystem interface {
	ReadDir(path string) ([]fs.DirEntry, error)
	Stat(path string) (fs.FileInfo, error)
	DiskUsage(path string) (totalBytes uint64, freeBytes uint64, err error)
	CreateDir(path string) error
	CreateFile(path string) (File, error)
	OpenFile(path string, flag int) (File, error)
	Remove(path string) error
	Rename(oldpath, newpath string) error
	ShortName(path string) (string, error)
	Capabilities() Capabilities
}

// Capabilities advertises optional behaviors a backend implements.
type Capabilities struct {
	CatSearch     bool
	ChildCount    bool
	ReadDirRange  bool
	DirAttributes bool
	ReadOnlyState bool
}

// Factory constructs a FileSystem from normalized Params. Backends
// register themselves with Register from package init().
type Factory func(Params) (FileSystem, error)

var (
	registryMu sync.RWMutex
	registry   = map[string]Factory{}
)

// Register associates a backend name with its factory. Duplicate names
// panic so missing/duplicate build tags surface immediately rather
// than silently overriding a default backend.
func Register(name string, f Factory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("vfs: backend %q already registered", name))
	}
	registry[name] = f
}

// New dispatches to the factory registered under name. The returned
// error includes the list of registered backends when no factory matches.
func New(name string, p Params) (FileSystem, error) {
	registryMu.RLock()
	f, ok := registry[name]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("vfs: no backend registered for %q (registered: %v)", name, RegisteredNames())
	}
	return f(p)
}

// RegisteredNames returns a sorted snapshot of the backend names
// currently registered. Useful in error messages and tests.
func RegisteredNames() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
