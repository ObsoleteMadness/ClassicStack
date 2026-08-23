package smb

import (
	"errors"
	"io"
	stdfs "io/fs"
	"os"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"

	protocol "github.com/ObsoleteMadness/ClassicStack/core/protocol/smb"
)

// --- SMB1 file I/O over the §9 share seam: OPEN_ANDX / OPEN / CREATE open a FID
// against the share's data fork; READ[_ANDX] / WRITE[_ANDX] do positional I/O on
// the open File; CLOSE / FLUSH manage the handle. Every path reaches storage only
// through sh.FS() (the data fork is the file itself), so the engine holds no
// AppleDouble / NTFS-stream / Netatalk-EA knowledge — the resource-fork container
// is the AFP side's concern over the same ForkEngine. These mirror the legacy
// service/smb command_file_io.go wire layouts the field validated against
// Win9x/WfW/classic-Mac, re-expressed over the share codec. ---

// openFlagFor maps an SMB AccessMode/DesiredAccess low-3-bit mode to an os open
// flag: 0=read, 1=write, 2=read/write (3=execute → read).
func openFlagFor(accessMode uint16) int {
	switch accessMode & 0x07 {
	case 1:
		return os.O_WRONLY
	case 2:
		return os.O_RDWR
	default:
		return os.O_RDONLY
	}
}

// accessWritable reports whether an SMB AccessMode permits writes.
func accessWritable(accessMode uint16) bool {
	m := accessMode & 0x07
	return m == 1 || m == 2
}

// handleOpenAndX answers SMB_COM_OPEN_ANDX (0x2D), the Win9x open path. The
// OpenFunction word selects create/truncate behaviour; the file is opened (or
// created) against the share's data fork and a FID is granted. Reply WCT=15
// (FID/attrs/size/granted-access/action).
//
// Request words ([MS-CIFS] §2.2.4.41.1, WCT=15): AndXCommand(1) AndXReserved(1)
// AndXOffset(2) Flags(2) AccessMode(2) SearchAttrs(2) FileAttrs(2)
// CreationTime(4) OpenFunction(2) AllocationSize(4) Timeout(4) Reserved(4).
func (s *Service) handleOpenAndX(sess *smbSession, h protocol.Header, req []byte) []byte {
	sh, st := s.treeFor(sess, h)
	if st != statusSuccess {
		return errResponse(h, st)
	}
	words, area, ok := reqBody(req)
	if !ok || len(words) < 30 {
		return errResponse(h, statusObjectNameInvalid)
	}
	desiredAccess := bp.LE16(words[6:8])
	openFunction := bp.LE16(words[16:18])

	store, st := resolvePath(sh, area, h.Flags2)
	if st != statusSuccess {
		return errResponse(h, st)
	}

	// OpenFunction (low nibble = action if exists, high nibble = action if
	// missing): 0x0001 open, 0x0002 truncate, 0x0010 create-if-missing. The
	// omitted-flag case (0) is treated leniently (create allowed), matching the
	// observed legacy clients.
	failIfMissing := openFunction&0x00F0 == 0 && openFunction&0x000F != 0
	truncate := openFunction&0x000F == 0x0002

	flag := openFlagFor(desiredAccess)
	if truncate {
		flag |= os.O_TRUNC
		if flag&(os.O_WRONLY|os.O_RDWR) == 0 {
			flag = os.O_RDWR
		}
	}

	created := false
	f, err := sh.FS().OpenFile(store, flag)
	if err != nil {
		if failIfMissing || !errors.Is(err, stdfs.ErrNotExist) {
			return errResponse(h, mapFSErr(err))
		}
		f, err = sh.FS().CreateFile(store)
		if err != nil {
			return errResponse(h, mapFSErr(err))
		}
		created = true
	}

	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return errResponse(h, statusUnsuccessful)
	}

	writable := created || accessWritable(desiredAccess)
	fid := sess.allocFID(&fileHandle{share: sh, file: f, path: store, writable: writable})

	action := uint16(0x0001) // existed and opened
	if created {
		action = 0x0002 // created
	}
	granted := desiredAccess
	if granted == 0 {
		granted = 0x0002 // default read/write
	}

	w := make([]byte, 30)
	w[0] = protocol.CommandNoAndXCommand
	bp.PutLE16(w[4:6], fid)
	bp.PutLE16(w[6:8], sh.AttrsFor(store, info))
	bp.PutLE32(w[8:12], 0) // LastWriteTime (UTIME) — 0 / unknown
	bp.PutLE32(w[12:16], uint32(info.Size()))
	bp.PutLE16(w[16:18], granted)
	bp.PutLE16(w[18:20], 0) // FileType = disk file
	bp.PutLE16(w[20:22], 0) // DeviceState
	bp.PutLE16(w[22:24], action)
	return reply(h, statusSuccess, 15, w, nil)
}

// handleOpen answers SMB_COM_OPEN (0x02), opening an existing regular file.
// Request words: AccessMode(2) SearchAttrs(2). Reply WCT=7.
func (s *Service) handleOpen(sess *smbSession, h protocol.Header, req []byte) []byte {
	sh, st := s.treeFor(sess, h)
	if st != statusSuccess {
		return errResponse(h, st)
	}
	words, area, ok := reqBody(req)
	if !ok || len(words) < 2 {
		return errResponse(h, statusObjectNameInvalid)
	}
	accessMode := bp.LE16(words[0:2])

	store, st := resolvePath(sh, area, h.Flags2)
	if st != statusSuccess {
		return errResponse(h, st)
	}
	info, err := sh.FS().Stat(store)
	if err != nil {
		return errResponse(h, statusObjectNameNotFound)
	}
	if info.IsDir() {
		return errResponse(h, statusFileIsADirectory)
	}
	f, err := sh.FS().OpenFile(store, openFlagFor(accessMode))
	if err != nil {
		return errResponse(h, mapFSErr(err))
	}
	fid := sess.allocFID(&fileHandle{share: sh, file: f, path: store, writable: accessWritable(accessMode)})

	w := make([]byte, 14)
	bp.PutLE16(w[0:2], fid)
	bp.PutLE16(w[2:4], sh.AttrsFor(store, info))
	bp.PutLE32(w[4:8], 0) // LastModified UTIME
	bp.PutLE32(w[8:12], uint32(info.Size()))
	bp.PutLE16(w[12:14], accessMode&0x07)
	return reply(h, statusSuccess, 7, w, nil)
}

// handleCreate answers SMB_COM_CREATE (0x03): create a new file (or truncate an
// existing one) and return a read/write FID. Request words: FileAttributes(2)
// CreationTime(4). Reply WCT=1 (FID).
func (s *Service) handleCreate(sess *smbSession, h protocol.Header, req []byte) []byte {
	sh, st := s.treeFor(sess, h)
	if st != statusSuccess {
		return errResponse(h, st)
	}
	_, area, ok := reqBody(req)
	if !ok {
		return errResponse(h, statusObjectNameInvalid)
	}
	store, st := resolvePath(sh, area, h.Flags2)
	if st != statusSuccess {
		return errResponse(h, st)
	}
	if info, err := sh.FS().Stat(store); err == nil && info.IsDir() {
		return errResponse(h, statusFileIsADirectory)
	}
	f, err := sh.FS().CreateFile(store)
	if err != nil {
		return errResponse(h, mapFSErr(err))
	}
	fid := sess.allocFID(&fileHandle{share: sh, file: f, path: store, writable: true})

	w := make([]byte, 2)
	bp.PutLE16(w[0:2], fid)
	return reply(h, statusSuccess, 1, w, nil)
}

// handleReadAndX answers SMB_COM_READ_ANDX (0x2E). Request words (WCT=10 or 12):
// AndXCommand(1) AndXReserved(1) AndXOffset(2) FID(2) Offset(4) MaxCount(2)
// MinCount(2) Timeout/MaxCountHigh(4) Remaining(2) [OffsetHigh(4)]. Reply WCT=12
// with the data block at a header-relative DataOffset.
func (s *Service) handleReadAndX(sess *smbSession, h protocol.Header, req []byte) []byte {
	words, _, ok := reqBody(req)
	if !ok || len(words) < 20 {
		return errResponse(h, statusUnsuccessful)
	}
	fid := bp.LE16(words[4:6])
	offset := uint64(bp.LE32(words[6:10]))
	maxCount := int(bp.LE16(words[10:12]))
	if len(words) >= 24 {
		offset |= uint64(bp.LE32(words[20:24])) << 32
	}
	data, st := s.readAt(sess, fid, int64(offset), maxCount)
	if st != statusSuccess {
		return errResponse(h, st)
	}
	return buildReadAndXResponse(h, data)
}

// handleRead answers the CORE SMB_COM_READ (0x0A). Request words (WCT=5): FID(2)
// CountOfBytesToRead(2) ReadOffsetInBytes(4) EstimateOfRemaining(2). Reply WCT=5
// with a BufferFormat-prefixed data area.
func (s *Service) handleRead(sess *smbSession, h protocol.Header, req []byte) []byte {
	words, _, ok := reqBody(req)
	if !ok || len(words) < 10 {
		return errResponse(h, statusUnsuccessful)
	}
	fid := bp.LE16(words[0:2])
	maxCount := int(bp.LE16(words[2:4]))
	offset := int64(bp.LE32(words[4:8]))
	data, st := s.readAt(sess, fid, offset, maxCount)
	if st != statusSuccess {
		return errResponse(h, st)
	}
	w := make([]byte, 10)
	bp.PutLE16(w[0:2], uint16(len(data))) // CountOfBytesReturned
	area := make([]byte, 3+len(data))
	area[0] = 0x01 // SMB_FORMAT_DATA buffer format
	bp.PutLE16(area[1:3], uint16(len(data)))
	copy(area[3:], data)
	return reply(h, statusSuccess, 5, w, area)
}

// readAt reads up to maxCount bytes at offset from the open FID, returning the
// bytes and statusSuccess (a short/at-EOF read returns the bytes available — the
// client detects EOF from a returned count below MaxCount, the SMB convention).
func (s *Service) readAt(sess *smbSession, fid uint16, offset int64, maxCount int) ([]byte, uint32) {
	hnd, ok := sess.fileByFID(fid)
	if !ok || hnd.file == nil {
		return nil, statusInvalidHandle
	}
	if offset < 0 || maxCount < 0 {
		return nil, statusUnsuccessful
	}
	if maxCount == 0 {
		return nil, statusSuccess
	}
	buf := make([]byte, maxCount)
	n, err := hnd.file.ReadAt(buf, offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, statusUnsuccessful
	}
	return buf[:n], statusSuccess
}

// handleWriteAndX answers SMB_COM_WRITE_ANDX (0x2F). Request words (WCT=12 or
// 14): AndXCommand(1) AndXReserved(1) AndXOffset(2) FID(2) Offset(4) Timeout(4)
// WriteMode(2) Remaining(2) DataLengthHigh(2) DataLength(2) DataOffset(2)
// [OffsetHigh(4)]. The data sits at a header-relative DataOffset. A zero-length
// write truncates the file to Offset. Reply WCT=6 (Count + Available=0xFFFF).
func (s *Service) handleWriteAndX(sess *smbSession, h protocol.Header, req []byte) []byte {
	words, _, ok := reqBody(req)
	if !ok || len(words) < 24 {
		return errResponse(h, statusUnsuccessful)
	}
	fid := bp.LE16(words[4:6])
	offset := uint64(bp.LE32(words[6:10]))
	dataLen := int(bp.LE16(words[20:22]))
	dataOff := int(bp.LE16(words[22:24]))
	if len(words) >= 28 {
		offset |= uint64(bp.LE32(words[24:28])) << 32
	}
	// DataOffset is relative to the SMB header (frame) start.
	if dataOff < 0 || dataOff+dataLen > len(req) || dataOff > dataOff+dataLen {
		return errResponse(h, statusUnsuccessful)
	}
	data := req[dataOff : dataOff+dataLen]

	n, st := s.writeAt(sess, fid, int64(offset), data)
	if st != statusSuccess {
		return errResponse(h, st)
	}
	w := make([]byte, 12)
	w[0] = protocol.CommandNoAndXCommand
	bp.PutLE16(w[4:6], uint16(n))
	bp.PutLE16(w[6:8], 0xFFFF) // Available — disk files must report 0xFFFF
	return reply(h, statusSuccess, 6, w, nil)
}

// handleWrite answers the CORE SMB_COM_WRITE (0x0B). Request words (WCT=5): FID(2)
// CountOfBytesToWrite(2) WriteOffsetInBytes(4) EstimateOfRemaining(2). The data
// rides the byte area as BufferFormat(1) DataLength(2) Data[]. A zero count
// truncates to Offset. Reply WCT=1 (Count).
func (s *Service) handleWrite(sess *smbSession, h protocol.Header, req []byte) []byte {
	words, area, ok := reqBody(req)
	if !ok || len(words) < 10 {
		return errResponse(h, statusUnsuccessful)
	}
	fid := bp.LE16(words[0:2])
	count := int(bp.LE16(words[2:4]))
	offset := int64(bp.LE32(words[4:8]))

	var data []byte
	if count > 0 {
		if len(area) < 3 {
			return errResponse(h, statusUnsuccessful)
		}
		data = area[3:]
		if len(data) > count {
			data = data[:count]
		}
	}
	n, st := s.writeAt(sess, fid, offset, data)
	if st != statusSuccess {
		return errResponse(h, st)
	}
	w := make([]byte, 2)
	bp.PutLE16(w[0:2], uint16(n))
	return reply(h, statusSuccess, 1, w, nil)
}

// handleWriteAndClose answers SMB_COM_WRITE_AND_CLOSE (0x2C, LAN Manager 1.0;
// superseded by WRITE_ANDX in later dialects but still issued by OS/2 Warp's
// Workplace Shell — [MS-CIFS] §2.2.4.40, netbeui.pcap 2026-07-13 frame 843). Request
// words (WCT=6 or 12): FID(2) CountOfBytesToWrite(2) WriteOffsetInBytes(4)
// LastWriteTime(4) [Reserved[3] ULONG, 12-word form only, MUST be zero — not
// an EA-list field; WRITE_AND_CLOSE carries no EAs on the wire at any
// WordCount]. Data rides the byte area as Pad(1) Data[CountOfBytesToWrite],
// identical to SMB_COM_WRITE. Behaves as WRITE followed by CLOSE. Reply WCT=1
// (CountOfBytesWritten).
func (s *Service) handleWriteAndClose(sess *smbSession, h protocol.Header, req []byte) []byte {
	words, area, ok := reqBody(req)
	if !ok || len(words) < 12 {
		return errResponse(h, statusUnsuccessful)
	}
	fid := bp.LE16(words[0:2])
	count := int(bp.LE16(words[2:4]))
	offset := int64(bp.LE32(words[4:8]))

	var data []byte
	if count > 0 {
		if len(area) < 1 {
			return errResponse(h, statusUnsuccessful)
		}
		data = area[1:]
		if len(data) > count {
			data = data[:count]
		}
	}
	n, st := s.writeAt(sess, fid, offset, data)
	if st == statusSuccess {
		// [MS-CIFS] §3.3.5.34 specifies WRITE_AND_CLOSE as seek+write only, with
		// no implicit resize — but it predates SetEndOfFile/SetFileSize as a
		// mechanism, and OS/2 Workplace Shell relies on WRITE_AND_CLOSE alone to
		// rewrite its "\WP ROOT. SF" state file: it reopens with OpenFunction
		// 0x0011 (open-existing, no truncate) and issues a single WRITE_AND_CLOSE
		// of the new (possibly shorter) content from offset 0, never sending a
		// separate resize. Treating WRITE_AND_CLOSE as this FID's terminal write
		// and truncating to offset+n at close time matches that expectation;
		// without it, a shorter rewrite left stale trailing bytes from the
		// previous write past the new EOF (netbeui.pcap 2026-07-15, WP ROOT. SF
		// written 383 bytes then 346 bytes on a fresh FID — file stayed 383).
		if hnd, ok := sess.fileByFID(fid); ok && hnd.file != nil {
			_ = hnd.file.Truncate(offset + int64(n))
		}
	}
	sess.closeFID(fid)
	if st != statusSuccess {
		return errResponse(h, st)
	}
	w := make([]byte, 2)
	bp.PutLE16(w[0:2], uint16(n))
	return reply(h, statusSuccess, 1, w, nil)
}

// writeAt writes data at offset to the open FID, returning the byte count and
// status. A zero-length write truncates the file to offset ([MS-CIFS] §2.2.4.12).
// A write to a read-only handle is STATUS_ACCESS_DENIED.
func (s *Service) writeAt(sess *smbSession, fid uint16, offset int64, data []byte) (int, uint32) {
	hnd, ok := sess.fileByFID(fid)
	if !ok || hnd.file == nil {
		return 0, statusInvalidHandle
	}
	if !hnd.writable {
		return 0, statusAccessDenied
	}
	if offset < 0 {
		return 0, statusUnsuccessful
	}
	if len(data) == 0 {
		if err := hnd.file.Truncate(offset); err != nil {
			return 0, mapFSErr(err)
		}
		return 0, statusSuccess
	}
	n, err := hnd.file.WriteAt(data, offset)
	if err != nil {
		return 0, mapFSErr(err)
	}
	return n, statusSuccess
}

// handleClose answers SMB_COM_CLOSE (0x04): release the FID's open handle. Request
// words (WCT=3): FID(2) LastWriteTime(4). Reply WCT=0.
func (s *Service) handleClose(sess *smbSession, h protocol.Header, req []byte) []byte {
	words, _, ok := reqBody(req)
	if !ok || len(words) < 2 {
		return errResponse(h, statusUnsuccessful)
	}
	sess.closeFID(bp.LE16(words[0:2]))
	return successNoData(h)
}

// handleFlush answers SMB_COM_FLUSH (0x05): Sync one open FID, or every open file
// when FID=0xFFFF. Request words (WCT=1): FID(2). A Sync error is treated as a
// no-op (a read-only handle has no buffered writes; the kernel commits on close).
func (s *Service) handleFlush(sess *smbSession, h protocol.Header, req []byte) []byte {
	words, _, ok := reqBody(req)
	if !ok || len(words) < 2 {
		return errResponse(h, statusUnsuccessful)
	}
	fid := bp.LE16(words[0:2])
	if fid == 0xFFFF {
		sess.mu.Lock()
		handles := make([]*fileHandle, 0, len(sess.fids))
		for _, hd := range sess.fids {
			handles = append(handles, hd)
		}
		sess.mu.Unlock()
		for _, hd := range handles {
			if hd.file != nil {
				_ = hd.file.Sync()
			}
		}
		return successNoData(h)
	}
	hnd, ok := sess.fileByFID(fid)
	if !ok || hnd.file == nil {
		return errResponse(h, statusInvalidHandle)
	}
	_ = hnd.file.Sync()
	return successNoData(h)
}

// handleQueryInformation2 answers SMB_COM_QUERY_INFORMATION2 (0x23), the
// LANMAN1.0 FID-based sibling of QUERY_INFORMATION (0x08) — DOS/OS2/Win16
// redirectors query attributes of a file they already hold open by FID instead
// of re-walking its path. Request words (WCT=1): FID(2). Reply WCT=11:
// CreationDate/Time(2+2) LastAccessDate/Time(2+2) LastWriteDate/Time(2+2)
// DataSize(4) AllocationSize(4) Attributes(2) ([LM1.0] "Get File Attributes
// Using Handle"; unimplemented here answered STATUS_NOT_SUPPORTED, seen as
// "Invalid function" against real DOS/Win16 clients — netbeui.pcap frame 1766).
func (s *Service) handleQueryInformation2(sess *smbSession, h protocol.Header, req []byte) []byte {
	words, _, ok := reqBody(req)
	if !ok || len(words) < 2 {
		return errResponse(h, statusUnsuccessful)
	}
	fid := bp.LE16(words[0:2])
	hnd, ok := sess.fileByFID(fid)
	if !ok {
		return errResponse(h, statusInvalidHandle)
	}
	info, err := hnd.share.FS().Stat(hnd.path)
	if err != nil {
		return errResponse(h, mapFSErr(err))
	}
	created := dosTimeDate(info.ModTime())
	w := make([]byte, 22)
	bp.PutLE32(w[0:4], created)  // CreationDate/Time
	bp.PutLE32(w[4:8], created)  // LastAccessDate/Time
	bp.PutLE32(w[8:12], created) // LastWriteDate/Time
	if !info.IsDir() {
		bp.PutLE32(w[12:16], uint32(info.Size())) // DataSize
		bp.PutLE32(w[16:20], uint32(info.Size())) // AllocationSize
	}
	bp.PutLE16(w[20:22], hnd.share.AttrsFor(hnd.path, info))
	return reply(h, statusSuccess, 11, w, nil)
}

// buildReadAndXResponse builds the SMB_COM_READ_ANDX reply (WCT=12): the andx
// terminator + DataLength + a header-relative DataOffset, then a pad to even
// alignment and the data ([MS-CIFS] §2.2.4.42.2).
func buildReadAndXResponse(h protocol.Header, data []byte) []byte {
	rh := responseHeader(h, statusSuccess)
	out := rh.Encode(nil)
	out = append(out, 12) // WCT

	// DataOffset is measured from the SMB header start: header(32) + WCT(1) +
	// words(24) + BCC(2), padded to an even boundary.
	base := protocol.HeaderLen + 1 + 24 + 2
	pad := 0
	if base%2 != 0 {
		pad = 1
	}
	dataOffset := base + pad

	w := make([]byte, 24)
	w[0] = protocol.CommandNoAndXCommand
	bp.PutLE16(w[10:12], uint16(len(data))) // DataLength
	bp.PutLE16(w[12:14], uint16(dataOffset))
	out = append(out, w...)

	bcc := len(data) + pad
	out = append(out, byte(bcc), byte(bcc>>8))
	if pad > 0 {
		out = append(out, 0)
	}
	out = append(out, data...)
	return out
}
