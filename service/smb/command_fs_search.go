package smb

import (
	"bytes"
	"encoding/binary"
	"io/fs"
	"strings"
	"time"
)

func (s *Service) handleQueryInformationDisk(req []byte, conn *connState) []byte {
	if len(req) < smbHeaderLen {
		return buildSMBErrorResponse(req, smbStatusBadTID)
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

	totalBytes, freeBytes, err := fs.DiskUsage("")
	if err != nil {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	return buildQueryInformationDiskResponse(req, totalBytes, freeBytes)
}

func (s *Service) handleCheckDirectory(req []byte, conn *connState) []byte {
	if len(req) < smbHeaderLen {
		return buildSMBErrorResponse(req, smbStatusBadTID)
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

	path, ok := parseSMBPath(req)
	if !ok || path == "" {
		return buildSMBErrorResponse(req, 0xC000007F) // STATUS_OBJECT_NAME_NOT_FOUND
	}

	info, err := fs.Stat(path)
	if err != nil {
		return buildSMBErrorResponse(req, 0xC000007F) // STATUS_OBJECT_NAME_NOT_FOUND
	}

	if !info.IsDir() {
		return buildSMBErrorResponse(req, 0xC0000103) // STATUS_NOT_A_DIRECTORY
	}

	return buildSimpleSuccessResponse(req)
}

// handleSearch (0x81) performs directory enumeration with pattern matching.
// Returns entries in DOS 8.3 format suitable for Win9x/DOS clients.
func (s *Service) handleSearch(req []byte, conn *connState) []byte {
	if len(req) < smbHeaderLen+11 {
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

	// Parse request parameters
	wct := int(req[smbHeaderLen])
	if wct < 2 {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	attrs := binary.LittleEndian.Uint16(req[smbHeaderLen+1 : smbHeaderLen+3])
	pattern, ok := parseSMBPath(req)
	if !ok || pattern == "" {
		pattern = "*"
	}

	// For now, do a simple search - don't handle resume keys yet
	// Get the directory part of the pattern
	lastSlash := strings.LastIndex(pattern, "\\")
	var dirPath, filePattern string
	if lastSlash >= 0 {
		dirPath = pattern[:lastSlash]
		filePattern = pattern[lastSlash+1:]
	} else {
		dirPath = ""
		filePattern = pattern
	}

	// Read directory
	entries, err := fs.ReadDir(dirPath)
	if err != nil {
		return buildSearchEmptyResponse(req)
	}

	// Filter and match entries
	var results []searchResultEntry
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		// Filter by attributes
		if !matchesSearchAttrs(info, attrs) {
			continue
		}

		// Match filename pattern (simple: check if matches *.* or specific name)
		if !matchesPattern(entry.Name(), filePattern) {
			continue
		}

		results = append(results, searchResultEntry{
			name:       entry.Name(),
			size:       info.Size(),
			modTime:    info.ModTime(),
			isDir:      info.IsDir(),
			attributes: getSearchAttrs(info),
		})

		if len(results) >= 10 {
			break
		}
	}

	return buildSearchResponse(req, results)
}

// handleOpenAndX (0x2D) opens or creates a file, returning a file handle.

type searchResultEntry struct {
	name       string
	size       int64
	modTime    time.Time
	isDir      bool
	attributes uint16
}

func matchesSearchAttrs(info fs.FileInfo, searchAttrs uint16) bool {
	// searchAttrs indicates which types of files to include
	// If bit not set, the file type should not be included
	if info.IsDir() {
		return (searchAttrs & SearchAttributeDirectory) != 0
	}

	// Regular files always match unless only searching for specific types
	// that don't apply to normal files
	return true
}

func matchesPattern(name string, pattern string) bool {
	if pattern == "*" || pattern == "*.*" {
		return true
	}
	// Simple pattern matching: just check for exact match or wildcard suffix
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix))
	}
	return strings.ToLower(name) == strings.ToLower(pattern)
}

func getSearchAttrs(info fs.FileInfo) uint16 {
	var attrs uint16
	if info.IsDir() {
		attrs |= SearchAttributeDirectory
	}
	// TODO: check file mode/permissions for read-only, hidden, system, archive
	attrs |= SearchAttributeArchive
	return attrs
}

func buildSearchEmptyResponse(req []byte) []byte {
	if len(req) < smbHeaderLen || string(req[0:4]) != "\xffSMB" {
		return nil
	}

	out := make([]byte, smbHeaderLen+1+2+2)
	copy(out[:smbHeaderLen], req[:smbHeaderLen])
	binary.LittleEndian.PutUint32(out[smbOffStatus:smbOffStatus+4], smbStatusSuccess)
	out[smbOffFlags] |= 0x80
	out[smbHeaderLen] = 1 // WCT
	w := out[smbHeaderLen+1:]
	binary.LittleEndian.PutUint16(w[0:2], 0) // EntryCount = 0
	binary.LittleEndian.PutUint16(w[2:4], 0) // ByteCount = 0
	return out
}

func buildSearchResponse(req []byte, entries []searchResultEntry) []byte {
	if len(req) < smbHeaderLen || string(req[0:4]) != "\xffSMB" {
		return nil
	}

	if len(entries) == 0 {
		return buildSearchEmptyResponse(req)
	}

	// Calculate data size
	var dataBuf bytes.Buffer
	for _, entry := range entries {
		// ResumeKey (21 bytes)
		dataBuf.Write(make([]byte, 21)) // Placeholder resume key

		// Attributes (2 bytes)
		var attrsBytes [2]byte
		binary.LittleEndian.PutUint16(attrsBytes[:], entry.attributes)
		dataBuf.Write(attrsBytes[:])

		// LastWriteTime (4 bytes, DOS format)
		timeVal := uint32(0) // TODO: convert time.Time to DOS format
		var timeBytes [4]byte
		binary.LittleEndian.PutUint32(timeBytes[:], timeVal)
		dataBuf.Write(timeBytes[:])

		// FileSize (4 bytes)
		var sizeBytes [4]byte
		binary.LittleEndian.PutUint32(sizeBytes[:], uint32(entry.size))
		dataBuf.Write(sizeBytes[:])

		// Filename (NUL-terminated)
		dataBuf.WriteString(entry.name)
		dataBuf.WriteByte(0)
	}

	dataBytes := dataBuf.Bytes()
	out := make([]byte, smbHeaderLen+1+2+2+len(dataBytes))
	copy(out[:smbHeaderLen], req[:smbHeaderLen])
	binary.LittleEndian.PutUint32(out[smbOffStatus:smbOffStatus+4], smbStatusSuccess)
	out[smbOffFlags] |= 0x80
	out[smbHeaderLen] = 1 // WCT
	w := out[smbHeaderLen+1:]
	binary.LittleEndian.PutUint16(w[0:2], uint16(len(entries)))
	binary.LittleEndian.PutUint16(w[2:4], uint16(len(dataBytes)))
	copy(w[4:], dataBytes)
	return out
}

func parseTreeConnectShareName(req []byte) (string, bool) {
	bytesArea, ok := smbBytesArea(req)
	if !ok || len(bytesArea) == 0 {
		return "", false
	}

	for _, part := range splitNULStrings(bytesArea) {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		if strings.Contains(p, "\\") {
			trimmed := strings.TrimLeft(p, "\\")
			segments := strings.Split(trimmed, "\\")
			if len(segments) >= 2 && segments[1] != "" {
				return segments[1], true
			}
		}
	}
	return "", false
}

// parseSMBPath extracts a path from the bytes area of an SMB request.
func parseSMBPath(req []byte) (string, bool) {
	bytesArea, ok := smbBytesArea(req)
	if !ok || len(bytesArea) == 0 {
		return "", false
	}

	// Skip the path format indicator (typically buffer format code 0x04)
	rest := bytesArea
	if len(rest) > 0 && rest[0] == 0x04 {
		rest = rest[1:]
	}

	// Find the first NUL-terminated string
	if nulIdx := bytes.IndexByte(rest, 0); nulIdx >= 0 {
		pathStr := string(rest[:nulIdx])
		path := strings.TrimSpace(pathStr)
		// Strip leading separators and normalize
		path = strings.TrimLeft(path, "\\")
		return path, path != ""
	}

	return "", false
}

func smbBytesArea(req []byte) ([]byte, bool) {
	if len(req) < smbHeaderLen+3 {
		return nil, false
	}
	wct := int(req[smbHeaderLen])
	bytesOffset := smbHeaderLen + 1 + (wct * 2)
	if bytesOffset+2 > len(req) {
		return nil, false
	}
	byteCount := int(binary.LittleEndian.Uint16(req[bytesOffset : bytesOffset+2]))
	if byteCount < 0 || bytesOffset+2+byteCount > len(req) {
		return nil, false
	}
	return req[bytesOffset+2 : bytesOffset+2+byteCount], true
}

func splitNULStrings(b []byte) []string {
	parts := make([]string, 0, 4)
	start := 0
	for i := 0; i < len(b); i++ {
		if b[i] != 0 {
			continue
		}
		if i > start {
			parts = append(parts, string(b[start:i]))
		}
		start = i + 1
	}
	if start < len(b) {
		parts = append(parts, string(b[start:]))
	}
	return parts
}

// buildSimpleSuccessResponse returns an SMB response with success status,
// WCT=0 and ByteCount=0. Suitable for simple acknowledgement commands
// like Tree Disconnect where no payload is required.
func buildSimpleSuccessResponse(req []byte) []byte {
	if len(req) < smbHeaderLen || string(req[0:4]) != "\xffSMB" {
		return nil
	}
	out := make([]byte, smbHeaderLen+3)
	copy(out[:smbHeaderLen], req[:smbHeaderLen])
	binary.LittleEndian.PutUint32(out[smbOffStatus:smbOffStatus+4], smbStatusSuccess)
	out[smbOffFlags] |= 0x80
	out[smbHeaderLen] = 0
	binary.LittleEndian.PutUint16(out[smbHeaderLen+1:smbHeaderLen+3], 0)
	return out
}

// buildQueryInformationDiskResponse constructs an SMB_COM_QUERY_INFORMATION_DISK
// response. Uses 512-byte blocks with 8-block allocation units (4KB clusters).
func buildQueryInformationDiskResponse(req []byte, totalBytes, freeBytes uint64) []byte {
	if len(req) < smbHeaderLen || string(req[0:4]) != "\xffSMB" {
		return nil
	}

	const blockSize = 512
	const blocksPerUnit = 8
	const allocationUnitSize = blockSize * blocksPerUnit

	totalUnits := uint16(totalBytes / allocationUnitSize)
	freeUnits := uint16(freeBytes / allocationUnitSize)

	out := make([]byte, smbHeaderLen+1+(5*2)+2)
	copy(out[:smbHeaderLen], req[:smbHeaderLen])
	binary.LittleEndian.PutUint32(out[smbOffStatus:smbOffStatus+4], smbStatusSuccess)
	out[smbOffFlags] |= 0x80
	out[smbHeaderLen] = 5 // WCT
	w := out[smbHeaderLen+1:]
	binary.LittleEndian.PutUint16(w[0:2], totalUnits)
	binary.LittleEndian.PutUint16(w[2:4], blocksPerUnit)
	binary.LittleEndian.PutUint16(w[4:6], blockSize)
	binary.LittleEndian.PutUint16(w[6:8], freeUnits)
	binary.LittleEndian.PutUint16(w[8:10], 0)  // Reserved
	binary.LittleEndian.PutUint16(w[10:12], 0) // ByteCount
	return out
}
