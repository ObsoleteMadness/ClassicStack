package smb

import (
	"encoding/binary"
	"io/fs"

	"github.com/ObsoleteMadness/ClassicStack/pkg/vfs"
)

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

	_ = desiredAccess
	_ = searchAttrs
	_ = createTime

	path, ok := parseSMBPath(req)
	if !ok || path == "" {
		return buildSMBErrorResponse(req, 0xC000007F) // STATUS_OBJECT_NAME_NOT_FOUND
	}

	// Determine open mode
	var file vfs.File
	var err error

	// OPEN_FUNCTION: bits 0-3: action, bits 4-7: mode
	mode := openFunction >> 4
	_ = openFunction & 0x0F // action (unused for now)

	// Try to open existing file / create new
	if mode == 1 {
		// OPEN_IF_EXISTS
		file, err = fs.OpenFile(path, 0) // Read mode
	} else if mode == 2 {
		// OPEN_EXCLUSIVE
		file, err = fs.CreateFile(path)
	} else {
		// Default: try to open, create if not found
		file, err = fs.OpenFile(path, 0)
		if err != nil {
			file, err = fs.CreateFile(path)
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
		path:     path,
		tid:      tid,
		writable: (fileAttrs & FileAttributeReadOnly) == 0,
	}
	conn.mu.Unlock()

	return buildOpenAndXResponse(req, fid, info, fileAttrs)
}

// handleReadAndX (0x2E) reads data from an open file.
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

	// Look up file handle
	conn.mu.Lock()
	handle, ok := conn.fids[fid]
	conn.mu.Unlock()
	if !ok || handle == nil || handle.file == nil {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	// Clamp read size
	if maxCount > 4096 {
		maxCount = 4096
	}

	// Read from file
	data := make([]byte, maxCount)
	n, err := handle.file.ReadAt(data, int64(offset))
	if err != nil && err.Error() != "EOF" {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	data = data[:n]
	return buildReadAndXResponse(req, data)
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
	handle, ok := conn.fids[fid]
	if ok {
		if handle != nil && handle.file != nil {
			handle.file.Close()
		}
		s.releaseLocksForFIDLocked(conn, fid)
		delete(conn.fids, fid)
	}
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

	out := make([]byte, smbHeaderLen+1+(12*2)+2+len(data))
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
	dataOffset := smbHeaderLen + 1 + (12 * 2) + 2
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
	binary.LittleEndian.PutUint16(w[22:24], uint16(len(data)))

	// Data
	copy(w[24:], data)

	return out
}

func buildOpenAndXResponse(req []byte, fid uint16, info fs.FileInfo, fileAttrs uint16) []byte {
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
	binary.LittleEndian.PutUint16(w[16:18], 0x0001) // Read access

	// FileType
	binary.LittleEndian.PutUint16(w[18:20], 0) // DISK_FILE

	// DeviceState
	binary.LittleEndian.PutUint16(w[20:22], 0)

	// ActionOpened
	binary.LittleEndian.PutUint16(w[22:24], 0x0001) // FILE_OPENED

	// Reserved
	binary.LittleEndian.PutUint32(w[24:28], 0)

	// Reserved
	binary.LittleEndian.PutUint16(w[28:30], 0)

	// ByteCount = 0
	binary.LittleEndian.PutUint16(w[30:32], 0)

	return out
}
