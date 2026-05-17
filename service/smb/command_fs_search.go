package smb

import (
	"bytes"
	"encoding/binary"
	"io/fs"
	"path/filepath"
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
	diskPath := "."
	if slot.shareIdx >= 0 && slot.shareIdx < len(s.shares) {
		if p := strings.TrimSpace(s.shares[slot.shareIdx].Path); p != "" {
			diskPath = p
		}
	}
	s.mu.Unlock()
	if !ok || fs == nil {
		return buildSMBErrorResponse(req, smbStatusBadTID)
	}

	totalBytes, freeBytes, err := fs.DiskUsage(diskPath)
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
	rootPath := s.shareRootPath(slot.shareIdx)

	path, ok := parseSMBPath(req)
	if !ok || path == "" {
		return buildSMBErrorResponse(req, 0xC000007F) // STATUS_OBJECT_NAME_NOT_FOUND
	}
	resolvedPath, err := resolveExistingPath(fs, rootPath, path)
	if err != nil {
		return buildSMBErrorResponse(req, 0xC000007F) // STATUS_OBJECT_NAME_NOT_FOUND
	}

	info, err := fs.Stat(resolvedPath)
	if err != nil {
		return buildSMBErrorResponse(req, 0xC000007F) // STATUS_OBJECT_NAME_NOT_FOUND
	}

	if !info.IsDir() {
		return buildSMBErrorResponse(req, 0xC0000103) // STATUS_NOT_A_DIRECTORY
	}

	return buildSimpleSuccessResponse(req)
}

// handleSearch (0x81) performs directory enumeration with pattern
// matching. Returns entries in DOS 8.3 format suitable for the CORE
// dialect (WfW 3.11, MS-DOS clients). The protocol is paged: the first
// request carries a filename pattern; follow-up requests have an empty
// filename and a 21-byte resume key copied verbatim from the previous
// reply's last entry. We pack our SID into the resume key and store
// the full match list under that SID on the connection so the next
// request can pick up where it left off. When the list is exhausted
// we return ERRDOS/ERRnofiles which signals end-of-search.
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
	fsys, ok := s.shareFSes[slot.shareIdx]
	s.mu.Unlock()
	if !ok || fsys == nil {
		return buildSMBErrorResponse(req, smbStatusBadTID)
	}
	rootPath := s.shareRootPath(slot.shareIdx)

	wct := int(req[smbHeaderLen])
	if wct < 2 {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}
	// MaxCount: spec 2.2.4.58.1 calls this a session-wide limit, but
	// WfW 3.11 sends MaxCount=1 on the initial request and MaxCount=20
	// on every continuation — only per-response semantics make sense
	// of that. Real-world CIFS servers behave the same way. See
	// spec/errata.md "SMB_COM_SEARCH MaxCount".
	maxCount := int(binary.LittleEndian.Uint16(req[smbHeaderLen+1 : smbHeaderLen+3]))
	if maxCount <= 0 {
		maxCount = 1
	}
	attrs := binary.LittleEndian.Uint16(req[smbHeaderLen+3 : smbHeaderLen+5])

	pattern, _ := parseSMBPath(req)
	resumeKey, hasResume := parseSearchResumeKey(req)
	isContinuation := hasResume && pattern == ""

	// ClientState (bytes 17-20 of the resume key) is opaque to us and
	// MUST be echoed back unmodified in every response per CIFS spec
	// 2.2.4.58.1.
	var clientState [4]byte
	if hasResume {
		copy(clientState[:], resumeKey[17:21])
	}

	if isContinuation {
		// Our private state lives entirely inside the ServerState block
		// (bytes 1-16). SID at bytes 13-14, last-returned-index at
		// bytes 9-12. ClientState (bytes 17-20) is the client's.
		sid := binary.LittleEndian.Uint16(resumeKey[13:15])
		offset := int(binary.LittleEndian.Uint32(resumeKey[9:13]))
		conn.mu.Lock()
		handle := conn.searches[sid]
		conn.mu.Unlock()
		if handle == nil || offset >= len(handle.entries) {
			return buildSMBErrorResponse(req, smbStatusNoMoreFiles)
		}
		batch, nextOffset := sliceSearchBatch(handle.entries, offset, maxCount)
		if len(batch) == 0 {
			conn.mu.Lock()
			delete(conn.searches, sid)
			conn.mu.Unlock()
			return buildSMBErrorResponse(req, smbStatusNoMoreFiles)
		}
		if nextOffset >= len(handle.entries) {
			conn.mu.Lock()
			delete(conn.searches, sid)
			conn.mu.Unlock()
		}
		return buildCoreSearchResponse(req, batch, sid, nextOffset, clientState)
	}

	if pattern == "" {
		pattern = "*"
	}

	lastSlash := strings.LastIndex(pattern, "\\")
	var dirPath, filePattern string
	if lastSlash >= 0 {
		dirPath = pattern[:lastSlash]
		filePattern = pattern[lastSlash+1:]
	} else {
		filePattern = pattern
	}

	queryDir, err := resolveExistingPath(fsys, rootPath, dirPath)
	if err != nil {
		return buildSMBErrorResponse(req, smbStatusNoMoreFiles)
	}
	entries, err := fsys.ReadDir(queryDir)
	if err != nil {
		return buildSMBErrorResponse(req, smbStatusNoMoreFiles)
	}

	matches := make([]findFirst2Row, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if !matchesSearchAttrs(info, attrs) {
			continue
		}
		shortName := entry.Name()
		if n, err := fsys.ShortName(filepath.Join(queryDir, entry.Name())); err == nil && n != "" {
			shortName = n
		}
		if !matchesPattern(shortName, filePattern) && !matchesPattern(entry.Name(), filePattern) {
			continue
		}
		matches = append(matches, findFirst2Row{name: entry.Name(), shortName: shortName, info: info})
	}

	if len(matches) == 0 {
		return buildSMBErrorResponse(req, smbStatusNoMoreFiles)
	}

	sid := allocSearchSID(conn)
	batch, nextOffset := sliceSearchBatch(matches, 0, maxCount)
	if nextOffset < len(matches) {
		storeSearchHandle(conn, sid, matches, nextOffset, filePattern, attrs)
	}
	return buildCoreSearchResponse(req, batch, sid, nextOffset, clientState)
}

// parseSearchResumeKey returns the 21-byte resume-key block from a
// SMB_COM_SEARCH request's bytes area, if present. The bytes area
// shape is: BufferFormat(0x04) FileName 0x00 BufferFormat(0x05)
// ResumeKeyLength(uint16) ResumeKey[ResumeKeyLength].
func parseSearchResumeKey(req []byte) ([]byte, bool) {
	bytesArea, ok := smbBytesArea(req)
	if !ok {
		return nil, false
	}
	rest := bytesArea
	if len(rest) == 0 || rest[0] != 0x04 {
		return nil, false
	}
	rest = rest[1:]
	nul := bytes.IndexByte(rest, 0)
	if nul < 0 {
		return nil, false
	}
	rest = rest[nul+1:]
	if len(rest) < 3 || rest[0] != 0x05 {
		return nil, false
	}
	rkLen := int(binary.LittleEndian.Uint16(rest[1:3]))
	if rkLen != 21 || len(rest) < 3+rkLen {
		return nil, false
	}
	return rest[3 : 3+21], true
}

func sliceSearchBatch(matches []findFirst2Row, offset, maxCount int) ([]findFirst2Row, int) {
	if offset >= len(matches) {
		return nil, offset
	}
	end := offset + maxCount
	if end > len(matches) {
		end = len(matches)
	}
	return matches[offset:end], end
}

// formatSearchFileName returns the 13-byte FileName field for an
// SMB_COM_SEARCH directory record. Spec 2.2.4.58.2 says the field is
// space-padded to 12 chars + NUL; we NUL-pad instead because WfW 3.11
// treats every byte before the first NUL as the filename. See
// spec/errata.md "SMB_COM_SEARCH FileName padding".
func formatSearchFileName(name string) []byte {
	base, ext := splitDOSName(strings.ToUpper(name))
	if len(base) > 8 {
		base = base[:8]
	}
	if len(ext) > 3 {
		ext = ext[:3]
	}
	out := make([]byte, 13)
	n := copy(out, base)
	if ext != "" {
		out[n] = '.'
		n++
		copy(out[n:], ext)
	}
	// Bytes n..12 are already zero from make().
	return out
}

// handleOpenAndX (0x2D) opens or creates a file, returning a file handle.

// matchesSearchAttrs implements SMB_COM_SEARCH's inclusive attribute
// filter per CIFS spec 2.2.4.58.1. The SearchAttributes field uses the
// SMB_FILE_ATTRIBUTE bits (not the SMB_SEARCH_ATTRIBUTE high-byte set):
// normal files always match; directories match only if ATTR_DIRECTORY
// (0x0010) is set; hidden/system match only if their bits are set; and
// VOLUME (0x0008) is exclusive — when set, only the volume label is
// returned. WfW 3.11 sends 0x0031 (READONLY|DIRECTORY|ARCHIVE) when
// browsing a folder, so we must accept the low-byte directory bit.
func matchesSearchAttrs(info fs.FileInfo, searchAttrs uint16) bool {
	if searchAttrs&FileAttributeVolume != 0 {
		return false // volume label only — we don't expose one
	}
	if info.IsDir() {
		return searchAttrs&FileAttributeDirectory != 0
	}
	return true
}

// matchesPattern matches a filename against a DOS-style 8.3 wildcard
// pattern. `?` matches any single character (or nothing if the name's
// segment ends short, per DOS semantics), and `*` matches any run of
// characters within the basename or extension. The pattern and the
// candidate are split on the first `.` so that `????????.???` matches
// `README.TXT` (8 chars + 3 chars, with `?` permitted to fall off the
// end of the actual name).
func matchesPattern(name string, pattern string) bool {
	if pattern == "" || pattern == "*" || pattern == "*.*" {
		return true
	}
	pBase, pExt := splitDOSName(pattern)
	nBase, nExt := splitDOSName(name)
	return matchDOSSegment(nBase, pBase) && matchDOSSegment(nExt, pExt)
}

func splitDOSName(s string) (string, string) {
	dot := strings.Index(s, ".")
	if dot < 0 {
		return s, ""
	}
	return s[:dot], s[dot+1:]
}

// matchDOSSegment matches a single 8.3 component (basename or extension).
// `?` consumes one character of name or matches an early end-of-name;
// `*` consumes the rest of the segment greedily; any other character
// must match case-insensitively.
func matchDOSSegment(name, pattern string) bool {
	n, p := 0, 0
	nl, pl := len(name), len(pattern)
	for p < pl {
		switch pattern[p] {
		case '*':
			return true // greedy: matches whatever is left in this segment
		case '?':
			if n < nl {
				n++
			}
			p++
		default:
			if n >= nl {
				return false
			}
			if toLowerASCII(pattern[p]) != toLowerASCII(name[n]) {
				return false
			}
			n++
			p++
		}
	}
	return n == nl
}

func toLowerASCII(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}

// getSearchAttrs returns the SMB_FILE_ATTRIBUTES byte for an entry.
// Only the low-byte FileAttribute bits (0x01-0x20) belong here — the
// SearchAttribute high-byte bits (0x0100+) are request-only filters
// and would be truncated by the response's 1-byte FileAttributes
// field anyway.
func getSearchAttrs(info fs.FileInfo) uint16 {
	var attrs uint16
	if info.IsDir() {
		attrs |= FileAttributeDirectory
	} else {
		attrs |= FileAttributeArchive
	}
	return attrs
}

// buildCoreSearchResponse encodes a SMB_COM_SEARCH (0x81) reply. Each
// directory entry is a 43-byte record: 21-byte resume key, 1-byte
// attributes, 4-byte DOS LastWriteTime+Date, 4-byte file size, 13-byte
// 8.3 name (NUL-terminated, space-padded, dot included).
//
// Resume-key layout per CIFS spec 2.2.4.58.1:
//
//	byte  0     Reserved (server-defined; we set 0x81 as a sanity tag)
//	bytes 1-16  ServerState (opaque to client) — we pack:
//	            1-8   8.3 base name (uppercase, space-padded)
//	            9-12  little-endian uint32: index of next entry
//	            13-14 little-endian uint16: SID
//	            15-16 reserved (0)
//	bytes 17-20 ClientState — echoed back verbatim from the request
//
// We intentionally keep all of our state inside ServerState so we
// never clobber the client's ClientState bytes.
func buildCoreSearchResponse(req []byte, entries []findFirst2Row, sid uint16, nextOffset int, clientState [4]byte) []byte {
	if len(req) < smbHeaderLen || string(req[0:4]) != "\xffSMB" {
		return nil
	}

	const recordLen = 43
	dataBytes := make([]byte, 0, len(entries)*recordLen)
	for i, entry := range entries {
		var rk [21]byte
		rk[0] = 0x81
		base, _ := splitDOSName(strings.ToUpper(entry.shortName))
		if len(base) > 8 {
			base = base[:8]
		}
		// Pad base into bytes 1-8 with spaces so the resume key looks
		// well-formed if a debugger inspects it; the client treats the
		// whole ServerState block as opaque.
		copy(rk[1:9], "        ")
		copy(rk[1:9], base)
		entryIndex := nextOffset - len(entries) + i + 1
		binary.LittleEndian.PutUint32(rk[9:13], uint32(entryIndex))
		binary.LittleEndian.PutUint16(rk[13:15], sid)
		copy(rk[17:21], clientState[:])

		var rec [recordLen]byte
		copy(rec[0:21], rk[:])
		rec[21] = byte(getSearchAttrs(entry.info))
		binary.LittleEndian.PutUint32(rec[22:26], dosTimeDate(entry.info.ModTime()))
		size := entry.info.Size()
		if size < 0 {
			size = 0
		}
		if size > 0xFFFFFFFF {
			size = 0xFFFFFFFF
		}
		binary.LittleEndian.PutUint32(rec[26:30], uint32(size))
		copy(rec[30:43], formatSearchFileName(entry.shortName))
		dataBytes = append(dataBytes, rec[:]...)
	}

	out := make([]byte, smbHeaderLen+1+2+2+1+2+len(dataBytes))
	copy(out[:smbHeaderLen], req[:smbHeaderLen])
	binary.LittleEndian.PutUint32(out[smbOffStatus:smbOffStatus+4], smbStatusSuccess)
	out[smbOffFlags] |= 0x80
	out[smbHeaderLen] = 1 // WCT
	w := out[smbHeaderLen+1:]
	binary.LittleEndian.PutUint16(w[0:2], uint16(len(entries))) // Count
	bcc := 1 + 2 + len(dataBytes)
	binary.LittleEndian.PutUint16(w[2:4], uint16(bcc))
	w[4] = 0x05 // BufferFormat = Variable Block
	binary.LittleEndian.PutUint16(w[5:7], uint16(len(dataBytes)))
	copy(w[7:], dataBytes)
	return out
}

// dosTimeDate packs a Go time.Time into the 32-bit DOS date+time used
// by SMB_COM_SEARCH replies (low 16 bits = time, high 16 bits = date).
// Dates before 1980 (the DOS epoch) are clamped to 1980-01-01.
func dosTimeDate(t time.Time) uint32 {
	if t.IsZero() {
		t = time.Unix(0, 0)
	}
	t = t.UTC()
	year := t.Year()
	if year < 1980 {
		return uint32(1) | (uint32(1) << 5) // 1980-01-01 00:00:00
	}
	dosTime := uint16(t.Second()/2) | (uint16(t.Minute()) << 5) | (uint16(t.Hour()) << 11)
	dosDate := uint16(t.Day()) | (uint16(t.Month()) << 5) | (uint16(year-1980) << 9)
	return uint32(dosTime) | (uint32(dosDate) << 16)
}


func parseTreeConnectShareName(req []byte) (string, bool) {
	bytesArea, ok := smbBytesArea(req)
	if !ok || len(bytesArea) == 0 {
		return "", false
	}

	for _, part := range splitNULStrings(bytesArea) {
		// SMB_COM_TREE_CONNECT (0x70) prefixes each string with a
		// buffer-format byte (0x04 = ASCII string). TREE_CONNECT_ANDX
		// (0x75) places the path raw. Strip the prefix if present so
		// both shapes parse identically.
		if len(part) > 0 && part[0] == 0x04 {
			part = part[1:]
		}
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
