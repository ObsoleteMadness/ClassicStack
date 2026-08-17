//go:build fuse && cgo && (darwin || linux)

package fuse

import (
	"errors"
	"os"
	"sync"
	"time"

	cgofuse "github.com/winfsp/cgofuse/fuse"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

type hostFS struct {
	*Adapter
	cgofuse.FileSystemBase
}

func (h *hostFS) Init() {
	if h.onInit != nil {
		h.onInit()
	}
}

func (h *hostFS) errno(err error) int {
	if err == nil {
		return 0
	}
	if errors.Is(err, errNoAttr) {
		return -cgofuse.ENOATTR
	}
	if isNotExist(err) {
		return -cgofuse.ENOENT
	}
	if errors.Is(err, os.ErrPermission) {
		return -cgofuse.EACCES
	}
	if errors.Is(err, os.ErrExist) {
		return -cgofuse.EEXIST
	}
	if errors.Is(err, os.ErrInvalid) {
		return -cgofuse.EINVAL
	}
	if errors.Is(err, errIsDir) {
		return -cgofuse.EISDIR
	}
	if errors.Is(err, errNotDir) {
		return -cgofuse.ENOTDIR
	}
	if errors.Is(err, errInvalidName) {
		return -cgofuse.EINVAL
	}
	return -cgofuse.EIO
}

func osFlags(fuseFlags int) int {
	acc := fuseFlags & cgofuse.O_ACCMODE
	var f int
	switch acc {
	case cgofuse.O_WRONLY:
		f = os.O_WRONLY
	case cgofuse.O_RDWR:
		f = os.O_RDWR
	default:
		f = os.O_RDONLY
	}
	if fuseFlags&cgofuse.O_CREAT != 0 {
		f |= os.O_CREATE
	}
	if fuseFlags&cgofuse.O_TRUNC != 0 {
		f |= os.O_TRUNC
	}
	if fuseFlags&cgofuse.O_APPEND != 0 {
		f |= os.O_APPEND
	}
	if fuseFlags&cgofuse.O_EXCL != 0 {
		f |= os.O_EXCL
	}
	return f
}

func fillCgoStat(dst *cgofuse.Stat_t, st Stat, uid, gid uint32) {
	if st.IsDir {
		dst.Mode = cgofuse.S_IFDIR | st.Mode
	} else {
		dst.Mode = cgofuse.S_IFREG | st.Mode
	}
	dst.Nlink = 1
	dst.Uid = uid
	dst.Gid = gid
	dst.Size = st.Size
	dst.Atim = cgofuse.NewTimespec(st.Atime)
	dst.Mtim = cgofuse.NewTimespec(st.Mtime)
	dst.Ctim = cgofuse.NewTimespec(st.Ctime)
	dst.Birthtim = cgofuse.NewTimespec(st.Birthtime)
	dst.Ino = st.Ino
	dst.Flags = st.Flags
	if dst.Blksize == 0 {
		dst.Blksize = 4096
	}
	if st.Size > 0 {
		dst.Blocks = (st.Size + 511) / 512
	}
}

func (h *hostFS) Getattr(path string, stat *cgofuse.Stat_t, fh uint64) int {
	st, err := h.Adapter.Getattr(path, fh)
	if err != nil {
		return h.errno(err)
	}
	fillCgoStat(stat, st, h.uid, h.gid)
	return 0
}

func (h *hostFS) Statfs(_ string, stat *cgofuse.Statfs_t) int {
	total, free, err := h.Adapter.Statfs()
	if err != nil {
		return h.errno(err)
	}
	const bsize = 4096
	stat.Bsize = bsize
	stat.Frsize = bsize
	stat.Blocks = total / bsize
	stat.Bfree = free / bsize
	stat.Bavail = free / bsize
	stat.Namemax = 255
	return 0
}

func (h *hostFS) Open(path string, flags int) (int, uint64) {
	fh, err := h.Adapter.Open(path, osFlags(flags))
	if err != nil {
		return h.errno(err), ^uint64(0)
	}
	return 0, fh
}

func (h *hostFS) Create(path string, flags int, mode uint32) (int, uint64) {
	fh, err := h.Adapter.Create(path, osFlags(flags)|os.O_CREATE, mode)
	if err != nil {
		return h.errno(err), ^uint64(0)
	}
	return 0, fh
}

func (h *hostFS) Mkdir(path string, mode uint32) int {
	return h.errno(h.Adapter.Mkdir(path, mode))
}

func (h *hostFS) Unlink(path string) int { return h.errno(h.Adapter.Unlink(path)) }
func (h *hostFS) Rmdir(path string) int  { return h.errno(h.Adapter.Rmdir(path)) }

func (h *hostFS) Rename(oldpath, newpath string) int {
	return h.errno(h.Adapter.Rename(oldpath, newpath))
}

func (h *hostFS) Read(path string, buff []byte, ofst int64, fh uint64) int {
	n, err := h.Adapter.Read(path, buff, ofst, fh)
	if err != nil && n == 0 {
		return h.errno(err)
	}
	return n
}

func (h *hostFS) Write(path string, buff []byte, ofst int64, fh uint64) int {
	n, err := h.Adapter.Write(path, buff, ofst, fh)
	if err != nil && n == 0 {
		return h.errno(err)
	}
	return n
}

func (h *hostFS) Truncate(path string, size int64, fh uint64) int {
	return h.errno(h.Adapter.Truncate(path, size, fh))
}

func (h *hostFS) Flush(path string, fh uint64) int {
	return h.errno(h.Adapter.Flush(path, fh))
}

func (h *hostFS) Release(path string, fh uint64) int {
	return h.errno(h.Adapter.Release(path, fh))
}

func (h *hostFS) Fsync(path string, datasync bool, fh uint64) int {
	return h.errno(h.Adapter.Fsync(path, datasync, fh))
}

func (h *hostFS) Opendir(path string) (int, uint64) {
	fh, err := h.Adapter.Opendir(path)
	if err != nil {
		return h.errno(err), ^uint64(0)
	}
	return 0, fh
}

func (h *hostFS) Releasedir(path string, fh uint64) int {
	return h.errno(h.Adapter.Releasedir(path, fh))
}

func (h *hostFS) Readdir(path string, fill func(name string, stat *cgofuse.Stat_t, ofst int64) bool, ofst int64, fh uint64) int {
	ents, err := h.Adapter.Readdir(path, fh)
	if err != nil {
		return h.errno(err)
	}
	for _, e := range ents {
		if !fill(e.Name, nil, 0) {
			break
		}
	}
	_ = ofst
	return 0
}

func (h *hostFS) Utimens(path string, tmsp []cgofuse.Timespec) int {
	var ts []time.Time
	for _, t := range tmsp {
		ts = append(ts, t.Time())
	}
	return h.errno(h.Adapter.Utimens(path, ts))
}

func (h *hostFS) Chmod(path string, mode uint32) int {
	return h.errno(h.Adapter.Chmod(path, mode))
}

func (h *hostFS) Chown(path string, uid, gid uint32) int {
	return h.errno(h.Adapter.Chown(path, uid, gid))
}

func (h *hostFS) Access(path string, mask uint32) int {
	return h.errno(h.Adapter.Access(path, mask))
}

func (h *hostFS) Setxattr(path, name string, value []byte, flags int) int {
	return h.errno(h.Adapter.Setxattr(path, name, value, flags))
}

func (h *hostFS) Getxattr(path, name string) (int, []byte) {
	b, err := h.Adapter.Getxattr(path, name)
	if err != nil {
		return h.errno(err), nil
	}
	return 0, b
}

func (h *hostFS) SetxattrP(path, name string, value []byte, flags int, position uint32) int {
	return h.errno(h.Adapter.SetxattrP(path, name, value, flags, position))
}

func (h *hostFS) GetxattrP(path, name string, position uint32) (int, []byte) {
	b, err := h.Adapter.GetxattrP(path, name, position)
	if err != nil {
		return h.errno(err), nil
	}
	return 0, b
}

func (h *hostFS) Removexattr(path, name string) int {
	return h.errno(h.Adapter.Removexattr(path, name))
}

func (h *hostFS) Listxattr(path string, fill func(name string) bool) int {
	names, err := h.Adapter.Listxattr(path)
	if err != nil {
		return h.errno(err)
	}
	for _, n := range names {
		if !fill(n) {
			break
		}
	}
	return 0
}

func (h *hostFS) Chflags(path string, flags uint32) int {
	return h.errno(h.Adapter.Chflags(path, flags))
}

func (h *hostFS) Setcrtime(path string, tmsp cgofuse.Timespec) int {
	return h.errno(h.Adapter.Setcrtime(path, tmsp.Time()))
}

var (
	_ cgofuse.FileSystemInterface = (*hostFS)(nil)
	_ cgofuse.FileSystemXattrP    = (*hostFS)(nil)
	_ cgofuse.FileSystemChflags   = (*hostFS)(nil)
	_ cgofuse.FileSystemSetcrtime = (*hostFS)(nil)
)

// MountAt builds an Adapter over fsys and mounts it at mountpoint via cgofuse.
func MountAt(fsys fs.ForkFS, mountpoint string, opts Options) (*Mount, error) {
	a := newAdapter(fsys, opts)
	ready := make(chan struct{})
	var once sync.Once
	a.onInit = func() { once.Do(func() { close(ready) }) }

	fsop := &hostFS{Adapter: a}
	host := cgofuse.NewFileSystemHost(fsop)
	host.SetCapCaseInsensitive(true)

	args := []string{"-o", "fsname=ClassicStack"}
	if a.volLabel != "" {
		args = append(args, "-o", "volname="+a.volLabel)
	}

	done := make(chan struct{})
	m := &Mount{
		unmount: func() { host.Unmount() },
		wait:    func() { <-done },
	}
	go func() {
		defer close(done)
		_ = host.Mount(mountpoint, args)
	}()
	select {
	case <-ready:
		return m, nil
	case <-done:
		return nil, errors.New("fuse: mount failed (is macFUSE/libfuse installed?)")
	case <-time.After(15 * time.Second):
		host.Unmount()
		return nil, errors.New("fuse: mount timed out")
	}
}
