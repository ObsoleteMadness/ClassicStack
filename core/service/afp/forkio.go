package afp

import (
	"errors"
	"io"
	stdfs "io/fs"
	"os"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// --- fork I/O (FPOpenFork / FPRead / FPWrite / FPCloseFork / FPGetForkParms;
// Inside Macintosh: Networking, AFP 2.x §5 "Fork access"). These reach storage
// only through the fork engine (v.FS().OpenFork) and positional File I/O, so the
// spine stays free of AppleDouble / NTFS-stream / Netatalk-EA knowledge: the data
// fork is the file itself, the resource fork is whatever container the share's
// fork backend presents, identically shaped here. ---

// Fork-type byte for FPOpenFork (Inside Macintosh: Networking, "OpenFork"). The
// high bit selects the resource fork; clear selects the data fork.
const (
	forkFlagData     uint8 = 0x00
	forkFlagResource uint8 = 0x80
)

// FPOpenFork access-mode bits (AFP 2.x "OpenFork access mode"). Only the
// read/write intent matters to the spine; deny-mode bits are accepted but not
// enforced (single-user-equivalent compatibility server).
const (
	accessRead  uint16 = 0x01
	accessWrite uint16 = 0x02
)

// fromEndFlag is the high bit of the FPRead/FPWrite flag byte: the offset is
// measured from the end of the fork rather than the start.
const fromEndFlag uint8 = 0x80

// afpOpenFork opens a file's data or resource fork and returns a fork reference.
//
// Request: cmd(1) flag(1) volID(2) dirID(4) bitmap(2) accessMode(2) pathType(1)
//
//	pathname...
//
// (flag bit 0x80 → resource fork.) Reply: bitmap(2) forkRefNum(2) <file params>.
// The file-params block mirrors FPGetFileDirParms; this spine packs the spine's
// file-bitmap subset (LongName / DataForkLen) so the reply is self-consistent
// with the returned bitmap.
func (s *Service) afpOpenFork(a *afpSession, block []byte) ([]byte, int32) {
	if len(block) < 13 {
		return nil, afpErrParamErr
	}
	forkByte := block[1]
	vol, ok := a.openVols[be16(block[2:4])]
	if !ok {
		return nil, afpErrParamErr
	}
	bitmap := be16(block[8:10])
	accessMode := be16(block[10:12])
	pathType := block[12]
	store, code := resolveBlockPath(vol, block, 13, pathType)
	if code != afpNoErr {
		return nil, code
	}

	info, err := vol.Stat(store)
	if err != nil {
		return nil, mapStatErr(err)
	}
	if info.IsDir() {
		return nil, afpErrObjectNotFnd // a fork can only be opened on a file
	}

	fork := fs.DataFork
	if forkByte&forkFlagResource != 0 {
		fork = fs.ResourceFork
	}
	writable := accessMode&accessWrite != 0
	flag := os.O_RDONLY
	if writable {
		flag = os.O_RDWR
	}

	f, err := vol.FS().OpenFork(store, fork, flag)
	if err != nil {
		// A write open that the backend rejects (e.g. read-only fork container)
		// falls back to read-only so a Finder "get info" still succeeds; a read
		// open that fails is a real not-found / access error.
		if writable {
			if rf, rerr := vol.FS().OpenFork(store, fork, os.O_RDONLY); rerr == nil {
				f, writable = rf, false
			} else {
				return nil, mapForkOpenErr(err)
			}
		} else {
			return nil, mapForkOpenErr(err)
		}
	}

	ref, ok := a.forks.open(&forkHandle{vol: vol, file: f, path: store, fork: fork, writable: writable})
	if !ok {
		_ = f.Close()
		return nil, afpErrMiscErr // fork-ref space exhausted (kFPTooManyFilesOpen)
	}

	out := make([]byte, 0, 32)
	out = putBE16(out, bitmap)
	out = putBE16(out, ref)
	out = packEntryParams(out, vol, store, info, bitmap, pathType)
	return out, afpNoErr
}

// afpRead reads from an open fork.
//
// Request: cmd(1) pad(1) forkRefNum(2) offset(4) reqCount(4) [newLineMask(1)
//
//	newLineChar(1)].
//
// Reply: the fork bytes, raw. A short read (fewer bytes than requested, including
// a read starting at or past EOF) returns the bytes read with result kFPEOFErr,
// the convention the .XPP driver and Finder expect (legacy parity).
func (s *Service) afpRead(a *afpSession, block []byte) ([]byte, int32) {
	if len(block) < 12 {
		return nil, afpErrParamErr
	}
	h, ok := a.forks.get(be16(block[2:4]))
	if !ok {
		return nil, afpErrParamErr
	}
	offset := int64(int32(be32(block[4:8])))
	reqCount := int64(int32(be32(block[8:12])))
	if offset < 0 || reqCount < 0 {
		return nil, afpErrParamErr
	}
	if reqCount == 0 {
		return nil, afpNoErr
	}

	buf := make([]byte, reqCount)
	n, err := h.file.ReadAt(buf, offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, afpErrParamErr
	}
	if n == 0 {
		return nil, afpErrEOFErr // nothing available at/after offset
	}
	if int64(n) < reqCount {
		return buf[:n], afpErrEOFErr // partial read: bytes + EOF
	}
	return buf[:n], afpNoErr
}

// afpWrite writes to an open fork.
//
// Request: cmd(1) flag(1) forkRefNum(2) offset(4) reqCount(4) data...
// (flag bit 0x80 → offset measured from end of fork.)
// Reply: lastWritten(4) — the fork offset one past the last byte written.
func (s *Service) afpWrite(a *afpSession, block []byte) ([]byte, int32) {
	if len(block) < 12 {
		return nil, afpErrParamErr
	}
	h, ok := a.forks.get(be16(block[2:4]))
	if !ok {
		return nil, afpErrParamErr
	}
	if !h.writable {
		return nil, afpErrAccessDenied
	}
	fromEnd := block[1]&fromEndFlag != 0
	offset := int64(int32(be32(block[4:8])))
	reqCount := int64(int32(be32(block[8:12])))
	if reqCount < 0 {
		return nil, afpErrParamErr
	}
	data := block[12:]
	if int64(len(data)) > reqCount {
		data = data[:reqCount]
	}

	if fromEnd {
		n, err := h.vol.ForkLen(h.path, h.fork)
		if err != nil {
			return nil, afpErrMiscErr
		}
		offset += n
	}
	if offset < 0 {
		return nil, afpErrParamErr
	}

	if _, err := h.file.WriteAt(data, offset); err != nil {
		return nil, mapWriteErr(err)
	}

	lastWritten := offset + int64(len(data))
	out := putBE32(make([]byte, 0, 4), uint32(int32(lastWritten)))
	return out, afpNoErr
}

// afpCloseFork closes an open fork and releases its reference.
//
// Request: cmd(1) pad(1) forkRefNum(2). Reply: empty.
func (s *Service) afpCloseFork(a *afpSession, block []byte) ([]byte, int32) {
	if len(block) < 4 {
		return nil, afpErrParamErr
	}
	h, ok := a.forks.close(be16(block[2:4]))
	if !ok {
		return nil, afpErrParamErr
	}
	if h.file != nil {
		_ = h.file.Close()
	}
	return nil, afpNoErr
}

// afpFlush flushes every open fork on a volume (FPFlush is whole-volume).
//
// Request: cmd(1) pad(1) volID(2). Reply: empty. Best-effort: a fork that can't
// sync (e.g. a read-only handle) is skipped rather than failing the call.
func (s *Service) afpFlush(a *afpSession, block []byte) ([]byte, int32) {
	if len(block) < 4 {
		return nil, afpErrParamErr
	}
	vol, ok := a.openVols[be16(block[2:4])]
	if !ok {
		return nil, afpErrParamErr
	}
	a.forks.mu.Lock()
	handles := make([]*forkHandle, 0, len(a.forks.byRef))
	for _, h := range a.forks.byRef {
		if h.vol == vol {
			handles = append(handles, h)
		}
	}
	a.forks.mu.Unlock()
	for _, h := range handles {
		if h.file != nil {
			_ = h.file.Sync()
		}
	}
	return nil, afpNoErr
}

// afpFlushFork flushes one open fork.
//
// Request: cmd(1) pad(1) forkRefNum(2). Reply: empty.
func (s *Service) afpFlushFork(a *afpSession, block []byte) ([]byte, int32) {
	if len(block) < 4 {
		return nil, afpErrParamErr
	}
	h, ok := a.forks.get(be16(block[2:4]))
	if !ok {
		return nil, afpErrParamErr
	}
	if h.file != nil {
		_ = h.file.Sync()
	}
	return nil, afpNoErr
}

// afpGetForkParms returns the file parameters for the file backing an open fork,
// with the fork lengths read live from the fork engine (an in-flight write may
// not yet be reflected by a stale Stat).
//
// Request: cmd(1) pad(1) forkRefNum(2) bitmap(2).
// Reply: bitmap(2) <file params>.
func (s *Service) afpGetForkParms(a *afpSession, block []byte) ([]byte, int32) {
	if len(block) < 6 {
		return nil, afpErrParamErr
	}
	h, ok := a.forks.get(be16(block[2:4]))
	if !ok {
		return nil, afpErrParamErr
	}
	bitmap := be16(block[4:6])
	info, err := h.vol.Stat(h.path)
	if err != nil {
		return nil, mapStatErr(err)
	}
	out := make([]byte, 0, 32)
	out = putBE16(out, bitmap)
	// packEntryParams reads DataForkLen through the fork engine, so the length is
	// already authoritative; pathType 0 keeps any LongName store-native (the only
	// charset-free choice when there is no request path-type byte).
	out = packEntryParams(out, h.vol, h.path, info, bitmap, 0)
	return out, afpNoErr
}

// --- error mapping ---

// mapForkOpenErr maps an OpenFork failure to an AFP result code.
func mapForkOpenErr(err error) int32 {
	switch {
	case isNotExist(err):
		return afpErrObjectNotFnd
	case errors.Is(err, stdfs.ErrPermission):
		return afpErrAccessDenied
	default:
		return afpErrMiscErr
	}
}

// mapWriteErr maps a fork WriteAt failure to an AFP result code. Disk-full
// (ENOSPC) is a platform-specific errno the OS adapter layer can refine into
// kFPDiskFull; core stays OS-agnostic and reports permission vs. generic param
// errors only.
func mapWriteErr(err error) int32 {
	if errors.Is(err, stdfs.ErrPermission) {
		return afpErrAccessDenied
	}
	return afpErrParamErr
}
