//go:build xattr && (linux || darwin)

package fs

import (
	"golang.org/x/sys/unix"

	"github.com/ObsoleteMadness/ClassicStack/core/metastore"
)

// xattrDOSAttrName is the extended-attribute name Samba uses for DOS attributes,
// so a value written here is read by Samba (and vice-versa) on the same host file.
const xattrDOSAttrName = "user.DOSATTRIB"

// The xattr DOS-attribute backend stores the XATTR_DOSINFO blob in the
// user.DOSATTRIB extended attribute of the host file — byte-compatible with Samba
// (spec/16-storage-seam.md, errata "Samba DOSATTRIB interop"). It is gated by the
// `xattr` build tag and a linux/darwin GOOS; the metastore cache is written
// through so the data survives a backend change.
func init() {
	hostXattrDOSAttr = func(host HostPather, cache DOSAttrStore) (DOSAttrStore, bool) {
		return &xattrDOSAttrStore{host: host, cache: cache}, true
	}
}

type xattrDOSAttrStore struct {
	host  HostPather
	cache DOSAttrStore
}

func (s *xattrDOSAttrStore) Get(path string) (DOSAttr, bool) {
	hp, ok := s.host.HostPath(path)
	if !ok {
		return s.cache.Get(path)
	}
	buf := make([]byte, 64) // a v3 record is 26 bytes; 64 covers Samba's larger arms
	n, err := unix.Getxattr(hp, xattrDOSAttrName, buf)
	if err != nil || n <= 0 {
		return s.cache.Get(path)
	}
	attr, err := metastore.DecodeDOSInfo(buf[:n])
	if err != nil {
		return s.cache.Get(path)
	}
	_ = s.cache.Set(path, attr)
	return attr, true
}

func (s *xattrDOSAttrStore) Set(path string, attr DOSAttr) error {
	_ = s.cache.Set(path, attr)
	hp, ok := s.host.HostPath(path)
	if !ok {
		return nil
	}
	attr.Attrs &= metastore.DOSStorableMask
	blob := metastore.EncodeDOSInfo(attr)
	// Best-effort: a host/filesystem without user xattrs (or a read-only mount)
	// leaves the cache as the source of truth rather than failing the operation.
	_ = unix.Setxattr(hp, xattrDOSAttrName, blob, 0)
	return nil
}

func (s *xattrDOSAttrStore) Delete(path string) error {
	_ = s.cache.Delete(path)
	if hp, ok := s.host.HostPath(path); ok {
		_ = unix.Removexattr(hp, xattrDOSAttrName)
	}
	return nil
}

func (s *xattrDOSAttrStore) Rename(oldPath, newPath string) error {
	// The xattr rides the file, so the host rename already moved it; just carry the
	// cache entry across.
	return s.cache.Rename(oldPath, newPath)
}
