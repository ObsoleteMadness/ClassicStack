package fuse

import (
	"errors"
	"io"
	iofs "io/fs"
	"os"
	"syscall"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/metastore"
)

// Adapter wraps a core/fs.ForkFS and implements the FUSE operations as Go
// methods (errors, not errno ints) so tests can drive it without cgofuse.
type Adapter struct {
	fsys        fs.ForkFS
	readOnly    bool
	volLabel    string
	nativeForks bool
	layout      XattrLayout
	uid, gid    uint32
	handles     *handleTable
	log         log.Logger
	onInit      func()
}

// New builds an Adapter over an already-connected ForkFS without mounting it.
func New(fsys fs.ForkFS, opts Options) *Adapter { return newAdapter(fsys, opts) }

func newAdapter(fsys fs.ForkFS, opts Options) *Adapter {
	label := opts.VolumeLabel
	if label == "" {
		label = "ClassicStack"
	}
	uid, gid := currentUIDGID()
	return &Adapter{
		fsys:        fsys,
		readOnly:    opts.ReadOnly || fsys.Capabilities().ReadOnly,
		volLabel:    label,
		nativeForks: opts.NativeForks,
		layout:      opts.resolvedLayout(),
		uid:         uid,
		gid:         gid,
		handles:     newHandleTable(),
		log:         log.New("csmount.fuse"),
	}
}

func (a *Adapter) flagFor(flags int) int {
	acc := flags & (os.O_RDONLY | os.O_WRONLY | os.O_RDWR)
	if a.readOnly {
		return os.O_RDONLY
	}
	if acc == os.O_WRONLY || acc == os.O_RDWR {
		return os.O_RDWR
	}
	return os.O_RDONLY
}

// Getattr returns metadata for path. fh is 0 when FUSE has no open handle.
func (a *Adapter) Getattr(path string, fh uint64) (Stat, error) {
	store, err := toStorePath(path)
	if err != nil {
		return Stat{}, err
	}
	base, kind := splitNamedFork(store)
	if kind != namedNone {
		return a.getattrNamedFork(base, kind)
	}
	if fh != 0 {
		if h, ok := a.handles.get(fh); ok {
			store = h.path
			if h.rsrc {
				return a.getattrNamedFork(store, namedForkRsrc)
			}
		}
	}
	fi, err := a.fsys.Stat(store)
	if err != nil {
		trace("Getattr %q → err=%v", store, err)
		return Stat{}, err
	}
	st := a.fillStat(store, fi)
	trace("Getattr %q size=%d dir=%v", store, st.Size, st.IsDir)
	a.log.Log1(log.Debug, "fuse getattr", log.Str("path", store))
	return st, nil
}

func (a *Adapter) Statfs() (total, free uint64, err error) {
	total, free, err = a.fsys.DiskUsage("")
	if err != nil || total == 0 {
		total, free = 8<<40, 8<<40
		err = nil
	}
	trace("Statfs total=%d free=%d", total, free)
	return total, free, nil
}

func (a *Adapter) Open(path string, flags int) (uint64, error) {
	store, err := toStorePath(path)
	if err != nil {
		return 0, err
	}
	base, kind := splitNamedFork(store)
	if kind != namedNone {
		return a.openNamedFork(base, kind, flags)
	}
	fi, err := a.fsys.Stat(store)
	if err != nil {
		trace("Open %q → err=%v", store, err)
		return 0, err
	}
	h := &openFile{path: store, isDir: fi.IsDir(), flag: a.flagFor(flags)}
	if !fi.IsDir() {
		f, err := a.fsys.OpenFile(store, h.flag)
		if err != nil {
			trace("Open %q → err=%v", store, err)
			return 0, err
		}
		h.f = f
	}
	fh := a.handles.add(h)
	trace("Open %q → fh=%d dir=%v", store, fh, h.isDir)
	a.log.Log2(log.Debug, "fuse open", log.Str("path", store), log.Int("fh", int64(fh)))
	return fh, nil
}

func (a *Adapter) Create(path string, flags int, mode uint32) (uint64, error) {
	if a.readOnly {
		return 0, os.ErrPermission
	}
	store, err := toStorePath(path)
	if err != nil {
		return 0, err
	}
	base, kind := splitNamedFork(store)
	if kind != namedNone {
		return a.openNamedFork(base, kind, flags|os.O_CREATE)
	}
	f, err := a.fsys.CreateFile(store)
	if err != nil {
		trace("Create %q → err=%v", store, err)
		return 0, err
	}
	h := &openFile{path: store, f: f, flag: os.O_RDWR}
	fh := a.handles.add(h)
	trace("Create %q → fh=%d", store, fh)
	a.log.Log2(log.Debug, "fuse create", log.Str("path", store), log.Int("fh", int64(fh)))
	_ = mode
	return fh, nil
}

func (a *Adapter) Mkdir(path string, _ uint32) error {
	if a.readOnly {
		return os.ErrPermission
	}
	store, err := toStorePath(path)
	if err != nil {
		return err
	}
	if err := a.fsys.CreateDir(store); err != nil {
		trace("Mkdir %q → err=%v", store, err)
		return err
	}
	trace("Mkdir %q", store)
	a.log.Log1(log.Debug, "fuse mkdir", log.Str("path", store))
	return nil
}

func (a *Adapter) Unlink(path string) error {
	if a.readOnly {
		return os.ErrPermission
	}
	store, err := toStorePath(path)
	if err != nil {
		return err
	}
	base, kind := splitNamedFork(store)
	if kind == namedForkRsrc {
		return a.truncateResource(base)
	}
	if kind == namedForkDir {
		return os.ErrPermission
	}
	if err := a.fsys.Remove(store); err != nil {
		trace("Unlink %q → err=%v", store, err)
		return err
	}
	trace("Unlink %q", store)
	a.log.Log1(log.Debug, "fuse unlink", log.Str("path", store))
	return nil
}

func (a *Adapter) Rmdir(path string) error {
	return a.Unlink(path)
}

func (a *Adapter) Rename(oldpath, newpath string) error {
	if a.readOnly {
		return os.ErrPermission
	}
	src, err := toStorePath(oldpath)
	if err != nil {
		return err
	}
	dst, err := toStorePath(newpath)
	if err != nil {
		return err
	}
	if _, kind := splitNamedFork(src); kind != namedNone {
		return os.ErrPermission
	}
	if _, kind := splitNamedFork(dst); kind != namedNone {
		return os.ErrPermission
	}
	if err := a.fsys.Rename(src, dst); err != nil {
		trace("Rename %q → %q err=%v", src, dst, err)
		return err
	}
	trace("Rename %q → %q", src, dst)
	a.log.Log2(log.Debug, "fuse rename", log.Str("from", src), log.Str("to", dst))
	return nil
}

func (a *Adapter) Read(path string, buff []byte, ofst int64, fh uint64) (int, error) {
	h, ok := a.handles.get(fh)
	if !ok {
		return 0, os.ErrInvalid
	}
	if h.f == nil {
		return 0, errIsDir
	}
	n, err := h.f.ReadAt(buff, ofst)
	if n > 0 && (err == nil || errors.Is(err, io.EOF)) {
		trace("Read %q off=%d n=%d", h.path, ofst, n)
		a.log.Log2(log.Debug, "fuse read", log.Str("path", h.path), log.Int("n", int64(n)))
		return n, nil
	}
	if errors.Is(err, io.EOF) {
		return 0, nil
	}
	trace("Read %q off=%d err=%v", h.path, ofst, err)
	_ = path
	return n, err
}

func (a *Adapter) Write(path string, buff []byte, ofst int64, fh uint64) (int, error) {
	if a.readOnly {
		return 0, os.ErrPermission
	}
	h, ok := a.handles.get(fh)
	if !ok {
		return 0, os.ErrInvalid
	}
	if h.f == nil {
		return 0, errIsDir
	}
	n, err := h.f.WriteAt(buff, ofst)
	trace("Write %q off=%d n=%d err=%v", h.path, ofst, n, err)
	a.log.Log2(log.Debug, "fuse write", log.Str("path", h.path), log.Int("n", int64(n)))
	_ = path
	return n, err
}

func (a *Adapter) Truncate(path string, size int64, fh uint64) error {
	if a.readOnly {
		return os.ErrPermission
	}
	if fh != 0 {
		if h, ok := a.handles.get(fh); ok && h.f != nil {
			return h.f.Truncate(size)
		}
	}
	store, err := toStorePath(path)
	if err != nil {
		return err
	}
	base, kind := splitNamedFork(store)
	if kind == namedForkRsrc {
		f, err := a.fsys.OpenFork(base, fs.ResourceFork, os.O_RDWR|os.O_CREATE)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		return f.Truncate(size)
	}
	f, err := a.fsys.OpenFile(store, os.O_RDWR)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return f.Truncate(size)
}

func (a *Adapter) Flush(_ string, fh uint64) error {
	h, ok := a.handles.get(fh)
	if !ok || h.f == nil {
		return nil
	}
	return h.f.Sync()
}

func (a *Adapter) Release(_ string, fh uint64) error {
	h, ok := a.handles.remove(fh)
	if !ok {
		return nil
	}
	if h.f != nil {
		return h.f.Close()
	}
	return nil
}

func (a *Adapter) Fsync(_ string, _ bool, fh uint64) error {
	return a.Flush("", fh)
}

func (a *Adapter) Opendir(path string) (uint64, error) {
	return a.Open(path, os.O_RDONLY)
}

func (a *Adapter) Releasedir(path string, fh uint64) error {
	return a.Release(path, fh)
}

func (a *Adapter) Readdir(path string, fh uint64) ([]Dirent, error) {
	store := ""
	if fh != 0 {
		if h, ok := a.handles.get(fh); ok {
			store = h.path
			if h.rsrc {
				return nil, errNotDir
			}
		}
	}
	if store == "" {
		var err error
		store, err = toStorePath(path)
		if err != nil {
			return nil, err
		}
	}
	base, kind := splitNamedFork(store)
	if kind == namedForkDir {
		if !a.nativeForks {
			return nil, os.ErrNotExist
		}
		return []Dirent{{Name: namedForkRsrcName}}, nil
	}
	if kind == namedForkRsrc {
		return nil, errNotDir
	}
	entries, err := a.fsys.ReadDir(store)
	if err != nil {
		trace("Readdir %q → err=%v", store, err)
		return nil, err
	}
	out := make([]Dirent, 0, len(entries)+2)
	out = append(out, Dirent{Name: "."}, Dirent{Name: ".."})
	for _, de := range entries {
		out = append(out, Dirent{Name: de.Name(), IsDir: de.IsDir()})
	}
	trace("Readdir %q entries=%d", store, len(entries))
	a.log.Log2(log.Debug, "fuse readdir", log.Str("path", store), log.Int("n", int64(len(entries))))
	_ = base
	return out, nil
}

// Dirent is one Readdir entry.
type Dirent struct {
	Name  string
	IsDir bool
}

func (a *Adapter) Utimens(path string, tmsp []time.Time) error {
	if a.readOnly {
		return os.ErrPermission
	}
	store, err := toStorePath(path)
	if err != nil {
		return err
	}
	attr, _ := a.fsys.Meta().Attrs(store)
	if len(tmsp) > 0 && !tmsp[0].IsZero() {
		attr.AccessTime = tmsp[0]
	}
	return a.fsys.Meta().SetAttrs(store, attr)
}

func (a *Adapter) Setcrtime(path string, tmsp time.Time) error {
	if a.readOnly {
		return os.ErrPermission
	}
	store, err := toStorePath(path)
	if err != nil {
		return err
	}
	attr, _ := a.fsys.Meta().Attrs(store)
	attr.CreateTime = tmsp
	return a.fsys.Meta().SetAttrs(store, attr)
}

func (a *Adapter) Chflags(path string, flags uint32) error {
	if a.readOnly {
		return os.ErrPermission
	}
	store, err := toStorePath(path)
	if err != nil {
		return err
	}
	attr, _ := a.fsys.Meta().Attrs(store)
	if flags&ufHidden != 0 {
		attr.Attrs |= metastore.DOSHidden
	} else {
		attr.Attrs &^= metastore.DOSHidden
	}
	return a.fsys.Meta().SetAttrs(store, attr)
}

func (a *Adapter) Chmod(path string, mode uint32) error {
	if a.readOnly {
		return os.ErrPermission
	}
	store, err := toStorePath(path)
	if err != nil {
		return err
	}
	attr, _ := a.fsys.Meta().Attrs(store)
	if mode&0o222 == 0 {
		attr.Attrs |= metastore.DOSReadOnly
	} else {
		attr.Attrs &^= metastore.DOSReadOnly
	}
	return a.fsys.Meta().SetAttrs(store, attr)
}

func (a *Adapter) Chown(string, uint32, uint32) error { return nil }

func (a *Adapter) Access(string, uint32) error { return nil }

func isNotExist(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, iofs.ErrNotExist) || errors.Is(err, syscall.ENOENT)
}
