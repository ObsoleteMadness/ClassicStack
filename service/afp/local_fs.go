package afp

import (
	"io/fs"
	"os"
)

type LocalFileSystem struct{}

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
