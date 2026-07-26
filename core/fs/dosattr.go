package fs

import (
	"errors"
	"io"
	stdfs "io/fs"
	"os"
	"strings"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/metastore"
)

// DOSAttrStore is the DOS-attribute facade a MetaEngine backend wraps
// internally (meta_store.go/meta_xattr.go/meta_ads.go); file services reach it
// through the built share's Meta().Attrs/SetAttrs/DeleteAttrs/RenameAttrs, not
// this type directly. It is an alias of the metastore facade type so a MetaEngine
// backend imports one name; the concrete backend is selected at BuildShare time
// per the share's meta_backend.
type DOSAttrStore = metastore.DOSAttrStore

// DOSAttr re-exports the metastore value type so a service need not import
// core/metastore just for the attribute struct.
type DOSAttr = metastore.DOSAttr

// DOS attribute bits re-exported for the file services (same values as
// metastore's, which match FILE_ATTRIBUTE_*).
const (
	DOSReadOnly  = metastore.DOSReadOnly
	DOSHidden    = metastore.DOSHidden
	DOSSystem    = metastore.DOSSystem
	DOSVolume    = metastore.DOSVolume
	DOSDirectory = metastore.DOSDirectory
	DOSArchive   = metastore.DOSArchive

	// DOSStorableMask is the subset of attribute bits that are persisted (RO/HID/
	// SYS/ARCH); structural bits (Directory/Volume) are derived from the entry.
	DOSStorableMask = metastore.DOSStorableMask
)

// dosAttrBackend names the DOS-attribute persistence backend buildDOSAttrStore
// selects internally (meta_store.go/meta_xattr.go/meta_ads.go each force one via
// their dosBackend* constant). "auto" picks the best host-interop backend
// available (native on Windows, xattr where the host supports it) and always
// layers the metastore as a cache; the explicit names force one. "metastore" is
// the definitive, host-independent store; "sidecar" works on every filesystem;
// "native"/"xattr" are host-interop backends gated by build/GOOS.
const (
	dosBackendAuto      = "auto"
	dosBackendMetastore = "metastore"
	dosBackendSidecar   = "sidecar"
	dosBackendNative    = "native"
	dosBackendXattr     = "xattr"
)

// hostXattrSetter / hostNativeSetter are the build-gated host-interop backend
// constructors. They are nil in a build that does not compile the corresponding
// backend (no `xattr` tag, or a non-Windows GOOS for native), so the selector
// falls through to sidecar/metastore. A per-OS / build-tagged file assigns them in
// its init(). Each returns a DOSAttrStore that writes through to the host (xattr or
// native attributes) AND caches in the supplied metastore-backed store, or
// (nil,false) when the host path/feature is unavailable for this share.
var (
	hostXattrDOSAttr  func(host HostPather, cache DOSAttrStore) (DOSAttrStore, bool)
	hostNativeDOSAttr func(host HostPather, cache DOSAttrStore) (DOSAttrStore, bool)
)

// buildDOSAttrStore assembles the DOS-attribute store for a share from its
// configured backend over the share's metastore (the definitive cache) and the
// base FileSystem (for host-path resolution / sidecar writes). An unknown backend
// name falls back to "auto". The returned store is never nil. A nil logger gets a
// no-op logger.
func buildDOSAttrStore(backend string, base FileSystem, store metastore.Store, logger log.Logger) DOSAttrStore {
	if logger == nil {
		logger = log.New("dosattr")
	}
	cache := metastore.NewDOSAttrStore(store, logger)
	host, _ := base.(HostPather)

	switch strings.ToLower(strings.TrimSpace(backend)) {
	case dosBackendMetastore:
		return cache
	case dosBackendSidecar:
		return newSidecarDOSAttrStore(base, cache)
	case dosBackendNative:
		if host != nil && hostNativeDOSAttr != nil {
			if s, ok := hostNativeDOSAttr(host, cache); ok {
				return s
			}
		}
		// Forced native but unavailable → degrade to sidecar (still host-portable).
		return newSidecarDOSAttrStore(base, cache)
	case dosBackendXattr:
		if host != nil && hostXattrDOSAttr != nil {
			if s, ok := hostXattrDOSAttr(host, cache); ok {
				return s
			}
		}
		return newSidecarDOSAttrStore(base, cache)
	case dosBackendAuto, "":
		// A FileSystem whose own Stat carries the DOS attributes natively (a remote
		// client FS — SMB/AFP — that reads them off the wire) is authoritative: read
		// from it directly, no host path or sidecar needed. This is preferred over the
		// host-interop backends because it needs no local storage and never goes stale.
		if base.Capabilities().DirAttributes {
			return newFSNativeDOSAttrStore(base, cache)
		}
		// Otherwise prefer Windows-native passthrough, then a Samba-compatible xattr,
		// then a sidecar — each only when host-backed; otherwise the metastore alone.
		if host != nil && hostNativeDOSAttr != nil {
			if s, ok := hostNativeDOSAttr(host, cache); ok {
				return s
			}
		}
		if host != nil && hostXattrDOSAttr != nil {
			if s, ok := hostXattrDOSAttr(host, cache); ok {
				return s
			}
		}
		if host != nil {
			return newSidecarDOSAttrStore(base, cache)
		}
		return cache
	default:
		return cache
	}
}

// --- fs-native backend: read DOS attributes straight from the base FileSystem's own
// Stat, for a backend whose FileInfo carries them natively (a remote SMB/AFP client that
// reads the server's FileAttributes off the wire). No host path, no sidecar, no staleness —
// the source filesystem IS the store. Writes cache in the metastore (we cannot generally
// push attributes back over the wire yet); reads prefer the live Stat and fall back to the
// cache for anything the wire did not carry (e.g. a value only this session Set). ---

// fsNativeDOSAttrStore reads DOS attributes from base.Stat(path), whose returned
// FileInfo.Sys() implements DOSAttrInfo. It is selected by buildDOSAttrStore for a base
// FileSystem advertising Capabilities().DirAttributes.
type fsNativeDOSAttrStore struct {
	fs      FileSystem
	cache   DOSAttrStore
	logging log.Logger
}

func newFSNativeDOSAttrStore(base FileSystem, cache DOSAttrStore) *fsNativeDOSAttrStore {
	return &fsNativeDOSAttrStore{fs: base, cache: cache, logging: log.New("dosattr.fsnative")}
}

func (s *fsNativeDOSAttrStore) Get(path string) (DOSAttr, bool) {
	// A value this session explicitly Set wins (the wire has no way to have learned it).
	if attr, ok := s.cache.Get(path); ok {
		return attr, true
	}
	fi, err := s.fs.Stat(path)
	if err != nil {
		s.logging.Log1(log.Debug, "fs-native stat miss", log.Str("path", path))
		return DOSAttr{}, false
	}
	sys := fi.Sys()
	var bits uint16
	if da, ok := sys.(DOSAttrInfo); ok {
		bits = da.DOSAttrs() & DOSStorableMask
	}
	var create time.Time
	if ct, ok := sys.(DOSCreateTimeInfo); ok {
		create = ct.DOSCreateTime()
	}
	if bits == 0 && create.IsZero() {
		// A plain file with no stored attributes and no known create time: report "nothing
		// stored" so the reader derives everything from the entry, matching the metastore's
		// miss semantics.
		return DOSAttr{}, false
	}
	return DOSAttr{Attrs: bits, CreateTime: create}, true
}

func (s *fsNativeDOSAttrStore) Set(path string, attr DOSAttr) error {
	// No generic wire write-back yet; keep it in the cache so this session sees it.
	return s.cache.Set(path, attr)
}

func (s *fsNativeDOSAttrStore) Delete(path string) error { return s.cache.Delete(path) }

func (s *fsNativeDOSAttrStore) Rename(oldPath, newPath string) error {
	return s.cache.Rename(oldPath, newPath)
}

// --- sidecar backend: a ".dosattr/<name>" companion holding the XATTR_DOSINFO
// blob, readable on every filesystem (no xattr/native support needed). It writes
// through to the metastore cache so a later switch to metastore-only keeps the
// data. ---

// sidecarDOSAttrStore stores the XATTR_DOSINFO blob in a per-file companion under
// a ".dosattr" subdirectory of the file's own directory, via the base FileSystem.
// It works on any filesystem (FAT, network shares, read-only-xattr hosts) — the
// universal fallback. Reads consult the cache first, then the sidecar; writes
// update both.
type sidecarDOSAttrStore struct {
	fs      FileSystem
	cache   DOSAttrStore
	logging log.Logger
}

func newSidecarDOSAttrStore(base FileSystem, cache DOSAttrStore) *sidecarDOSAttrStore {
	return &sidecarDOSAttrStore{fs: base, cache: cache, logging: log.New("dosattr.sidecar")}
}

// dosSidecarPath returns the ".dosattr/<base>" companion path for a store path.
func dosSidecarPath(path string) string {
	dir, base := splitPath(path)
	if dir == "" {
		return ".dosattr/" + base
	}
	return dir + "/.dosattr/" + base
}

func (s *sidecarDOSAttrStore) Get(path string) (DOSAttr, bool) {
	if attr, ok := s.cache.Get(path); ok {
		return attr, true
	}
	s.logging.Log1(log.Debug, "sidecar cache miss, reading companion file", log.Str("path", path))
	b, err := readWhole(s.fs, dosSidecarPath(path))
	if err != nil {
		s.logging.Log1(log.Debug, "sidecar file miss", log.Str("path", path))
		return DOSAttr{}, false
	}
	attr, err := metastore.DecodeDOSInfo(b)
	if err != nil {
		s.logging.Log2(log.Debug, "sidecar file decode failed", log.Str("path", path), log.Str("err", err.Error()))
		return DOSAttr{}, false
	}
	_ = s.cache.Set(path, attr) // re-warm the cache
	return attr, true
}

func (s *sidecarDOSAttrStore) Set(path string, attr DOSAttr) error {
	if err := s.cache.Set(path, attr); err != nil {
		return err
	}
	attr.Attrs &= metastore.DOSStorableMask
	sp := dosSidecarPath(path)
	if dir, _ := splitPath(sp); dir != "" {
		_ = s.fs.CreateDir(dir) // ensure the .dosattr companion dir exists
	}
	return writeWhole(s.fs, sp, metastore.EncodeDOSInfo(attr))
}

func (s *sidecarDOSAttrStore) Delete(path string) error {
	_ = s.cache.Delete(path)
	err := s.fs.Remove(dosSidecarPath(path))
	if err != nil && !isNotExist(err) {
		return err
	}
	return nil
}

func (s *sidecarDOSAttrStore) Rename(oldPath, newPath string) error {
	if err := s.cache.Rename(oldPath, newPath); err != nil {
		return err
	}
	err := s.fs.Rename(dosSidecarPath(oldPath), dosSidecarPath(newPath))
	if err != nil && !isNotExist(err) {
		return err
	}
	return nil
}

// isNotExist reports a not-exist error from the base FileSystem.
func isNotExist(err error) bool { return errors.Is(err, stdfs.ErrNotExist) }

// readWhole reads an entire file from a FileSystem into memory.
func readWhole(fsys FileSystem, path string) ([]byte, error) {
	f, err := fsys.OpenFile(path, os.O_RDONLY)
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

// writeWhole replaces a file's contents in a FileSystem (create+truncate).
func writeWhole(fsys FileSystem, path string, b []byte) error {
	f, err := fsys.OpenFile(path, os.O_RDWR|os.O_CREATE)
	if err != nil {
		f, err = fsys.CreateFile(path)
		if err != nil {
			return err
		}
	}
	defer f.Close()
	if err := f.Truncate(0); err != nil {
		return err
	}
	if len(b) > 0 {
		if _, err := f.WriteAt(b, 0); err != nil {
			return err
		}
	}
	return f.Sync()
}
