//go:build windows

package fs

import "syscall"

// We use stdlib syscall (not golang.org/x/sys/windows) for the three calls and
// the FILE_ATTRIBUTE_* constants this backend needs: x/sys/windows transitively
// pulls encoding/binary → reflect, which the core ring forbids (§1 / archtest).
// syscall is already a permitted core dependency (os pulls it) and carries
// GetFileAttributes/SetFileAttributes/UTF16PtrFromString + the attribute consts.

// On Windows the DOS attribute bits ARE the host's file attributes, so a share's
// DOS-attribute store maps straight through to GetFileAttributes /
// SetFileAttributes — no side storage needed. This works on every Windows volume
// including non-system drives (where the OS 8.3-name service is often disabled),
// because file attributes are a core NTFS/FAT feature independent of the
// short-name service. The metastore cache is still written through so a later
// switch to a non-Windows host (or a metastore-only share) keeps the data.
func init() {
	hostNativeDOSAttr = func(host HostPather, cache DOSAttrStore) (DOSAttrStore, bool) {
		return &windowsDOSAttrStore{host: host, cache: cache}, true
	}
}

// windowsDOSAttrStore reads/writes the real Windows file attributes for the host
// path of a store path, caching in the metastore. Reads prefer the live host
// attributes (authoritative on Windows); the cache backs create-time, which the
// host also records but which we keep uniform across backends.
type windowsDOSAttrStore struct {
	host  HostPather
	cache DOSAttrStore
}

func (s *windowsDOSAttrStore) Get(path string) (DOSAttr, bool) {
	hp, ok := s.host.HostPath(path)
	if !ok {
		return s.cache.Get(path)
	}
	p, err := syscall.UTF16PtrFromString(hp)
	if err != nil {
		return s.cache.Get(path)
	}
	raw, err := syscall.GetFileAttributes(p)
	if err != nil {
		return s.cache.Get(path)
	}
	attr := DOSAttr{Attrs: fromWindowsAttrs(raw)}
	// Create-time is not returned by GetFileAttributes; take it from the cache if
	// present so the value is uniform with the other backends.
	if c, ok := s.cache.Get(path); ok {
		attr.CreateTime = c.CreateTime
	}
	return attr, true
}

func (s *windowsDOSAttrStore) Set(path string, attr DOSAttr) error {
	_ = s.cache.Set(path, attr) // cache create-time + bits
	hp, ok := s.host.HostPath(path)
	if !ok {
		return nil
	}
	p, err := syscall.UTF16PtrFromString(hp)
	if err != nil {
		return nil
	}
	// Preserve any host attribute bits we do not model (e.g. COMPRESSED) by OR-ing
	// our storable bits onto the current set after clearing the storable ones.
	cur, err := syscall.GetFileAttributes(p)
	if err != nil {
		cur = 0
	}
	const storable = syscall.FILE_ATTRIBUTE_READONLY | syscall.FILE_ATTRIBUTE_HIDDEN |
		syscall.FILE_ATTRIBUTE_SYSTEM | syscall.FILE_ATTRIBUTE_ARCHIVE
	next := (cur &^ storable) | toWindowsAttrs(attr.Attrs)
	if next == 0 {
		next = syscall.FILE_ATTRIBUTE_NORMAL
	}
	return syscall.SetFileAttributes(p, next)
}

func (s *windowsDOSAttrStore) Delete(path string) error { return s.cache.Delete(path) }
func (s *windowsDOSAttrStore) Rename(o, n string) error { return s.cache.Rename(o, n) }

// fromWindowsAttrs maps the Windows attribute word to our storable DOS bits.
func fromWindowsAttrs(raw uint32) uint16 {
	var a uint16
	if raw&syscall.FILE_ATTRIBUTE_READONLY != 0 {
		a |= DOSReadOnly
	}
	if raw&syscall.FILE_ATTRIBUTE_HIDDEN != 0 {
		a |= DOSHidden
	}
	if raw&syscall.FILE_ATTRIBUTE_SYSTEM != 0 {
		a |= DOSSystem
	}
	if raw&syscall.FILE_ATTRIBUTE_ARCHIVE != 0 {
		a |= DOSArchive
	}
	return a
}

// toWindowsAttrs maps our storable DOS bits to the Windows attribute word.
func toWindowsAttrs(a uint16) uint32 {
	var raw uint32
	if a&DOSReadOnly != 0 {
		raw |= syscall.FILE_ATTRIBUTE_READONLY
	}
	if a&DOSHidden != 0 {
		raw |= syscall.FILE_ATTRIBUTE_HIDDEN
	}
	if a&DOSSystem != 0 {
		raw |= syscall.FILE_ATTRIBUTE_SYSTEM
	}
	if a&DOSArchive != 0 {
		raw |= syscall.FILE_ATTRIBUTE_ARCHIVE
	}
	return raw
}
