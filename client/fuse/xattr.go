package fuse

import (
	"os"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/core/appledouble"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
)

func (a *Adapter) Getxattr(path, name string) ([]byte, error) {
	return a.GetxattrP(path, name, 0)
}

// GetxattrP returns the FULL attribute value; the cgofuse host applies Darwin
// position when copying into the FUSE buffer.
func (a *Adapter) GetxattrP(path, name string, _ uint32) ([]byte, error) {
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
	switch a.classifyXattr(name) {
	case xattrKindFinder:
		info, ok, err := a.fsys.ReadFinderInfo(store)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, errNoAttr
		}
		trace("Getxattr %q %s len=32", store, name)
		a.log.Log2(log.Debug, "fuse getxattr finderinfo", log.Str("path", store), log.Str("name", name))
		return info[:], nil
	case xattrKindResource:
		n, err := a.fsys.ForkLen(store, fs.ResourceFork)
		if err != nil {
			return nil, err
		}
		if n == 0 {
			return nil, errNoAttr
		}
		f, err := a.fsys.OpenFork(store, fs.ResourceFork, os.O_RDONLY)
		if err != nil {
			if isNotExist(err) {
				return nil, errNoAttr
			}
			return nil, err
		}
		defer func() { _ = f.Close() }()
		buf := make([]byte, n)
		got, err := f.ReadAt(buf, 0)
		if err != nil && got == 0 {
			return nil, err
		}
		trace("Getxattr %q %s len=%d", store, name, got)
		a.log.Log2(log.Debug, "fuse getxattr rsrc", log.Str("path", store), log.Int("n", int64(got)))
		return buf[:got], nil
	case xattrKindMetadata:
		return a.readNetatalkMetadata(store)
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
		a.log.Log1(log.Debug, "fuse setxattr finderinfo", log.Str("path", store))
		return a.fsys.WriteFinderInfo(store, info)
	case xattrKindResource:
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
	var names []string
	switch a.layout {
	case XattrLayoutNetatalk:
		if _, ok, err := a.fsys.ReadFinderInfo(store); err == nil && ok {
			names = append(names, xattrUserPrefix+xattrNetatalkMetadata)
		} else if c, ok := a.fsys.ReadComment(store); ok && len(c) > 0 {
			names = append(names, xattrUserPrefix+xattrNetatalkMetadata)
		}
		if n, err := a.fsys.ForkLen(store, fs.ResourceFork); err == nil && n > 0 {
			names = append(names, xattrUserPrefix+xattrNetatalkResourceFork)
		}
	default:
		if _, ok, err := a.fsys.ReadFinderInfo(store); err == nil && ok {
			names = append(names, xattrAppleFinderInfo)
		}
		if n, err := a.fsys.ForkLen(store, fs.ResourceFork); err == nil && n > 0 {
			names = append(names, xattrAppleResourceFork)
		}
	}
	trace("Listxattr %q n=%d", store, len(names))
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
	a.log.Log2(log.Debug, "fuse setxattr rsrc", log.Str("path", store), log.Int("n", int64(len(value))))
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

func (a *Adapter) readNetatalkMetadata(store string) ([]byte, error) {
	info, hasFinder, err := a.fsys.ReadFinderInfo(store)
	if err != nil {
		return nil, err
	}
	comment, hasComment := a.fsys.ReadComment(store)
	rsrcLen, err := a.fsys.ForkLen(store, fs.ResourceFork)
	if err != nil {
		return nil, err
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
		_ = a.truncateResource(store)
	}
	trace("Setxattr %q metadata", store)
	a.log.Log1(log.Debug, "fuse setxattr metadata", log.Str("path", store))
	return nil
}
