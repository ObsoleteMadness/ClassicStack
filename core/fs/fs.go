package fs

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/metastore"
)

// File is a per-open-handle. Implementations must not retain p past Write/WriteAt.
type File interface {
	ReadAt(p []byte, off int64) (int, error)
	WriteAt(p []byte, off int64) (int, error)
	Truncate(size int64) error
	Stat() (fs.FileInfo, error)
	Sync() error
	Close() error
}

// FileSystem is the cross-service backend contract.
type FileSystem interface {
	ReadDir(path string) ([]fs.DirEntry, error)
	Stat(path string) (fs.FileInfo, error)
	DiskUsage(path string) (total, free uint64, err error)
	CreateDir(path string) error
	CreateFile(path string) (File, error)
	OpenFile(path string, flag int) (File, error)
	Remove(path string) error
	Rename(old, new string) error
	ShortName(path string) (string, error)
	MediumName(path string) (string, error)
	Capabilities() Capabilities
}

type Capabilities struct {
	CatSearch, ChildCount, ReadDirRange, DirAttributes, ReadOnly bool
}

type ForkType uint8

const (
	DataFork ForkType = iota
	ResourceFork
)

type ForkEngine interface {
	OpenFork(path string, fork ForkType, flag int) (File, error)
	ForkLen(path string, fork ForkType) (int64, error)
	ReadFinderInfo(path string) (info [32]byte, ok bool, err error)
	WriteFinderInfo(path string, info [32]byte) error
	ReadComment(path string) (c []byte, ok bool)
	WriteComment(path string, c []byte) error
	MoveMetadata(old, new string) error
	DeleteMetadata(path string) error
}

type ForkFS interface {
	FileSystem
	ForkEngine
}

// Coded is implemented by a built share that carries a FilenameCodec, so a file
// service can thread its per-request wire charset (§2a) through Decode/Encode
// without reaching past the FileSystem interface. BuildShare's result satisfies
// it; type-assert the ForkFS to reach the codec.
type Coded interface {
	Codec() FilenameCodec
}

type NameKind uint8

const (
	ShortName NameKind = iota
	MediumName
)

type NameEngine interface {
	Bind(dir, long string, kind NameKind) string
	ToLong(dir, derived string, kind NameKind) (string, bool)
}

type StoredName []byte

// WireEncoding identifies the charset of filename bytes on the client wire.
type WireEncoding uint8

const (
	WireMacRoman WireEncoding = iota
	WireUTF8
	WireANSI
	WireUTF16
)

func (e WireEncoding) String() string {
	switch e {
	case WireMacRoman:
		return "macroman"
	case WireUTF8:
		return "utf8"
	case WireANSI:
		return "ansi"
	case WireUTF16:
		return "utf16"
	default:
		return "unknown"
	}
}

// FilenameCodec converts a filename element between a client wire charset and the
// share's store-native bytes. It is reversible: Encode(Decode(wire, c), c) == wire
// for every charset c in Wire(). See codec.go for the implementations.
type FilenameCodec interface {
	Decode(wire []byte, src WireEncoding) (StoredName, error)
	Encode(stored StoredName, dst WireEncoding) (wire []byte, err error)
	Wire() []WireEncoding
	Profile() FilenameProfile
}

// FilenameProfile describes what a codec advertises: the wire charsets it
// implements, the store charset name, an optional max element length, the
// backend-declared reserved-character set, and a final element validator.
type FilenameProfile struct {
	Wire         []WireEncoding
	StoreCharset string
	MaxElement   int
	Reserved     ReservedSet
	Validate     func(elem StoredName) error
}

var (
	ErrUnrepresentable = errors.New("fs: filename not representable in store charset")
	ErrWireUnsupported = errors.New("fs: wire encoding not supported by codec")
)

type ShareSpec struct {
	Name          string
	FSType        string
	ForkBackend   string
	FilenameCodec string
	NameEngine    string
	Metastore     string
	// Path is the near-universal backend location: the host directory for
	// local_fs, the image file for hfs-image/fat-image, the archive for zipfs.
	// Synthetic backends (memfs, macgarden) leave it empty.
	Path     string
	ReadOnly bool
	// Extra carries backend-specific params a given fs_type documents and reads
	// (e.g. ftp: "url"/"username"/"password"; hfs-image: "partition"). It is a
	// plain carrier — never reflection-marshalled in core.
	Extra map[string]any
}

// Param declares one config key a FileSystem factory consumes. The set for an
// fs_type is registered alongside its Factory (RegisterFSWithParams) and read back
// via ParamsFor, so BuildShare can validate required keys before constructing the
// backend and a UI can render a per-share form. Secret keys (passwords) are masked
// in the UI and redacted in logs/diagnostics.
type Param struct {
	Key      string
	Required bool
	Secret   bool
	Doc      string
}

// PathKey is the reserved Param key naming the typed ShareSpec.Path field, so a
// factory can mark its location param required (and the UI render it) without it
// living in Extra.
const PathKey = "path"

type Factory func(ShareSpec, bus.Bus, metastore.Store) (FileSystem, error)

type registeredFS struct {
	factory Factory
	params  []Param
}

var (
	fsFactoryMu sync.RWMutex
	fsFactories = map[string]registeredFS{}
)

// RegisterFS registers a FileSystem factory with no declared params (backends that
// need no config, or whose validation is internal). Most real backends should use
// RegisterFSWithParams so BuildShare can validate their required config.
func RegisterFS(fsType string, f Factory) {
	RegisterFSWithParams(fsType, f)
}

// RegisterFSWithParams registers a factory plus the config-param schema BuildShare
// validates and ParamsFor exposes.
func RegisterFSWithParams(fsType string, f Factory, params ...Param) {
	fsFactoryMu.Lock()
	defer fsFactoryMu.Unlock()
	fsFactories[strings.ToLower(fsType)] = registeredFS{factory: f, params: params}
}

// ParamsFor returns the declared param schema for an fs_type (nil if the type is
// unknown or declares none). The UI/config layer renders a per-share form from it.
func ParamsFor(fsType string) []Param {
	fsFactoryMu.RLock()
	defer fsFactoryMu.RUnlock()
	return fsFactories[strings.ToLower(fsType)].params
}

func lookupFactory(fsType string) (Factory, bool) {
	fsFactoryMu.RLock()
	defer fsFactoryMu.RUnlock()
	r, ok := fsFactories[strings.ToLower(fsType)]
	return r.factory, ok
}

// BuildShare assembles one per-share stack and validates key compatibility pairs.
func BuildShare(spec ShareSpec, b bus.Bus) (ForkFS, error) {
	spec = withDefaults(spec)
	if err := validateShareSpec(spec); err != nil {
		return nil, err
	}
	if err := validateParams(spec); err != nil {
		return nil, err
	}

	if b == nil {
		b = NewBus(0)
	}

	store, err := metastore.Open(spec.Metastore, "")
	if err != nil {
		return nil, err
	}

	f, ok := lookupFactory(spec.FSType)
	if !ok {
		return nil, errors.New("fs: unknown fs type")
	}
	base, err := f(spec, b, store)
	if err != nil {
		return nil, err
	}

	codec, err := codecByName(spec.FilenameCodec)
	if err != nil {
		return nil, err
	}
	nameEngine, err := nameEngineByName(spec.NameEngine, store)
	if err != nil {
		return nil, err
	}
	forkEngine, err := forkEngineByName(spec.ForkBackend, base)
	if err != nil {
		return nil, err
	}

	return &shareFS{FileSystem: base, ForkEngine: forkEngine, codec: codec, names: nameEngine}, nil
}

func withDefaults(spec ShareSpec) ShareSpec {
	if spec.FSType == "" {
		spec.FSType = "memfs"
	}
	if spec.ForkBackend == "" {
		spec.ForkBackend = "appledouble"
	}
	if spec.FilenameCodec == "" {
		spec.FilenameCodec = "identity"
	}
	if spec.NameEngine == "" {
		spec.NameEngine = "passthrough"
	}
	if spec.Metastore == "" {
		spec.Metastore = "mem"
	}
	return spec
}

// validateShareSpec checks the fs_type × fork_backend × filename_codec triple is
// a buildable combination before any component is constructed, so a bad share
// config fails loudly at build time rather than mangling names at runtime.
func validateShareSpec(spec ShareSpec) error {
	fsType := strings.ToLower(spec.FSType)
	codecName := strings.ToLower(spec.FilenameCodec)
	fork := strings.ToLower(spec.ForkBackend)

	// The codec name must resolve, and its declared store charset must suit the
	// fs type's on-disk charset.
	codec, err := codecByName(spec.FilenameCodec)
	if err != nil {
		return err
	}
	storeCharset := codec.Profile().StoreCharset

	// An HFS image stores MacRoman bytes natively; a UTF-8 store charset would
	// double-encode names on disk.
	if fsType == "hfs-image" && storeCharset != "macroman" {
		return errors.New("fs: hfs-image requires a macroman-native filename codec")
	}
	// A read-only zip volume cannot host native/xattr/ads forks (nothing can be
	// written), so resource forks must come from AppleDouble sidecars baked into
	// the archive.
	if fsType == "zipfs" && spec.ReadOnly && fork != "appledouble" {
		return errors.New("fs: read-only zipfs requires appledouble fork backend")
	}
	// A native-charset codec only advertises MacRoman; pairing it with a backend
	// that needs UTF-8/Unicode wire names (SMB) would fail every NT request, so
	// reject the combination up front.
	if codecName == "macroman-native" && fork == "xattr" {
		return errors.New("fs: macroman-native codec is incompatible with the xattr fork backend")
	}
	return nil
}

// validateParams checks that every Required param the fs_type declares is present
// (in the typed Path field for PathKey, or in Extra otherwise), so an
// under-specified share — e.g. an ftp backend with no url — fails loudly on Apply
// rather than at first request. Backends that declare no schema are unconstrained.
func validateParams(spec ShareSpec) error {
	for _, p := range ParamsFor(spec.FSType) {
		if !p.Required {
			continue
		}
		if p.Key == PathKey {
			if strings.TrimSpace(spec.Path) == "" {
				return errors.New("fs: " + spec.FSType + " share requires a path")
			}
			continue
		}
		if v, ok := spec.Extra[p.Key]; !ok || isEmptyParam(v) {
			return errors.New("fs: " + spec.FSType + " share requires param " + p.Key)
		}
	}
	return nil
}

// isEmptyParam reports whether a param value is effectively unset (nil, or a blank
// string after trimming). Non-string params are taken as present once non-nil.
func isEmptyParam(v any) bool {
	if v == nil {
		return true
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s) == ""
	}
	return false
}

type shareFS struct {
	FileSystem
	ForkEngine
	codec FilenameCodec
	names NameEngine
}

// Rename moves a path and carries its metadata container in one call: the data
// fork via the FileSystem, then the sidecar/ADS/xattr via the ForkEngine. Callers
// above the FS therefore never pair Rename with MoveMetadata by hand (§9).
func (s *shareFS) Rename(old, new string) error {
	if err := s.FileSystem.Rename(old, new); err != nil {
		return err
	}
	return s.ForkEngine.MoveMetadata(old, new)
}

// Remove deletes a path and its metadata container in one call, metadata first so
// a failure leaves the data fork in place to retry against (§9).
func (s *shareFS) Remove(path string) error {
	if err := s.ForkEngine.DeleteMetadata(path); err != nil {
		return err
	}
	return s.FileSystem.Remove(path)
}

// ShortName and MediumName derive a per-directory short/medium name for the
// final path element via the share's NameEngine.
func (s *shareFS) ShortName(path string) (string, error) {
	dir, base := splitPath(path)
	return s.names.Bind(dir, base, ShortName), nil
}

func (s *shareFS) MediumName(path string) (string, error) {
	dir, base := splitPath(path)
	return s.names.Bind(dir, base, MediumName), nil
}

// Codec exposes the share codec for adapter wiring/tests.
func (s *shareFS) Codec() FilenameCodec { return s.codec }

// CatSearch forwards the optional catalog-search capability to the base
// FileSystem when it implements CatSearcher, so the wrapping of the share stack
// does not hide a backend's search support. A base that does not implement
// CatSearcher leaves shareFS without the method, so a CatSearcher type-assertion
// on the built share fails — the file service then reports "not supported", which
// is the correct answer for a backend that declines CatSearch.
func (s *shareFS) CatSearch(crit CatSearchCriteria, cursor CatSearchCursor) ([]CatSearchResult, CatSearchCursor, error) {
	cs, ok := s.FileSystem.(CatSearcher)
	if !ok {
		return nil, nil, ErrCatSearchUnsupported
	}
	return cs.CatSearch(crit, cursor)
}

// NewNullForkEngine returns a metadata no-op fork implementation for placeholder shares.
func NewNullForkEngine() ForkEngine { return nullForkEngine{} }

type nullForkEngine struct{}

func (nullForkEngine) OpenFork(path string, fork ForkType, flag int) (File, error) {
	_ = path
	_ = fork
	_ = flag
	return nil, fs.ErrNotExist
}

func (nullForkEngine) ForkLen(path string, fork ForkType) (int64, error) {
	_ = path
	_ = fork
	return 0, nil
}

func (nullForkEngine) ReadFinderInfo(path string) (info [32]byte, ok bool, err error) {
	_ = path
	return [32]byte{}, false, nil
}

func (nullForkEngine) WriteFinderInfo(path string, info [32]byte) error {
	_ = path
	_ = info
	return nil
}

func (nullForkEngine) ReadComment(path string) (c []byte, ok bool) {
	_ = path
	return nil, false
}

func (nullForkEngine) WriteComment(path string, c []byte) error {
	_ = path
	_ = c
	return nil
}

func (nullForkEngine) MoveMetadata(old, new string) error {
	_ = old
	_ = new
	return nil
}

func (nullForkEngine) DeleteMetadata(path string) error {
	_ = path
	return nil
}

// NewPassthroughNameEngine returns a placeholder name engine that preserves names.
func NewPassthroughNameEngine() NameEngine {
	return passthroughNameEngine{}
}

type passthroughNameEngine struct{}

func (passthroughNameEngine) Bind(dir, long string, kind NameKind) string {
	_ = dir
	_ = kind
	return long
}

func (passthroughNameEngine) ToLong(dir, derived string, kind NameKind) (string, bool) {
	_ = dir
	_ = kind
	return derived, true
}

func nameEngineByName(name string, store metastore.Store) (NameEngine, error) {
	switch strings.ToLower(name) {
	case "passthrough", "":
		return NewPassthroughNameEngine(), nil
	case "short", "medium":
		// One engine serves both kinds; the kind is passed per call. See name.go.
		return NewDerivedNameEngine(store), nil
	default:
		return nil, errors.New("fs: unknown name engine")
	}
}

type memFS struct {
	mu   sync.RWMutex
	data map[string][]byte
	dirs map[string]struct{}
}

func newMemFS(spec ShareSpec) FileSystem {
	m := &memFS{
		data: make(map[string][]byte),
		dirs: map[string]struct{}{"": {}},
	}
	if spec.ReadOnly {
		return &readOnlyFS{inner: m}
	}
	return m
}

type readOnlyFS struct{ inner *memFS }

func (r *readOnlyFS) ReadDir(path string) ([]fs.DirEntry, error) { return r.inner.ReadDir(path) }
func (r *readOnlyFS) Stat(path string) (fs.FileInfo, error)      { return r.inner.Stat(path) }
func (r *readOnlyFS) DiskUsage(path string) (uint64, uint64, error) {
	return r.inner.DiskUsage(path)
}
func (r *readOnlyFS) CreateDir(path string) error          { return fs.ErrPermission }
func (r *readOnlyFS) CreateFile(path string) (File, error) { return nil, fs.ErrPermission }
func (r *readOnlyFS) OpenFile(path string, flag int) (File, error) {
	return r.inner.OpenFile(path, flag)
}
func (r *readOnlyFS) Remove(path string) error               { return fs.ErrPermission }
func (r *readOnlyFS) Rename(old, new string) error           { return fs.ErrPermission }
func (r *readOnlyFS) ShortName(path string) (string, error)  { return r.inner.ShortName(path) }
func (r *readOnlyFS) MediumName(path string) (string, error) { return r.inner.MediumName(path) }
func (r *readOnlyFS) Capabilities() Capabilities {
	c := r.inner.Capabilities()
	c.ReadOnly = true
	return c
}

// CatSearch forwards to the inner memFS so a read-only memfs volume still
// satisfies the search capability it advertises.
func (r *readOnlyFS) CatSearch(crit CatSearchCriteria, cursor CatSearchCursor) ([]CatSearchResult, CatSearchCursor, error) {
	return r.inner.CatSearch(crit, cursor)
}

func (m *memFS) ReadDir(path string) ([]fs.DirEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.dirs[path]; !ok {
		return nil, fs.ErrNotExist
	}
	out := make([]fs.DirEntry, 0)
	prefix := path
	if prefix != "" {
		prefix += "/"
	}
	seen := map[string]struct{}{}
	for p := range m.dirs {
		if !strings.HasPrefix(p, prefix) || p == path {
			continue
		}
		next := strings.TrimPrefix(p, prefix)
		if strings.Contains(next, "/") {
			next = strings.Split(next, "/")[0]
		}
		if _, ok := seen[next]; ok {
			continue
		}
		seen[next] = struct{}{}
		out = append(out, memDirEntry{name: next, dir: true})
	}
	for p, b := range m.data {
		if !strings.HasPrefix(p, prefix) {
			continue
		}
		next := strings.TrimPrefix(p, prefix)
		if strings.Contains(next, "/") {
			next = strings.Split(next, "/")[0]
		}
		if _, ok := seen[next]; ok {
			continue
		}
		seen[next] = struct{}{}
		out = append(out, memDirEntry{name: next, dir: false, size: int64(len(b))})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out, nil
}

func (m *memFS) Stat(path string) (fs.FileInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.dirs[path]; ok {
		return memFileInfo{name: baseName(path), dir: true}, nil
	}
	if b, ok := m.data[path]; ok {
		return memFileInfo{name: baseName(path), size: int64(len(b))}, nil
	}
	return nil, fs.ErrNotExist
}

func (m *memFS) DiskUsage(path string) (total, free uint64, err error) {
	_ = path
	return 0, 0, nil
}

func (m *memFS) CreateDir(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dirs[path] = struct{}{}
	return nil
}

func (m *memFS) CreateFile(path string) (File, error) {
	m.mu.Lock()
	m.data[path] = nil
	m.mu.Unlock()
	return m.OpenFile(path, os.O_RDWR)
}

func (m *memFS) OpenFile(path string, flag int) (File, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.data[path]; !ok {
		if flag&os.O_CREATE == 0 {
			return nil, fs.ErrNotExist
		}
		m.data[path] = nil
	}
	return &memFile{fs: m, path: path}, nil
}

func (m *memFS) Remove(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, path)
	delete(m.dirs, path)
	return nil
}

func (m *memFS) Rename(old, new string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if b, ok := m.data[old]; ok {
		m.data[new] = b
		delete(m.data, old)
		return nil
	}
	if _, ok := m.dirs[old]; ok {
		m.dirs[new] = struct{}{}
		delete(m.dirs, old)
		return nil
	}
	return fs.ErrNotExist
}

func (m *memFS) ShortName(path string) (string, error) { return path, nil }

func (m *memFS) MediumName(path string) (string, error) { return path, nil }

func (m *memFS) Capabilities() Capabilities {
	return Capabilities{ChildCount: true, CatSearch: true}
}

// CatSearch satisfies the optional CatSearcher capability with the default
// predicate tree-walk. memfs is a plain hierarchical store, so the shared
// WalkCatSearch is exactly right; a synthetic backend would implement its own.
func (m *memFS) CatSearch(crit CatSearchCriteria, cursor CatSearchCursor) ([]CatSearchResult, CatSearchCursor, error) {
	return WalkCatSearch(m, crit, cursor)
}

type memFile struct {
	fs     *memFS
	path   string
	closed bool
}

func (f *memFile) ReadAt(p []byte, off int64) (int, error) {
	f.fs.mu.RLock()
	defer f.fs.mu.RUnlock()
	if f.closed {
		return 0, fs.ErrClosed
	}
	b, ok := f.fs.data[f.path]
	if !ok {
		return 0, fs.ErrNotExist
	}
	if off >= int64(len(b)) {
		return 0, io.EOF
	}
	n := copy(p, b[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (f *memFile) WriteAt(p []byte, off int64) (int, error) {
	f.fs.mu.Lock()
	defer f.fs.mu.Unlock()
	if f.closed {
		return 0, fs.ErrClosed
	}
	b := f.fs.data[f.path]
	need := int(off) + len(p)
	if need > len(b) {
		nb := make([]byte, need)
		copy(nb, b)
		b = nb
	}
	copy(b[off:], p)
	f.fs.data[f.path] = b
	return len(p), nil
}

func (f *memFile) Truncate(size int64) error {
	f.fs.mu.Lock()
	defer f.fs.mu.Unlock()
	if f.closed {
		return fs.ErrClosed
	}
	if size < 0 {
		return fs.ErrInvalid
	}
	b := f.fs.data[f.path]
	if int(size) <= len(b) {
		f.fs.data[f.path] = append([]byte(nil), b[:size]...)
		return nil
	}
	nb := make([]byte, size)
	copy(nb, b)
	f.fs.data[f.path] = nb
	return nil
}

func (f *memFile) Stat() (fs.FileInfo, error) {
	f.fs.mu.RLock()
	defer f.fs.mu.RUnlock()
	b, ok := f.fs.data[f.path]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return memFileInfo{name: baseName(f.path), size: int64(len(b))}, nil
}

func (f *memFile) Sync() error { return nil }

func (f *memFile) Close() error {
	f.closed = true
	return nil
}

type memFileInfo struct {
	name string
	size int64
	dir  bool
}

func (m memFileInfo) Name() string { return m.name }
func (m memFileInfo) Size() int64  { return m.size }
func (m memFileInfo) Mode() fs.FileMode {
	if m.dir {
		return fs.ModeDir | 0o755
	}
	return 0o644
}
func (m memFileInfo) ModTime() time.Time { return time.Time{} }
func (m memFileInfo) IsDir() bool        { return m.dir }
func (m memFileInfo) Sys() any           { return nil }

type memDirEntry struct {
	name string
	dir  bool
	size int64
}

func (d memDirEntry) Name() string { return d.name }
func (d memDirEntry) IsDir() bool  { return d.dir }
func (d memDirEntry) Type() fs.FileMode {
	if d.dir {
		return fs.ModeDir
	}
	return 0
}
func (d memDirEntry) Info() (fs.FileInfo, error) {
	return memFileInfo{name: d.name, size: d.size, dir: d.dir}, nil
}

func baseName(path string) string {
	if path == "" {
		return ""
	}
	parts := strings.Split(path, "/")
	return parts[len(parts)-1]
}

func init() {
	RegisterFS("memfs", func(spec ShareSpec, b bus.Bus, store metastore.Store) (FileSystem, error) {
		_ = b
		_ = store
		return newMemFS(spec), nil
	})
}
