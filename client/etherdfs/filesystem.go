package etherdfs

import (
	"errors"
	"io"
	stdfs "io/fs"
	"os"
	"path"
	"strings"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	proto "github.com/ObsoleteMadness/ClassicStack/core/protocol/etherdfs"
)

// filesystem.go implements fs.FileSystem over an open EtherDFS Session. EtherDFS is a
// DOS network-redirector protocol: it addresses files by a DOS wire path relative to
// the bound drive (backslash-separated), returns 8.3 FCB names from its finds, and
// carries a per-open file ID for READ/WRITE/CLOSE. It has no native fork, so
// client.Connect layers the AppleDouble backend over this base — the server's "._NAME"
// sidecars are ordinary 8.3 files the adapter opens/reads/writes.

// FS is an EtherDFS client bound to one mounted drive (one Session). It satisfies
// fs.FileSystem.
type FS struct {
	sess *Session

	// onClose runs after the session is closed (the factory sets it to release the
	// owning transport/link).
	onClose func()

	readOnly bool
}

var _ fs.FileSystem = (*FS)(nil)

// New builds an FS over an established session.
func New(sess *Session) *FS { return &FS{sess: sess} }

// wirePath renders a '/'-separated, drive-root-relative store path into the DOS wire
// path EtherDFS carries (backslash-separated, leading backslash). The server's
// NormalizePath strips the leading separator and folds it to a store path. An empty
// path names the drive root (sent as "\").
func wirePath(p string) string {
	p = strings.Trim(p, "/")
	if p == "" {
		return "\\"
	}
	return "\\" + strings.ReplaceAll(p, "/", "\\")
}

// dosError maps an EtherDFS AX status code to the fs sentinel errors the shareFS layer
// and xfer expect. A zero status is success (nil).
func dosError(op string, status uint16) error {
	switch status {
	case proto.ErrNone:
		return nil
	case proto.ErrFileNotFound, proto.ErrPathNotFound, proto.ErrNoMoreFiles:
		return stdfs.ErrNotExist
	case proto.ErrAccessDenied:
		return stdfs.ErrPermission
	case proto.ErrFileExists:
		return stdfs.ErrExist
	default:
		return &dfsError{op: op, status: status}
	}
}

// dfsError wraps an unmapped EtherDFS status with the operation name.
type dfsError struct {
	op     string
	status uint16
}

func (e *dfsError) Error() string {
	return "etherdfs: " + e.op + ": DOS error 0x" + hexWord(e.status)
}

// ReadDir lists a directory via AL_FINDFIRST / AL_FINDNEXT, paging the cursor the server
// returns until it reports no-more-files. The '/'-path is drive-root-relative.
func (f *FS) ReadDir(dir string) ([]stdfs.DirEntry, error) {
	var out []stdfs.DirEntry

	// AL_FINDFIRST searches the directory with a "*.*"-equivalent wildcard leaf and an
	// attribute filter that admits files + subdirectories (hidden/system too).
	searchPath := wirePath(dir)
	if !strings.HasSuffix(searchPath, "\\") {
		searchPath += "\\"
	}
	searchPath += "*.*"
	const findAttr = proto.AttrHidden | proto.AttrSystem | proto.AttrDirectory

	status, body, err := f.sess.command(proto.OpFindFirst, proto.EncodeFindFirstRequest(proto.FindFirstRequest{
		Attr: findAttr, Path: searchPath,
	}))
	if err != nil {
		return nil, err
	}
	if e := dosError("FINDFIRST", status); e != nil {
		if errors.Is(e, stdfs.ErrNotExist) {
			return out, nil // empty directory
		}
		return nil, e
	}
	entry, perr := proto.DecodeFindReply(body)
	if perr != nil {
		return nil, errMalformed("FINDFIRST reply")
	}
	out = appendFind(out, entry)

	for {
		status, body, err := f.sess.command(proto.OpFindNext, proto.EncodeFindNextRequest(proto.FindNextRequest{
			DirID:    entry.DirID,
			Position: entry.Position,
			Attr:     findAttr,
			Mask:     proto.FilenameToFCB("*.*"),
		}))
		if err != nil {
			return nil, err
		}
		if e := dosError("FINDNEXT", status); e != nil {
			if errors.Is(e, stdfs.ErrNotExist) {
				break // no more files — clean end
			}
			return nil, e
		}
		entry, perr = proto.DecodeFindReply(body)
		if perr != nil {
			return nil, errMalformed("FINDNEXT reply")
		}
		out = appendFind(out, entry)
	}
	return out, nil
}

// appendFind converts a find reply to an fs.DirEntry, skipping the "." and ".." pseudo
// entries a DOS directory listing may include.
func appendFind(out []stdfs.DirEntry, e proto.FindReply) []stdfs.DirEntry {
	name := proto.FCBToFilename(e.FCB)
	if name == "." || name == ".." || name == "" {
		return out
	}
	return append(out, dirEntry{
		name: name,
		dir:  e.Attr&proto.AttrDirectory != 0,
		size: int64(e.Size),
	})
}

// Stat resolves one path via AL_GETATTR. The root path is always a directory.
func (f *FS) Stat(p string) (stdfs.FileInfo, error) {
	if strings.Trim(p, "/") == "" {
		return fileInfo{name: "", dir: true}, nil
	}
	status, body, err := f.sess.command(proto.OpGetattr, proto.EncodePathRequest(wirePath(p)))
	if err != nil {
		return nil, err
	}
	if e := dosError("GETATTR", status); e != nil {
		return nil, e
	}
	r, perr := proto.DecodeGetAttrReply(body)
	if perr != nil {
		return nil, errMalformed("GETATTR reply")
	}
	return fileInfo{
		name: leaf(p),
		dir:  r.Attr&proto.AttrDirectory != 0,
		size: int64(r.Size),
	}, nil
}

// DiskUsage reports the drive's total/free bytes via AL_DISKSPACE. The server reports
// counts in fixed 32 KiB clusters (proto.DiskSpaceStatus geometry).
func (f *FS) DiskUsage(path string) (total, free uint64, err error) {
	status, body, err := f.sess.command(proto.OpDiskspace, nil)
	if err != nil {
		return 0, 0, err
	}
	// AL_DISKSPACE's AX is the fixed DiskSpaceStatus data value, not an error code, so
	// only a transport error is fatal here.
	_ = status
	totalCl, bytesPerSector, freeCl, perr := proto.DecodeDiskSpaceReply(body)
	if perr != nil {
		return 0, 0, errMalformed("DISKSPACE reply")
	}
	// One cluster = one sector of bytesPerSector (the server reports 1 sector/cluster).
	clusterBytes := uint64(bytesPerSector)
	return uint64(totalCl) * clusterBytes, uint64(freeCl) * clusterBytes, nil
}

// CreateDir creates a directory via AL_MKDIR.
func (f *FS) CreateDir(p string) error {
	status, _, err := f.sess.command(proto.OpMkdir, proto.EncodePathRequest(wirePath(p)))
	if err != nil {
		return err
	}
	return dosError("MKDIR", status)
}

// CreateFile creates (or truncates) a file via AL_CREATE and returns an open r/w handle.
func (f *FS) CreateFile(p string) (fs.File, error) {
	status, body, err := f.sess.command(proto.OpCreate, proto.EncodeOpenRequest(proto.OpenRequest{Path: wirePath(p)}))
	if err != nil {
		return nil, err
	}
	if e := dosError("CREATE", status); e != nil {
		return nil, e
	}
	r, perr := proto.DecodeOpenReply(body)
	if perr != nil {
		return nil, errMalformed("CREATE reply")
	}
	return &fileHandle{fs: f, path: p, fileID: r.FileID, size: int64(r.Size), writable: true}, nil
}

// OpenFile opens a file's data fork via AL_OPEN. O_CREATE creates it if missing;
// O_TRUNC truncates after opening.
func (f *FS) OpenFile(p string, flag int) (fs.File, error) {
	status, body, err := f.sess.command(proto.OpOpen, proto.EncodeOpenRequest(proto.OpenRequest{Path: wirePath(p)}))
	if err != nil {
		return nil, err
	}
	if e := dosError("OPEN", status); e != nil {
		if errors.Is(e, stdfs.ErrNotExist) && flag&os.O_CREATE != 0 {
			return f.CreateFile(p)
		}
		return nil, e
	}
	r, perr := proto.DecodeOpenReply(body)
	if perr != nil {
		return nil, errMalformed("OPEN reply")
	}
	writable := flag&(os.O_WRONLY|os.O_RDWR) != 0
	h := &fileHandle{fs: f, path: p, fileID: r.FileID, size: int64(r.Size), writable: writable}
	if flag&os.O_TRUNC != 0 && writable {
		if err := h.Truncate(0); err != nil {
			_ = h.Close()
			return nil, err
		}
	}
	return h, nil
}

// Remove deletes a file (AL_DELETE) or an empty directory (AL_RMDIR). It stats the path
// to choose.
func (f *FS) Remove(p string) error {
	info, err := f.Stat(p)
	if err != nil {
		return err
	}
	op := proto.OpDelete
	name := "DELETE"
	if info.IsDir() {
		op = proto.OpRmdir
		name = "RMDIR"
	}
	status, _, err := f.sess.command(op, proto.EncodePathRequest(wirePath(p)))
	if err != nil {
		return err
	}
	return dosError(name, status)
}

// Rename moves oldPath to newPath via AL_RENAME (the server handles same-dir rename and
// cross-dir move).
func (f *FS) Rename(oldPath, newPath string) error {
	status, _, err := f.sess.command(proto.OpRename, proto.EncodeRenameRequest(proto.RenameRequest{
		Src: wirePath(oldPath),
		Dst: wirePath(newPath),
	}))
	if err != nil {
		return err
	}
	return dosError("RENAME", status)
}

// ShortName / MediumName return the leaf; the shareFS MetaEngine derives the real 8.3
// name locally, so the client only needs a stable value.
func (f *FS) ShortName(p string) (string, error)  { return leaf(p), nil }
func (f *FS) MediumName(p string) (string, error) { return leaf(p), nil }

// Capabilities reports the mounted drive's capabilities.
func (f *FS) Capabilities() fs.Capabilities {
	return fs.Capabilities{ReadOnly: f.readOnly}
}

// Close ends the EtherDFS session (fs.FSCloser).
func (f *FS) Close() error {
	err := f.sess.Close()
	if f.onClose != nil {
		f.onClose()
	}
	return err
}

// --- fileHandle: fs.File over an EtherDFS file ID ---

// fileHandle is an open EtherDFS file addressed by its server file ID. Positional I/O
// uses AL_READFIL / AL_WRITEFIL, chunked at the transport's one-frame ceiling. Truncate
// uses a zero-length AL_WRITEFIL at the target offset (the DOS truncate convention the
// server honours).
type fileHandle struct {
	fs       *FS
	path     string
	fileID   uint16
	size     int64
	writable bool
	closed   bool
}

func (h *fileHandle) ReadAt(p []byte, off int64) (int, error) {
	if h.closed {
		return 0, stdfs.ErrClosed
	}
	maxIO := h.fs.sess.MaxPayload()
	total := 0
	for total < len(p) {
		want := len(p) - total
		if want > maxIO {
			want = maxIO
		}
		reqOff := uint32(off) + uint32(total)
		status, body, err := h.fs.sess.command(proto.OpReadfil, proto.EncodeReadRequest(proto.ReadRequest{
			Offset: reqOff, FileID: h.fileID, Length: uint16(want),
		}))
		if err != nil {
			return total, err
		}
		if e := dosError("READFIL", status); e != nil {
			return total, e
		}
		n := copy(p[total:], body)
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
	maxIO := h.fs.sess.MaxPayload()
	total := 0
	for total < len(p) {
		want := len(p) - total
		if want > maxIO {
			want = maxIO
		}
		reqOff := uint32(off) + uint32(total)
		chunk := p[total : total+want]
		status, body, err := h.fs.sess.command(proto.OpWritefil, proto.EncodeWriteRequest(proto.WriteRequest{
			Offset: reqOff, FileID: h.fileID, Data: chunk,
		}))
		if err != nil {
			return total, err
		}
		if e := dosError("WRITEFIL", status); e != nil {
			return total, e
		}
		n, perr := proto.DecodeWriteReply(body)
		if perr != nil {
			return total, errMalformed("WRITEFIL reply")
		}
		total += int(n)
		if int(n) < want {
			break // server accepted a short write; stop rather than spin
		}
	}
	if off+int64(total) > h.size {
		h.size = off + int64(total)
	}
	return total, nil
}

// Truncate sets the file length via a zero-length AL_WRITEFIL at that offset (the DOS
// convention: a write of zero bytes truncates the file to Offset).
func (h *fileHandle) Truncate(size int64) error {
	if h.closed {
		return stdfs.ErrClosed
	}
	if !h.writable {
		return stdfs.ErrPermission
	}
	status, _, err := h.fs.sess.command(proto.OpWritefil, proto.EncodeWriteRequest(proto.WriteRequest{
		Offset: uint32(size), FileID: h.fileID, Data: nil,
	}))
	if err != nil {
		return err
	}
	if e := dosError("WRITEFIL truncate", status); e != nil {
		return e
	}
	h.size = size
	return nil
}

func (h *fileHandle) Stat() (stdfs.FileInfo, error) {
	return fileInfo{name: leaf(h.path), size: h.size}, nil
}

// Sync flushes the open handle via AL_CMMTFIL.
func (h *fileHandle) Sync() error {
	if h.closed {
		return stdfs.ErrClosed
	}
	status, _, err := h.fs.sess.command(proto.OpCmmtfil, proto.EncodeFileIDBody(h.fileID))
	if err != nil {
		return err
	}
	return dosError("CMMTFIL", status)
}

func (h *fileHandle) Close() error {
	if h.closed {
		return nil
	}
	h.closed = true
	status, _, err := h.fs.sess.command(proto.OpClsfil, proto.EncodeFileIDBody(h.fileID))
	if err != nil {
		return err
	}
	return dosError("CLSFIL", status)
}

// --- helpers ---

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

// leaf returns the last '/'-separated element of a drive-relative path.
func leaf(p string) string {
	p = strings.Trim(p, "/")
	if p == "" {
		return ""
	}
	return path.Base(p)
}

// errMalformed reports a reply the parser could not decode.
func errMalformed(what string) error {
	return errors.New("etherdfs: malformed " + what)
}

// hexWord renders a 16-bit value as four lowercase hex digits.
func hexWord(v uint16) string {
	const hexdigits = "0123456789abcdef"
	return string([]byte{
		hexdigits[(v>>12)&0xF], hexdigits[(v>>8)&0xF],
		hexdigits[(v>>4)&0xF], hexdigits[v&0xF],
	})
}
