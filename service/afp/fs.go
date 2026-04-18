package afp

import (
	"io/fs"
)

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
