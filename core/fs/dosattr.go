package fs

import (
	"errors"
	"io"
	stdfs "io/fs"
	"os"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/core/metastore"
)

// DOSAttrStore is the per-share DOS-attribute facade the file services reach
// through the built share (type-assert the ForkFS to DOSAttred). It is an alias of
// the metastore facade type so a service imports one name; the concrete backend is
// selected at BuildShare time per the share's dos_attr_backend.
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

// DOSAttred is the optional interface a built share satisfies when it carries a
// DOS-attribute store. A file service type-asserts the ForkFS to it (like Coded /
// HostPather); a share whose backend declines leaves the assertion failing and the
// service derives attributes from the entry instead.
type DOSAttred interface {
	DOSAttrs() DOSAttrStore
}

// Named is the optional interface a built share satisfies to expose its NameEngine
// directly, so a file service can REVERSE a derived name a client sent (8.3 from a
// DOS client, 31-char medium from classic AFP) back to the stored host name —
// FileSystem.ShortName/MediumName only go forward. EtherDFS uses this to resolve a
// wire 8.3 path to the real host file; AFP/NCP use it for medium names.
type Named interface {
	Names() NameEngine
}

// dosAttrBackend names the DOS-attribute persistence backend a share selects via
// dos_attr_backend. "auto" picks the best host-interop backend available (native
// on Windows, xattr where the host supports it) and always layers the metastore
// as a cache; the explicit names force one. "metastore" is the definitive,
// host-independent store; "sidecar" works on every filesystem; "native"/"xattr"
// are host-interop backends gated by build/GOOS.
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
// name falls back to "auto". The returned store is never nil.
func buildDOSAttrStore(backend string, base FileSystem, store metastore.Store) DOSAttrStore {
	cache := metastore.NewDOSAttrStore(store)
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
		// Prefer Windows-native passthrough, then a Samba-compatible xattr, then a
		// sidecar — each only when host-backed; otherwise the metastore alone.
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
	fs    FileSystem
	cache DOSAttrStore
}

func newSidecarDOSAttrStore(base FileSystem, cache DOSAttrStore) *sidecarDOSAttrStore {
	return &sidecarDOSAttrStore{fs: base, cache: cache}
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
	b, err := readWhole(s.fs, dosSidecarPath(path))
	if err != nil {
		return DOSAttr{}, false
	}
	attr, err := metastore.DecodeDOSInfo(b)
	if err != nil {
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
