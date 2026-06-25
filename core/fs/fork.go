package fs

import (
	"errors"
	"io"
	stdfs "io/fs"
	"os"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/core/appledouble"
)

// Fork-adapter names for the AppleDouble family. Each sidecar LAYOUT is its own
// registered adapter that inherits the base AppleDouble behaviour and overrides only
// where the sidecar lives (the AppleDouble byte format, core/appledouble, is identical
// across all of them). The plain "appledouble" name aliases the default "._name" layout
// so existing configs keep working.
const (
	ForkAppleDoubleDefault = "appledouble-default" // "._name" beside the file (Netatalk)
	ForkAppleDoubleOSXZip  = "appledouble-osxzip"  // "__MACOSX/dir/._name" (OS-X archives)
	ForkAppleDoubleDir     = "appledouble-dir"     // "dir/.AppleDouble/name" (Netatalk folder)
)

// init registers the fork adapters that live in core/fs's AppleDouble family. Each
// adapter self-registers (rather than living in a switch) so the set of fork backends
// is the set linked into the build — the same registry seam fs backends use. See
// fork_registry.go and spec/16-storage-seam.md §9.
//
//   - The AppleDouble family is one base engine inherited by a per-LAYOUT adapter — the
//     layouts differ only in WHERE the sidecar lives. "appledouble" (+ "auto"/"native")
//     alias "appledouble-default". TODO(phase4): "native" becomes a real per-platform
//     host-fork adapter from the adapter/ ring under a build tag, and stops aliasing.
//   - "nofork" (aliases "null", "none") carries NO metadata: the explicit "this share
//     has no resource forks" adapter, so every share has exactly one adapter and a
//     fork-less share is a deliberate choice, not a silent fallback.
//
// "ads" and "xattr" register themselves from fork_ads.go / fork_xattr.go.
func init() {
	register := func(name string, sidecar func(string) string, aliases ...string) {
		f := func(spec ShareSpec, base FileSystem) (ForkEngine, error) {
			_ = spec
			return newAppleDoubleForkEngine(base, sidecar), nil
		}
		RegisterForkAdapter(name, f)
		for _, a := range aliases {
			RegisterForkAdapter(a, f)
		}
	}
	// "appledouble" / "auto" / "native" all resolve to the default "._name" layout.
	register(ForkAppleDoubleDefault, netatalkSidecarPath, "appledouble", "auto", "native")
	register(ForkAppleDoubleOSXZip, osxZipSidecarPath)
	register(ForkAppleDoubleDir, appleDoubleDirSidecarPath)

	nofork := func(spec ShareSpec, base FileSystem) (ForkEngine, error) {
		_ = spec
		_ = base
		return NewNoForkAdapter(), nil
	}
	RegisterForkAdapter("nofork", nofork)
	RegisterForkAdapter("null", nofork)
	RegisterForkAdapter("none", nofork)
}

// netatalkSidecarPath is the default "._name" sidecar beside each file.
func netatalkSidecarPath(path string) string {
	dir, base := splitPath(path)
	if dir == "" {
		return "._" + base
	}
	return dir + "/._" + base
}

// osxZipSidecarPath is the convention an OS-X-created .zip uses: the AppleDouble sidecar
// for "dir/name" lives at "__MACOSX/dir/._name" (and "__MACOSX/._name" at the root).
func osxZipSidecarPath(path string) string {
	dir, base := splitPath(path)
	if dir == "" {
		return "__MACOSX/._" + base
	}
	return "__MACOSX/" + dir + "/._" + base
}

// appleDoubleDirSidecarPath is the Netatalk ".AppleDouble" folder form: the sidecar for
// "dir/name" lives at "dir/.AppleDouble/name" (no "._" prefix — the folder disambiguates).
func appleDoubleDirSidecarPath(path string) string {
	dir, base := splitPath(path)
	if dir == "" {
		return ".AppleDouble/" + base
	}
	return dir + "/.AppleDouble/" + base
}

// appleDoubleForkEngine is the BASE AppleDouble adapter: it stores resource forks and
// Finder metadata in AppleDouble v2 sidecars read/written through the share's
// FileSystem, round-tripping through the core/appledouble codec. The per-layout adapters
// (appledouble-default / -osxzip / -dir) all use this engine and differ ONLY in the
// sidecar function injected here — the byte format and all fork logic are shared.
type appleDoubleForkEngine struct {
	fs FileSystem
	// sidecar maps a data path to its sidecar's store path; the injected layout.
	sidecar func(path string) string
}

func newAppleDoubleForkEngine(base FileSystem, sidecar func(string) string) *appleDoubleForkEngine {
	if sidecar == nil {
		sidecar = netatalkSidecarPath
	}
	return &appleDoubleForkEngine{fs: base, sidecar: sidecar}
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
	b, err := e.readAll(e.sidecar(path))
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
	return e.writeAll(e.sidecar(path), out)
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
	src := e.sidecar(old)
	if _, err := e.fs.Stat(src); err != nil {
		if errors.Is(err, stdfs.ErrNotExist) {
			return nil // nothing to move
		}
		return err
	}
	return e.fs.Rename(src, e.sidecar(new))
}

func (e *appleDoubleForkEngine) DeleteMetadata(path string) error {
	err := e.fs.Remove(e.sidecar(path))
	if errors.Is(err, stdfs.ErrNotExist) {
		return nil
	}
	return err
}

// MetadataPaths reports the AppleDouble sidecar store path for a data path (the
// fs.ForkContainers capability): the separate container the §10d coordination must
// follow when a peer service renames/removes the same host file. Exactly one path —
// this adapter keeps all its metadata in a single sidecar (whatever layout the variant
// places it at).
func (e *appleDoubleForkEngine) MetadataPaths(storePath string) []string {
	return []string{e.sidecar(storePath)}
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
