//go:build afp || all

package afp

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/pgodw/omnitalk/netlog"
	"github.com/pgodw/omnitalk/pkg/appledouble"
	"github.com/pgodw/omnitalk/pkg/binutil"
)

func (s *Service) handleOpenFork(req *FPOpenForkReq) (*FPOpenForkRes, int32) {
	parentPath, ok := s.getDIDPath(req.VolumeID, req.DirID)
	if !ok && req.DirID != 0 {
		return &FPOpenForkRes{}, ErrObjectNotFound
	} else if !ok && req.DirID == 0 {
		parentPath, _ = s.getDIDPath(req.VolumeID, CNIDRoot)
	}

	targetPath := parentPath
	if req.Path != "" {
		resolvedPath, errCode := s.resolvePath(parentPath, req.Path, req.PathType)
		if errCode != NoErr {
			return &FPOpenForkRes{}, errCode
		}
		targetPath = resolvedPath
	}

	resolvedPath, info, err := s.statPathWithAppleDoubleFallback(targetPath)
	if err != nil || info == nil || info.IsDir() {
		return &FPOpenForkRes{}, ErrObjectNotFound
	}
	targetPath = resolvedPath

	if req.AccessMode&0x02 != 0 && s.volumeIsReadOnly(req.VolumeID) {
		return &FPOpenForkRes{}, ErrVolLocked
	}

	var handle *forkHandle

	if req.Fork == ForkResource {
		writable := req.AccessMode&0x02 != 0
		m := s.metaFor(req.VolumeID)
		if m == nil {
			handle = &forkHandle{isRsrc: true}
		} else {
			f, info, err := m.OpenResourceFork(targetPath, writable)
			if err != nil {
				// Backend couldn't open/create metadata storage - serve empty fork.
				handle = &forkHandle{isRsrc: true}
			} else {
				handle = &forkHandle{
					file:           f,
					isRsrc:         true,
					rsrcOff:        info.Offset,
					rsrcLen:        info.Length,
					rsrcLenFieldAt: info.LengthFieldOffset,
				}
			}
		}
	} else {
		// Data fork
		backend := s.fsForPath(targetPath)
		if backend == nil {
			return &FPOpenForkRes{}, ErrObjectNotFound
		}
		f, err := backend.OpenFile(targetPath, os.O_RDWR)
		if err != nil && req.AccessMode&0x02 == 0 {
			f, err = backend.OpenFile(targetPath, os.O_RDONLY)
		}
		if err != nil {
			return &FPOpenForkRes{}, ErrObjectNotFound
		}
		handle = &forkHandle{file: f}
	}

	handle.volID = req.VolumeID
	handle.filePath = targetPath

	forkID := s.forks.register(handle)

	forkType := "data"
	if handle.isRsrc {
		forkType = fmt.Sprintf("rsrc(off=%d,len=%d)", handle.rsrcOff, handle.rsrcLen)
	}
	rwMode := "R/W"
	if req.AccessMode&0x02 == 0 {
		rwMode = "R/O"
	}
	netlog.Debug("[AFP] OpenFork forkID=%d %s %s path=%q", forkID, rwMode, forkType, targetPath)

	resData := new(bytes.Buffer)
	s.packFileInfo(resData, req.VolumeID, req.Bitmap, filepath.Dir(targetPath), filepath.Base(targetPath), info, false)

	res := &FPOpenForkRes{
		Bitmap: req.Bitmap,
		ForkID: forkID,
		Data:   resData.Bytes(),
	}

	return res, NoErr
}

func (s *Service) handleCloseFork(req *FPCloseForkReq) (*FPCloseForkRes, int32) {
	handle, ok := s.forks.close(req.OForkRefNum)
	if !ok {
		return &FPCloseForkRes{}, ErrParamErr
	}
	if handle.file != nil {
		handle.file.Close()
	}
	return &FPCloseForkRes{}, NoErr
}

func (s *Service) handleFlush(req *FPFlushReq) (*FPFlushRes, int32) {
	for _, h := range s.forks.snapshot() {
		if h.volID == req.VolumeID && h.file != nil {
			h.file.Sync() //nolint:errcheck
		}
	}
	return &FPFlushRes{}, NoErr
}

func (s *Service) handleFlushFork(req *FPFlushForkReq) (*FPFlushForkRes, int32) {
	handle, ok := s.forks.get(req.OForkRefNum)
	if !ok {
		return &FPFlushForkRes{}, ErrParamErr
	}
	if handle.file != nil {
		handle.file.Sync() //nolint:errcheck
	}
	return &FPFlushForkRes{}, NoErr
}

func (s *Service) handleByteRangeLock(req *FPByteRangeLockReq) (*FPByteRangeLockRes, int32) {
	defer s.forks.lock()()

	handle, ok := s.forks.forks[req.ForkID]
	if !ok {
		return &FPByteRangeLockRes{}, ErrParamErr
	}

	if req.Length == 0 || req.Length < -1 {
		return &FPByteRangeLockRes{}, ErrParamErr
	}
	if req.Unlock && req.FromEnd {
		// Spec: Start/EndFlag is valid only when locking.
		return &FPByteRangeLockRes{}, ErrParamErr
	}

	// Determine fork size for FromEnd adjustment.
	var forkSize int64
	if handle.isRsrc {
		forkSize = handle.rsrcLen
	} else {
		if handle.file == nil {
			return &FPByteRangeLockRes{}, ErrAccessDenied
		}
		st, err := handle.file.Stat()
		if err != nil {
			return &FPByteRangeLockRes{}, ErrAccessDenied
		}
		forkSize = st.Size()
	}

	offset := req.Offset
	if req.FromEnd && !req.Unlock {
		offset += forkSize
	}
	if offset < 0 {
		return &FPByteRangeLockRes{}, ErrParamErr
	}

	lockKey := byteRangeLockKey(handle)

	if req.Unlock {
		for i := range s.forks.locks {
			lk := s.forks.locks[i]
			if lk.lockKey == lockKey && lk.ownerFork == req.ForkID && lk.start == offset && lk.length == req.Length {
				s.forks.locks = append(s.forks.locks[:i], s.forks.locks[i+1:]...)
				return &FPByteRangeLockRes{Offset: offset}, NoErr
			}
		}
		return &FPByteRangeLockRes{}, ErrRangeNotLocked
	}

	for i := range s.forks.locks {
		lk := s.forks.locks[i]
		if lk.lockKey != lockKey {
			continue
		}
		if !byteRangeOverlaps(lk.start, lk.length, offset, req.Length) {
			continue
		}
		if lk.ownerFork == req.ForkID {
			return &FPByteRangeLockRes{}, ErrRangeOverlap
		}
		return &FPByteRangeLockRes{}, ErrLockErr
	}

	if len(s.forks.locks) >= s.forks.maxLocks {
		return &FPByteRangeLockRes{}, ErrNoMoreLocks
	}

	s.forks.locks = append(s.forks.locks, byteRangeLock{
		lockKey:   lockKey,
		ownerFork: req.ForkID,
		start:     offset,
		length:    req.Length,
	})

	return &FPByteRangeLockRes{Offset: offset}, NoErr
}

func byteRangeLockKey(handle *forkHandle) string {
	if handle.isRsrc {
		return "rsrc:" + handle.filePath
	}
	return "data:" + handle.filePath
}

func byteRangeOverlaps(aStart, aLen, bStart, bLen int64) bool {
	aEnd, aOpen := byteRangeEnd(aStart, aLen)
	bEnd, bOpen := byteRangeEnd(bStart, bLen)

	if aOpen && bOpen {
		return true
	}
	if aOpen {
		return aStart < bEnd
	}
	if bOpen {
		return bStart < aEnd
	}
	return aStart < bEnd && bStart < aEnd
}

func byteRangeEnd(start, length int64) (int64, bool) {
	if length == -1 {
		return 0, true
	}
	return start + length, false
}

func (s *Service) handleRead(req *FPReadReq) (*FPReadRes, int32) {
	handle, ok := s.forks.get(req.ForkID)

	if !ok {
		return &FPReadRes{}, ErrParamErr
	}
	if req.ReqCount < 0 || req.Offset < 0 {
		return &FPReadRes{}, ErrParamErr
	}
	if req.ReqCount == 0 {
		return &FPReadRes{Data: nil}, NoErr
	}
	if s.maxReadSize > 0 && req.ReqCount > s.maxReadSize {
		req.ReqCount = s.maxReadSize
	}

	if handle.isRsrc {
		netlog.Debug("[AFP] Read forkID=%d rsrc: rsrcLen=%d req offset=%d count=%d", req.ForkID, handle.rsrcLen, req.Offset, req.ReqCount)
		if handle.file == nil || handle.rsrcLen == 0 || req.Offset >= handle.rsrcLen {
			netlog.Debug("[AFP] Read forkID=%d rsrc: -> ErrEOFErr (offset past end or empty fork)", req.ForkID)
			return &FPReadRes{}, ErrEOFErr
		}
		remaining := handle.rsrcLen - req.Offset
		readLen := int64(req.ReqCount)
		if readLen > remaining {
			readLen = remaining
		}
		buf := make([]byte, readLen)
		n, err := handle.file.ReadAt(buf, handle.rsrcOff+req.Offset)
		if err != nil && err != io.EOF {
			netlog.Debug("[AFP] Read forkID=%d rsrc: ReadAt error: %v", req.ForkID, err)
			return &FPReadRes{}, ErrParamErr
		}
		if n == 0 {
			netlog.Debug("[AFP] Read forkID=%d rsrc: -> ErrEOFErr (n=0)", req.ForkID)
			return &FPReadRes{}, ErrEOFErr
		}
		if int64(n) < int64(req.ReqCount) {
			netlog.Debug("[AFP] Read forkID=%d rsrc: -> %d bytes + ErrEOFErr (partial, requested %d)", req.ForkID, n, req.ReqCount)
			return &FPReadRes{Data: buf[:n]}, ErrEOFErr
		}
		netlog.Debug("[AFP] Read forkID=%d rsrc: -> %d bytes NoErr", req.ForkID, n)
		return &FPReadRes{Data: buf[:n]}, NoErr
	}

	var fileSize int64
	if fi, err := handle.file.Stat(); err == nil {
		fileSize = fi.Size()
	}
	netlog.Debug("[AFP] Read forkID=%d data: fileSize=%d req offset=%d count=%d", req.ForkID, fileSize, req.Offset, req.ReqCount)
	buf := make([]byte, req.ReqCount)
	n, err := handle.file.ReadAt(buf, req.Offset)
	if err != nil && err != io.EOF {
		netlog.Debug("[AFP] Read forkID=%d data: ReadAt error: %v", req.ForkID, err)
		return &FPReadRes{}, ErrParamErr
	}
	if n == 0 {
		netlog.Debug("[AFP] Read forkID=%d data: -> ErrEOFErr (n=0)", req.ForkID)
		return &FPReadRes{}, ErrEOFErr
	}
	if n < req.ReqCount {
		netlog.Debug("[AFP] Read forkID=%d data: -> %d bytes + ErrEOFErr (partial, requested %d)", req.ForkID, n, req.ReqCount)
		return &FPReadRes{Data: buf[:n]}, ErrEOFErr
	}
	netlog.Debug("[AFP] Read forkID=%d data: -> %d bytes NoErr", req.ForkID, n)
	return &FPReadRes{Data: buf[:n]}, NoErr
}

func (s *Service) handleWrite(req *FPWriteReq) (*FPWriteRes, int32) {
	handle, ok := s.forks.get(req.ForkID)

	if !ok {
		return &FPWriteRes{}, ErrParamErr
	}

	if handle.file == nil {
		return &FPWriteRes{}, ErrAccessDenied
	}
	if req.Offset < 0 {
		return &FPWriteRes{}, ErrParamErr
	}

	var writeAt int64
	if handle.isRsrc {
		offset := req.Offset
		if req.FromEnd {
			offset += handle.rsrcLen
		}
		if offset < 0 {
			return &FPWriteRes{}, ErrParamErr
		}
		writeAt = handle.rsrcOff + offset
	} else {
		offset := req.Offset
		if req.FromEnd {
			st, err := handle.file.Stat()
			if err != nil {
				return &FPWriteRes{}, ErrAccessDenied
			}
			offset += st.Size()
		}
		if offset < 0 {
			return &FPWriteRes{}, ErrParamErr
		}
		writeAt = offset
	}

	netlog.Debug("[AFP] Write forkID=%d isRsrc=%t writeAt=%d dataLen=%d", req.ForkID, handle.isRsrc, writeAt, len(req.WriteData))
	_, err := handle.file.WriteAt(req.WriteData, writeAt)
	if err != nil {
		var errno syscall.Errno
		if errors.As(err, &errno) && errno == syscall.ENOSPC {
			netlog.Debug("[AFP] Write forkID=%d: -> ErrDFull", req.ForkID)
			return &FPWriteRes{}, ErrDFull
		}
		if errors.Is(err, fs.ErrPermission) {
			netlog.Debug("[AFP] Write forkID=%d: -> ErrAccessDenied: %v", req.ForkID, err)
			return &FPWriteRes{}, ErrAccessDenied
		}
		netlog.Debug("[AFP] Write forkID=%d: -> ErrParamErr: %v", req.ForkID, err)
		return &FPWriteRes{}, ErrParamErr
	}

	if handle.isRsrc {
		// Compute fork-relative offset used for rsrcLen updates.
		forkOff := req.Offset
		if req.FromEnd {
			forkOff += handle.rsrcLen
		}
		newEnd := forkOff + int64(len(req.WriteData))
		if newEnd > handle.rsrcLen {
			handle.rsrcLen = newEnd
			// Update the resource fork length field in the AppleDouble header.
			lenBuf := make([]byte, 4)
			binary.BigEndian.PutUint32(lenBuf, uint32(handle.rsrcLen))
			handle.file.WriteAt(lenBuf, appledouble.ResourceLenFileOffset)
		}
	}

	lastWritten := req.Offset + int64(len(req.WriteData))
	if req.FromEnd {
		// When writing "from end", LastWritten is the absolute fork offset after write.
		if handle.isRsrc {
			lastWritten = handle.rsrcLen
		} else {
			st, err := handle.file.Stat()
			if err == nil {
				lastWritten = st.Size()
			}
		}
	}
	netlog.Debug("[AFP] Write forkID=%d: -> LastWritten=%d NoErr", req.ForkID, lastWritten)
	return &FPWriteRes{LastWritten: lastWritten}, NoErr
}

// handleGetForkParms returns the same parameter block as FPGetFileDirParms
// for the file backing an open fork (AFP 2.x §5.1.27). It must replace
// DataForkLen / RsrcForkLen with the live values tracked on the fork handle:
// in-flight writes may not yet be reflected in Stat or in the AppleDouble
// header. Packing a partial block crashes Finder ("error type 10").
func (s *Service) handleGetForkParms(req *FPGetForkParmsReq) (*FPGetForkParmsRes, int32) {
	handle, ok := s.forks.get(req.OForkRefNum)
	if !ok {
		return &FPGetForkParmsRes{}, ErrParamErr
	}

	if handle.filePath == "" {
		// No associated file path (shouldn't happen after OpenFork): fall back
		// to the fork-length-only legacy behaviour.
		return &FPGetForkParmsRes{Bitmap: req.Bitmap, Data: packForkLengthsOnly(handle, req.Bitmap)}, NoErr
	}

	backend := s.fsForPath(handle.filePath)
	if backend == nil {
		return &FPGetForkParmsRes{}, ErrObjectNotFound
	}
	info, err := backend.Stat(handle.filePath)
	if err != nil {
		return &FPGetForkParmsRes{}, ErrObjectNotFound
	}
	resData := new(bytes.Buffer)
	parent := filepath.Dir(handle.filePath)
	name := filepath.Base(handle.filePath)
	s.packFileInfo(resData, handle.volID, req.Bitmap, parent, name, info, false)

	body := resData.Bytes()
	overwriteLiveForkLengths(body, req.Bitmap, handle)

	netlog.Debug("[AFP] GetForkParms forkID=%d isRsrc=%t bitmap=0x%04x bodyLen=%d",
		req.OForkRefNum, handle.isRsrc, req.Bitmap, len(body))
	return &FPGetForkParmsRes{Bitmap: req.Bitmap, Data: body}, NoErr
}

// overwriteLiveForkLengths patches the DataForkLen / RsrcForkLen fields of
// an already-packed FileBitmap parameter block with the authoritative lengths
// read from the open fork handle. Walks the bitmap in declared field order to
// land on the right offset; fields not selected by the bitmap occupy zero
// bytes in the body.
func overwriteLiveForkLengths(body []byte, bitmap uint16, handle *forkHandle) {
	off := 0
	if bitmap&FileBitmapAttributes != 0 {
		off += 2
	}
	if bitmap&FileBitmapParentDID != 0 {
		off += 4
	}
	if bitmap&FileBitmapCreateDate != 0 {
		off += 4
	}
	if bitmap&FileBitmapModDate != 0 {
		off += 4
	}
	if bitmap&FileBitmapBackupDate != 0 {
		off += 4
	}
	if bitmap&FileBitmapFinderInfo != 0 {
		off += 32
	}
	if bitmap&FileBitmapLongName != 0 {
		off += 2
	}
	if bitmap&FileBitmapShortName != 0 {
		off += 2
	}
	if bitmap&FileBitmapFileNum != 0 {
		off += 4
	}
	if bitmap&FileBitmapDataForkLen != 0 {
		var dataLen uint32
		if !handle.isRsrc && handle.file != nil {
			if fi, err := handle.file.Stat(); err == nil {
				dataLen = uint32(fi.Size())
			}
		} else {
			dataLen = binary.BigEndian.Uint32(body[off : off+4])
		}
		binary.BigEndian.PutUint32(body[off:off+4], dataLen)
		off += 4
	}
	if bitmap&FileBitmapRsrcForkLen != 0 {
		var rsrcLen uint32
		if handle.isRsrc {
			rsrcLen = uint32(handle.rsrcLen)
		} else {
			rsrcLen = binary.BigEndian.Uint32(body[off : off+4])
		}
		binary.BigEndian.PutUint32(body[off:off+4], rsrcLen)
	}
}

// packForkLengthsOnly emits the legacy fork-length-only reply used when the
// fork handle has no associated file path.
func packForkLengthsOnly(handle *forkHandle, bitmap uint16) []byte {
	resData := new(bytes.Buffer)
	if bitmap&FileBitmapDataForkLen != 0 {
		var dataLen uint32
		if !handle.isRsrc && handle.file != nil {
			if fi, err := handle.file.Stat(); err == nil {
				dataLen = uint32(fi.Size())
			}
		}
		binutil.WriteU32(resData, dataLen)
	}
	if bitmap&FileBitmapRsrcForkLen != 0 {
		var rsrcLen uint32
		if handle.isRsrc {
			rsrcLen = uint32(handle.rsrcLen)
		}
		binutil.WriteU32(resData, rsrcLen)
	}
	return resData.Bytes()
}

func (s *Service) handleSetForkParms(req *FPSetForkParmsReq) (*FPSetForkParmsRes, int32) {
	handle, ok := s.forks.get(req.OForkRefNum)
	if !ok {
		netlog.Debug("[AFP] FPSetForkParms: unknown forkID=%d", req.OForkRefNum)
		return &FPSetForkParmsRes{}, ErrParamErr
	}
	if s.volumeIsReadOnly(handle.volID) {
		return &FPSetForkParmsRes{}, ErrVolLocked
	}
	// Per AFP 2.x section 5.1.31: Bitmap should have exactly one fork-length bit set,
	// and it must correspond to the open fork type. The Fork Length value
	// always occupies the same 4 bytes; both fields in the struct decode from
	// bytes 6..10, so whichever bit is set carries the same value.
	if req.Bitmap&(FileBitmapDataForkLen|FileBitmapRsrcForkLen) == 0 {
		return &FPSetForkParmsRes{}, ErrBitmapErr
	}
	var newLen int64
	if req.Bitmap&FileBitmapDataForkLen != 0 {
		newLen = req.DataForkLen
	} else {
		newLen = req.RsrcForkLen
	}

	if !handle.isRsrc {
		if handle.file == nil {
			return &FPSetForkParmsRes{}, ErrParamErr
		}
		if err := handle.file.Truncate(newLen); err != nil {
			netlog.Debug("[AFP] FPSetForkParms: truncate data fork to %d: %v", newLen, err)
			return &FPSetForkParmsRes{}, ErrMiscErr
		}
		netlog.Debug("[AFP] FPSetForkParms forkID=%d data newLen=%d", req.OForkRefNum, newLen)
		return &FPSetForkParmsRes{}, NoErr
	}

	// Resource fork: truncate the AppleDouble sidecar and update the entry's length field.
	if handle.file == nil {
		// Empty-rsrc handle (no sidecar was opened). Accept no-op if newLen==0.
		netlog.Debug("[AFP] FPSetForkParms forkID=%d rsrc (empty handle) newLen=%d", req.OForkRefNum, newLen)
		if newLen == 0 {
			handle.rsrcLen = 0
			return &FPSetForkParmsRes{}, NoErr
		}
		return &FPSetForkParmsRes{}, ErrMiscErr
	}
	lenFieldAt := handle.rsrcLenFieldAt
	m := s.metaFor(handle.volID)
	if m == nil {
		return &FPSetForkParmsRes{}, ErrMiscErr
	}
	if err := m.TruncateResourceFork(handle.file, ResourceForkInfo{
		Offset:            handle.rsrcOff,
		Length:            handle.rsrcLen,
		LengthFieldOffset: lenFieldAt,
	}, newLen); err != nil {
		netlog.Debug("[AFP] FPSetForkParms: truncate rsrc fork to %d: %v", newLen, err)
		return &FPSetForkParmsRes{}, ErrMiscErr
	}
	handle.rsrcLen = newLen
	netlog.Debug("[AFP] FPSetForkParms forkID=%d rsrc newLen=%d rsrcOff=%d lenFieldAt=%d", req.OForkRefNum, newLen, handle.rsrcOff, lenFieldAt)
	return &FPSetForkParmsRes{}, NoErr
}

// initForkMetadata picks between an injected single ForkMetadataBackend
// (used by tests) and the per-volume map populated by installAppleDoubleBackend
// during volume construction.
func (s *Service) initForkMetadata(options Options) {
	if options.ForkMetadataBackend != nil {
		s.meta = options.ForkMetadataBackend
		return
	}
	s.metas = make(map[uint16]ForkMetadataBackend)
}
