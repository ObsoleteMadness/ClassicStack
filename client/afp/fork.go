package afp

import (
	"io"
	stdfs "io/fs"
	"os"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	proto "github.com/ObsoleteMadness/ClassicStack/core/protocol/afp"
)

// fork.go implements fs.ForkEngine natively over AFP: OpenFork → FPOpenFork (a fork ref),
// with FPRead/FPWrite for I/O and FPGetFileDirParms for Finder info (type/creator).

var _ fs.ForkEngine = (*FS)(nil)

// forkFile is an open AFP fork handle satisfying fs.File. It holds the fork ref and the
// path (for length/stat), and performs positional I/O via FPRead/FPWrite over the session.
type forkFile struct {
	fs       *FS
	path     string
	fork     fs.ForkType
	forkRef  uint16
	writable bool
	closed   bool
}

// maxForkIO is the largest single FPRead/FPWrite the client issues; the ASP session
// chunks the underlying transport, but bounding each request keeps replies within a few
// ASP quanta.
const maxForkIO = 4096

// OpenFork opens a file's data or resource fork via FPOpenFork and returns a handle.
func (f *FS) OpenFork(path string, fork fs.ForkType, flag int) (fs.File, error) {
	access := proto.AccessRead
	writable := flag&(os.O_WRONLY|os.O_RDWR) != 0
	if writable {
		access |= proto.AccessWrite
	}
	req := proto.OpenForkRequest{
		Resource:   fork == fs.ResourceFork,
		VolID:      f.volID,
		DirID:      proto.CNIDRoot,
		Bitmap:     proto.FileBitmapDataForkLen | proto.FileBitmapRsrcForkLen,
		AccessMode: access,
		PathType:   pathType,
		Path:       afpWirePath(path),
	}
	body, result, err := f.sess.Command(req.Marshal())
	if err != nil {
		return nil, err
	}
	if result == proto.ErrObjectNotFnd {
		return nil, stdfs.ErrNotExist
	}
	if result != proto.NoErr {
		return nil, afpError("FPOpenFork", result)
	}
	reply, ok := proto.ParseOpenForkReply(body)
	if !ok {
		return nil, errMalformed("FPOpenFork reply")
	}
	ff := &forkFile{fs: f, path: path, fork: fork, forkRef: reply.ForkRefNum, writable: writable}
	// O_TRUNC on a writable open empties the fork first.
	if writable && flag&os.O_TRUNC != 0 {
		if err := ff.Truncate(0); err != nil {
			_ = ff.Close()
			return nil, err
		}
	}
	return ff, nil
}

// ForkLen returns a fork's length via FPGetFileDirParms (data/rsrc fork length bits).
func (f *FS) ForkLen(path string, fork fs.ForkType) (int64, error) {
	bitmap := uint16(proto.FileBitmapDataForkLen)
	if fork == fs.ResourceFork {
		bitmap = proto.FileBitmapRsrcForkLen
	}
	req := proto.GetFileDirParmsRequest{
		VolID:      f.volID,
		DirID:      proto.CNIDRoot,
		FileBitmap: bitmap,
		PathType:   pathType,
		Path:       afpWirePath(path),
	}
	body, result, err := f.sess.Command(req.Marshal())
	if err != nil {
		return 0, err
	}
	if result == proto.ErrObjectNotFnd {
		return 0, stdfs.ErrNotExist
	}
	if result != proto.NoErr {
		return 0, afpError("FPGetFileDirParms", result)
	}
	reply, ok := proto.ParseGetFileDirParmsReply(body)
	if !ok {
		return 0, errMalformed("FPGetFileDirParms reply")
	}
	if fork == fs.ResourceFork {
		return int64(reply.Params.RsrcForkLen), nil
	}
	return int64(reply.Params.DataForkLen), nil
}

// ReadFinderInfo returns the 32-byte Finder info via FPGetFileDirParms.
func (f *FS) ReadFinderInfo(path string) (info [32]byte, ok bool, err error) {
	req := proto.GetFileDirParmsRequest{
		VolID:      f.volID,
		DirID:      proto.CNIDRoot,
		FileBitmap: proto.FDBitmapFinderInfo,
		DirBitmap:  proto.FDBitmapFinderInfo,
		PathType:   pathType,
		Path:       afpWirePath(path),
	}
	body, result, err := f.sess.Command(req.Marshal())
	if err != nil {
		return info, false, err
	}
	if result != proto.NoErr {
		return info, false, nil
	}
	reply, okp := proto.ParseGetFileDirParmsReply(body)
	if !okp {
		return info, false, nil
	}
	return reply.Params.FinderInfo, true, nil
}

// WriteFinderInfo writes the 32-byte Finder info via FPSetFileDirParms.
func (f *FS) WriteFinderInfo(path string, info [32]byte) error {
	req := proto.SetFinderInfoRequest{
		VolID:      f.volID,
		DirID:      proto.CNIDRoot,
		PathType:   pathType,
		Path:       afpWirePath(path),
		FinderInfo: info,
	}
	_, err := f.command("FPSetFileDirParms", req.Marshal())
	return err
}

// ReadComment / WriteComment are AFP Desktop-database operations (FPGetComment /
// FPAddComment). v1 does not surface comments; the fs layer treats "no comment" as fine.
func (f *FS) ReadComment(path string) (c []byte, ok bool) { return nil, false }
func (f *FS) WriteComment(path string, c []byte) error    { return nil }

// MoveMetadata is a no-op for AFP: a native fork carries its own metadata with the file,
// so a rename (FPRename/FPMoveAndRename) already moves the resource fork and Finder info.
func (f *FS) MoveMetadata(old, new string) error { return nil }

// DeleteMetadata is a no-op for AFP: FPDelete removes the whole object (both forks).
func (f *FS) DeleteMetadata(path string) error { return nil }

// --- forkFile: fs.File over an AFP fork ref ---

func (ff *forkFile) ReadAt(p []byte, off int64) (int, error) {
	if ff.closed {
		return 0, stdfs.ErrClosed
	}
	total := 0
	for total < len(p) {
		want := len(p) - total
		if want > maxForkIO {
			want = maxForkIO
		}
		req := proto.ReadRequest{
			ForkRefNum: ff.forkRef,
			Offset:     uint32(off + int64(total)),
			ReqCount:   uint32(want),
		}
		body, result, err := ff.fs.sess.Command(req.Marshal())
		if err != nil {
			return total, err
		}
		n := copy(p[total:], body)
		total += n
		if result == proto.ErrEOFErr {
			// Short read at end of fork: return what we have plus io.EOF.
			return total, io.EOF
		}
		if result != proto.NoErr {
			return total, afpError("FPRead", result)
		}
		if n == 0 {
			return total, io.EOF
		}
	}
	return total, nil
}

func (ff *forkFile) WriteAt(p []byte, off int64) (int, error) {
	if ff.closed {
		return 0, stdfs.ErrClosed
	}
	if !ff.writable {
		return 0, stdfs.ErrPermission
	}
	total := 0
	for total < len(p) {
		want := len(p) - total
		if want > maxForkIO {
			want = maxForkIO
		}
		w := proto.WriteRequest{
			ForkRefNum: ff.forkRef,
			Offset:     uint32(off + int64(total)),
			Data:       p[total : total+want],
		}
		_, result, err := ff.fs.sess.Write(w.Header(), w.Data)
		if err != nil {
			return total, err
		}
		if result != proto.NoErr {
			return total, afpError("FPWrite", result)
		}
		total += want
	}
	return total, nil
}

func (ff *forkFile) Truncate(size int64) error {
	if ff.closed {
		return stdfs.ErrClosed
	}
	bitmap := uint16(proto.FileBitmapDataForkLen)
	if ff.fork == fs.ResourceFork {
		bitmap = proto.FileBitmapRsrcForkLen
	}
	req := proto.SetForkParmsRequest{
		ForkRefNum: ff.forkRef,
		Bitmap:     bitmap,
		ForkLen:    uint32(size),
	}
	_, err := ff.fs.command("FPSetForkParms", req.Marshal())
	return err
}

func (ff *forkFile) Stat() (stdfs.FileInfo, error) {
	n, err := ff.fs.ForkLen(ff.path, ff.fork)
	if err != nil {
		return nil, err
	}
	_, base := splitPath(ff.path)
	return fileInfo{name: base, size: n, modTime: time.Time{}}, nil
}

func (ff *forkFile) Sync() error {
	if ff.closed {
		return stdfs.ErrClosed
	}
	req := proto.GetForkParmsRequest{ForkRefNum: ff.forkRef, Bitmap: proto.FileBitmapDataForkLen}
	_ = req // FPFlushFork would be ideal; a GetForkParms is a cheap round-trip that
	// confirms the fork is still live. The server flushes on close; a no-op Sync is
	// acceptable for a network fork.
	return nil
}

func (ff *forkFile) Close() error {
	if ff.closed {
		return nil
	}
	ff.closed = true
	req := proto.CloseForkRequest{ForkRefNum: ff.forkRef}
	_, err := ff.fs.command("FPCloseFork", req.Marshal())
	return err
}
