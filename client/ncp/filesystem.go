package ncp

import (
	"errors"
	"io"
	stdfs "io/fs"
	"os"
	"path"
	"strings"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	proto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ncp"
)

// filesystem.go implements fs.FileSystem over an open NCP Session. NCP addresses files
// by a directory handle + a relative NetWare path (backslash-separated 8.3 names); this
// adapter anchors every operation on the session's root dir handle and renders the
// '/'-separated, volume-root-relative store path into that NetWare wire form. NCP has
// no native fork, so client.Connect layers the AppleDouble backend over this base — the
// server's "._NAME" sidecars are ordinary 8.3 files the adapter opens/reads/writes over
// the DOS name space, no fork-specific NCP code needed.

// FS is an NCP client bound to one mounted volume (one Session, one root dir handle).
// It satisfies fs.FileSystem.
type FS struct {
	sess *Session

	// onClose runs after the session is closed (the factory sets it to release the
	// owning transport/link).
	onClose func()

	readOnly bool
}

var _ fs.FileSystem = (*FS)(nil)

// New builds an FS over an established session. Open (the attach flow) is done by the
// factory; New just wraps it as a FileSystem.
func New(sess *Session) *FS { return &FS{sess: sess} }

// wirePath renders a '/'-separated, volume-root-relative store path into the NetWare
// relative wire path (backslash-separated) the file calls resolve against the root dir
// handle. An empty path names the root directory itself (sent as "").
func wirePath(p string) string {
	p = strings.Trim(p, "/")
	if p == "" {
		return ""
	}
	return strings.ReplaceAll(p, "/", "\\")
}

// ReadDir lists a directory. It uses the NetWare 3.x File Search Initialize (0x3E) +
// File Search Continue (0x3F) pair — the scan a real NetWare 3.x/4.x server answers — and
// falls back to the FCB-era Search for a File (0x40) for the ClassicStack server, which
// implements 0x40. NetWare's search-attribute picks EITHER files OR directories per pass
// (directory bit 0x10), so — like a DOS DIR shell — each method makes two passes (files,
// then subdirectories). The '/'-path is volume-root-relative.
func (f *FS) ReadDir(dir string) ([]stdfs.DirEntry, error) {
	// Try the 0x3E/0x3F scan first (real servers). If Initialize itself is unsupported
	// (an error completion), fall back to the 0x40 scan (our own server).
	if entries, ok, err := f.readDirSearch3x(dir); ok {
		return entries, err
	}
	return f.readDir40(dir)
}

// readDirSearch3x enumerates dir with File Search Initialize/Continue. ok is false when
// the server does not support File Search Initialize (so the caller falls back to 0x40);
// once Initialize succeeds, ok is true and any later error is returned as-is.
func (f *FS) readDirSearch3x(dir string) (entries []stdfs.DirEntry, ok bool, err error) {
	rep, ierr := f.sess.command("File Search Initialize", func(r *proto.Requester) []byte {
		return r.BuildFileSearchInit(f.sess.rootDir, wirePath(dir))
	})
	if ierr != nil {
		return nil, false, nil // unsupported / not a directory here → fall back
	}
	base, perr := proto.ParseFileSearchInit(rep.Body)
	if perr != nil {
		return nil, false, nil
	}

	files, err := f.searchContinuePass(base, proto.NwSearchAttrFiles)
	if err != nil {
		return nil, true, err
	}
	dirs, err := f.searchContinuePass(base, proto.NwSearchAttrDirs)
	if err != nil {
		return nil, true, err
	}
	return append(files, dirs...), true, nil
}

// searchContinuePass pages one File Search Continue scan (one search-attribute) from the
// Initialize context to its end (completion 0xFF), returning the entries. The context's
// sequence is fresh per pass, so each pass restarts from the Initialize sequence.
func (f *FS) searchContinuePass(base proto.FileSearchContext, attr uint8) ([]stdfs.DirEntry, error) {
	var out []stdfs.DirEntry
	ctx := base // copy: each pass restarts from the Initialize sequence
	for {
		rep, err := f.sess.command("File Search Continue", func(r *proto.Requester) []byte {
			return r.BuildFileSearchContinue(ctx, attr, proto.SearchAllPattern())
		})
		if err != nil {
			if isNoMoreFiles(err) {
				break // clean end of scan
			}
			return nil, translateErr(err)
		}
		e, perr := proto.ParseFileSearchContinue(rep.Body, &ctx)
		if perr != nil {
			return nil, errMalformed("File Search Continue reply")
		}
		if e.Name == "" || e.Name == "." || e.Name == ".." {
			continue
		}
		out = append(out, dirEntry{name: e.Name, dir: e.IsDir, size: int64(e.Size)})
	}
	return out, nil
}

// readDir40 lists a directory via Search for a File (0x40), the FCB-era one-call-per-entry
// scan the ClassicStack server implements. Two passes (files, then subdirectories), each
// paging the returned NextSeq until the scan ends.
func (f *FS) readDir40(dir string) ([]stdfs.DirEntry, error) {
	searchPath := searchWildcard(dir)
	files, err := f.searchPass(searchPath, proto.NwSearchAttrFiles)
	if err != nil {
		return nil, err
	}
	dirs, err := f.searchPass(searchPath, proto.NwSearchAttrDirs)
	if err != nil {
		return nil, err
	}
	return append(files, dirs...), nil
}

// searchPass pages one Search for a File (0x40) scan (a single search-attribute) to its
// end, returning the entries it yields. A clean end-of-scan returns the entries gathered.
func (f *FS) searchPass(searchPath string, attr uint8) ([]stdfs.DirEntry, error) {
	var out []stdfs.DirEntry
	seq := proto.SearchBefore
	for {
		rep, err := f.sess.command("Search for a File", func(r *proto.Requester) []byte {
			return r.BuildSearchForFile(seq, f.sess.rootDir, attr, searchPath)
		})
		if err != nil {
			if isNoMoreFiles(err) {
				break // clean end of scan
			}
			return nil, translateErr(err)
		}
		e, perr := proto.ParseSearchReply(rep.Body)
		if perr != nil {
			return nil, errMalformed("Search for a File reply")
		}
		out = append(out, dirEntry{name: e.Name, dir: e.IsDir, size: int64(e.Size)})
		seq = e.NextSeq
	}
	return out, nil
}

// searchWildcard builds the NetWare search path for a directory: the directory part in
// wire form joined to the "*.*" wildcard leaf that matches every entry.
func searchWildcard(dir string) string {
	d := wirePath(dir)
	if d == "" {
		return "*.*"
	}
	return d + "\\*.*"
}

// Stat resolves one path. NCP has no single "stat" call; the adapter searches the
// parent directory for the leaf name, trying the files pass then the directories pass
// (NetWare's search-attribute selects one kind per pass). The root path is always a
// directory.
func (f *FS) Stat(p string) (stdfs.FileInfo, error) {
	if strings.Trim(p, "/") == "" {
		return fileInfo{name: "", dir: true}, nil
	}
	_, base := splitPath(p)
	searchPath := wirePath(p) // the full path; the server splits off the leaf as the pattern
	for _, attr := range []uint8{proto.NwSearchAttrFiles, proto.NwSearchAttrDirs} {
		rep, err := f.sess.command("Search for a File", func(r *proto.Requester) []byte {
			return r.BuildSearchForFile(proto.SearchBefore, f.sess.rootDir, attr, searchPath)
		})
		if err != nil {
			if isNoMoreFiles(err) {
				continue // not found in this kind; try the next
			}
			return nil, translateErr(err)
		}
		e, perr := proto.ParseSearchReply(rep.Body)
		if perr != nil {
			return nil, errMalformed("Search for a File reply")
		}
		return fileInfo{name: base, dir: e.IsDir, size: int64(e.Size)}, nil
	}
	return nil, stdfs.ErrNotExist
}

// DiskUsage reports the mounted volume's total/free bytes via Get Volume Info with the
// root dir handle.
func (f *FS) DiskUsage(path string) (total, free uint64, err error) {
	rep, err := f.sess.command("Get Volume Info", func(r *proto.Requester) []byte {
		return r.BuildGetVolumeInfo(f.sess.rootDir)
	})
	if err != nil {
		return 0, 0, translateErr(err)
	}
	vi, perr := proto.ParseVolumeInfo(rep.Body)
	if perr != nil {
		return 0, 0, errMalformed("Get Volume Info reply")
	}
	return vi.TotalBytes(), vi.FreeBytes(), nil
}

// CreateDir creates a directory via Create Directory (0x16/0x0A).
func (f *FS) CreateDir(p string) error {
	_, err := f.sess.command("Create Directory", func(r *proto.Requester) []byte {
		return r.BuildCreateDir(f.sess.rootDir, wirePath(p))
	})
	return translateErr(err)
}

// CreateFile creates a file via Create File (0x43) and returns an open r/w handle.
func (f *FS) CreateFile(p string) (fs.File, error) {
	rep, err := f.sess.command("Create File", func(r *proto.Requester) []byte {
		return r.BuildCreateFile(f.sess.rootDir, wirePath(p))
	})
	if err != nil {
		return nil, translateErr(err)
	}
	o, perr := proto.ParseOpenReply(rep.Body)
	if perr != nil {
		return nil, errMalformed("Create File reply")
	}
	return &fileHandle{fs: f, path: p, handle: o.FileHandle, size: int64(o.Size), writable: true}, nil
}

// OpenFile opens a file's data fork with os flags. O_CREATE creates it first
// (create-if-missing); NetWare's Create overwrites, so a plain O_RDWR uses Open.
func (f *FS) OpenFile(p string, flag int) (fs.File, error) {
	if flag&os.O_CREATE != 0 {
		// Create-if-missing: Open first; on not-found, Create.
		h, err := f.openExisting(p, flag)
		if err == nil {
			return h, nil
		}
		if !errors.Is(err, stdfs.ErrNotExist) {
			return nil, err
		}
		return f.CreateFile(p)
	}
	return f.openExisting(p, flag)
}

// openExisting runs Open File (0x4C) and returns a handle.
func (f *FS) openExisting(p string, flag int) (fs.File, error) {
	rep, err := f.sess.command("Open File", func(r *proto.Requester) []byte {
		return r.BuildOpenFile(f.sess.rootDir, wirePath(p))
	})
	if err != nil {
		return nil, translateErr(err)
	}
	o, perr := proto.ParseOpenReply(rep.Body)
	if perr != nil {
		return nil, errMalformed("Open File reply")
	}
	writable := flag&(os.O_WRONLY|os.O_RDWR) != 0
	h := &fileHandle{fs: f, path: p, handle: o.FileHandle, size: int64(o.Size), writable: writable}
	if flag&os.O_TRUNC != 0 && writable {
		if err := h.Truncate(0); err != nil {
			_ = h.Close()
			return nil, err
		}
	}
	return h, nil
}

// Remove deletes a file (Erase File, 0x44) or an empty directory (Delete Directory,
// 0x16/0x0B). It stats the path to choose.
func (f *FS) Remove(p string) error {
	info, err := f.Stat(p)
	if err != nil {
		return err
	}
	if info.IsDir() {
		_, derr := f.sess.command("Delete Directory", func(r *proto.Requester) []byte {
			return r.BuildDeleteDir(f.sess.rootDir, wirePath(p))
		})
		return translateErr(derr)
	}
	_, eerr := f.sess.command("Erase File", func(r *proto.Requester) []byte {
		return r.BuildEraseFile(f.sess.rootDir, wirePath(p))
	})
	return translateErr(eerr)
}

// Rename renames or moves a path via Rename File (0x45), which resolves both the
// source and destination against the root dir handle (the server handles same-dir
// rename and cross-dir move).
func (f *FS) Rename(oldPath, newPath string) error {
	_, err := f.sess.command("Rename File", func(r *proto.Requester) []byte {
		return r.BuildRenameFile(f.sess.rootDir, wirePath(oldPath), f.sess.rootDir, wirePath(newPath))
	})
	return translateErr(err)
}

// ShortName / MediumName return the path's leaf; the shareFS MetaEngine derives the
// real 8.3/medium name locally, so the client only needs a stable value.
func (f *FS) ShortName(p string) (string, error)  { return leaf(p), nil }
func (f *FS) MediumName(p string) (string, error) { return leaf(p), nil }

// Capabilities reports the mounted volume's capabilities. ChildCount is off (the
// client does not compute it); ReadOnly follows the connection option.
func (f *FS) Capabilities() fs.Capabilities {
	return fs.Capabilities{ReadOnly: f.readOnly}
}

// Close ends the NCP session (fs.FSCloser), so client.Connect's ForkFS.Close tears the
// whole connection down (dealloc handle + DestroyConnection + transport close).
func (f *FS) Close() error {
	err := f.sess.Close()
	if f.onClose != nil {
		f.onClose()
	}
	return err
}

// --- fileHandle: fs.File over an NCP file handle ---

// fileHandle is an open NCP file addressed by its 6-byte server file handle. Positional
// I/O uses Read File (0x48) / Write File (0x49), chunked at the negotiated buffer. NCP
// has no explicit truncate; Truncate is a no-op beyond adjusting the tracked size when
// growing (a create already starts empty, and the AppleDouble backend overwrites whole
// sidecars).
type fileHandle struct {
	fs       *FS
	path     string
	handle   [6]byte
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
		rep, err := h.fs.sess.command("Read File", func(r *proto.Requester) []byte {
			return r.BuildReadFile(h.handle, reqOff, uint16(want))
		})
		if err != nil {
			return total, translateErr(err)
		}
		data, perr := proto.ParseReadReply(rep.Body, reqOff)
		if perr != nil {
			return total, errMalformed("Read File reply")
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
	maxIO := h.fs.sess.MaxPayload()
	total := 0
	for total < len(p) {
		want := len(p) - total
		if want > maxIO {
			want = maxIO
		}
		reqOff := uint32(off) + uint32(total)
		chunk := p[total : total+want]
		_, err := h.fs.sess.command("Write File", func(r *proto.Requester) []byte {
			return r.BuildWriteFile(h.handle, reqOff, chunk)
		})
		if err != nil {
			return total, translateErr(err)
		}
		total += want
	}
	if off+int64(total) > h.size {
		h.size = off + int64(total)
	}
	return total, nil
}

// Truncate has no direct NCP call; the client tracks the intended size. A create opens
// an empty file and the AppleDouble backend overwrites whole sidecars, so shrinking a
// file is not exercised by the client's own flows. Growing updates the tracked size.
func (h *fileHandle) Truncate(size int64) error {
	if h.closed {
		return stdfs.ErrClosed
	}
	if !h.writable {
		return stdfs.ErrPermission
	}
	h.size = size
	return nil
}

func (h *fileHandle) Stat() (stdfs.FileInfo, error) {
	return fileInfo{name: leaf(h.path), size: h.size}, nil
}

// Sync is a no-op: every WriteAt is a synchronous Write File; the server commits on
// close. (A Commit File round trip could be added if a backend needs an explicit flush.)
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
	_, err := h.fs.sess.command("Close File", func(r *proto.Requester) []byte {
		return r.BuildCloseFile(h.handle)
	})
	return translateErr(err)
}

// --- helpers ---

// dirEntry / fileInfo are the minimal fs.DirEntry / fs.FileInfo the adapter returns.
// NCP DOS date/time are not surfaced (the client fs layer tolerates a zero time,
// matching the SMB client).
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
	return fileInfo(e), nil
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

// leaf returns the last '/'-separated element of a volume-relative path.
func leaf(p string) string {
	p = strings.Trim(p, "/")
	if p == "" {
		return ""
	}
	return path.Base(p)
}

// splitPath splits a '/'-separated path into its parent and final element.
func splitPath(p string) (dir, base string) {
	p = strings.Trim(p, "/")
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[:i], p[i+1:]
	}
	return "", p
}

// --- error mapping ---

// ncpError wraps a non-success NCP completion code with the operation name.
type ncpError struct {
	op         string
	completion uint8
}

func (e *ncpError) Error() string {
	return e.op + ": NCP completion 0x" + hexByte(e.completion)
}

// hexByte renders a byte as two lowercase hex digits.
func hexByte(b uint8) string {
	const hexdigits = "0123456789abcdef"
	return string([]byte{hexdigits[b>>4], hexdigits[b&0x0F]})
}

// isNoMoreFiles reports whether err is a directory-scan end (completion 0x9C no-more-
// files or 0xFF not-found — both end a Search scan).
func isNoMoreFiles(err error) bool {
	var ne *ncpError
	if errors.As(err, &ne) {
		return ne.completion == proto.CompletionNoFiles || ne.completion == proto.CompletionNoSuchFile
	}
	return false
}

// translateErr maps an ncpError completion code to the fs sentinel errors the shareFS
// layer and xfer expect (ErrNotExist / ErrPermission), leaving other errors as-is.
func translateErr(err error) error {
	if err == nil {
		return nil
	}
	var ne *ncpError
	if !errors.As(err, &ne) {
		return err
	}
	switch ne.completion {
	case proto.CompletionNoSuchFile, proto.CompletionNoSuchVolume, proto.CompletionNoFiles:
		return stdfs.ErrNotExist
	case proto.CompletionAccessDenied, proto.CompletionConnNotLogged:
		return stdfs.ErrPermission
	default:
		return err
	}
}

// errMalformed reports a reply the parser could not decode.
func errMalformed(what string) error {
	return errors.New("ncp: malformed " + what)
}
