package afp

import (
	"errors"
	"io"
	stdfs "io/fs"
	"os"
	"time"

	aspclient "github.com/ObsoleteMadness/ClassicStack/client/asp"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	proto "github.com/ObsoleteMadness/ClassicStack/core/protocol/afp"
	aspproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/asp"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/atp"
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
	// size is the fork length from FPOpenFork (or the last Truncate/WriteAt).
	// hasSize lets ReadAt cap FPRead to the remaining bytes so a FUSE 4 KiB
	// read of a 100-byte file asks for one ATP packet, not a full quantum.
	size    int64
	hasSize bool
}

// maxForkIO is the largest single FPRead/FPWrite the client issues — one ASP
// quantum (8 × 578). Each Command's ATP bitmap matches this payload so System 7
// replies without EOM still complete (classicstack-web readForkRange).
const maxForkIO = aspproto.QuantumSize

// OpenFork opens a file's data or resource fork via FPOpenFork and returns a handle.
func (f *FS) OpenFork(path string, fork fs.ForkType, flag int) (fs.File, error) {
	access := proto.AccessRead
	writable := flag&(os.O_WRONLY|os.O_RDWR) != 0
	if writable {
		access |= proto.AccessWrite
	}
	ref, size, err := f.openForkRef(path, fork, access)
	if err != nil {
		return nil, err
	}
	ff := &forkFile{fs: f, path: path, fork: fork, forkRef: ref, writable: writable, size: size, hasSize: true}
	// O_TRUNC on a writable open empties the fork first.
	if writable && flag&os.O_TRUNC != 0 {
		if err := ff.Truncate(0); err != nil {
			_ = ff.Close()
			return nil, err
		}
	}
	return ff, nil
}

// openForkRef runs FPOpenFork and returns the fork reference number and the
// length bit requested in the open bitmap (classicstack-web parseOpenFork).
func (f *FS) openForkRef(path string, fork fs.ForkType, access uint16) (uint16, int64, error) {
	// FPOpenFork's bitmap requests parameters for the fork being opened — so ask only for
	// the length bit matching that fork. A strict server (observed: System 7.5 Personal
	// File Sharing) returns kFPBitmapErr (-5004) when the data-fork open also requests the
	// resource-fork-length bit (and vice versa).
	bitmap := uint16(proto.FileBitmapDataForkLen)
	if fork == fs.ResourceFork {
		bitmap = proto.FileBitmapRsrcForkLen
	}
	body, result, err := f.sessCommand("FPOpenFork", path, func(volID uint16) []byte {
		req := proto.OpenForkRequest{
			Resource:   fork == fs.ResourceFork,
			VolID:      volID,
			DirID:      proto.CNIDRoot,
			Bitmap:     bitmap,
			AccessMode: access,
			PathType:   pathType,
			Path:       afpWirePath(path),
		}
		return req.Marshal()
	})
	if err != nil {
		return 0, 0, err
	}
	if result == proto.ErrObjectNotFnd {
		return 0, 0, stdfs.ErrNotExist
	}
	if result != proto.NoErr {
		return 0, 0, afpError("FPOpenFork", result)
	}
	reply, ok := proto.ParseOpenForkReply(body)
	if !ok {
		return 0, 0, errMalformed("FPOpenFork reply")
	}
	n := int64(reply.Params.DataForkLen)
	if fork == fs.ResourceFork {
		n = int64(reply.Params.RsrcForkLen)
	}
	return reply.ForkRefNum, n, nil
}

// ForkLen returns a fork's length via FPGetFileDirParms (data/rsrc fork length bits).
func (f *FS) ForkLen(path string, fork fs.ForkType) (int64, error) {
	bitmap := uint16(proto.FileBitmapDataForkLen)
	if fork == fs.ResourceFork {
		bitmap = proto.FileBitmapRsrcForkLen
	}
	body, result, err := f.sessCommand("FPGetFileDirParms", path, func(volID uint16) []byte {
		req := proto.GetFileDirParmsRequest{
			VolID:      volID,
			DirID:      proto.CNIDRoot,
			FileBitmap: bitmap,
			PathType:   pathType,
			Path:       afpWirePath(path),
		}
		return req.Marshal()
	})
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
	body, result, err := f.sessCommand("FPGetFileDirParms", path, func(volID uint16) []byte {
		req := proto.GetFileDirParmsRequest{
			VolID:      volID,
			DirID:      proto.CNIDRoot,
			FileBitmap: proto.FDBitmapFinderInfo,
			DirBitmap:  proto.FDBitmapFinderInfo,
			PathType:   pathType,
			Path:       afpWirePath(path),
		}
		return req.Marshal()
	})
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
	_, err := f.command("FPSetFileDirParms", path, func(volID uint16) []byte {
		req := proto.SetFinderInfoRequest{
			VolID:      volID,
			DirID:      proto.CNIDRoot,
			PathType:   pathType,
			Path:       afpWirePath(path),
			FinderInfo: info,
		}
		return req.Marshal()
	})
	if err == nil {
		f.cache.invalidate(path)
	}
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

// reopen obtains a fresh fork ref after the ASP session was re-established (old refs
// die with the session).
func (ff *forkFile) reopen() error {
	access := proto.AccessRead
	if ff.writable {
		access |= proto.AccessWrite
	}
	ref, size, err := ff.fs.openForkRef(ff.path, ff.fork, access)
	if err != nil {
		return err
	}
	ff.forkRef = ref
	ff.size = size
	ff.hasSize = true
	return nil
}

// forkReadWant is how many bytes one FPRead should request. Cap to the known
// remaining fork length (from OpenFork) so the ATP bitmap matches the payload
// the server will actually send — a FUSE 4 KiB read of a short file must not
// ask for 8 slots (classicstack-web readForkRange / bitmapForPayload).
func forkReadWant(bufLeft int, off int64, size int64, hasSize bool) int {
	want := bufLeft
	if hasSize {
		remain := size - off
		if remain <= 0 {
			return 0
		}
		if int64(want) > remain {
			want = int(remain)
		}
	}
	if want > maxForkIO {
		want = maxForkIO
	}
	return want
}

func (ff *forkFile) ReadAt(p []byte, off int64) (int, error) {
	if ff.closed {
		return 0, stdfs.ErrClosed
	}
	total := 0
	retried := false
	for total < len(p) {
		want := forkReadWant(len(p)-total, off+int64(total), ff.size, ff.hasSize)
		if want == 0 {
			if total == 0 {
				return 0, io.EOF
			}
			return total, io.EOF
		}
		offset := uint32(off + int64(total))
		body, result, err := ff.fs.sessForkCommand("FPRead", ff.path, atp.MaxRespForPayload(want), func(uint16) []byte {
			return proto.ReadRequest{
				ForkRefNum: ff.forkRef,
				Offset:     offset,
				ReqCount:   uint32(want),
			}.Marshal()
		}, log.Int("off", int64(offset)), log.Int("want", int64(want)), log.Int("forkRef", int64(ff.forkRef)))
		if errors.Is(err, aspclient.ErrSessionClosed) && !retried {
			if rerr := ff.reopen(); rerr != nil {
				return total, err
			}
			retried = true
			continue
		}
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
	retried := false
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
		_, result, err := ff.fs.sessWrite(ff.path, w.Header(), w.Data)
		if errors.Is(err, aspclient.ErrSessionClosed) && !retried {
			if rerr := ff.reopen(); rerr != nil {
				return total, err
			}
			retried = true
			continue
		}
		if err != nil {
			return total, err
		}
		if result != proto.NoErr {
			return total, afpError("FPWrite", result)
		}
		total += want
		if end := off + int64(total); !ff.hasSize || end > ff.size {
			ff.size = end
			ff.hasSize = true
		}
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
	retried := false
	for {
		_, result, err := ff.fs.sessForkCommand("FPSetForkParms", ff.path, 1, func(uint16) []byte {
			return proto.SetForkParmsRequest{
				ForkRefNum: ff.forkRef,
				Bitmap:     bitmap,
				ForkLen:    uint32(size),
			}.Marshal()
		})
		if errors.Is(err, aspclient.ErrSessionClosed) && !retried {
			if rerr := ff.reopen(); rerr != nil {
				return err
			}
			retried = true
			continue
		}
		if err != nil {
			return err
		}
		if result != proto.NoErr {
			return afpError("FPSetForkParms", result)
		}
		ff.size = size
		ff.hasSize = true
		return nil
	}
}

func (ff *forkFile) Stat() (stdfs.FileInfo, error) {
	n := ff.size
	if !ff.hasSize {
		var err error
		n, err = ff.fs.ForkLen(ff.path, ff.fork)
		if err != nil {
			return nil, err
		}
		ff.size = n
		ff.hasSize = true
	}
	_, base := splitPath(ff.path)
	return fileInfo{name: base, size: n, modTime: time.Time{}}, nil
}

func (ff *forkFile) Sync() error {
	if ff.closed {
		return stdfs.ErrClosed
	}
	// FPFlushFork would be ideal; the server flushes on close. A no-op Sync is
	// acceptable for a network fork.
	return nil
}

func (ff *forkFile) Close() error {
	if ff.closed {
		return nil
	}
	ff.closed = true
	// Best-effort: if the session already died, the fork is gone with it. Do not
	// reconnect just to send CloseFork — that would be wasteful and surprising.
	_, result, _, err := ff.fs.sessCommandOnce("FPCloseFork", ff.path, 1, func(uint16) []byte {
		return proto.CloseForkRequest{ForkRefNum: ff.forkRef}.Marshal()
	})
	if errors.Is(err, aspclient.ErrSessionClosed) {
		return nil
	}
	if err != nil {
		return err
	}
	if result != proto.NoErr {
		return afpError("FPCloseFork", result)
	}
	return nil
}
