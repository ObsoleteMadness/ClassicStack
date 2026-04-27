//go:build afp

package afp

import (
	"errors"
	"fmt"
	"io/fs"
	"sort"
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
	out := make([]string, 0, len(fsRegistry))
	for k := range fsRegistry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ForkMetadata contains AFP metadata that may be stored outside the data fork.
type ForkMetadata struct {
	FinderInfo      [32]byte
	ResourceForkLen int64
	HasResourceFork bool
}

// ResourceForkInfo describes where a resource fork lives in backend storage.
type ResourceForkInfo struct {
	Offset            int64
	Length            int64
	LengthFieldOffset int64
}

type AppleDoubleMode string

const (
	AppleDoubleModeModern AppleDoubleMode = "netatalk modern"
	AppleDoubleModeLegacy AppleDoubleMode = "netatalk legacy"
)

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

// ErrCopySourceReadEOF indicates a source read failure during copy that should
// map to AFP ErrEOFErr.
var ErrCopySourceReadEOF = errors.New("copy source read eof")

// NotSupportedError indicates a filesystem operation exists but is not
// supported by a specific backend.
type NotSupportedError struct {
	Operation string
}

func (e *NotSupportedError) Error() string {
	if e == nil || e.Operation == "" {
		return "not supported"
	}
	return fmt.Sprintf("not supported: %s", e.Operation)
}

func newNotSupported(op string) error {
	return &NotSupportedError{Operation: op}
}

func isNotSupported(err error) bool {
	var ns *NotSupportedError
	return errors.As(err, &ns)
}

// ForkMetadataBackend abstracts where AFP metadata and resource forks are stored.
// The default implementation is AppleDoubleBackend, but other backends can map
// to alternate streams, xattrs, or different sidecar layouts.
type ForkMetadataBackend interface {
	StatWithMetadataFallback(path string) (string, fs.FileInfo, error)
	ReadForkMetadata(path string) (ForkMetadata, error)
	WriteFinderInfo(path string, finderInfo [32]byte) error
	OpenResourceFork(path string, writable bool) (File, ResourceForkInfo, error)
	TruncateResourceFork(file File, info ResourceForkInfo, newLen int64) error
	MoveMetadata(oldpath, newpath string) error
	DeleteMetadata(path string) error
	CopyMetadata(srcPath, dstPath string) error
	CopyMetadataFrom(source ForkMetadataBackend, srcPath, dstPath string) error
	ExchangeMetadata(pathA, pathB string) error
	IsMetadataArtifact(name string, isDir bool) bool

	// MetadataPath returns the AppleDouble sidecar path for a host file path.
	MetadataPath(path string) string

	// IconFileName returns the host filesystem name for the Mac "Icon\r" file,
	// accounting for decomposed filenames and AppleDouble mode.
	// In legacy mode this is "Icon_"; otherwise "Icon0x0D" (decomposed) or
	// "Icon\r" (literal).
	IconFileName() string
}

// CommentBackend can read/write/delete Finder comments stored in sidecar metadata.
type CommentBackend interface {
	ReadComment(path string) ([]byte, bool)
	WriteComment(path string, comment []byte) error
	RemoveComment(path string) error
}

type File interface {
	ReadAt(p []byte, off int64) (n int, err error)
	WriteAt(p []byte, off int64) (n int, err error)
	Truncate(size int64) error
	Close() error
	Stat() (fs.FileInfo, error)
	Sync() error
}
