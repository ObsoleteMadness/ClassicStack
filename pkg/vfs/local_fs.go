package vfs

import (
	"io/fs"
	"os"
)

// LocalFSName is the registry key for the host-filesystem backend.
const LocalFSName = "local_fs"

// LocalFileSystem is a thin wrapper over the host filesystem. Every
// path it receives must already be a UTF-8 absolute host path; this
// type performs no translation. Services that need translation
// (e.g. AFP MacRoman ↔ host) compose this type rather than
// re-implementing the universal operations.
//
// LocalFileSystem holds no state, so it is safe for concurrent use
// from any number of goroutines.
type LocalFileSystem struct{}

// NewLocalFileSystem constructs an empty LocalFileSystem. Constructed
// instances are equivalent because the type is stateless; the
// constructor exists for API symmetry with future stateful backends.
func NewLocalFileSystem() *LocalFileSystem { return &LocalFileSystem{} }

func init() {
	Register(LocalFSName, func(_ Params) (FileSystem, error) {
		return NewLocalFileSystem(), nil
	})
}

// ReadDir implements FileSystem.
func (l *LocalFileSystem) ReadDir(path string) ([]fs.DirEntry, error) {
	return os.ReadDir(path)
}

// Stat implements FileSystem.
func (l *LocalFileSystem) Stat(path string) (fs.FileInfo, error) {
	return os.Stat(path)
}

// DiskUsage implements FileSystem.
func (l *LocalFileSystem) DiskUsage(path string) (totalBytes uint64, freeBytes uint64, err error) {
	return diskUsage(path)
}

// CreateDir implements FileSystem.
func (l *LocalFileSystem) CreateDir(path string) error {
	return os.Mkdir(path, 0o755)
}

// CreateFile implements FileSystem.
func (l *LocalFileSystem) CreateFile(path string) (File, error) {
	return os.Create(path)
}

// OpenFile implements FileSystem.
func (l *LocalFileSystem) OpenFile(path string, flag int) (File, error) {
	return os.OpenFile(path, flag, 0o644)
}

// Remove implements FileSystem. It removes a single entry only and
// does not recurse — callers that need recursive removal compose it.
func (l *LocalFileSystem) Remove(path string) error {
	return os.Remove(path)
}

// Rename implements FileSystem.
func (l *LocalFileSystem) Rename(oldpath, newpath string) error {
	return os.Rename(oldpath, newpath)
}

// Capabilities implements FileSystem. The local backend exposes
// child-count, directory-attributes, and read-only-state as cheap
// follow-up syscalls; richer optional behaviors (e.g. AFP CatSearch)
// live on the consuming service's wrapper.
func (l *LocalFileSystem) Capabilities() Capabilities {
	return Capabilities{
		ChildCount:    true,
		DirAttributes: true,
		ReadOnlyState: true,
	}
}
