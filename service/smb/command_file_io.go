package smb

import (
	"errors"
	"encoding/binary"
	"io"
	"io/fs"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/pkg/vfs"
)

func (s *Service) closeFID(conn *connState, fid uint16) {
	conn.mu.Lock()
	defer conn.mu.Unlock()
	s.closeFIDLocked(conn, fid)
}

func (s *Service) closeFIDLocked(conn *connState, fid uint16) {
	handle, ok := conn.fids[fid]
	if ok {
		if handle != nil && handle.file != nil {
			handle.file.Close()
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

	w := req[smbHeaderLen+1:]
	desiredAccess := binary.LittleEndian.Uint16(w[0:2])
	searchAttrs := binary.LittleEndian.Uint16(w[2:4])
	fileAttrs := binary.LittleEndian.Uint16(w[4:6])
	createTime := binary.LittleEndian.Uint32(w[6:10])
	openFunction := binary.LittleEndian.Uint16(w[10:12])

	_ = searchAttrs
	_ = createTime

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

	// OPEN_FUNCTION: low nibble controls open/create behavior.
	// 0x0001 means open-if-exists and fail if missing.
	openOnly := (openFunction & 0x000F) == 0x0001

	// Try to open existing file / create new
	activePath := openPath
	file, err = fs.OpenFile(openPath, 0)
	if err != nil && !openOnly {
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
		file.Close()
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
		tid:      tid,
		writable: (fileAttrs & FileAttributeReadOnly) == 0,
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

	// Parse request
	wct := int(req[smbHeaderLen])
	if wct < 5 {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	w := req[smbHeaderLen+1:]
	fid := binary.LittleEndian.Uint16(w[2:4])
	offset := binary.LittleEndian.Uint32(w[4:8])
	maxCount := binary.LittleEndian.Uint16(w[8:10])
	_ = binary.LittleEndian.Uint16(w[10:12]) // minCount (unused)

	data, ok := readBytesFromHandle(conn, fid, int64(offset), maxCount)
	if !ok {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	return buildReadAndXResponse(req, data)
}

// handleReadMPX rejects SMB_COM_READ_MPX with STATUS_SMB_USE_STANDARD,
// prompting the client to fall back to SMB_COM_READ. We do not advertise
// CAP_MPX_MODE in the NEGOTIATE response, but Win9x over Direct IPX may
// still attempt ReadMPX as the only large-block read on connectionless
// transports. Mirror Samba's reply_readbmpx (source3/smbd/reply.c), which
// also unconditionally returns ERRSRV/ERRuseSTD.
func (s *Service) handleReadMPX(req []byte, conn *connState) []byte {
	_ = conn
	return buildSMBErrorResponse(req, smbStatusUseStandard)
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

	// Parse request
	wct := int(req[smbHeaderLen])
	if wct < 6 {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	w := req[smbHeaderLen+1:]
	fid := binary.LittleEndian.Uint16(w[2:4])
	offset := binary.LittleEndian.Uint32(w[4:8])
	_ = binary.LittleEndian.Uint16(w[12:14]) // writeMode (unused)

	// Get data from bytes area
	bytesArea, ok := smbBytesArea(req)
	if !ok {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	// Look up file handle
	conn.mu.Lock()
	handle, ok := conn.fids[fid]
	conn.mu.Unlock()
	if !ok || handle == nil || handle.file == nil {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	if !handle.writable {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	// Write to file
	n, err := handle.file.WriteAt(bytesArea, int64(offset))
	if err != nil {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
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

// handleFlush (0x05) flushes (syncs) writes to an open file.
func (s *Service) handleFlush(req []byte, conn *connState) []byte {
	if len(req) < smbHeaderLen+5 {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	// Parse request
	wct := int(req[smbHeaderLen])
	if wct < 1 {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	w := req[smbHeaderLen+1:]
	fid := binary.LittleEndian.Uint16(w[0:2])

	// Look up file handle
	conn.mu.Lock()
	handle, ok := conn.fids[fid]
	conn.mu.Unlock()
	if !ok || handle == nil || handle.file == nil {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	// Sync the file
	if err := handle.file.Sync(); err != nil {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

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

	// Count (bytes written)
	binary.LittleEndian.PutUint16(w[4:6], count)

	// Remaining (bytes left in transaction)
	binary.LittleEndian.PutUint16(w[6:8], 0)

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
