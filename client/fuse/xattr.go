package fuse

import (
	"errors"
	"io"
	"os"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/core/appledouble"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
)

func (a *Adapter) Getxattr(path, name string) ([]byte, error) {
	n, err := a.XattrSize(path, name)
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, errNoAttr
	}
	return a.GetxattrRange(path, name, 0, int64(n))
}

func (a *Adapter) GetxattrP(path, name string, position uint32) ([]byte, error) {
	n, err := a.XattrSize(path, name)
	if err != nil {
		return nil, err
	}
	remain := int64(n) - int64(position)
	if remain <= 0 {
		return nil, errNoAttr
	}
	return a.GetxattrRange(path, name, int64(position), remain)
}

// XattrSize is the FUSE size=0 probe: return the attribute length without
// reading it. Resource-fork length comes from Stat (Enumerate/GetFileDirParms),
// not OpenFork+FPRead of the whole fork.
func (a *Adapter) XattrSize(path, name string) (int, error) {
	if !a.nativeForks {
		return 0, errNoAttr
	}
	store, err := toStorePath(path)
	if err != nil {
		return 0, err
	}
	if _, kind := splitNamedFork(store); kind != namedNone {
		return 0, errNoAttr
	}
	switch a.classifyXattr(name) {
	case xattrKindFinder:
		_, ok, err := a.finderInfo(store)
		if err != nil {
			return 0, err
		}
		if !ok {
			return 0, errNoAttr
		}
		return 32, nil
	case xattrKindResource:
		n, err := a.resourceLen(store)
		if err != nil {
			if isNotExist(err) {
				return 0, errNoAttr
			}
			return 0, err
		}
		if n <= 0 {
			return 0, errNoAttr
		}
		return int(n), nil
	case xattrKindMetadata:
		b, err := a.readNetatalkMetadata(store)
		if err != nil {
			return 0, err
		}
		return len(b), nil
	default:
		return 0, errNoAttr
	}
}

// GetxattrRange reads [off, off+length) of an xattr. FUSE passes the kernel
// buffer size so AFP FPRead uses that offset+count, not EOF.
func (a *Adapter) GetxattrRange(path, name string, off, length int64) ([]byte, error) {
	if !a.nativeForks {
		return nil, errNoAttr
	}
	store, err := toStorePath(path)
	if err != nil {
		return nil, err
	}
	if _, kind := splitNamedFork(store); kind != namedNone {
		return nil, errNoAttr
	}
	if length <= 0 {
		return nil, errNoAttr
	}
	switch a.classifyXattr(name) {
	case xattrKindFinder:
		info, ok, err := a.finderInfo(store)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, errNoAttr
		}
		trace("Getxattr %q %s off=%d n=%d", store, name, off, length)
		a.dbg(nil, "fuse getxattr", log.Str("path", store), log.Str("name", name), log.Int("off", off), log.Int("n", length))
		return sliceRange(info[:], off, length), nil
	case xattrKindResource:
		data, err := a.readForkRange(store, fs.ResourceFork, off, length)
		if err != nil {
			if isNotExist(err) {
				return nil, errNoAttr
			}
			return nil, err
		}
		if len(data) == 0 && off > 0 {
			return data, nil
		}
		if len(data) == 0 {
			return nil, errNoAttr
		}
		trace("Getxattr %q %s off=%d n=%d", store, name, off, int64(len(data)))
		a.dbg(nil, "fuse getxattr", log.Str("path", store), log.Str("name", name), log.Int("off", off), log.Int("n", int64(len(data))))
		return data, nil
	case xattrKindMetadata:
		b, err := a.readNetatalkMetadata(store)
		if err != nil {
			return nil, err
		}
		return sliceRange(b, off, length), nil
	default:
		return nil, errNoAttr
	}
}

func (a *Adapter) Setxattr(path, name string, value []byte, flags int) error {
	return a.SetxattrP(path, name, value, flags, 0)
}

func (a *Adapter) SetxattrP(path, name string, value []byte, flags int, position uint32) error {
	if !a.nativeForks {
		return errNoAttr
	}
	if a.readOnly {
		return os.ErrPermission
	}
	store, err := toStorePath(path)
	if err != nil {
		return err
	}
	if _, kind := splitNamedFork(store); kind != namedNone {
		return os.ErrPermission
	}
	_ = flags
	switch a.classifyXattr(name) {
	case xattrKindFinder:
		var info [32]byte
		copy(info[:], value)
		trace("Setxattr %q finderinfo", store)
		a.dbg(nil, "fuse setxattr", log.Str("path", store), log.Str("name", name), log.Int("n", int64(len(value))))
		return a.fsys.WriteFinderInfo(store, info)
	case xattrKindResource:
		a.invalidateXattrFork(store)
		return a.writeResource(store, value, position)
	case xattrKindMetadata:
		return a.writeNetatalkMetadata(store, value)
	default:
		return errNoAttr
	}
}

func (a *Adapter) Removexattr(path, name string) error {
	if !a.nativeForks {
		return errNoAttr
	}
	if a.readOnly {
		return os.ErrPermission
	}
	store, err := toStorePath(path)
	if err != nil {
		return err
	}
	switch a.classifyXattr(name) {
	case xattrKindFinder:
		return a.fsys.WriteFinderInfo(store, [32]byte{})
	case xattrKindResource:
		a.invalidateXattrFork(store)
		return a.truncateResource(store)
	case xattrKindMetadata:
		if err := a.fsys.WriteFinderInfo(store, [32]byte{}); err != nil {
			return err
		}
		return a.fsys.WriteComment(store, nil)
	default:
		return errNoAttr
	}
}

func (a *Adapter) Listxattr(path string) ([]string, error) {
	if !a.nativeForks {
		return nil, nil
	}
	store, err := toStorePath(path)
	if err != nil {
		return nil, err
	}
	if _, kind := splitNamedFork(store); kind != namedNone {
		return nil, nil
	}
	fi, err := a.fsys.Stat(store)
	if err != nil {
		return nil, err
	}
	_, hasFinder := wireFinderInfo(fi)
	rsrcLen, hasRsrc := wireRsrcLen(fi)
	var names []string
	switch a.layout {
	case XattrLayoutNetatalk:
		if hasFinder {
			names = append(names, xattrUserPrefix+xattrNetatalkMetadata)
		} else if _, ok, err := a.fsys.ReadFinderInfo(store); err == nil && ok {
			names = append(names, xattrUserPrefix+xattrNetatalkMetadata)
		} else if c, ok := a.fsys.ReadComment(store); ok && len(c) > 0 {
			names = append(names, xattrUserPrefix+xattrNetatalkMetadata)
		}
		if a.hasResourceFork(store, rsrcLen, hasRsrc) {
			names = append(names, xattrUserPrefix+xattrNetatalkResourceFork)
		}
	default:
		if hasFinder {
			names = append(names, xattrAppleFinderInfo)
		} else if _, ok, err := a.fsys.ReadFinderInfo(store); err == nil && ok {
			names = append(names, xattrAppleFinderInfo)
		}
		if a.hasResourceFork(store, rsrcLen, hasRsrc) {
			names = append(names, xattrAppleResourceFork)
		}
	}
	trace("Listxattr %q n=%d", store, len(names))
	a.dbg(nil, "fuse listxattr", log.Str("path", store), log.Int("n", int64(len(names))))
	return names, nil
}

type xattrKind uint8

const (
	xattrKindUnknown xattrKind = iota
	xattrKindFinder
	xattrKindResource
	xattrKindMetadata
)

func (a *Adapter) classifyXattr(name string) xattrKind {
	n := strings.TrimPrefix(name, xattrUserPrefix)
	switch a.layout {
	case XattrLayoutNetatalk:
		switch n {
		case xattrNetatalkMetadata:
			return xattrKindMetadata
		case xattrNetatalkResourceFork:
			return xattrKindResource
		}
	default:
		switch n {
		case xattrAppleFinderInfo:
			return xattrKindFinder
		case xattrAppleResourceFork:
			return xattrKindResource
		}
	}
	return xattrKindUnknown
}

func (a *Adapter) writeResource(store string, value []byte, position uint32) error {
	f, err := a.fsys.OpenFork(store, fs.ResourceFork, os.O_RDWR|os.O_CREATE)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if position == 0 {
		if err := f.Truncate(int64(len(value))); err != nil {
			return err
		}
	}
	_, err = f.WriteAt(value, int64(position))
	trace("Setxattr %q rsrc pos=%d len=%d err=%v", store, position, len(value), err)
	a.dbg(err, "fuse setxattr", log.Str("path", store), log.Str("name", "rsrc"), log.Int("n", int64(len(value))), log.Int("pos", int64(position)))
	return err
}

func (a *Adapter) truncateResource(store string) error {
	f, err := a.fsys.OpenFork(store, fs.ResourceFork, os.O_RDWR|os.O_CREATE)
	if err != nil {
		if isNotExist(err) {
			return nil
		}
		return err
	}
	defer func() { _ = f.Close() }()
	return f.Truncate(0)
}

func (a *Adapter) finderInfo(store string) ([32]byte, bool, error) {
	if fi, err := a.fsys.Stat(store); err == nil {
		if info, ok := wireFinderInfo(fi); ok {
			return info, true, nil
		}
	}
	return a.fsys.ReadFinderInfo(store)
}

func (a *Adapter) hasResourceFork(store string, rsrcLen int64, fromWire bool) bool {
	if fromWire {
		return rsrcLen > 0
	}
	n, err := a.fsys.ForkLen(store, fs.ResourceFork)
	return err == nil && n > 0
}

func (a *Adapter) resourceLen(store string) (int64, error) {
	if fi, err := a.fsys.Stat(store); err == nil {
		if n, ok := wireRsrcLen(fi); ok {
			return n, nil
		}
	}
	return a.fsys.ForkLen(store, fs.ResourceFork)
}

func sliceRange(b []byte, off, length int64) []byte {
	if off < 0 {
		off = 0
	}
	if off >= int64(len(b)) {
		return nil
	}
	end := off + length
	if end > int64(len(b)) {
		end = int64(len(b))
	}
	return append([]byte(nil), b[off:end]...)
}

// readForkRange FPReads [off, off+length) via a cached OpenFork when Finder
// walks com.apple.ResourceFork in sequential chunks (classicstack-web keeps
// one fork ref for the whole readForkRange session).
func (a *Adapter) readForkRange(store string, fork fs.ForkType, off, length int64) ([]byte, error) {
	if length <= 0 {
		return nil, nil
	}
	f, err := a.acquireXattrFork(store, fork)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, length)
	n, err := f.ReadAt(buf, off)
	a.touchXattrFork()
	if err != nil && !errors.Is(err, io.EOF) && n == 0 {
		return nil, err
	}
	return buf[:n], nil
}

func (a *Adapter) readNetatalkMetadata(store string) ([]byte, error) {
	info, hasFinder, err := a.finderInfo(store)
	if err != nil {
		return nil, err
	}
	comment, hasComment := a.fsys.ReadComment(store)
	rsrcLen := int64(0)
	if fi, err := a.fsys.Stat(store); err == nil {
		if n, ok := wireRsrcLen(fi); ok {
			rsrcLen = n
		} else if n, err := a.fsys.ForkLen(store, fs.ResourceFork); err == nil {
			rsrcLen = n
		}
	} else if n, err := a.fsys.ForkLen(store, fs.ResourceFork); err == nil {
		rsrcLen = n
	}
	if !hasFinder && !hasComment && rsrcLen == 0 {
		return nil, errNoAttr
	}
	p := appledouble.Parsed{
		FinderInfo: info,
		HasFinder:  hasFinder,
		Comment:    comment,
		HasComment: hasComment && len(comment) > 0,
	}
	b := fs.EncodeNetatalkMetadataEA(p, uint32(rsrcLen))
	trace("Getxattr %q metadata len=%d", store, len(b))
	return b, nil
}

func (a *Adapter) writeNetatalkMetadata(store string, value []byte) error {
	p, rsrcLen, err := fs.ParseNetatalkMetadataEA(value)
	if err != nil {
		// Wrong-magic is "no metadata" on the storage side; on a Set from a
		// client it is an invalid attribute value.
		return err
	}
	if p.HasFinder {
		if err := a.fsys.WriteFinderInfo(store, p.FinderInfo); err != nil {
			return err
		}
	}
	if p.HasComment {
		if err := a.fsys.WriteComment(store, p.Comment); err != nil {
			return err
		}
	} else {
		_ = a.fsys.WriteComment(store, nil)
	}
	// Keep the recorded resource length in step: if Metadata claims a new
	// length of 0, truncate the fork. Growing the fork is the ResourceFork EA's
	// job (Netatalk invariant).
	if rsrcLen == 0 {
		a.invalidateXattrFork(store)
		_ = a.truncateResource(store)
	}
	trace("Setxattr %q metadata", store)
	a.dbg(nil, "fuse setxattr", log.Str("path", store), log.Str("name", "metadata"))
	return nil
}
