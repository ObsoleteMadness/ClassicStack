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

// ForkContainers is an OPTIONAL capability a fork adapter implements to report the
// store-relative paths whose rename/remove must accompany the data fork's — i.e. its
// SEPARATE metadata containers (AppleDouble sidecars, an AppleSingle file). It is the
// seam the §10d same-host-path coordination uses: when one service renames/removes a
// file on a host path another service also shares, the peer consults MetadataPaths to
// know which container files moved alongside the data (so it can re-stat them and
// re-derive shortnames) without reaching into the other adapter's layout knowledge.
//
// An adapter whose metadata RIDES WITH the file — ads (NTFS streams), xattr (extended
// attributes), nofork (none), native (host fork) — returns nil: there is no separate
// container path to coordinate. The AppleDouble family returns its sidecar path. The
// share stack (shareFS) forwards this through to the fork adapter; a fork adapter that
// does not implement it is treated as "no separate containers" (nil).
type ForkContainers interface {
	MetadataPaths(storePath string) []string
}

// ForkFS is a base FileSystem paired with its mandatory fork adapter (ForkEngine).
// BuildShare always assembles exactly one fork adapter over the fork-unaware base —
// resolved by name through the fork-adapter registry (fork_registry.go), defaulting to
// "appledouble" and selectable to "nofork" when a share carries no resource forks.
// There is no FS-without-an-adapter path: a fork-less share uses the explicit "nofork"
// adapter, never a silent fallback.
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

// HostPather is implemented by a FileSystem backed by a real host directory tree
// (local_fs): it maps a '/'-separated, share-relative store path to its absolute
// host path. The DOS-attribute / shortname interop backends (Windows-native
// passthrough, Samba user.DOSATTRIB xattr) need the host path to reach the file
// with an OS syscall; a backend whose FileSystem does NOT implement HostPather
// (memfs, zipfs, a synthetic store) cannot use those interop backends and falls
// back to the metastore/sidecar, which need no host path. ok is false when the
// path cannot be resolved (e.g. it escapes the root). The share stack forwards
// this through to the base FileSystem.
type HostPather interface {
	HostPath(storePath string) (hostPath string, ok bool)
}

// FSCloser is an OPTIONAL capability a FileSystem backend implements when it owns a
// resource that GC cannot reclaim on its own — a long-lived OS handle, a background
// goroutine, a network session. It is NOT part of the FileSystem interface (most
// backends own nothing and need no teardown), so a backend opts in by defining
// Close; the share stack forwards it (shareFS.Close → base.Close) and the file
// services call it at DEFINITIVE teardown only — service Stop, when no session can
// still hold the share. It is deliberately NOT called from RemoveShare/UpdateShare,
// which keep the in-flight contract (a session holding the displaced share rides it
// out; its FS is reclaimed by GC when the last reference drops) — closing there would
// pull the FS out from under a live handle. Close must be idempotent and safe to call
// on a backend with no open handles; a backend that owns nothing simply omits it.
// macgarden (background scraper goroutine) and zipfs (per-handle archive fds, plus a
// best-effort flush) implement it; local_fs/memfs do not.
type FSCloser interface {
	Close() error
}

// CloseFS closes a FileSystem if it implements the optional FSCloser, else it is a
// no-op returning nil. The file services call this at service Stop to release a
// backend's GC-invisible resources; a plain backend needs nothing.
func CloseFS(f FileSystem) error {
	if c, ok := f.(FSCloser); ok {
		return c.Close()
	}
	return nil
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
	// DOSAttrBackend selects how DOS file attributes (RO/HID/SYS/ARCH + create-time)
	// that the host filesystem cannot represent are persisted: "auto" (default —
	// native passthrough on Windows, Samba user.DOSATTRIB xattr where the host
	// supports it, else a sidecar, always cached in the metastore), "metastore"
	// (definitive store only, host-independent), "sidecar" (works on every
	// filesystem), "native" (Windows host attributes), or "xattr" (Samba-compatible
	// user.DOSATTRIB). Empty == auto. Backends needing host syscalls are gated by
	// build/GOOS; an unavailable backend degrades to sidecar/metastore.
	DOSAttrBackend string
	// Path is the near-universal backend location: the host directory for
	// local_fs, the image file for hfs-image/fat-image, the archive for zipfs.
	// Synthetic backends (memfs, macgarden) leave it empty.
	Path     string
	ReadOnly bool
	// AllowedUsers is the share's access allow-list (protocol-layer policy, NOT a
	// backend param): the usernames permitted to see/bind the share. Empty means
	// guest/anonymous access. It is not secret and is consumed by no FS backend —
	// core/share lifts it into Permissions; the file services enforce it at login
	// enumeration and tree-connect/OpenVol. Matching is case-insensitive.
	AllowedUsers []string
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

// SpecConstraints is the resolved view of the rest of a share's stack that a backend
// validator inspects to accept or reject a combination. It carries the (already
// defaulted) ShareSpec, the resolved filename-codec profile (StoreCharset etc.), and
// the lower-cased fork-backend name — so a factory can express its own
// fs_type×codec / fs_type×fork rules WITHOUT the core knowing the rule. BuildShare
// assembles this and calls the registered Validator before constructing the backend.
type SpecConstraints struct {
	Spec         ShareSpec
	CodecProfile FilenameProfile
	ForkBackend  string // lower-cased; "" defaults already applied
}

// Validator is a backend's optional self-validation hook: it rejects an unbuildable
// combination of its own fs_type with the chosen codec/fork (e.g. hfs-image requires a
// macroman store charset; read-only zipfs requires appledouble forks). Returning an
// error fails the share build loudly at config time. A backend with no such constraint
// registers none. This inverts the dependency: the core no longer hardcodes any
// plugin's name — each plugin declares its own rules (Open-Closed).
type Validator func(SpecConstraints) error

type registeredFS struct {
	factory  Factory
	params   []Param
	validate Validator
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
// validates and ParamsFor exposes. The factory declares no cross-component constraint;
// use RegisterFSWithValidator when the backend must reject certain codec/fork pairings.
func RegisterFSWithParams(fsType string, f Factory, params ...Param) {
	RegisterFSWithValidator(fsType, f, nil, params...)
}

// RegisterFSWithValidator registers a factory plus an optional Validator (its
// self-declared fs_type×codec / fs_type×fork constraints) and the param schema. The
// Validator keeps the core free of hardcoded plugin names: BuildShare calls it for the
// share's fs_type instead of branching on the type string itself. A nil validator means
// the backend imposes no cross-component constraint.
func RegisterFSWithValidator(fsType string, f Factory, v Validator, params ...Param) {
	fsFactoryMu.Lock()
	defer fsFactoryMu.Unlock()
	fsFactories[strings.ToLower(fsType)] = registeredFS{factory: f, params: params, validate: v}
}

// validatorFor returns the registered Validator for an fs_type, or nil when the type
// is unknown or declares none.
func validatorFor(fsType string) Validator {
	fsFactoryMu.RLock()
	defer fsFactoryMu.RUnlock()
	return fsFactories[strings.ToLower(fsType)].validate
}

// ParamsFor returns the declared param schema for an fs_type (nil if the type is
// unknown or declares none). The UI/config layer renders a per-share form from it.
func ParamsFor(fsType string) []Param {
	fsFactoryMu.RLock()
	defer fsFactoryMu.RUnlock()
	return fsFactories[strings.ToLower(fsType)].params
}

// Types returns the registered fs_type names, sorted for deterministic order. The
// control plane's ListFSTypes surfaces this so a UI can populate an fs-type dropdown
// and then fetch each type's ParamsFor schema to render its per-share form.
func Types() []string {
	fsFactoryMu.RLock()
	out := make([]string, 0, len(fsFactories))
	for t := range fsFactories {
		out = append(out, t)
	}
	fsFactoryMu.RUnlock()
	sort.Strings(out)
	return out
}

// secretKeys returns the set of option keys an fs_type marks fs.Param.Secret
// (lower-cased for case-insensitive matching), or nil if the type declares none.
func secretKeys(fsType string) map[string]bool {
	var out map[string]bool
	for _, p := range ParamsFor(fsType) {
		if p.Secret {
			if out == nil {
				out = make(map[string]bool)
			}
			out[strings.ToLower(p.Key)] = true
		}
	}
	return out
}

// MaskSecretOptions returns a copy of an "key=value" option list (the codec-friendly
// carrier for ShareSpec.Extra) in which the value of every key the fs_type marks
// Secret is replaced by sentinel. An empty value is left empty (so "unset" stays
// distinguishable from "hidden"); a non-secret key is copied verbatim. When the type
// declares no secret params the input is returned copied but unchanged. This is the
// AFP-volume / SMB-share SecretMasker.MaskedClone helper.
func MaskSecretOptions(fsType string, options []string, sentinel string) []string {
	secrets := secretKeys(fsType)
	out := make([]string, len(options))
	for i, opt := range options {
		k, v, hasEq := strings.Cut(opt, "=")
		if !hasEq || !secrets[strings.ToLower(strings.TrimSpace(k))] || strings.TrimSpace(v) == "" {
			out[i] = opt
			continue
		}
		out[i] = strings.TrimSpace(k) + "=" + sentinel
	}
	return out
}

// UnmaskSecretOptions returns a copy of an inbound option list in which any secret
// key still holding sentinel is restored from prev (the live stored option list).
// A secret key whose value differs from sentinel is a genuine edit and is kept; a
// sentinel-valued key with no prior value is dropped (cleared) rather than persisting
// the placeholder. Non-secret keys are copied verbatim. This is the inverse of
// MaskSecretOptions and the SecretMasker.Unmask helper.
func UnmaskSecretOptions(fsType string, options, prev []string, sentinel string) []string {
	secrets := secretKeys(fsType)
	if len(secrets) == 0 {
		out := make([]string, len(options))
		copy(out, options)
		return out
	}
	prior := make(map[string]string, len(prev))
	for _, opt := range prev {
		if k, v, ok := strings.Cut(opt, "="); ok {
			prior[strings.ToLower(strings.TrimSpace(k))] = v
		}
	}
	out := make([]string, 0, len(options))
	for _, opt := range options {
		k, v, hasEq := strings.Cut(opt, "=")
		key := strings.ToLower(strings.TrimSpace(k))
		if !hasEq || !secrets[key] || v != sentinel {
			out = append(out, opt)
			continue
		}
		// Sentinel value for a secret key → restore the stored value, or drop the
		// entry entirely when there is nothing to restore (no prior secret set).
		if pv, ok := prior[key]; ok {
			out = append(out, strings.TrimSpace(k)+"="+pv)
		}
	}
	return out
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
	// A fork adapter is MANDATORY: BuildShare always resolves exactly one over the
	// fork-unaware base FS (withDefaults sets "appledouble" when unspecified; "nofork"
	// is the explicit no-forks choice). An unknown name is a hard error.
	forkEngine, err := forkAdapterByName(spec.ForkBackend, spec, base)
	if err != nil {
		return nil, err
	}
	// DOS-attribute store: the per-share backend (auto/native/xattr/sidecar/metastore)
	// over the same metastore, so all four file services persist DOS attributes the
	// host filesystem cannot represent through one swappable seam.
	dosAttrs := buildDOSAttrStore(spec.DOSAttrBackend, base, store)

	return &shareFS{FileSystem: base, ForkEngine: forkEngine, codec: codec, names: nameEngine, dosAttrs: dosAttrs}, nil
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
//
// The per-fs_type rules are NOT hardcoded here: each backend declares its own
// constraints via the Validator it registered (RegisterFSWithValidator), which this
// calls with the resolved codec profile + fork backend. The core therefore needs no
// knowledge of any plugin's name — a new fs_type (iso9660-image, …) carries its own
// rules. Only genuinely cross-component rules that belong to no single backend (a
// codec×fork incompatibility) live here.
func validateShareSpec(spec ShareSpec) error {
	codecName := strings.ToLower(spec.FilenameCodec)
	fork := strings.ToLower(spec.ForkBackend)

	// The codec name must resolve; its profile is handed to the backend validator.
	codec, err := codecByName(spec.FilenameCodec)
	if err != nil {
		return err
	}

	// Delegate the fs_type's own constraints to its registered validator (e.g.
	// hfs-image requires a macroman store charset; read-only zipfs requires
	// appledouble forks). A backend with no constraint registers none.
	if v := validatorFor(spec.FSType); v != nil {
		if err := v(SpecConstraints{Spec: spec, CodecProfile: codec.Profile(), ForkBackend: fork}); err != nil {
			return err
		}
	}

	// Cross-component rule owned by no single backend: a native-charset codec only
	// advertises MacRoman; pairing it with a fork backend that needs UTF-8/Unicode
	// wire names (SMB) would fail every NT request, so reject it up front.
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
	codec    FilenameCodec
	names    NameEngine
	dosAttrs DOSAttrStore
}

// Rename moves a path and carries its metadata container in one call: the data
// fork via the FileSystem, then the container (sidecar/ADS/xattr) via the ForkEngine,
// which OWNS what its containers are and where they live. Callers above the FS
// therefore never pair Rename with MoveMetadata by hand (§9). Data-fork-first so a
// metadata failure leaves the renamed data with a stale-but-present container to retry.
func (s *shareFS) Rename(old, new string) error {
	if err := s.FileSystem.Rename(old, new); err != nil {
		return err
	}
	return s.ForkEngine.MoveMetadata(old, new)
}

// Remove deletes a path and its metadata container in one call, metadata first so
// a failure leaves the data fork in place to retry against (§9). The ForkEngine owns
// which container(s) to drop.
func (s *shareFS) Remove(path string) error {
	if err := s.ForkEngine.DeleteMetadata(path); err != nil {
		return err
	}
	return s.FileSystem.Remove(path)
}

// MetadataPaths forwards the optional fs.ForkContainers capability to the fork adapter:
// the store-relative container paths (sidecars) that accompany a data path, for §10d
// same-host-path coordination. A fork adapter whose metadata rides with the file
// (ads/xattr/nofork) — or that does not implement the capability — yields nil.
func (s *shareFS) MetadataPaths(storePath string) []string {
	if fc, ok := s.ForkEngine.(ForkContainers); ok {
		return fc.MetadataPaths(storePath)
	}
	return nil
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

// DOSAttrs exposes the share's DOS-attribute store (fs.DOSAttred), so a file
// service (SMB/EtherDFS/NCP/AFP) persists and serves DOS attributes the host
// filesystem cannot represent without reaching past the share stack.
func (s *shareFS) DOSAttrs() DOSAttrStore { return s.dosAttrs }

// Names exposes the share's NameEngine (fs.Named), so a file service can map
// between a host (long) name and its derived 8.3 short / 31-char medium name — and
// reverse a derived name a client sent back to the stored host name. This is the
// shortname interface EtherDFS uses for 8.3↔host mapping and AFP/NCP use for
// medium names.
func (s *shareFS) Names() NameEngine { return s.names }

// HostPath forwards fs.HostPather to the base FileSystem when it is host-backed
// (local_fs), so the DOS-attribute / shortname interop backends can resolve a real
// host path through the assembled share stack. A base that is not a HostPather
// leaves shareFS without a usable host path (ok=false), so those backends decline
// and the metastore/sidecar fallback is used instead.
func (s *shareFS) HostPath(storePath string) (string, bool) {
	hp, ok := s.FileSystem.(HostPather)
	if !ok {
		return "", false
	}
	return hp.HostPath(storePath)
}

// Close forwards the optional FSCloser teardown to the base FileSystem, so closing the
// assembled share stack releases a backend's GC-invisible resources (zipfs handles,
// macgarden goroutine). A base that owns nothing (local_fs/memfs) is a no-op. The fork
// engine / DOS-attr store assembled above the base hold no such resources, so only the
// base is closed. shareFS always exposes Close (it satisfies FSCloser), forwarding to
// CloseFS which itself no-ops on a non-closing base.
func (s *shareFS) Close() error {
	return CloseFS(s.FileSystem)
}

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

// NewNoForkAdapter returns the "nofork" adapter: a metadata no-op fork engine that
// carries no resource forks or Finder info. It is the EXPLICIT "this share has no
// forks" choice in the mandatory-adapter model (registered under "nofork"/"null"/
// "none"), so a fork-less share is deliberate rather than a silent fallback. Also used
// for placeholder shares.
func NewNoForkAdapter() ForkEngine { return noForkAdapter{} }

// NewNullForkEngine is the former name of NewNoForkAdapter, kept for callers that
// constructed the no-op engine directly. Prefer NewNoForkAdapter.
//
// Deprecated: use NewNoForkAdapter.
func NewNullForkEngine() ForkEngine { return NewNoForkAdapter() }

type noForkAdapter struct{}

func (noForkAdapter) OpenFork(path string, fork ForkType, flag int) (File, error) {
	_ = path
	_ = fork
	_ = flag
	return nil, fs.ErrNotExist
}

func (noForkAdapter) ForkLen(path string, fork ForkType) (int64, error) {
	_ = path
	_ = fork
	return 0, nil
}

func (noForkAdapter) ReadFinderInfo(path string) (info [32]byte, ok bool, err error) {
	_ = path
	return [32]byte{}, false, nil
}

func (noForkAdapter) WriteFinderInfo(path string, info [32]byte) error {
	_ = path
	_ = info
	return nil
}

func (noForkAdapter) ReadComment(path string) (c []byte, ok bool) {
	_ = path
	return nil, false
}

func (noForkAdapter) WriteComment(path string, c []byte) error {
	_ = path
	_ = c
	return nil
}

func (noForkAdapter) MoveMetadata(old, new string) error {
	_ = old
	_ = new
	return nil
}

func (noForkAdapter) DeleteMetadata(path string) error {
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
	mu       sync.RWMutex
	data     map[string][]byte
	dirs     map[string]struct{}
	readOnly bool
}

// newMemFS builds the in-memory reference backend. Read-only is enforced INSIDE memFS
// (the mutators reject writes, Capabilities reports ReadOnly) rather than by an external
// wrapper — exactly how local_fs and zipfs honour spec.ReadOnly. This is deliberate: a
// wrapper struct that re-lists every method silently drops any optional capability the
// inner FS gains (HostPather, CatSearcher, …) unless the wrapper is hand-updated to
// forward it. Folding the policy into the backend removes that whole class of bug — the
// one concrete FileSystem value carries every capability it implements, read-only or not.
func newMemFS(spec ShareSpec) FileSystem {
	return &memFS{
		data:     make(map[string][]byte),
		dirs:     map[string]struct{}{"": {}},
		readOnly: spec.ReadOnly,
	}
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
	if m.readOnly {
		return fs.ErrPermission
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dirs[path] = struct{}{}
	return nil
}

func (m *memFS) CreateFile(path string) (File, error) {
	if m.readOnly {
		return nil, fs.ErrPermission
	}
	m.mu.Lock()
	m.data[path] = nil
	m.mu.Unlock()
	return m.OpenFile(path, os.O_RDWR)
}

func (m *memFS) OpenFile(path string, flag int) (File, error) {
	// A read-only volume rejects any write/create open; a pure read open is allowed.
	if m.readOnly && flag&(os.O_WRONLY|os.O_RDWR|os.O_APPEND|os.O_TRUNC|os.O_CREATE) != 0 {
		return nil, fs.ErrPermission
	}
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
	if m.readOnly {
		return fs.ErrPermission
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, path)
	delete(m.dirs, path)
	return nil
}

func (m *memFS) Rename(old, new string) error {
	if m.readOnly {
		return fs.ErrPermission
	}
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
	return Capabilities{ChildCount: true, CatSearch: true, ReadOnly: m.readOnly}
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
