package smb

import (
	"errors"
	"io"
	stdfs "io/fs"
	"os"
	"path"
	"strings"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	proto "github.com/ObsoleteMadness/ClassicStack/core/protocol/smb"
)

// filesystem.go implements fs.FileSystem over an open SMB Session. SMB carries no
// resource fork, so this base implements only FileSystem; client.Connect layers the
// "appledouble" fork backend over it, which reads/writes the server's "._name"
// sidecars as ordinary data-fork files through OpenFile/CreateFile — the client needs
// no fork-specific SMB code, the AppleDouble adapter does it all over the data fork.

// FS is an SMB client bound to one mounted share (one Session/TID). It satisfies
// fs.FileSystem.
type FS struct {
	sess *Session

	// onClose runs after the session is closed (the factory sets it if it owns extra
	// resources beyond the session's transport).
	onClose func()

	readOnly bool
}

var _ fs.FileSystem = (*FS)(nil)

// New builds an FS over an established session. Open (the session handshake) is done by
// the factory; New just wraps it as a FileSystem.
func New(sess *Session) *FS { return &FS{sess: sess} }

// ReadDir lists a directory via TRANS2 FIND_FIRST2 / FIND_NEXT2, paging until the
// server reports end-of-search. The '/'-path is share-root-relative.
func (f *FS) ReadDir(dir string) ([]stdfs.DirEntry, error) {
	var out []stdfs.DirEntry
	unicode := f.sess.Unicode()

	resp, err := f.sess.send(func(b *proto.Builder) []byte {
		return b.BuildFindFirst2(dir, 256)
	})
	if err != nil {
		return nil, err
	}
	res, err := proto.ParseFind(resp, true, unicode)
	if err != nil {
		return nil, translateErr(err)
	}
	out = appendFindEntries(out, res.Entries)

	for !res.EndOfSearch {
		sid := res.SID
		resp, err := f.sess.send(func(b *proto.Builder) []byte {
			return b.BuildFindNext2(sid, 256)
		})
		if err != nil {
			return nil, err
		}
		res, err = proto.ParseFind(resp, false, unicode)
		if err != nil {
			if errors.Is(translateErr(err), stdfs.ErrNotExist) {
				break // NO_MORE_FILES — clean end
			}
			return nil, translateErr(err)
		}
		out = appendFindEntries(out, res.Entries)
	}
	return out, nil
}

// appendFindEntries converts protocol FindEntries to fs.DirEntry rows.
func appendFindEntries(out []stdfs.DirEntry, entries []proto.FindEntry) []stdfs.DirEntry {
	for _, e := range entries {
		out = append(out, dirEntry{
			name: e.Name,
			dir:  e.IsDir(),
			size: int64(e.Size),
		})
	}
	return out
}

// Stat resolves one path via SMB_COM_QUERY_INFORMATION (the CORE stat every dialect
// answers). The root path ("") is always a directory.
func (f *FS) Stat(p string) (stdfs.FileInfo, error) {
	if strings.Trim(p, "/") == "" {
		return fileInfo{name: "", dir: true}, nil
	}
	resp, err := f.sess.send(func(b *proto.Builder) []byte {
		return b.BuildQueryInformation(p)
	})
	if err != nil {
		return nil, err
	}
	info, err := proto.ParseQueryInformation(resp)
	if err != nil {
		return nil, translateErr(err)
	}
	return fileInfo{
		name: leaf(p),
		dir:  info.IsDir(),
		size: int64(info.Size),
	}, nil
}

// DiskUsage is not surfaced over the client's minimal command set; report unknown
// (0,0), which the fs layer treats as a mounted, non-empty volume. (A TRANS2
// QUERY_FS_INFORMATION round trip could fill this in later.)
func (f *FS) DiskUsage(path string) (total, free uint64, err error) {
	return 0, 0, nil
}

// CreateDir creates a directory via SMB_COM_CREATE_DIRECTORY.
func (f *FS) CreateDir(p string) error {
	resp, err := f.sess.send(func(b *proto.Builder) []byte {
		return b.BuildCreateDirectory(p)
	})
	if err != nil {
		return err
	}
	return translateErr(proto.ParseCreateDirectory(resp))
}

// CreateFile creates (or truncates) a file via OPEN_ANDX and returns an open r/w
// handle to its data fork.
func (f *FS) CreateFile(p string) (fs.File, error) {
	return f.open(p, proto.OpenParams{ReadWrite: true, Create: true, Truncate: true})
}

// OpenFile opens a file's data fork with os flags translated to OPEN_ANDX behaviour.
func (f *FS) OpenFile(p string, flag int) (fs.File, error) {
	params := proto.OpenParams{
		ReadWrite: flag&(os.O_WRONLY|os.O_RDWR) != 0,
		Create:    flag&os.O_CREATE != 0,
		Truncate:  flag&os.O_TRUNC != 0,
	}
	return f.open(p, params)
}

// open runs OPEN_ANDX and returns a fileHandle for the granted FID.
func (f *FS) open(p string, params proto.OpenParams) (fs.File, error) {
	resp, err := f.sess.send(func(b *proto.Builder) []byte {
		return b.BuildOpenAndX(p, params)
	})
	if err != nil {
		return nil, err
	}
	res, err := proto.ParseOpenAndX(resp)
	if err != nil {
		return nil, translateErr(err)
	}
	return &fileHandle{fs: f, path: p, fid: res.FID, size: int64(res.Size), writable: params.ReadWrite || params.Create}, nil
}

// Remove deletes a file or empty directory. It stats the path to choose DELETE
// (file) vs DELETE_DIRECTORY.
func (f *FS) Remove(p string) error {
	info, err := f.Stat(p)
	if err != nil {
		return err
	}
	if info.IsDir() {
		resp, err := f.sess.send(func(b *proto.Builder) []byte {
			return b.BuildDeleteDirectory(p)
		})
		if err != nil {
			return err
		}
		return translateErr(proto.ParseDeleteDirectory(resp))
	}
	resp, err := f.sess.send(func(b *proto.Builder) []byte {
		return b.BuildDelete(p)
	})
	if err != nil {
		return err
	}
	return translateErr(proto.ParseDelete(resp))
}

// Rename moves oldPath to newPath via SMB_COM_RENAME (which handles both same-dir
// rename and cross-dir move on the server).
func (f *FS) Rename(oldPath, newPath string) error {
	resp, err := f.sess.send(func(b *proto.Builder) []byte {
		return b.BuildRename(oldPath, newPath)
	})
	if err != nil {
		return err
	}
	return translateErr(proto.ParseRename(resp))
}

// ShortName / MediumName return the leaf; the shareFS MetaEngine derives the real 8.3
// name locally, so the client only needs a stable value (the SMB server's own 8.3 name
// is available in the FIND ShortName field but multi-name listing is deferred).
func (f *FS) ShortName(p string) (string, error)  { return leaf(p), nil }
func (f *FS) MediumName(p string) (string, error) { return leaf(p), nil }

// Capabilities reports the mounted share's capabilities. ChildCount is off (the client
// does not compute it); ReadOnly follows the connection option.
func (f *FS) Capabilities() fs.Capabilities {
	return fs.Capabilities{ReadOnly: f.readOnly}
}

// Close ends the SMB session (fs.FSCloser), so client.Connect's ForkFS.Close tears the
// whole circuit down (TREE_DISCONNECT + LOGOFF + transport close).
func (f *FS) Close() error {
	err := f.sess.Close()
	if f.onClose != nil {
		f.onClose()
	}
	return err
}

// --- fileHandle: fs.File over an SMB FID ---

// fileHandle is an open SMB file (data fork) addressed by FID. Positional I/O uses
// READ_ANDX / WRITE_ANDX, chunked at maxIO. Truncate uses a zero-length WRITE_ANDX at
// the target offset (the SMB truncate convention the server honours).
type fileHandle struct {
	fs       *FS
	path     string
	fid      uint16
	size     int64
	writable bool
	closed   bool
}

func (h *fileHandle) ReadAt(p []byte, off int64) (int, error) {
	if h.closed {
		return 0, stdfs.ErrClosed
	}
	maxIO := h.fs.sess.MaxIO()
	total := 0
	for total < len(p) {
		want := len(p) - total
		if want > maxIO {
			want = maxIO
		}
		reqOff := off + int64(total)
		resp, err := h.fs.sess.send(func(b *proto.Builder) []byte {
			return b.BuildReadAndX(h.fid, reqOff, uint16(want))
		})
		if err != nil {
			return total, err
		}
		data, err := proto.ParseReadAndX(resp)
		if err != nil {
			return total, translateErr(err)
		}
		n := copy(p[total:], data)
		total += n
		if n < want {
			return total, io.EOF // short read: end of file
		}
	}
	return total, nil
}

func (h *fileHandle) WriteAt(p []byte, off int64) (int, error) {
	if h.closed {
		return 0, stdfs.ErrClosed
	}
	if !h.writable {
		return 0, stdfs.ErrPermission
	}
	maxIO := h.fs.sess.MaxIO()
	total := 0
	for total < len(p) {
		want := len(p) - total
		if want > maxIO {
			want = maxIO
		}
		reqOff := off + int64(total)
		chunk := p[total : total+want]
		resp, err := h.fs.sess.send(func(b *proto.Builder) []byte {
			return b.BuildWriteAndX(h.fid, reqOff, chunk)
		})
		if err != nil {
			return total, err
		}
		n, err := proto.ParseWriteAndX(resp)
		if err != nil {
			return total, translateErr(err)
		}
		total += n
		if n < want {
			break // server accepted a short write; stop rather than spin
		}
	}
	if off+int64(total) > h.size {
		h.size = off + int64(total)
	}
	return total, nil
}

// Truncate sets the file length to size via a zero-length WRITE_ANDX at that offset
// (the SMB convention: a write of zero bytes truncates the file to Offset).
func (h *fileHandle) Truncate(size int64) error {
	if h.closed {
		return stdfs.ErrClosed
	}
	if !h.writable {
		return stdfs.ErrPermission
	}
	resp, err := h.fs.sess.send(func(b *proto.Builder) []byte {
		return b.BuildWriteAndX(h.fid, size, nil)
	})
	if err != nil {
		return err
	}
	if _, err := proto.ParseWriteAndX(resp); err != nil {
		return translateErr(err)
	}
	h.size = size
	return nil
}

func (h *fileHandle) Stat() (stdfs.FileInfo, error) {
	return fileInfo{name: leaf(h.path), size: h.size}, nil
}

// Sync is a no-op: the server commits on close, and this client sends no buffered
// writes (every WriteAt is a synchronous WRITE_ANDX). A FLUSH round trip could be added
// if a backend needs an explicit commit.
func (h *fileHandle) Sync() error {
	if h.closed {
		return stdfs.ErrClosed
	}
	return nil
}

func (h *fileHandle) Close() error {
	if h.closed {
		return nil
	}
	h.closed = true
	resp, err := h.fs.sess.send(func(b *proto.Builder) []byte {
		return b.BuildClose(h.fid)
	})
	if err != nil {
		return err
	}
	return translateErr(proto.ParseClose(resp))
}

// --- helpers ---

// dirEntry is a minimal fs.DirEntry from a FIND record.
type dirEntry struct {
	name string
	dir  bool
	size int64
}

func (e dirEntry) Name() string { return e.name }
func (e dirEntry) IsDir() bool  { return e.dir }
func (e dirEntry) Type() stdfs.FileMode {
	if e.dir {
		return stdfs.ModeDir
	}
	return 0
}
func (e dirEntry) Info() (stdfs.FileInfo, error) {
	return fileInfo{name: e.name, dir: e.dir, size: e.size}, nil
}

// fileInfo is a minimal fs.FileInfo. SMB timestamps are not surfaced by the client's
// minimal command set, so ModTime is the zero time (the fs layer tolerates it).
type fileInfo struct {
	name string
	dir  bool
	size int64
}

func (fi fileInfo) Name() string { return fi.name }
func (fi fileInfo) Size() int64  { return fi.size }
func (fi fileInfo) Mode() stdfs.FileMode {
	if fi.dir {
		return stdfs.ModeDir | 0o755
	}
	return 0o644
}
func (fi fileInfo) ModTime() time.Time { return time.Time{} }
func (fi fileInfo) IsDir() bool        { return fi.dir }
func (fi fileInfo) Sys() any           { return nil }

// leaf returns the last '/'-separated element of a share-relative path.
func leaf(p string) string {
	p = strings.Trim(p, "/")
	if p == "" {
		return ""
	}
	return path.Base(p)
}

// translateErr maps an SMB protocol ErrStatus to the fs sentinel errors the shareFS
// layer and xfer expect (ErrNotExist / ErrExist / ErrPermission), leaving other errors
// as-is. A nil error passes through.
func translateErr(err error) error {
	if err == nil {
		return nil
	}
	var st *proto.ErrStatus
	if !errors.As(err, &st) {
		return err
	}
	switch st.Status {
	case statusObjectNameNotFound, statusObjectPathNotFound, statusNoSuchFile, statusNoMoreFiles:
		return stdfs.ErrNotExist
	case statusObjectNameCollision:
		return stdfs.ErrExist
	case statusAccessDenied:
		return stdfs.ErrPermission
	default:
		return err
	}
}

// SMB NTSTATUS values the client maps to fs sentinels ([MS-ERREF]). The client always
// negotiates NT status, so the wire values are the raw NTSTATUS.
const (
	statusObjectNameNotFound  uint32 = 0xC0000034
	statusObjectPathNotFound  uint32 = 0xC000003A
	statusNoSuchFile          uint32 = 0xC000000F
	statusNoMoreFiles         uint32 = 0x80000006
	statusObjectNameCollision uint32 = 0xC0000035
	statusAccessDenied        uint32 = 0xC0000022
)
