package afp

import (
	"io/fs"
	"os"
)

type LocalFileSystem struct{}

func init() {
	RegisterFS(FSTypeLocalFS, func(cfg VolumeConfig) (FileSystem, error) {
		return &LocalFileSystem{}, nil
	})
}

// LocalFileSystem expects already-converted UTF-8 host paths from AFP service logic.

func (l *LocalFileSystem) ReadDir(path string) ([]fs.DirEntry, error) {
	return os.ReadDir(path)
}

func (l *LocalFileSystem) Stat(path string) (fs.FileInfo, error) {
	return os.Stat(path)
}

func (l *LocalFileSystem) DiskUsage(path string) (totalBytes uint64, freeBytes uint64, err error) {
	return diskUsage(path)
}

func (l *LocalFileSystem) CreateDir(path string) error {
	return os.Mkdir(path, 0755)
}

func (l *LocalFileSystem) CreateFile(path string) (File, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (l *LocalFileSystem) OpenFile(path string, flag int) (File, error) {
	f, err := os.OpenFile(path, flag, 0644)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (l *LocalFileSystem) Remove(path string) error {
	return os.Remove(path) // Standard removal (not recursive)
}

func (l *LocalFileSystem) Rename(oldpath, newpath string) error {
	return os.Rename(oldpath, newpath)
}

func (l *LocalFileSystem) Capabilities() FileSystemCapabilities {
	return FileSystemCapabilities{
		ChildCount:    true,
		DirAttributes: true,
		ReadOnlyState: true,
	}
}

func (l *LocalFileSystem) CatSearch(_ string, _ string, _ int32, cursor [16]byte) ([]string, [16]byte, int32) {
	return nil, cursor, ErrCallNotSupported
}

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

func (l *LocalFileSystem) ReadDirRange(path string, startIndex uint16, reqCount uint16) ([]fs.DirEntry, uint16, error) {
	return nil, 0, newNotSupported("ReadDirRange")
}

func (l *LocalFileSystem) DirAttributes(_ string) (uint16, error) {
	return 0, nil
}

func (l *LocalFileSystem) IsReadOnly(_ string) (bool, error) {
	return false, nil
}

func (l *LocalFileSystem) SupportsCatSearch(_ string) (bool, error) {
	return false, nil
}
