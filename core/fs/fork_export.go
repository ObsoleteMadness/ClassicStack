package fs

import (
	"errors"
	"io"
	stdfs "io/fs"
	"os"
	"strings"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/appledouble"
	"github.com/ObsoleteMadness/ClassicStack/core/macresources"
)

// fork_export.go implements the CLIENT-MOUNT direction of sidecar fork backends:
// when a remote volume already has NATIVE forks (AFP passthrough) and the user
// selects a sidecar layout (-fork derez / appledouble / …), the mount must
// PROJECT those forks into the Windows namespace as ordinary sidecar files so a
// Windows tool can read/write them — the opposite of the server-hosting case,
// where the same adapters CONSUME sidecars from a local disk to feed OpenFork.
//
// Layering (WrapBase):
//
//	base FileSystem (+ native ForkEngine)  →  sidecarExportFS  →  shareFS
//	                                            ↑ synthesises .rdump/.idump/._name
//	Native ForkEngine stays the share's ForkEngine (passthrough); OpenFork still
//	hits the wire. Only the FileSystem namespace gains the projected sidecars.
//
// See spec/16-storage-seam.md §1 and client/winfsp/doc.go.

// ResourceLenInfo is an optional FileInfo.Sys() capability: the resource-fork
// length already known from a listing/stat bitmap, so a projector can decide
// whether to synthesise a resource sidecar without an extra ForkLen round-trip.
type ResourceLenInfo interface {
	ResourceForkLen() int64
}

// FinderInfoBits is an optional FileInfo.Sys() capability carrying the 32-byte
// Finder info from a listing/stat reply.
type FinderInfoBits interface {
	FinderInfo() (info [32]byte, ok bool)
}

// sidecarExportBackend reports whether name is a sidecar-layout fork backend that
// should PROJECT native forks into the FileSystem namespace when the base already
// implements ForkEngine (client mount over AFP).
func sidecarExportBackend(name string) bool {
	switch strings.ToLower(name) {
	case "derez",
		"appledouble", "appledouble-default", "auto",
		"appledouble-osxzip", "appledouble-dir":
		return true
	default:
		return false
	}
}

// sidecarExportFS wraps a native-fork base so ReadDir/Stat/OpenFile synthesise
// sidecar paths from OpenFork / FinderInfo.
type sidecarExportFS struct {
	FileSystem
	native ForkEngine
	format exportFormat
}

// newSidecarExportFS builds the projector for the named sidecar backend over a
// base that already implements ForkEngine.
func newSidecarExportFS(base FileSystem, native ForkEngine, backend string) FileSystem {
	return &sidecarExportFS{
		FileSystem: base,
		native:     native,
		format:     exportFormatFor(backend),
	}
}

// exportFormat knows how to name and encode/decode one sidecar layout.
type exportFormat interface {
	// sidecarsFor returns the synthesised sidecar basenames for a data-file
	// basename, given whether it has a resource fork / Finder info.
	sidecarsFor(base string, hasRsrc, hasFinder bool) []string
	// match reports whether name is a synthesised sidecar for some data file in
	// the same directory; dataBase is the data-file basename.
	match(name string) (dataBase string, kind exportKind, ok bool)
	// listSize approximates the sidecar's byte length from AFP enumerate hints
	// (FileBitmapRsrcForkLen + Finder info) without OpenFork/materialise.
	listSize(kind exportKind, rsrcLen int64, hasFinder bool) int64
	// materialize builds the sidecar file bytes from the native fork engine.
	materialize(native ForkEngine, dataPath string, kind exportKind) ([]byte, error)
	// apply writes sidecar file bytes back through the native fork engine.
	apply(native ForkEngine, dataPath string, kind exportKind, data []byte) error
}

type exportKind uint8

const (
	exportRdump       exportKind = iota // derez .rdump
	exportIdump                         // derez .idump
	exportAppleDouble                   // AppleDouble ._name (or layout variant)
)

func exportFormatFor(backend string) exportFormat {
	switch strings.ToLower(backend) {
	case "derez":
		return derezExport{}
	case "appledouble-osxzip":
		return appleDoubleExport{sidecar: osxZipSidecarPath}
	case "appledouble-dir":
		return appleDoubleExport{sidecar: appleDoubleDirSidecarPath}
	default: // appledouble / appledouble-default / auto
		return appleDoubleExport{sidecar: netatalkSidecarPath}
	}
}

// --- ReadDir / Stat / OpenFile ---------------------------------------------------------

func (e *sidecarExportFS) ReadDir(path string) ([]stdfs.DirEntry, error) {
	ents, err := e.FileSystem.ReadDir(path)
	if err != nil {
		return nil, err
	}
	out := make([]stdfs.DirEntry, 0, len(ents)*2)
	seen := make(map[string]struct{}, len(ents)*2)
	for _, de := range ents {
		name := de.Name()
		seen[name] = struct{}{}
		out = append(out, de)
		if de.IsDir() {
			continue
		}
		rsrcLen, hasRsrc, hasFinder := e.probe(de, joinExport(path, name))
		for _, sc := range e.format.sidecarsFor(name, hasRsrc, hasFinder) {
			if _, ok := seen[sc]; ok {
				continue // real sidecar already on the volume — don't duplicate
			}
			seen[sc] = struct{}{}
			// Size from enumerate hints (AFP FileBitmapRsrcForkLen). WireMetaComplete
			// on the DirEntry stops WinFsp fillFileInfo calling Meta().Attrs → Stat
			// (which used to materialise every ._name during directory listing).
			_, kind, _ := e.format.match(sc)
			out = append(out, exportDirEntry{info: sidecarFileInfoFromSource(
				sc,
				e.format.listSize(kind, rsrcLen, hasFinder),
				mustInfo(de),
			)})
		}
	}
	return out, nil
}

func (e *sidecarExportFS) Stat(path string) (stdfs.FileInfo, error) {
	if dataPath, kind, ok := e.matchPath(path); ok {
		// Stat returns the approximate sidecar size from enumerate / FPGetFileDirParms
		// hints only. Full AppleDouble bytes are built in openSidecar when Explorer
		// opens or reads the projected path.
		info, err := e.sidecarStatInfo(path, dataPath, kind)
		if err != nil {
			return nil, err
		}
		return info, nil
	}
	return e.FileSystem.Stat(path)
}

// sidecarStatInfo returns the projected sidecar FileInfo from source-path metadata.
// It never opens forks (ForkLen / ReadFinderInfo at most).
func (e *sidecarExportFS) sidecarStatInfo(sidecarPath, dataPath string, kind exportKind) (exportFileInfo, error) {
	src, err := e.FileSystem.Stat(dataPath)
	if err != nil {
		return exportFileInfo{}, err
	}
	rsrcLen, _, hasFinder := e.sidecarHints(dataPath)
	size := e.format.listSize(kind, rsrcLen, hasFinder)
	if size == 0 {
		return exportFileInfo{}, stdfs.ErrNotExist
	}
	_, base := splitPath(sidecarPath)
	return sidecarFileInfoFromSource(base, size, src), nil
}

// sidecarHints returns resource-fork length and Finder-info presence for a data path,
// preferring FileInfo.Sys() from enumerate/stat and falling back to the native engine.
func (e *sidecarExportFS) sidecarHints(dataPath string) (rsrcLen int64, hasRsrc, hasFinder bool) {
	if fi, err := e.FileSystem.Stat(dataPath); err == nil {
		rsrcLen, hasRsrc, hasFinder = hintsFromFileInfo(fi)
		if hasRsrc || hasFinder {
			return rsrcLen, hasRsrc, hasFinder
		}
	}
	if n, err := e.native.ForkLen(dataPath, ResourceFork); err == nil && n > 0 {
		rsrcLen, hasRsrc = n, true
	}
	if info, ok, err := e.native.ReadFinderInfo(dataPath); err == nil && ok && finderTypeCreatorSet(info) {
		hasFinder = true
	}
	return rsrcLen, hasRsrc, hasFinder
}

func (e *sidecarExportFS) OpenFile(path string, flag int) (File, error) {
	if dataPath, kind, ok := e.matchPath(path); ok {
		return e.openSidecar(dataPath, kind, path, flag)
	}
	return e.FileSystem.OpenFile(path, flag)
}

func (e *sidecarExportFS) CreateFile(path string) (File, error) {
	if dataPath, kind, ok := e.matchPath(path); ok {
		return e.openSidecar(dataPath, kind, path, os.O_RDWR|os.O_CREATE|os.O_TRUNC)
	}
	return e.FileSystem.CreateFile(path)
}

func (e *sidecarExportFS) Remove(path string) error {
	if dataPath, kind, ok := e.matchPath(path); ok {
		return e.format.apply(e.native, dataPath, kind, nil)
	}
	return e.FileSystem.Remove(path)
}

func (e *sidecarExportFS) openSidecar(dataPath string, kind exportKind, sidecarPath string, flag int) (File, error) {
	var data []byte
	if flag&os.O_TRUNC == 0 {
		b, err := e.format.materialize(e.native, dataPath, kind)
		if err != nil {
			if flag&os.O_CREATE == 0 {
				return nil, err
			}
			b = nil
		}
		data = append([]byte(nil), b...)
	}
	writable := flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC) != 0
	_, base := splitPath(sidecarPath)
	return &exportSidecarFile{
		engine:   e,
		dataPath: dataPath,
		kind:     kind,
		name:     base,
		data:     data,
		writable: writable,
	}, nil
}

func (e *sidecarExportFS) matchPath(path string) (dataPath string, kind exportKind, ok bool) {
	dir, base := splitPath(path)
	dataBase, kind, ok := e.format.match(base)
	if !ok {
		return "", 0, false
	}
	return joinExport(dir, dataBase), kind, true
}

func joinExport(dir, base string) string {
	if dir == "" {
		return base
	}
	return dir + "/" + base
}

// exportSidecarMeta marks a synthesised sidecar DirEntry/FileInfo as wire-complete
// so directory listing does not re-Stat through Meta().Attrs.
type exportSidecarMeta struct {
	attrs  uint16
	create time.Time
}

func (exportSidecarMeta) WireMetaComplete()          {}
func (m exportSidecarMeta) DOSAttrs() uint16         { return m.attrs }
func (m exportSidecarMeta) DOSCreateTime() time.Time { return m.create }

// exportDirEntry is a projected sidecar name returned from ReadDir.
type exportDirEntry struct {
	info exportFileInfo
}

func (d exportDirEntry) Name() string                  { return d.info.name }
func (d exportDirEntry) IsDir() bool                   { return false }
func (d exportDirEntry) Type() stdfs.FileMode          { return 0 }
func (d exportDirEntry) Info() (stdfs.FileInfo, error) { return d.info, nil }

// exportFileInfo is the FileInfo for a projected sidecar path (ReadDir / Stat).
type exportFileInfo struct {
	name    string
	size    int64
	modTime time.Time
	meta    exportSidecarMeta
}

func (fi exportFileInfo) Name() string         { return fi.name }
func (fi exportFileInfo) Size() int64          { return fi.size }
func (fi exportFileInfo) Mode() stdfs.FileMode { return 0o644 }
func (fi exportFileInfo) ModTime() time.Time   { return fi.modTime }
func (fi exportFileInfo) IsDir() bool          { return false }
func (fi exportFileInfo) Sys() any             { return fi.meta }

func sidecarFileInfoFromSource(name string, size int64, src stdfs.FileInfo) exportFileInfo {
	return exportFileInfo{
		name:    name,
		size:    size,
		modTime: src.ModTime(),
		meta: exportSidecarMeta{
			attrs:  sourceDOSAttrs(src) | DOSHidden,
			create: sourceCreateTime(src),
		},
	}
}

func sourceDOSAttrs(src stdfs.FileInfo) uint16 {
	if sys := src.Sys(); sys != nil {
		if da, ok := sys.(DOSAttrInfo); ok {
			return da.DOSAttrs() & DOSStorableMask
		}
	}
	return 0
}

func sourceCreateTime(src stdfs.FileInfo) time.Time {
	if sys := src.Sys(); sys != nil {
		if ct, ok := sys.(DOSCreateTimeInfo); ok {
			return ct.DOSCreateTime()
		}
	}
	return time.Time{}
}

func mustInfo(de stdfs.DirEntry) stdfs.FileInfo {
	info, err := de.Info()
	if err != nil {
		return memFileInfo{name: de.Name()}
	}
	return info
}

// probeEntry pulls resource-fork length / Finder hints from a DirEntry's Info().Sys()
// (AFP FPEnumerate already returns FileBitmapRsrcForkLen + FinderInfo).
func probeEntry(de stdfs.DirEntry) (rsrcLen int64, hasRsrc, hasFinder bool) {
	info, err := de.Info()
	if err != nil {
		return 0, false, false
	}
	return hintsFromFileInfo(info)
}

func hintsFromFileInfo(info stdfs.FileInfo) (rsrcLen int64, hasRsrc, hasFinder bool) {
	sys := info.Sys()
	if rl, ok := sys.(ResourceLenInfo); ok {
		rsrcLen = rl.ResourceForkLen()
		hasRsrc = rsrcLen > 0
	}
	if fi, ok := sys.(FinderInfoBits); ok {
		if info, present := fi.FinderInfo(); present && finderTypeCreatorSet(info) {
			hasFinder = true
		}
	}
	return rsrcLen, hasRsrc, hasFinder
}

// probe prefers DirEntry.Sys() hints (AFP enumerate already carries rsrc length +
// Finder info). When those are absent — memfs tests, or a native-fork base that
// does not decorate DirEntry — it falls back to ForkLen / ReadFinderInfo.
func (e *sidecarExportFS) probe(de stdfs.DirEntry, dataPath string) (rsrcLen int64, hasRsrc, hasFinder bool) {
	rsrcLen, hasRsrc, hasFinder = probeEntry(de)
	if hasRsrc && hasFinder {
		return rsrcLen, hasRsrc, hasFinder
	}
	sysMissing := true
	if info, err := de.Info(); err == nil && info.Sys() != nil {
		if _, ok := info.Sys().(ResourceLenInfo); ok {
			sysMissing = false
		}
		if _, ok := info.Sys().(FinderInfoBits); ok {
			sysMissing = false
		}
	}
	if !sysMissing {
		return rsrcLen, hasRsrc, hasFinder
	}
	if !hasRsrc {
		if n, err := e.native.ForkLen(dataPath, ResourceFork); err == nil && n > 0 {
			rsrcLen, hasRsrc = n, true
		}
	}
	if !hasFinder {
		if info, ok, err := e.native.ReadFinderInfo(dataPath); err == nil && ok && finderTypeCreatorSet(info) {
			hasFinder = true
		}
	}
	return rsrcLen, hasRsrc, hasFinder
}

func finderTypeCreatorSet(info [32]byte) bool {
	for i := 0; i < 8; i++ {
		if info[i] != 0 {
			return true
		}
	}
	return false
}

// --- exportSidecarFile -----------------------------------------------------------------

// exportSidecarFile is an in-memory view of a projected sidecar that flushes back
// through the native ForkEngine on Close/Sync.
type exportSidecarFile struct {
	engine   *sidecarExportFS
	dataPath string
	kind     exportKind
	name     string
	data     []byte
	dirty    bool
	writable bool
	closed   bool
}

func (f *exportSidecarFile) ReadAt(p []byte, off int64) (int, error) {
	if f.closed {
		return 0, stdfs.ErrClosed
	}
	if off < 0 {
		return 0, stdfs.ErrInvalid
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

func (f *exportSidecarFile) WriteAt(p []byte, off int64) (int, error) {
	if f.closed {
		return 0, stdfs.ErrClosed
	}
	if !f.writable {
		return 0, stdfs.ErrPermission
	}
	if off < 0 {
		return 0, stdfs.ErrInvalid
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

func (f *exportSidecarFile) Truncate(size int64) error {
	if f.closed {
		return stdfs.ErrClosed
	}
	if !f.writable {
		return stdfs.ErrPermission
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

func (f *exportSidecarFile) Stat() (stdfs.FileInfo, error) {
	if f.closed {
		return nil, stdfs.ErrClosed
	}
	return memFileInfo{name: f.name, size: int64(len(f.data))}, nil
}

func (f *exportSidecarFile) Sync() error {
	if !f.dirty || !f.writable {
		return nil
	}
	if err := f.engine.format.apply(f.engine.native, f.dataPath, f.kind, f.data); err != nil {
		return err
	}
	f.dirty = false
	return nil
}

func (f *exportSidecarFile) Close() error {
	if f.closed {
		return nil
	}
	f.closed = true
	return f.Sync()
}

// --- derez export ----------------------------------------------------------------------

type derezExport struct{}

func (derezExport) sidecarsFor(base string, hasRsrc, hasFinder bool) []string {
	var out []string
	if hasRsrc {
		out = append(out, base+derezRdumpExt)
	}
	if hasFinder {
		out = append(out, base+derezIdumpExt)
	}
	return out
}

func (derezExport) match(name string) (dataBase string, kind exportKind, ok bool) {
	if strings.HasSuffix(name, derezRdumpExt) {
		return strings.TrimSuffix(name, derezRdumpExt), exportRdump, true
	}
	if strings.HasSuffix(name, derezIdumpExt) {
		return strings.TrimSuffix(name, derezIdumpExt), exportIdump, true
	}
	return "", 0, false
}

// listSize: .idump is always 8 bytes (type+creator). .rdump is DeRez text whose
// length is not a fixed function of the binary fork, so report 0 and let Stat
// materialise the real size on demand.
func (derezExport) listSize(kind exportKind, _ int64, _ bool) int64 {
	if kind == exportIdump {
		return 8
	}
	return 0
}

func (derezExport) materialize(native ForkEngine, dataPath string, kind exportKind) ([]byte, error) {
	switch kind {
	case exportRdump:
		bin, err := readEntireFork(native, dataPath, ResourceFork)
		if err != nil {
			return nil, err
		}
		if len(bin) == 0 {
			return nil, stdfs.ErrNotExist
		}
		res, err := macresources.ParseResourceFork(bin)
		if err != nil {
			return nil, err
		}
		return macresources.FormatRez(res), nil
	case exportIdump:
		info, ok, err := native.ReadFinderInfo(dataPath)
		if err != nil {
			return nil, err
		}
		if !ok || !finderTypeCreatorSet(info) {
			return nil, stdfs.ErrNotExist
		}
		return append([]byte(nil), info[0:8]...), nil
	default:
		return nil, stdfs.ErrInvalid
	}
}

func (derezExport) apply(native ForkEngine, dataPath string, kind exportKind, data []byte) error {
	switch kind {
	case exportRdump:
		if len(data) == 0 {
			f, err := native.OpenFork(dataPath, ResourceFork, os.O_RDWR|os.O_CREATE|os.O_TRUNC)
			if err != nil {
				return err
			}
			defer func() { _ = f.Close() }()
			return f.Truncate(0)
		}
		res, err := macresources.ParseRez(data)
		if err != nil {
			return err
		}
		bin := macresources.BuildResourceFork(res)
		return writeEntireFork(native, dataPath, ResourceFork, bin)
	case exportIdump:
		info, _, _ := native.ReadFinderInfo(dataPath)
		if len(data) >= 8 {
			copy(info[0:8], data[0:8])
		} else {
			clear(info[0:8])
		}
		return native.WriteFinderInfo(dataPath, info)
	default:
		return stdfs.ErrInvalid
	}
}

// --- AppleDouble export ----------------------------------------------------------------

type appleDoubleExport struct {
	sidecar func(string) string
}

func (a appleDoubleExport) sidecarsFor(base string, hasRsrc, hasFinder bool) []string {
	if !hasRsrc && !hasFinder {
		return nil
	}
	// MetadataPaths-style: sidecar path for "base" in the current directory.
	p := a.sidecar(base)
	_, sc := splitPath(p)
	return []string{sc}
}

func (a appleDoubleExport) match(name string) (dataBase string, kind exportKind, ok bool) {
	// Invert the layout for a single directory entry. Default "._name" → "name".
	// osxzip / .AppleDouble layouts place sidecars in other directories, so a flat
	// directory listing only sees the default form; nested layouts still open by
	// full path via matchPath when the sidecar path is addressed directly.
	if strings.HasPrefix(name, "._") {
		return name[2:], exportAppleDouble, true
	}
	return "", 0, false
}

// listSize approximates a canonical AppleDouble sidecar from the enumerate
// resource-fork length: Build always emits FinderInfo + ResourceFork entries, so
// the file is HeaderSize + 2*EntrySize + 32 + rsrcLen (= ResourceForkStart + rsrcLen).
// Comments are not carried on the AFP client path, so they are omitted from the hint.
func (a appleDoubleExport) listSize(_ exportKind, rsrcLen int64, hasFinder bool) int64 {
	if rsrcLen < 0 {
		rsrcLen = 0
	}
	if rsrcLen == 0 && !hasFinder {
		return 0
	}
	return int64(appledouble.ResourceForkStart) + rsrcLen
}

func (a appleDoubleExport) materialize(native ForkEngine, dataPath string, kind exportKind) ([]byte, error) {
	_ = kind
	var p appledouble.Parsed
	bin, err := readEntireFork(native, dataPath, ResourceFork)
	if err != nil && !errors.Is(err, stdfs.ErrNotExist) {
		return nil, err
	}
	if len(bin) > 0 {
		p.Resource = bin
		p.HasResource = true
	}
	if info, ok, _ := native.ReadFinderInfo(dataPath); ok {
		p.FinderInfo = info
		p.HasFinder = true
	}
	if c, ok := native.ReadComment(dataPath); ok {
		p.Comment = c
		p.HasComment = true
	}
	if !p.HasResource && !p.HasFinder && !p.HasComment {
		return nil, stdfs.ErrNotExist
	}
	includeComment := p.HasComment && len(p.Comment) > 0
	var commentLen uint32
	if includeComment {
		commentLen = uint32(len(p.Comment))
	}
	return appledouble.Build(p, includeComment, commentLen), nil
}

func (a appleDoubleExport) apply(native ForkEngine, dataPath string, kind exportKind, data []byte) error {
	_ = kind
	if len(data) == 0 {
		_ = writeEntireFork(native, dataPath, ResourceFork, nil)
		return native.WriteFinderInfo(dataPath, [32]byte{})
	}
	p, err := appledouble.Parse(data)
	if err != nil {
		return err
	}
	if err := writeEntireFork(native, dataPath, ResourceFork, p.Resource); err != nil {
		return err
	}
	if p.HasFinder {
		if err := native.WriteFinderInfo(dataPath, p.FinderInfo); err != nil {
			return err
		}
	}
	if p.HasComment {
		_ = native.WriteComment(dataPath, p.Comment)
	}
	return nil
}

// --- fork I/O helpers ------------------------------------------------------------------

func readEntireFork(native ForkEngine, path string, fork ForkType) ([]byte, error) {
	f, err := native.OpenFork(path, fork, os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() == 0 {
		return nil, nil
	}
	buf := make([]byte, info.Size())
	n, err := f.ReadAt(buf, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return buf[:n], nil
}

func writeEntireFork(native ForkEngine, path string, fork ForkType, data []byte) error {
	f, err := native.OpenFork(path, fork, os.O_RDWR|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if err := f.Truncate(0); err != nil {
		return err
	}
	if len(data) == 0 {
		return f.Sync()
	}
	if _, err := f.WriteAt(data, 0); err != nil {
		return err
	}
	return f.Sync()
}
