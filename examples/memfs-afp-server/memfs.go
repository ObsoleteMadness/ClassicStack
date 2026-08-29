package main

import (
	"fmt"
	"io"
	iofs "io/fs"
	"strings"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/metastore"
)

// memExampleFSType is the fs.FileSystem factory name this example registers
// itself under (fs.RegisterFS) — the same extension point every real backend
// (local_fs, zipfs, hfs-image, …) uses. A VolumeSpec.Share.FSType of this name
// selects it.
const memExampleFSType = "memexample"

// startTime marks server start, so the dynamic "Server Info" file can report
// how long the process has been up.
var startTime = time.Now()

func init() {
	fs.RegisterFS(memExampleFSType, newMemExampleFS)
}

// memExampleFS is a from-scratch, read-only fs.FileSystem implementation: a
// small directory tree hardcoded in Go, plus one file ("Server Info") whose
// contents are generated fresh on every read. It exists to show the minimum
// method set a FileSystem backend needs — see core/fs/fs.go's FileSystem
// interface — rather than to reuse the built-in "memfs" fs_type.
//
// Paths are '/'-separated and root-relative, matching the convention every
// FileSystem implementation in this codebase uses (no leading slash; "" is
// the volume root).
type memExampleFS struct {
	mu    sync.RWMutex
	dirs  map[string]bool   // dir path -> present
	files map[string][]byte // static file path -> content
}

func newMemExampleFS(spec fs.ShareSpec, b bus.Bus, store metastore.Store) (fs.FileSystem, error) {
	_ = spec
	_ = b
	_ = store
	return &memExampleFS{
		dirs: map[string]bool{
			"":          true, // root
			"Documents": true,
		},
		files: map[string][]byte{
			"Documents/Welcome.txt": []byte("Welcome to MemFS, a ClassicStack server-SDK example volume.\r\n" +
				"This file and the tree it lives in are hardcoded Go values — there is no\r\n" +
				"real filesystem underneath. See examples/memfs-afp-server/memfs.go.\r\n"),
		},
	}, nil
}

// dynamicFile is the one path whose content is generated per-open rather than
// stored: reading it twice returns two different byte slices.
const dynamicFile = "Server Info"

func dynamicContent() []byte {
	return []byte(fmt.Sprintf(
		"Hello from ClassicStack.\r\nServer uptime: %s\r\nThis file's contents are generated fresh on every read.\r\n",
		time.Since(startTime).Round(time.Second)))
}

func (m *memExampleFS) ReadDir(path string) ([]iofs.DirEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.dirs[path] {
		return nil, iofs.ErrNotExist
	}
	prefix := path
	if prefix != "" {
		prefix += "/"
	}
	var out []iofs.DirEntry
	seen := map[string]bool{}
	add := func(name string, dir bool, size int64) {
		if !seen[name] {
			seen[name] = true
			out = append(out, memDirEntry{name: name, dir: dir, size: size})
		}
	}
	for d := range m.dirs {
		if d == path || !strings.HasPrefix(d, prefix) {
			continue
		}
		rest := strings.TrimPrefix(d, prefix)
		if i := strings.Index(rest, "/"); i >= 0 {
			rest = rest[:i]
		}
		add(rest, true, 0)
	}
	for f, data := range m.files {
		if !strings.HasPrefix(f, prefix) {
			continue
		}
		rest := strings.TrimPrefix(f, prefix)
		if strings.Contains(rest, "/") {
			continue
		}
		add(rest, false, int64(len(data)))
	}
	if path == "" {
		add(dynamicFile, false, int64(len(dynamicContent())))
	}
	return out, nil
}

func (m *memExampleFS) Stat(path string) (iofs.FileInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	name := baseName(path)
	if m.dirs[path] {
		return memFileInfo{name: name, dir: true}, nil
	}
	if path == dynamicFile {
		return memFileInfo{name: name, size: int64(len(dynamicContent()))}, nil
	}
	if data, ok := m.files[path]; ok {
		return memFileInfo{name: name, size: int64(len(data))}, nil
	}
	return nil, iofs.ErrNotExist
}

func (m *memExampleFS) DiskUsage(path string) (total, free uint64, err error) {
	_ = path
	return 1 << 30, 0, nil // report a fixed 1 GiB, entirely full (read-only)
}

func (m *memExampleFS) CreateDir(path string) error { _ = path; return iofs.ErrPermission }
func (m *memExampleFS) CreateFile(path string) (fs.File, error) {
	_ = path
	return nil, iofs.ErrPermission
}
func (m *memExampleFS) Remove(path string) error     { _ = path; return iofs.ErrPermission }
func (m *memExampleFS) Rename(old, new string) error { _, _ = old, new; return iofs.ErrPermission }

func (m *memExampleFS) OpenFile(path string, flag int) (fs.File, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if path == dynamicFile {
		return &memExampleFile{content: dynamicContent()}, nil
	}
	data, ok := m.files[path]
	if !ok {
		return nil, iofs.ErrNotExist
	}
	_ = flag // read-only volume: every open is effectively read-only
	return &memExampleFile{content: data}, nil
}

func (m *memExampleFS) ShortName(path string) (string, error)  { return path, nil }
func (m *memExampleFS) MediumName(path string) (string, error) { return path, nil }

func (m *memExampleFS) Capabilities() fs.Capabilities {
	return fs.Capabilities{ReadOnly: true, ChildCount: true}
}

// memExampleFile is the fs.File returned for every open path. Reads come from
// an immutable snapshot taken at open time (for the dynamic file, generated
// fresh by OpenFile above); writes are rejected since the volume is read-only.
type memExampleFile struct {
	content []byte
}

func (f *memExampleFile) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(f.content)) {
		return 0, io.EOF
	}
	n := copy(p, f.content[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (f *memExampleFile) WriteAt(p []byte, off int64) (int, error) {
	_, _ = p, off
	return 0, iofs.ErrPermission
}

func (f *memExampleFile) Truncate(size int64) error { _ = size; return iofs.ErrPermission }

func (f *memExampleFile) Stat() (iofs.FileInfo, error) {
	return memFileInfo{size: int64(len(f.content))}, nil
}

func (f *memExampleFile) Sync() error  { return nil }
func (f *memExampleFile) Close() error { return nil }

type memFileInfo struct {
	name string
	size int64
	dir  bool
}

func (i memFileInfo) Name() string       { return i.name }
func (i memFileInfo) Size() int64        { return i.size }
func (i memFileInfo) ModTime() time.Time { return startTime }
func (i memFileInfo) IsDir() bool        { return i.dir }
func (i memFileInfo) Sys() any           { return nil }
func (i memFileInfo) Mode() iofs.FileMode {
	if i.dir {
		return iofs.ModeDir | 0o555
	}
	return 0o444
}

type memDirEntry struct {
	name string
	dir  bool
	size int64
}

func (d memDirEntry) Name() string        { return d.name }
func (d memDirEntry) IsDir() bool         { return d.dir }
func (d memDirEntry) Type() iofs.FileMode { return memFileInfo{dir: d.dir}.Mode().Type() }
func (d memDirEntry) Info() (iofs.FileInfo, error) {
	return memFileInfo{name: d.name, dir: d.dir, size: d.size}, nil
}

func baseName(path string) string {
	if path == "" {
		return ""
	}
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}
