package fs

import (
	"errors"
	"io"
	stdfs "io/fs"
	"os"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/core/appledouble"
)

// forkEngineByName builds the fork backend named for a share over its base
// FileSystem. "appledouble" is the real engine; "native"/"auto" fall back to it
// (a true native/xattr/ads engine is host-specific and lands per-platform); the
// null engine is available for placeholder shares that carry no metadata.
func forkEngineByName(name string, base FileSystem) (ForkEngine, error) {
	switch strings.ToLower(name) {
	case "appledouble", "auto", "native", "ads", "xattr":
		// ads/xattr/native delegate to the AppleDouble sidecar engine until the
		// host-specific stream/EA backends land (M7 interop). They share the
		// same on-the-wire AfpInfo + resource-fork bytes, only the container
		// differs. See spec/16-storage-seam.md.
		return newAppleDoubleForkEngine(base), nil
	case "null", "none":
		return NewNullForkEngine(), nil
	default:
		return nil, errors.New("fs: unknown fork backend")
	}
}

// appleDoubleForkEngine stores resource forks and Finder metadata in "._name"
// AppleDouble v2 sidecars next to each file, read/written through the share's
// FileSystem. Metadata round-trips through the core/appledouble codec regardless
// of which FileSystem container backs the share.
type appleDoubleForkEngine struct {
	fs FileSystem
}

func newAppleDoubleForkEngine(base FileSystem) *appleDoubleForkEngine {
	return &appleDoubleForkEngine{fs: base}
}

// sidecarPath returns the "._name" sidecar path for a logical file path, using
// '/' separators to match the FileSystem path convention.
func sidecarPath(path string) string {
	dir, base := splitPath(path)
	if dir == "" {
		return "._" + base
	}
	return dir + "/._" + base
}

func splitPath(path string) (dir, base string) {
	i := strings.LastIndexByte(path, '/')
	if i < 0 {
		return "", path
	}
	return path[:i], path[i+1:]
}

// readSidecar reads and parses the sidecar for path, if present.
func (e *appleDoubleForkEngine) readSidecar(path string) (appledouble.Parsed, bool, error) {
	b, err := e.readAll(sidecarPath(path))
	if err != nil {
		if errors.Is(err, stdfs.ErrNotExist) {
			return appledouble.Parsed{}, false, nil
		}
		return appledouble.Parsed{}, false, err
	}
	p, err := appledouble.Parse(b)
	if err != nil {
		return appledouble.Parsed{}, false, err
	}
	return p, true, nil
}

// writeSidecar rebuilds and writes the sidecar for path from p.
func (e *appleDoubleForkEngine) writeSidecar(path string, p appledouble.Parsed) error {
	includeComment := p.HasComment && len(p.Comment) > 0
	var commentLen uint32
	if includeComment {
		commentLen = uint32(len(p.Comment))
	}
	out := appledouble.Build(p, includeComment, commentLen)
	return e.writeAll(sidecarPath(path), out)
}

func (e *appleDoubleForkEngine) readAll(path string) ([]byte, error) {
	f, err := e.fs.OpenFile(path, os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	buf := make([]byte, info.Size())
	if len(buf) == 0 {
		return buf, nil
	}
	n, err := f.ReadAt(buf, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return buf[:n], nil
}

func (e *appleDoubleForkEngine) writeAll(path string, b []byte) error {
	f, err := e.fs.OpenFile(path, os.O_RDWR|os.O_CREATE)
	if err != nil {
		f, err = e.fs.CreateFile(path)
		if err != nil {
			return err
		}
	}
	defer f.Close()
	if err := f.Truncate(0); err != nil {
		return err
	}
	if len(b) == 0 {
		return f.Sync()
	}
	if _, err := f.WriteAt(b, 0); err != nil {
		return err
	}
	return f.Sync()
}

// --- ForkEngine ---

// resourceForkFile is an in-memory view of a sidecar's resource fork that
// flushes back to the sidecar on Close/Sync. AFP resource forks are small
// relative to data forks, so buffering the whole fork keeps the engine simple
// and container-agnostic.
type resourceForkFile struct {
	engine *appleDoubleForkEngine
	path   string
	data   []byte
	dirty  bool
	closed bool
}

func (e *appleDoubleForkEngine) OpenFork(path string, fork ForkType, flag int) (File, error) {
	if fork == DataFork {
		// The data fork is the file itself; defer to the base FileSystem.
		return e.fs.OpenFile(path, flag)
	}
	p, ok, err := e.readSidecar(path)
	if err != nil {
		return nil, err
	}
	if !ok && flag&os.O_CREATE == 0 {
		return nil, stdfs.ErrNotExist
	}
	return &resourceForkFile{engine: e, path: path, data: append([]byte(nil), p.Resource...)}, nil
}

func (e *appleDoubleForkEngine) ForkLen(path string, fork ForkType) (int64, error) {
	if fork == DataFork {
		info, err := e.fs.Stat(path)
		if err != nil {
			return 0, err
		}
		return info.Size(), nil
	}
	p, ok, err := e.readSidecar(path)
	if err != nil || !ok {
		return 0, err
	}
	return int64(len(p.Resource)), nil
}

func (e *appleDoubleForkEngine) ReadFinderInfo(path string) (info [32]byte, ok bool, err error) {
	p, present, err := e.readSidecar(path)
	if err != nil || !present || !p.HasFinder {
		return [32]byte{}, false, err
	}
	return p.FinderInfo, true, nil
}

func (e *appleDoubleForkEngine) WriteFinderInfo(path string, info [32]byte) error {
	p, _, err := e.readSidecar(path)
	if err != nil {
		return err
	}
	p.FinderInfo = info
	p.HasFinder = true
	return e.writeSidecar(path, p)
}

func (e *appleDoubleForkEngine) ReadComment(path string) (c []byte, ok bool) {
	p, present, err := e.readSidecar(path)
	if err != nil || !present || !p.HasComment {
		return nil, false
	}
	return p.Comment, true
}

func (e *appleDoubleForkEngine) WriteComment(path string, c []byte) error {
	p, _, err := e.readSidecar(path)
	if err != nil {
		return err
	}
	p.Comment = append([]byte(nil), c...)
	p.HasComment = len(c) > 0
	return e.writeSidecar(path, p)
}

func (e *appleDoubleForkEngine) MoveMetadata(old, new string) error {
	src := sidecarPath(old)
	if _, err := e.fs.Stat(src); err != nil {
		if errors.Is(err, stdfs.ErrNotExist) {
			return nil // nothing to move
		}
		return err
	}
	return e.fs.Rename(src, sidecarPath(new))
}

func (e *appleDoubleForkEngine) DeleteMetadata(path string) error {
	err := e.fs.Remove(sidecarPath(path))
	if errors.Is(err, stdfs.ErrNotExist) {
		return nil
	}
	return err
}

// --- resourceForkFile (File) ---

func (f *resourceForkFile) ReadAt(p []byte, off int64) (int, error) {
	if f.closed {
		return 0, stdfs.ErrClosed
	}
	if off >= int64(len(f.data)) {
		return 0, io.EOF
	}
	n := copy(p, f.data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (f *resourceForkFile) WriteAt(p []byte, off int64) (int, error) {
	if f.closed {
		return 0, stdfs.ErrClosed
	}
	need := int(off) + len(p)
	if need > len(f.data) {
		nb := make([]byte, need)
		copy(nb, f.data)
		f.data = nb
	}
	copy(f.data[off:], p)
	f.dirty = true
	return len(p), nil
}

func (f *resourceForkFile) Truncate(size int64) error {
	if f.closed {
		return stdfs.ErrClosed
	}
	if size < 0 {
		return stdfs.ErrInvalid
	}
	if int(size) <= len(f.data) {
		f.data = append([]byte(nil), f.data[:size]...)
	} else {
		nb := make([]byte, size)
		copy(nb, f.data)
		f.data = nb
	}
	f.dirty = true
	return nil
}

func (f *resourceForkFile) Stat() (stdfs.FileInfo, error) {
	if f.closed {
		return nil, stdfs.ErrClosed
	}
	_, base := splitPath(f.path)
	return memFileInfo{name: base, size: int64(len(f.data))}, nil
}

func (f *resourceForkFile) Sync() error {
	if !f.dirty {
		return nil
	}
	p, _, err := f.engine.readSidecar(f.path)
	if err != nil {
		return err
	}
	p.Resource = append([]byte(nil), f.data...)
	p.HasResource = len(f.data) > 0
	if err := f.engine.writeSidecar(f.path, p); err != nil {
		return err
	}
	f.dirty = false
	return nil
}

func (f *resourceForkFile) Close() error {
	if f.closed {
		return nil
	}
	err := f.Sync()
	f.closed = true
	return err
}
