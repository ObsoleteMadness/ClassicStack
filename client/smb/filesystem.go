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

	// The search id is assigned by FIND_FIRST2 and identifies the server-side search for
	// the whole paging run; FIND_NEXT2 responses do NOT carry it (their param block is
	// SearchCount/EndOfSearch only), so it must be held from the first reply rather than
	// re-read from each page. (Re-reading res.SID gave 0 on the second FIND_NEXT2, which a
	// real Win98 rejected with ERRDOS/ERRbadfid — every listing over two pages failed.)
	sid := res.SID
	for !res.EndOfSearch {
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
		// A page that returns no entries is end-of-search even when the server did not set
		// the EndOfSearch flag: a real Win98 answers the FIND_NEXT2 that runs off the end
		// with SearchCount=0, DataCount=0, EndOfSearch=0 — relying only on the flag looped
		// FIND_NEXT2 forever (hundreds of thousands of empty round trips). Stop on either
		// signal.
		if len(res.Entries) == 0 {
			break
		}
	}
	return out, nil
}

// appendFindEntries converts protocol FindEntries to fs.DirEntry rows.
func appendFindEntries(out []stdfs.DirEntry, entries []proto.FindEntry) []stdfs.DirEntry {
	for _, e := range entries {
		out = append(out, dirEntry{
			name:    e.Name,
			dir:     e.IsDir(),
			size:    int64(e.Size),
			attrs:   e.Attrs,
			modTime: e.ModTime,
			create:  e.CreateTime,
		})
	}
	return out
}

// Stat resolves one path. It uses SMB_COM_QUERY_INFORMATION for the size (the CORE stat
// every dialect answers), then enriches the timestamps and attributes with a TRANS2
// QUERY_PATH_INFORMATION (SMB_QUERY_FILE_BASIC_INFO) — the legacy query returns a poor/
// zero LastWriteTime on a Win9x server, whereas BASIC_INFO carries the real FILETIMEs and
// a creation date. QUERY_PATH_INFO is best-effort: if the server does not answer it, the
// legacy values stand. The root path ("") is always a directory.
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
	fi := fileInfo{
		name:    leaf(p),
		dir:     info.IsDir(),
		size:    int64(info.Size),
		attrs:   info.Attrs,
		modTime: info.ModTime,
	}
	// Enrich with the TRANS2 basic-info timestamps (best-effort). Skipped once the server
	// has rejected QUERY_PATH_INFORMATION as unsupported (a Win9x share does), so we do not
	// pay a failed round trip per Stat.
	if !f.sess.PathInfoUnsupported() {
		if bi, err := f.queryBasicInfo(p); err == nil {
			if !bi.WriteTime.IsZero() {
				fi.modTime = bi.WriteTime
			}
			fi.create = bi.CreateTime
			if bi.Attrs != 0 {
				fi.attrs = bi.Attrs
			}
		} else {
			// A server that does not implement QUERY_PATH_INFORMATION answers "invalid
			// function"; remember that and stop issuing it for this session.
			f.sess.MarkPathInfoUnsupported()
		}
	}
	return fi, nil
}

// queryBasicInfo runs a TRANS2 QUERY_PATH_INFORMATION (BASIC_INFO) for path, returning the
// server's timestamps + attributes. Errors are returned so the caller can fall back to the
// legacy query values.
func (f *FS) queryBasicInfo(p string) (proto.BasicInfo, error) {
	resp, err := f.sess.send(func(b *proto.Builder) []byte {
		return b.BuildQueryPathInfo(p)
	})
	if err != nil {
		return proto.BasicInfo{}, err
	}
	return proto.ParseQueryPathInfo(resp)
}

// DiskUsage reports the share's total and free bytes via SMB_COM_QUERY_INFORMATION_DISK
// ([MS-CIFS] §2.2.4.24) — the CORE disk-space command every dialect answers (including
// Win9x File & Print). Without this, WinFsp falls back to a nominal 8 TiB volume.
func (f *FS) DiskUsage(path string) (total, free uint64, err error) {
	_ = path
	resp, err := f.sess.send(func(b *proto.Builder) []byte {
		return b.BuildQueryInformationDisk()
	})
	if err != nil {
		return 0, 0, err
	}
	info, err := proto.ParseQueryInformationDisk(resp)
	if err != nil {
		return 0, 0, translateErr(err)
	}
	return info.Total, info.Free, nil
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
	// DirAttributes: our Stat/ReadDir FileInfo carries the server's DOS attributes
	// natively (via fs.DOSAttrInfo on Sys()), so the share's MetaEngine reads them from
	// the wire rather than a local store — surfacing hidden/system/read-only to a DOS
	// client (and the WinFsp mount) with no extra round-trips.
	return fs.Capabilities{ReadOnly: f.readOnly, DirAttributes: true}
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
	name    string
	dir     bool
	size    int64
	attrs   uint16 // server's DOS FileAttributes from the FIND record
	modTime time.Time
	create  time.Time
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
	return fileInfo(e), nil
}

// fileInfo is a minimal fs.FileInfo. SMB timestamps are not surfaced by the client's
// minimal command set, so ModTime is the zero time (the fs layer tolerates it).
type fileInfo struct {
	name    string
	dir     bool
	size    int64
	attrs   uint16    // server's SMB FileAttributes (== DOS/FILE_ATTRIBUTE_* bits); 0 = unknown
	modTime time.Time // server's LastWriteTime; zero if the command did not carry one
	create  time.Time // server's CreationTime (FIND only); zero if unknown
}

func (fi fileInfo) Name() string { return fi.name }
func (fi fileInfo) Size() int64  { return fi.size }
func (fi fileInfo) Mode() stdfs.FileMode {
	if fi.dir {
		return stdfs.ModeDir | 0o755
	}
	return 0o644
}
func (fi fileInfo) ModTime() time.Time { return fi.modTime }
func (fi fileInfo) IsDir() bool        { return fi.dir }

// Sys exposes the server's DOS attribute bits (fs.DOSAttrInfo) and creation time
// (fs.DOSCreateTimeInfo) to a DOS/Windows consumer (the WinFsp mount, via the share's
// fs-native MetaEngine). The SMB Attr* bits are the same values as metastore.DOS* /
// FILE_ATTRIBUTE_*, so no translation is needed. nil when there is nothing to report
// (a plain file with no create time), so it is not treated as having attributes.
func (fi fileInfo) Sys() any {
	a := fi.attrs &^ proto.AttrDirectory
	if a == 0 && fi.create.IsZero() {
		return nil
	}
	return smbMeta{attrs: a, create: fi.create}
}

// smbMeta adapts the SMB FileAttributes + creation time to the fs meta interfaces.
type smbMeta struct {
	attrs  uint16
	create time.Time
}

func (m smbMeta) DOSAttrs() uint16         { return m.attrs }
func (m smbMeta) DOSCreateTime() time.Time { return m.create }

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
	// A DOS-error server (Win9x negotiates NT LM 0.12 WITHOUT CAP_STATUS32) packs the
	// header's 32-bit status field as ErrorClass(1) | Reserved(1) | ErrorCode(2 LE), read
	// here as class in the low byte and code in the high 16 bits (e.g. 0x00020001 =
	// class ERRDOS(1) code ERRbadfile(2)). Such a value never collides with a real
	// NTSTATUS, which always has the top severity bits set, so decode it first.
	if class := st.Status & 0xFF; class == dosErrClassDOS || class == dosErrClassSrv {
		switch code := uint16(st.Status >> 16); code {
		case dosErrBadFile, dosErrBadPath, dosErrNoFiles:
			return stdfs.ErrNotExist
		case dosErrFileExists:
			return stdfs.ErrExist
		case dosErrNoAccess, dosErrBadShare:
			return stdfs.ErrPermission
		default:
			return err
		}
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

// DOS/OS2 SMB error class + codes a DOS-error server (Win9x) returns in place of an
// NTSTATUS ([MS-CIFS] 2.2.1.5, SMB error class/code). ErrorClass sits in the status
// field's low byte, ErrorCode in the high 16 bits.
const (
	dosErrClassDOS uint32 = 0x01 // ERRDOS
	dosErrClassSrv uint32 = 0x02 // ERRSRV

	dosErrBadFile    uint16 = 2  // ERRbadfile — file not found
	dosErrBadPath    uint16 = 3  // ERRbadpath — directory component not found
	dosErrNoAccess   uint16 = 5  // ERRnoaccess — access denied
	dosErrFileExists uint16 = 80 // ERRfilexists — file already exists
	dosErrNoFiles    uint16 = 18 // ERRnofiles — no more files in a search
	dosErrBadShare   uint16 = 32 // ERRbadshare — sharing/lock violation
)

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
