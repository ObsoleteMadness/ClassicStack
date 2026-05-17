package smb

import (
	"encoding/binary"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/pkg/vfs"
)

// openFlagFromAccess maps SMB AccessMode (low 3 bits) to an os.OpenFile flag.
// 0=read, 1=write, 2=read/write, 3=execute (treated as read).
func openFlagFromAccess(accessMode uint16) int {
	switch accessMode & 0x07 {
	case 1:
		return os.O_WRONLY
	case 2:
		return os.O_RDWR
	default:
		return os.O_RDONLY
	}
}

// accessIsWritable reports whether an SMB AccessMode permits writes.
func accessIsWritable(accessMode uint16) bool {
	mode := accessMode & 0x07
	return mode == 1 || mode == 2
}

func (s *Service) closeFID(conn *connState, fid uint16) {
	conn.mu.Lock()
	defer conn.mu.Unlock()
	s.closeFIDLocked(conn, fid)
}

func (s *Service) closeFIDLocked(conn *connState, fid uint16) {
	handle, ok := conn.fids[fid]
	if ok {
		if handle != nil && handle.file != nil {
			_ = handle.file.Close()
		}
		s.releaseLocksForFIDLocked(conn, fid)
		delete(conn.fids, fid)
	}
}

func (s *Service) handleOpenAndX(req []byte, conn *connState) []byte {
	if len(req) < smbHeaderLen+15 {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	tid := binary.LittleEndian.Uint16(req[smbOffTID : smbOffTID+2])

	conn.mu.Lock()
	slot, ok := conn.tids[tid]
	conn.mu.Unlock()
	if !ok {
		return buildSMBErrorResponse(req, smbStatusBadTID)
	}

	s.mu.Lock()
	fs, ok := s.shareFSes[slot.shareIdx]
	s.mu.Unlock()
	if !ok || fs == nil {
		return buildSMBErrorResponse(req, smbStatusBadTID)
	}
	rootPath := s.shareRootPath(slot.shareIdx)

	// Parse request
	wct := int(req[smbHeaderLen])
	if wct < 15 {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	// SMB_COM_OPEN_ANDX request words per [MS-CIFS] 2.2.4.41.1 (WCT=15):
	//   AndXCommand(1) AndXReserved(1) AndXOffset(2) Flags(2)
	//   AccessMode(2)  SearchAttrs(2)  FileAttrs(2)  CreationTime(4)
	//   OpenFunction(2) AllocationSize(4) Timeout(4) Reserved(4)
	w := req[smbHeaderLen+1:]
	_ = binary.LittleEndian.Uint16(w[0:2])     // AndXCommand+Reserved
	_ = binary.LittleEndian.Uint16(w[2:4])     // AndXOffset
	_ = binary.LittleEndian.Uint16(w[4:6])     // Flags
	desiredAccess := binary.LittleEndian.Uint16(w[6:8])
	_ = binary.LittleEndian.Uint16(w[8:10])    // SearchAttrs
	fileAttrs := binary.LittleEndian.Uint16(w[10:12])
	_ = binary.LittleEndian.Uint32(w[12:16])   // CreationTime
	openFunction := binary.LittleEndian.Uint16(w[16:18])

	path, ok := parseSMBPath(req)
	if !ok || path == "" {
		return buildSMBErrorResponse(req, 0xC000007F) // STATUS_OBJECT_NAME_NOT_FOUND
	}

	requestedPath := strings.TrimSpace(path)
	createPath := smbJoinPath(rootPath, requestedPath)
	openPath := createPath
	if resolved, err := resolveExistingPath(fs, rootPath, requestedPath); err == nil {
		openPath = resolved
	}

	// Determine open mode
	var file vfs.File
	var err error
	created := false

	// OPEN_FUNCTION (low nibble) — action if file exists:
	//   0x0001 = open existing  0x0002 = truncate to zero
	// (high nibble) — action if file does not exist:
	//   0x0010 = create
	// We treat the omitted-flag case (openFunction == 0) leniently and
	// allow creation when missing, matching observed legacy clients.
	failIfMissing := (openFunction & 0x00F0) == 0x0000 && (openFunction & 0x000F) != 0x0000
	truncateIfExists := (openFunction & 0x000F) == 0x0002

	openFlag := openFlagFromAccess(desiredAccess)
	if truncateIfExists {
		openFlag |= os.O_TRUNC
		if openFlag&(os.O_WRONLY|os.O_RDWR) == 0 {
			openFlag = (openFlag &^ os.O_RDONLY) | os.O_RDWR
		}
	}

	// Try to open existing file / create new
	activePath := openPath
	file, err = fs.OpenFile(openPath, openFlag)
	if err != nil && !failIfMissing {
		activePath = createPath
		file, err = fs.CreateFile(createPath)
		if err == nil {
			created = true
		}
	}

	if err != nil {
		return buildSMBErrorResponse(req, 0xC000007F) // STATUS_OBJECT_NAME_NOT_FOUND
	}

	// Get file info
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	// Allocate FID
	conn.mu.Lock()
	conn.nextFID++
	fid := conn.nextFID
	if fid == 0 {
		conn.nextFID++
		fid = conn.nextFID
	}
	conn.fids[fid] = &fileHandle{
		file:     file,
		path:     activePath,
		writable: created || accessIsWritable(desiredAccess),
	}
	conn.mu.Unlock()

	grantedAccess := desiredAccess
	if grantedAccess == 0 {
		grantedAccess = 0x0002 // sensible default: read/write
	}
	action := uint16(0x0001) // existed and opened
	if created {
		action = 0x0002 // created
	}

	return buildOpenAndXResponse(req, fid, info, fileAttrs, grantedAccess, action)
}

// handleReadAndX (0x2E) reads data from an open file.
func (s *Service) handleRead(req []byte, conn *connState) []byte {
	if len(req) < smbHeaderLen+11 {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	wct := int(req[smbHeaderLen])
	if wct < 5 {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	w := req[smbHeaderLen+1:]
	fid := binary.LittleEndian.Uint16(w[0:2])
	maxCount := binary.LittleEndian.Uint16(w[2:4])
	offset := binary.LittleEndian.Uint32(w[4:8])

	data, ok := readBytesFromHandle(conn, fid, int64(offset), maxCount)
	if !ok {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	return buildReadResponse(req, data)
}

func (s *Service) handleReadAndX(req []byte, conn *connState) []byte {
	if len(req) < smbHeaderLen+11 {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	// SMB_COM_READ_ANDX request words per [MS-CIFS] 2.2.4.42.1 (WCT=10 or 12):
	//   AndXCommand(1) AndXReserved(1) AndXOffset(2)
	//   FID(2) Offset(4) MaxCount(2) MinCount(2)
	//   Timeout/MaxCountHigh(4) Remaining(2) [OffsetHigh(4)]
	wct := int(req[smbHeaderLen])
	if wct < 10 {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	w := req[smbHeaderLen+1:]
	fid := binary.LittleEndian.Uint16(w[4:6])
	offset := uint64(binary.LittleEndian.Uint32(w[6:10]))
	maxCount := binary.LittleEndian.Uint16(w[10:12])
	if wct >= 12 {
		offset |= uint64(binary.LittleEndian.Uint32(w[20:24])) << 32
	}

	data, ok := readBytesFromHandle(conn, fid, int64(offset), maxCount)
	if !ok {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	return buildReadAndXResponse(req, data)
}

// handleReadMPX implements SMB_COM_READ_MPX (0x1B) per [MS-CIFS]
// 2.2.4.23 and 3.3.5.25.
//
// The spec allows the server to return the requested data in one OR
// many response messages: each carries its own Offset and DataLength,
// and the client reassembles. The Count field in every response is the
// total bytes the server intends to return for this request, so the
// client knows when to stop. We return everything in a single response
// (capped at MaxBufferSize - response-header-overhead); on Direct IPX
// this lets a typical MPX read complete in one IPX datagram.
//
// Rejecting with STATUS_SMB_USE_STANDARD was the prior behavior and
// works (Win9x falls back to SMB_COM_READ), but implementing the
// proper response avoids the extra round-trip and mirrors the
// WriteMPX fix where we honor the protocol per spec.
func (s *Service) handleReadMPX(req []byte, conn *connState) []byte {
	if len(req) < smbHeaderLen+1 {
		return buildSMBErrorResponse(req, smbStatusErrSrvError)
	}
	wct := int(req[smbHeaderLen])
	if wct < 8 {
		return buildSMBErrorResponse(req, smbStatusErrSrvError)
	}

	// SMB_COM_READ_MPX request words (16 bytes, WCT=8) per [MS-CIFS] 2.2.4.23.1:
	//   FID(2) Offset(4) MaxCountOfBytesToReturn(2) MinCountOfBytesToReturn(2)
	//   Timeout(4) Reserved(2)
	w := req[smbHeaderLen+1:]
	fid := binary.LittleEndian.Uint16(w[0:2])
	offset := binary.LittleEndian.Uint32(w[2:6])
	maxCount := binary.LittleEndian.Uint16(w[6:8])

	// Cap the read at our negotiated MaxBufferSize minus the response
	// envelope (SMB header 32 + WCT/words 17 + ByteCount 2 + small Pad).
	// Anything bigger would overflow the client's receive buffer.
	const responseOverhead = smbHeaderLen + 1 + 16 + 2 + 4 // generous Pad allowance
	maxReadable := uint16(0xFFFF)
	if int(negotiateMaxBufferSize) > responseOverhead {
		bufCap := negotiateMaxBufferSize - responseOverhead
		if bufCap < uint32(maxReadable) {
			maxReadable = uint16(bufCap)
		}
	}
	if maxCount > maxReadable {
		maxCount = maxReadable
	}

	data, ok := readBytesFromHandle(conn, fid, int64(offset), maxCount)
	if !ok {
		return buildSMBErrorResponse(req, smbStatusInvalidHandle)
	}

	return buildReadMPXResponse(req, offset, data)
}

// buildReadMPXResponse builds the spec-defined SMB_COM_READ_MPX response
// per [MS-CIFS] 2.2.4.23.2: WCT=8, Words = Offset(4) + Count(2) +
// Remaining(2) + DataCompactionMode(2) + Reserved(2) + DataLength(2) +
// DataOffset(2), followed by ByteCount + Pad + Data.
//
//   - Offset: file offset where this chunk's data begins.
//   - Count: TOTAL bytes the server intends to return for the whole
//     request. Since we serve the entire read in one response, this
//     equals DataLength.
//   - Remaining: -1 (0xFFFF) for regular files per spec.
//   - DataLength: bytes carried by this response.
//   - DataOffset: offset within the SMB message at which Data starts.
func buildReadMPXResponse(req []byte, offset uint32, data []byte) []byte {
	if len(req) < smbHeaderLen || string(req[0:4]) != "\xffSMB" {
		return nil
	}
	dataLen := len(data)
	// WCT=8 (16 word bytes) + ByteCount(2). DataOffset is measured from
	// the SMB header start; the spec allows up to 3 bytes of Pad. We
	// use 1 byte of Pad so DataOffset lands on an odd boundary —
	// matching Samba's reply_readbmpx convention.
	const wctBytes = 16
	headerEnd := smbHeaderLen + 1 + wctBytes + 2 // SMB hdr + WCT + words + BCC
	pad := 1
	dataOffset := headerEnd + pad
	total := dataOffset + dataLen

	out := make([]byte, total)
	copy(out[:smbHeaderLen], req[:smbHeaderLen])
	binary.LittleEndian.PutUint32(out[smbOffStatus:smbOffStatus+4], smbStatusSuccess)
	stampSMBResponseHeader(out)
	out[smbHeaderLen] = 8 // WCT
	w := out[smbHeaderLen+1:]

	binary.LittleEndian.PutUint32(w[0:4], offset)            // Offset
	binary.LittleEndian.PutUint16(w[4:6], uint16(dataLen))   // Count (total)
	binary.LittleEndian.PutUint16(w[6:8], 0xFFFF)            // Remaining = -1 for regular files
	binary.LittleEndian.PutUint16(w[8:10], 0)                // DataCompactionMode
	binary.LittleEndian.PutUint16(w[10:12], 0)               // Reserved
	binary.LittleEndian.PutUint16(w[12:14], uint16(dataLen)) // DataLength
	binary.LittleEndian.PutUint16(w[14:16], uint16(dataOffset))

	// ByteCount = Pad + DataLength
	binary.LittleEndian.PutUint16(w[wctBytes:wctBytes+2], uint16(pad+dataLen))
	// Pad byte left as 0, then Data
	copy(out[dataOffset:], data)
	return out
}

// handleWriteMPX implements SMB_COM_WRITE_MPX (0x1E) per [MS-CIFS]
// 2.2.4.26 and 3.3.5.27.
//
// The protocol is: the client sends a *sequence* of WriteMPX requests
// sharing the same MID/CID, each carrying a chunk of data and a unique
// RequestMask bit. The server writes each chunk's data at its
// ByteOffsetToBeginWrite and accumulates the RequestMask values into a
// per-FID running OR. The server MUST NOT respond to non-final requests
// — replying acks them and breaks the client's window arithmetic.
//
// The final request in the sequence is identified by a NON-ZERO
// SequenceNumber in the SMB header's SecurityFeatures field (bytes
// 20..21 for connectionless transports). Only on that request does the
// server emit a single SMB_COM_WRITE_MPX response carrying the
// accumulated ResponseMask, after which the accumulator is reset for
// the next sequence.
//
// This is the spec-compliant approach; the previous "ack every chunk"
// shortcut produced silent file corruption because Win9x interprets
// each ack's mask bits as "those chunks landed" and slides past chunks
// it never actually sent.
func (s *Service) handleWriteMPX(req []byte, conn *connState) []byte {
	if len(req) < smbHeaderLen+1 {
		return buildSMBErrorResponse(req, smbStatusErrSrvError)
	}
	wct := int(req[smbHeaderLen])
	if wct < 12 {
		return buildSMBErrorResponse(req, smbStatusErrSrvError)
	}

	// SMB_COM_WRITE_MPX request words (24 bytes, WCT=12) per [MS-CIFS] 2.2.4.26.1:
	//   FID(2) TotalByteCount(2) Reserved(2) ByteOffsetToBeginWrite(4)
	//   Timeout(4) WriteMode(2) RequestMask(4) DataLength(2) DataOffset(2)
	w := req[smbHeaderLen+1:]
	fid := binary.LittleEndian.Uint16(w[0:2])
	offset := binary.LittleEndian.Uint32(w[6:10])
	requestMask := binary.LittleEndian.Uint32(w[16:20])
	dataLength := binary.LittleEndian.Uint16(w[20:22])
	dataOffset := binary.LittleEndian.Uint16(w[22:24])

	dataStart := int(dataOffset)
	dataEnd := dataStart + int(dataLength)
	if dataStart < 0 || dataEnd > len(req) || dataStart > dataEnd {
		return buildSMBErrorResponse(req, smbStatusErrSrvError)
	}
	data := req[dataStart:dataEnd]

	// SecurityFeatures.SequenceNumber at SMB header bytes 20..21 (the
	// SequenceNumber subfield of the 8-byte SecurityFeatures region on
	// connectionless transports). A nonzero value marks this request as
	// the final one in the sequence.
	sequenceNumber := binary.LittleEndian.Uint16(req[smbOffSequenceNumber : smbOffSequenceNumber+2])
	isFinal := sequenceNumber != 0

	conn.mu.Lock()
	handle, ok := conn.fids[fid]
	conn.mu.Unlock()
	if !ok || handle == nil || handle.file == nil {
		if isFinal {
			return buildSMBErrorResponse(req, smbStatusInvalidHandle)
		}
		return nil
	}
	if !handle.writable {
		if isFinal {
			return buildSMBErrorResponse(req, smbStatusAccessDenied)
		}
		return nil
	}

	if len(data) > 0 {
		if _, err := handle.file.WriteAt(data, int64(offset)); err != nil {
			// Per [MS-CIFS] 3.3.5.27 errors before the final response are
			// saved and returned later. We can't easily defer here, so
			// surface the error only on the sequenced request.
			if isFinal {
				return buildSMBErrorResponse(req, smbStatusAccessDenied)
			}
			return nil
		}
	}

	// Accumulate this request's RequestMask. Reply only on the final
	// request, then reset the accumulator for the next sequence.
	conn.mu.Lock()
	handle.mpxAccum |= requestMask
	accumulated := handle.mpxAccum
	if isFinal {
		handle.mpxAccum = 0
	}
	conn.mu.Unlock()

	if !isFinal {
		return nil
	}
	return buildWriteMPXResponse(req, accumulated)
}

// buildWriteMPXResponse builds the spec-defined SMB_COM_WRITE_MPX
// response per [MS-CIFS] 2.2.4.26.2: WCT=2 (one 4-byte ResponseMask),
// BCC=0. Sent only in reply to the sequenced (final) request.
func buildWriteMPXResponse(req []byte, responseMask uint32) []byte {
	if len(req) < smbHeaderLen || string(req[0:4]) != "\xffSMB" {
		return nil
	}
	out := make([]byte, smbHeaderLen+1+4+2)
	copy(out[:smbHeaderLen], req[:smbHeaderLen])
	binary.LittleEndian.PutUint32(out[smbOffStatus:smbOffStatus+4], smbStatusSuccess)
	stampSMBResponseHeader(out)
	out[smbHeaderLen] = 2 // WCT
	binary.LittleEndian.PutUint32(out[smbHeaderLen+1:smbHeaderLen+5], responseMask)
	binary.LittleEndian.PutUint16(out[smbHeaderLen+5:smbHeaderLen+7], 0) // ByteCount
	return out
}

// handleWriteRaw rejects SMB_COM_WRITE_RAW (0x1D) with the spec-mandated
// Final Server Response carrying Count=0. Per [MS-CIFS] 3.3.5.26 the
// server MUST verify CAP_RAW_MODE is in Server.Capabilities before
// honoring the request; we don't advertise that capability (and set
// MaxRawSize=0 in NEGOTIATE), so the canonical reject form is the
// zero-count Final Response (WCT=1, BCC=0). This matches SMBLibrary's
// WriteRawFinalResponse{Count = 0}.
func (s *Service) handleWriteRaw(req []byte, conn *connState) []byte {
	_ = conn
	if len(req) < smbHeaderLen || string(req[0:4]) != "\xffSMB" {
		return nil
	}
	out := make([]byte, smbHeaderLen+1+2+2)
	copy(out[:smbHeaderLen], req[:smbHeaderLen])
	binary.LittleEndian.PutUint32(out[smbOffStatus:smbOffStatus+4], smbStatusSuccess)
	out[smbOffFlags] |= 0x80
	out[smbHeaderLen] = 1                                                // WCT
	binary.LittleEndian.PutUint16(out[smbHeaderLen+1:smbHeaderLen+3], 0) // Count = 0
	binary.LittleEndian.PutUint16(out[smbHeaderLen+3:smbHeaderLen+5], 0) // ByteCount
	return out
}

func readBytesFromHandle(conn *connState, fid uint16, offset int64, maxCount uint16) ([]byte, bool) {
	// Look up file handle
	conn.mu.Lock()
	handle, ok := conn.fids[fid]
	conn.mu.Unlock()
	if !ok || handle == nil || handle.file == nil {
		return nil, false
	}

	// Read from file
	data := make([]byte, maxCount)
	n, err := handle.file.ReadAt(data, offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, false
	}

	return data[:n], true
}

// handleWriteAndX (0x2F) writes data to an open file.
func (s *Service) handleWriteAndX(req []byte, conn *connState) []byte {
	if len(req) < smbHeaderLen+13 {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	// SMB_COM_WRITE_ANDX request words per [MS-CIFS] 2.2.4.43.1 (WCT=12 or 14):
	//   AndXCommand(1) AndXReserved(1) AndXOffset(2)
	//   FID(2) Offset(4) Timeout(4) WriteMode(2) Remaining(2)
	//   DataLengthHigh(2) DataLength(2) DataOffset(2) [OffsetHigh(4)]
	wct := int(req[smbHeaderLen])
	if wct < 12 {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	w := req[smbHeaderLen+1:]
	fid := binary.LittleEndian.Uint16(w[4:6])
	offset := uint64(binary.LittleEndian.Uint32(w[6:10]))
	dataLength := binary.LittleEndian.Uint16(w[20:22])
	dataOffset := binary.LittleEndian.Uint16(w[22:24])
	if wct >= 14 {
		offset |= uint64(binary.LittleEndian.Uint32(w[24:28])) << 32
	}

	// DataOffset is relative to the SMB header start.
	dataStart := int(dataOffset)
	dataEnd := dataStart + int(dataLength)
	if dataStart < 0 || dataEnd > len(req) || dataStart > dataEnd {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}
	data := req[dataStart:dataEnd]

	// Look up file handle
	conn.mu.Lock()
	handle, ok := conn.fids[fid]
	conn.mu.Unlock()
	if !ok || handle == nil || handle.file == nil {
		return buildSMBErrorResponse(req, smbStatusInvalidHandle)
	}

	if !handle.writable {
		return buildSMBErrorResponse(req, smbStatusAccessDenied)
	}

	if len(data) == 0 {
		// Zero-length write — truncate to offset, mirroring SMB_COM_WRITE.
		if err := handle.file.Truncate(int64(offset)); err != nil {
			return buildSMBErrorResponse(req, smbStatusAccessDenied)
		}
		return buildWriteAndXResponse(req, 0)
	}

	n, err := handle.file.WriteAt(data, int64(offset))
	if err != nil {
		return buildSMBErrorResponse(req, smbStatusAccessDenied)
	}

	return buildWriteAndXResponse(req, uint16(n))
}

// handleClose (0x04) closes an open file and releases the file handle.
func (s *Service) handleClose(req []byte, conn *connState) []byte {
	if len(req) < smbHeaderLen+7 {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	// Parse request
	wct := int(req[smbHeaderLen])
	if wct < 3 {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	w := req[smbHeaderLen+1:]
	fid := binary.LittleEndian.Uint16(w[0:2])
	// lastWriteTime := binary.LittleEndian.Uint32(w[2:6]) // unused

	// Look up and close file handle
	conn.mu.Lock()
	s.closeFIDLocked(conn, fid)
	conn.mu.Unlock()

	return buildSimpleSuccessResponse(req)
}

// handleFlush (0x05) flushes buffered writes for one or all open files
// in the connection. Per [MS-CIFS] 2.2.4.6.1, FID=0xFFFF means "flush
// every file the requesting PID has open"; otherwise the named FID is
// flushed. The response is WCT=0/BCC=0 (success) or an error.
func (s *Service) handleFlush(req []byte, conn *connState) []byte {
	if len(req) < smbHeaderLen+1 {
		return buildSMBErrorResponse(req, smbStatusErrSrvError)
	}
	wct := int(req[smbHeaderLen])
	if wct < 1 {
		return buildSMBErrorResponse(req, smbStatusErrSrvError)
	}

	w := req[smbHeaderLen+1:]
	fid := binary.LittleEndian.Uint16(w[0:2])

	if fid == 0xFFFF {
		conn.mu.Lock()
		handles := make([]*fileHandle, 0, len(conn.fids))
		for _, h := range conn.fids {
			if h != nil && h.file != nil {
				handles = append(handles, h)
			}
		}
		conn.mu.Unlock()
		for _, h := range handles {
			_ = h.file.Sync()
		}
		return buildSimpleSuccessResponse(req)
	}

	conn.mu.Lock()
	handle, ok := conn.fids[fid]
	conn.mu.Unlock()
	if !ok || handle == nil || handle.file == nil {
		return buildSMBErrorResponse(req, smbStatusInvalidHandle)
	}

	// Sync best-effort. On Windows, FlushFileBuffers fails on handles
	// opened read-only — but a read-only file has no buffered writes
	// to flush, so reporting that as an error to the client is wrong.
	// Treat Sync failures as a noop: the kernel will commit any
	// pending writes when the handle closes.
	_ = handle.file.Sync()
	return buildSimpleSuccessResponse(req)
}

func (s *Service) handleSeek(req []byte, conn *connState) []byte {
	if len(req) < smbHeaderLen+9 {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	wct := int(req[smbHeaderLen])
	if wct < 4 {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	w := req[smbHeaderLen+1:]
	fid := binary.LittleEndian.Uint16(w[0:2])
	mode := binary.LittleEndian.Uint16(w[2:4])
	delta := int64(int32(binary.LittleEndian.Uint32(w[4:8])))

	conn.mu.Lock()
	handle, ok := conn.fids[fid]
	if !ok || handle == nil || handle.file == nil {
		conn.mu.Unlock()
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}
	current := handle.offset
	file := handle.file
	conn.mu.Unlock()

	var base int64
	switch mode {
	case 0:
		base = 0
	case 1:
		base = current
	case 2:
		info, err := file.Stat()
		if err != nil {
			return buildSMBErrorResponse(req, smbStatusNotSupported)
		}
		base = info.Size()
	default:
		return buildSMBErrorResponse(req, smbStatusErrBadFunc)
	}

	pos := base + delta
	if pos < 0 {
		return buildSMBErrorResponse(req, smbStatusErrBadFunc)
	}

	conn.mu.Lock()
	if handle := conn.fids[fid]; handle != nil {
		handle.offset = pos
	}
	conn.mu.Unlock()

	return buildSeekResponse(req, uint32(pos))
}

func buildSeekResponse(req []byte, offset uint32) []byte {
	if len(req) < smbHeaderLen || string(req[0:4]) != "\xffSMB" {
		return nil
	}

	out := make([]byte, smbHeaderLen+1+4+2)
	copy(out[:smbHeaderLen], req[:smbHeaderLen])
	binary.LittleEndian.PutUint32(out[smbOffStatus:smbOffStatus+4], smbStatusSuccess)
	out[smbOffFlags] |= 0x80
	out[smbHeaderLen] = 2
	binary.LittleEndian.PutUint32(out[smbHeaderLen+1:smbHeaderLen+5], offset)
	binary.LittleEndian.PutUint16(out[smbHeaderLen+5:smbHeaderLen+7], 0)
	return out
}

func buildWriteAndXResponse(req []byte, count uint16) []byte {
	if len(req) < smbHeaderLen || string(req[0:4]) != "\xffSMB" {
		return nil
	}

	out := make([]byte, smbHeaderLen+1+(6*2)+2)
	copy(out[:smbHeaderLen], req[:smbHeaderLen])
	binary.LittleEndian.PutUint32(out[smbOffStatus:smbOffStatus+4], smbStatusSuccess)
	out[smbOffFlags] |= 0x80
	out[smbHeaderLen] = 6 // WCT
	w := out[smbHeaderLen+1:]

	// AndXCommand, AndXReserved, AndXOffset
	w[0] = 0xFF
	w[1] = 0x00
	binary.LittleEndian.PutUint16(w[2:4], 0)

	// Count (bytes written, low 16 bits)
	binary.LittleEndian.PutUint16(w[4:6], count)

	// Available — per [MS-CIFS] 2.2.4.43.2 this MUST be 0xFFFF for disk
	// file writes. Some legacy clients refuse to advance unless they
	// see the sentinel.
	binary.LittleEndian.PutUint16(w[6:8], 0xFFFF)

	// Reserved
	binary.LittleEndian.PutUint32(w[8:12], 0)

	// ByteCount = 0
	binary.LittleEndian.PutUint16(w[12:14], 0)

	return out
}

func buildReadAndXResponse(req []byte, data []byte) []byte {
	if len(req) < smbHeaderLen || string(req[0:4]) != "\xffSMB" {
		return nil
	}

	padLen := readDataPadLength(smbHeaderLen + 1 + (12 * 2) + 2)
	out := make([]byte, smbHeaderLen+1+(12*2)+2+padLen+len(data))
	copy(out[:smbHeaderLen], req[:smbHeaderLen])
	binary.LittleEndian.PutUint32(out[smbOffStatus:smbOffStatus+4], smbStatusSuccess)
	out[smbOffFlags] |= 0x80
	out[smbHeaderLen] = 12 // WCT
	w := out[smbHeaderLen+1:]

	// AndXCommand, AndXReserved, AndXOffset
	w[0] = 0xFF
	w[1] = 0x00
	binary.LittleEndian.PutUint16(w[2:4], 0)

	// Remaining (words available for next command)
	binary.LittleEndian.PutUint16(w[4:6], 0)

	// DataCompactionMode
	binary.LittleEndian.PutUint16(w[6:8], 0)

	// Reserved
	binary.LittleEndian.PutUint16(w[8:10], 0)

	// DataLength
	binary.LittleEndian.PutUint16(w[10:12], uint16(len(data)))

	// DataOffset relative to SMB header
	dataOffset := smbHeaderLen + 1 + (12 * 2) + 2 + padLen
	binary.LittleEndian.PutUint16(w[12:14], uint16(dataOffset))

	// Reserved
	binary.LittleEndian.PutUint16(w[14:16], 0)

	// Reserved
	binary.LittleEndian.PutUint16(w[16:18], 0)

	// Reserved
	binary.LittleEndian.PutUint16(w[18:20], 0)

	// Reserved
	binary.LittleEndian.PutUint16(w[20:22], 0)

	// ByteCount
	binary.LittleEndian.PutUint16(w[22:24], uint16(len(data)+padLen))

	// Data
	copy(w[24+padLen:], data)

	return out
}

func buildReadResponse(req []byte, data []byte) []byte {
	if len(req) < smbHeaderLen || string(req[0:4]) != "\xffSMB" {
		return nil
	}

	// SMB_COM_READ (0x0A) response per [MS-CIFS] 2.2.4.11.2:
	//   WCT = 5
	//   Words: CountOfBytesReturned(2), Reserved[4](8 bytes = 4 x uint16)
	//   SMB_Data: ByteCount(2), BufferFormat(1)=0x01, CountOfBytesRead(2), Bytes[]
	const wct = 5
	// SMB_Data starts at: smbHeaderLen + 1(WCT) + wct*2(Words) + 2(ByteCount)
	// Bytes field: 1(BufferFormat) + 2(CountOfBytesRead) + len(data)
	bcc := uint16(3 + len(data))
	out := make([]byte, smbHeaderLen+1+(wct*2)+2+3+len(data))
	copy(out[:smbHeaderLen], req[:smbHeaderLen])
	binary.LittleEndian.PutUint32(out[smbOffStatus:smbOffStatus+4], smbStatusSuccess)
	out[smbOffFlags] |= 0x80
	out[smbHeaderLen] = wct
	w := out[smbHeaderLen+1:]

	// CountOfBytesReturned
	binary.LittleEndian.PutUint16(w[0:2], uint16(len(data)))
	// Reserved[4] = 8 bytes of zeros (already zero from make)

	// ByteCount
	binary.LittleEndian.PutUint16(w[wct*2:wct*2+2], bcc)

	// Bytes: BufferFormat = 0x01 (SMB_FORMAT_DATA)
	bytes := w[wct*2+2:]
	bytes[0] = 0x01
	binary.LittleEndian.PutUint16(bytes[1:3], uint16(len(data)))
	copy(bytes[3:], data)

	return out
}

func readDataPadLength(dataStart int) int {
	if dataStart%2 == 0 {
		return 0
	}
	return 1
}

func buildOpenAndXResponse(req []byte, fid uint16, info fs.FileInfo, fileAttrs uint16, grantedAccess uint16, action uint16) []byte {
	if len(req) < smbHeaderLen || string(req[0:4]) != "\xffSMB" {
		return nil
	}

	out := make([]byte, smbHeaderLen+1+(30)+2)
	copy(out[:smbHeaderLen], req[:smbHeaderLen])
	binary.LittleEndian.PutUint32(out[smbOffStatus:smbOffStatus+4], smbStatusSuccess)
	out[smbOffFlags] |= 0x80
	out[smbHeaderLen] = 15 // WCT
	w := out[smbHeaderLen+1:]

	attrs := uint16(0)
	if info.IsDir() {
		attrs |= FileAttributeDirectory
	} else {
		attrs |= FileAttributeArchive
	}

	// AndXCommand, AndXReserved, AndXOffset
	w[0] = 0xFF
	w[1] = 0x00
	binary.LittleEndian.PutUint16(w[2:4], 0)

	// FID
	binary.LittleEndian.PutUint16(w[4:6], fid)

	// FileAttributes
	binary.LittleEndian.PutUint16(w[6:8], attrs)

	// LastWriteTime (DOS format, for now 0)
	binary.LittleEndian.PutUint32(w[8:12], 0)

	// FileSize
	binary.LittleEndian.PutUint32(w[12:16], uint32(info.Size()))

	// GrantedAccess
	binary.LittleEndian.PutUint16(w[16:18], grantedAccess)

	// FileType
	binary.LittleEndian.PutUint16(w[18:20], 0) // DISK_FILE

	// DeviceState
	binary.LittleEndian.PutUint16(w[20:22], 0)

	// ActionOpened
	binary.LittleEndian.PutUint16(w[22:24], action)

	// Reserved
	binary.LittleEndian.PutUint32(w[24:28], 0)

	// Reserved
	binary.LittleEndian.PutUint16(w[28:30], 0)

	// ByteCount = 0
	binary.LittleEndian.PutUint16(w[30:32], 0)

	return out
}

// handleOpen implements SMB_COM_OPEN (0x02).
// Opens an existing regular file. Returns STATUS_OBJECT_NAME_NOT_FOUND if absent.
func (s *Service) handleOpen(req []byte, conn *connState) []byte {
	if len(req) < smbHeaderLen+1 {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}
	wct := int(req[smbHeaderLen])
	if wct < 2 {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	_, slot, fsys, ok := s.resolveRequestTree(req, conn)
	if !ok {
		return buildSMBErrorResponse(req, smbStatusBadTID)
	}
	rootPath := s.shareRootPath(slot.shareIdx)

	w := req[smbHeaderLen+1:]
	accessMode := binary.LittleEndian.Uint16(w[0:2])

	if accessIsWritable(accessMode) && s.shares[slot.shareIdx].ReadOnly {
		return buildSMBErrorResponse(req, smbStatusAccessDenied)
	}

	path, ok := parseSMBPath(req)
	if !ok || path == "" {
		return buildSMBErrorResponse(req, smbStatusNameNotFound)
	}
	resolved, err := resolveExistingPath(fsys, rootPath, path)
	if err != nil {
		return buildSMBErrorResponse(req, smbStatusNameNotFound)
	}
	info, err := fsys.Stat(resolved)
	if err != nil {
		return buildSMBErrorResponse(req, smbStatusNameNotFound)
	}
	if info.IsDir() {
		return buildSMBErrorResponse(req, smbStatusFileIsDirectory)
	}

	file, err := fsys.OpenFile(resolved, openFlagFromAccess(accessMode))
	if err != nil {
		return buildSMBErrorResponse(req, smbStatusAccessDenied)
	}

	conn.mu.Lock()
	conn.nextFID++
	fid := conn.nextFID
	if fid == 0 {
		conn.nextFID++
		fid = conn.nextFID
	}
	conn.fids[fid] = &fileHandle{
		file:     file,
		path:     resolved,
		writable: accessIsWritable(accessMode),
	}
	conn.mu.Unlock()

	return buildOpenResponse(req, fid, info, accessMode)
}

// handleCreate implements SMB_COM_CREATE (0x03).
// Creates a new file or truncates an existing one to zero length.
// Always returns a read/write FID.
func (s *Service) handleCreate(req []byte, conn *connState) []byte {
	if len(req) < smbHeaderLen+1 {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}
	wct := int(req[smbHeaderLen])
	if wct < 3 {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	_, slot, fsys, ok := s.resolveRequestTree(req, conn)
	if !ok {
		return buildSMBErrorResponse(req, smbStatusBadTID)
	}
	rootPath := s.shareRootPath(slot.shareIdx)
	if s.shares[slot.shareIdx].ReadOnly {
		return buildSMBErrorResponse(req, smbStatusAccessDenied)
	}

	path, ok := parseSMBPath(req)
	if !ok || path == "" {
		return buildSMBErrorResponse(req, smbStatusNameNotFound)
	}

	// Use strict-leaf matching: SMB_COM_CREATE truncates an existing file
	// at the requested name, but a fuzzy resolver could pick a sibling
	// (e.g. "setup" prefix-matching "SETUP.cab") and destroy the wrong
	// file. resolveSMBLeaf only accepts exact case-insensitive matches.
	parentHost, matched, info, _ := resolveSMBLeaf(fsys, rootPath, path)
	if matched != "" && info != nil && info.IsDir() {
		return buildSMBErrorResponse(req, smbStatusFileIsDirectory)
	}
	_, leaf := splitSMBParent(path)
	target := filepath.Join(parentHost, leaf)
	if matched != "" {
		target = filepath.Join(parentHost, matched)
	}

	file, err := fsys.CreateFile(target)
	if err != nil {
		return buildSMBErrorResponse(req, smbStatusAccessDenied)
	}

	conn.mu.Lock()
	conn.nextFID++
	fid := conn.nextFID
	if fid == 0 {
		conn.nextFID++
		fid = conn.nextFID
	}
	conn.fids[fid] = &fileHandle{
		file:     file,
		path:     target,
		writable: true,
	}
	conn.mu.Unlock()

	return buildCreateResponse(req, fid)
}

// handleWrite implements SMB_COM_WRITE (0x0B) per [MS-CIFS] 2.2.4.12.
// A zero-length write truncates the file to the supplied offset.
//
// Win9x over Direct IPX uses this synchronous form (the Mac client too,
// once we reject the multiplexed variants with ERRuseSTD): each request
// carries its own data and offset, and the response acks the byte count
// written. This is the preferred large-write path on connectionless
// transports because there is no per-window ack accounting to mishandle.
func (s *Service) handleWrite(req []byte, conn *connState) []byte {
	if len(req) < smbHeaderLen+1 {
		return buildSMBErrorResponse(req, smbStatusErrSrvError)
	}
	wct := int(req[smbHeaderLen])
	if wct < 5 {
		return buildSMBErrorResponse(req, smbStatusErrSrvError)
	}

	w := req[smbHeaderLen+1:]
	fid := binary.LittleEndian.Uint16(w[0:2])
	count := binary.LittleEndian.Uint16(w[2:4])
	offset := binary.LittleEndian.Uint32(w[4:8])

	conn.mu.Lock()
	handle, ok := conn.fids[fid]
	conn.mu.Unlock()
	if !ok || handle == nil || handle.file == nil {
		return buildSMBErrorResponse(req, smbStatusInvalidHandle)
	}
	if !handle.writable {
		return buildSMBErrorResponse(req, smbStatusAccessDenied)
	}

	if count == 0 {
		// Per [MS-CIFS] 2.2.4.12: a zero-length write truncates the file
		// to the supplied offset.
		if err := handle.file.Truncate(int64(offset)); err != nil {
			return buildSMBErrorResponse(req, smbStatusAccessDenied)
		}
		return buildWriteResponse(req, 0)
	}

	bytesArea, ok := smbBytesArea(req)
	if !ok {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}
	// Bytes layout: BufferFormat(1) + DataLength(2) + Data[count]
	if len(bytesArea) < 3 {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}
	data := bytesArea[3:]
	if len(data) > int(count) {
		data = data[:count]
	}

	n, err := handle.file.WriteAt(data, int64(offset))
	if err != nil {
		return buildSMBErrorResponse(req, smbStatusAccessDenied)
	}
	return buildWriteResponse(req, uint16(n))
}

// buildOpenResponse builds an SMB_COM_OPEN (0x02) response with WCT=7.
func buildOpenResponse(req []byte, fid uint16, info fs.FileInfo, accessMode uint16) []byte {
	if len(req) < smbHeaderLen || string(req[0:4]) != "\xffSMB" {
		return nil
	}
	out := make([]byte, smbHeaderLen+1+(7*2)+2)
	copy(out[:smbHeaderLen], req[:smbHeaderLen])
	binary.LittleEndian.PutUint32(out[smbOffStatus:smbOffStatus+4], smbStatusSuccess)
	out[smbOffFlags] |= 0x80
	out[smbHeaderLen] = 7
	w := out[smbHeaderLen+1:]

	attrs := uint16(FileAttributeArchive)
	if info != nil && info.IsDir() {
		attrs = FileAttributeDirectory
	}
	var size uint32
	if info != nil {
		size = uint32(info.Size())
	}

	binary.LittleEndian.PutUint16(w[0:2], fid)
	binary.LittleEndian.PutUint16(w[2:4], attrs)
	binary.LittleEndian.PutUint32(w[4:8], 0) // LastModified (UTIME)
	binary.LittleEndian.PutUint32(w[8:12], size)
	binary.LittleEndian.PutUint16(w[12:14], accessMode&0x07)
	binary.LittleEndian.PutUint16(w[14:16], 0) // ByteCount
	return out
}

// buildCreateResponse builds an SMB_COM_CREATE (0x03) response with WCT=1.
func buildCreateResponse(req []byte, fid uint16) []byte {
	if len(req) < smbHeaderLen || string(req[0:4]) != "\xffSMB" {
		return nil
	}
	out := make([]byte, smbHeaderLen+1+2+2)
	copy(out[:smbHeaderLen], req[:smbHeaderLen])
	binary.LittleEndian.PutUint32(out[smbOffStatus:smbOffStatus+4], smbStatusSuccess)
	out[smbOffFlags] |= 0x80
	out[smbHeaderLen] = 1
	binary.LittleEndian.PutUint16(out[smbHeaderLen+1:smbHeaderLen+3], fid)
	binary.LittleEndian.PutUint16(out[smbHeaderLen+3:smbHeaderLen+5], 0) // ByteCount
	return out
}

// buildWriteResponse builds an SMB_COM_WRITE (0x0B) response with WCT=1.
func buildWriteResponse(req []byte, count uint16) []byte {
	if len(req) < smbHeaderLen || string(req[0:4]) != "\xffSMB" {
		return nil
	}
	out := make([]byte, smbHeaderLen+1+2+2)
	copy(out[:smbHeaderLen], req[:smbHeaderLen])
	binary.LittleEndian.PutUint32(out[smbOffStatus:smbOffStatus+4], smbStatusSuccess)
	out[smbOffFlags] |= 0x80
	out[smbHeaderLen] = 1
	binary.LittleEndian.PutUint16(out[smbHeaderLen+1:smbHeaderLen+3], count)
	binary.LittleEndian.PutUint16(out[smbHeaderLen+3:smbHeaderLen+5], 0) // ByteCount
	return out
}
