package smb

import (
	"bytes"
	"encoding/binary"
	"io/fs"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

const (
	trans2SubcommandFindFirst2 = 0x0001
	trans2SubcommandFindNext2  = 0x0002
	findInfoLevelFileBothDir   = 0x0104
	findBothFixedBytes         = 94
	findFlagCloseAfterRequest  = 0x0001
	findFlagCloseAtEOS         = 0x0002
	findFlagContinueFromLast   = 0x0008
)

type fsReadDirStat interface {
	ReadDir(path string) ([]fs.DirEntry, error)
	Stat(path string) (fs.FileInfo, error)
}

type findFirst2Row struct {
	name string
	info fs.FileInfo
}

func (s *Service) handleQueryInformation(req []byte, conn *connState) []byte {
	if len(req) < smbHeaderLen+3 {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	_, slot, fsys, ok := s.resolveRequestTree(req, conn)
	if !ok {
		return buildSMBErrorResponse(req, smbStatusBadTID)
	}
	rootPath := ""
	s.mu.Lock()
	if slot.shareIdx >= 0 && slot.shareIdx < len(s.shares) {
		rootPath = strings.TrimSpace(s.shares[slot.shareIdx].Path)
	}
	s.mu.Unlock()

	path, ok := parseSMBPathAllowEmpty(req)
	if !ok {
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

	return buildQueryInformationResponse(req, info)
}

func buildQueryInformationResponse(req []byte, info fs.FileInfo) []byte {
	if len(req) < smbHeaderLen || string(req[0:4]) != "\xffSMB" {
		return nil
	}

	out := make([]byte, smbHeaderLen+1+20+2)
	copy(out[:smbHeaderLen], req[:smbHeaderLen])
	binary.LittleEndian.PutUint32(out[smbOffStatus:smbOffStatus+4], smbStatusSuccess)
	out[smbOffFlags] |= 0x80
	out[smbHeaderLen] = 10 // WCT

	attrs := uint16(0)
	if info.IsDir() {
		attrs |= FileAttributeDirectory
	} else {
		attrs |= FileAttributeArchive
	}

	w := out[smbHeaderLen+1:]
	binary.LittleEndian.PutUint16(w[0:2], attrs)
	binary.LittleEndian.PutUint32(w[2:6], 0) // DOS LastWriteTime placeholder
	if !info.IsDir() {
		binary.LittleEndian.PutUint32(w[6:10], uint32(info.Size()))
	}
	binary.LittleEndian.PutUint16(w[20:22], 0)
	return out
}

func (s *Service) handleTransaction2(req []byte, conn *connState) []byte {
	_, slot, fsys, ok := s.resolveRequestTree(req, conn)
	if !ok {
		return buildSMBErrorResponse(req, smbStatusBadTID)
	}

	subcommand, params, ok := parseTransaction2Request(req)
	if !ok {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	switch subcommand {
	case trans2SubcommandFindFirst2:
		rootPath := ""
		s.mu.Lock()
		if slot.shareIdx >= 0 && slot.shareIdx < len(s.shares) {
			rootPath = strings.TrimSpace(s.shares[slot.shareIdx].Path)
		}
		s.mu.Unlock()
		return s.handleTransaction2FindFirst2(req, conn, fsys, rootPath, params)
	case trans2SubcommandFindNext2:
		return s.handleTransaction2FindNext2(req, conn, params)
	default:
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}
}

func (s *Service) handleFindClose2(req []byte, conn *connState) []byte {
	if len(req) < smbHeaderLen+5 {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}
	wct := int(req[smbHeaderLen])
	if wct < 1 || conn == nil {
		return buildSimpleSuccessResponse(req)
	}
	w := req[smbHeaderLen+1:]
	sid := binary.LittleEndian.Uint16(w[0:2])
	conn.mu.Lock()
	delete(conn.searches, sid)
	conn.mu.Unlock()
	return buildSimpleSuccessResponse(req)
}

func parseTransaction2Request(req []byte) (subcommand uint16, params []byte, ok bool) {
	if len(req) < smbHeaderLen+1+28 || string(req[0:4]) != "\xffSMB" || req[4] != CommandTransaction2 {
		return 0, nil, false
	}

	wct := int(req[smbHeaderLen])
	if wct < 14 {
		return 0, nil, false
	}

	wStart := smbHeaderLen + 1
	wLen := wct * 2
	if wStart+wLen > len(req) {
		return 0, nil, false
	}
	w := req[wStart : wStart+wLen]

	paramCount := int(binary.LittleEndian.Uint16(w[18:20]))
	paramOffset := int(binary.LittleEndian.Uint16(w[20:22]))
	setupCount := int(w[26])
	if setupCount < 1 || 28+setupCount*2 > len(w) {
		return 0, nil, false
	}
	subcommand = binary.LittleEndian.Uint16(w[28:30])

	if paramCount < 0 || paramOffset < smbHeaderLen || paramOffset+paramCount > len(req) {
		return 0, nil, false
	}
	params = req[paramOffset : paramOffset+paramCount]
	return subcommand, params, true
}

func (s *Service) handleTransaction2FindFirst2(req []byte, conn *connState, fsys fsReadDirStat, rootPath string, params []byte) []byte {
	if len(params) < 12 {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	searchAttrs := binary.LittleEndian.Uint16(params[0:2])
	searchCount := int(binary.LittleEndian.Uint16(params[2:4]))
	if searchCount <= 0 {
		searchCount = 1
	}
	if searchCount > 256 {
		searchCount = 256
	}
	infoLevel := binary.LittleEndian.Uint16(params[6:8])
	if infoLevel != findInfoLevelFileBothDir {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	pattern := trans2PathFromParams(params)
	if pattern == "" {
		pattern = "*"
	}
	dirPath, filePattern := splitSearchPattern(pattern)
	resolvedDir, err := resolveExistingPath(fsys, rootPath, dirPath)
	if err != nil {
		return buildSMBErrorResponse(req, smbStatusNameNotFound)
	}
	queryDir := resolvedDir

	entries, err := fsys.ReadDir(queryDir)
	if err != nil {
		return buildSMBErrorResponse(req, smbStatusNameNotFound)
	}

	matches := make([]findFirst2Row, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !nameMatchesClientPattern(name, filePattern) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		matches = append(matches, findFirst2Row{name: name, info: info})
	}

	if len(matches) == 0 {
		if !strings.ContainsAny(filePattern, "*?") {
			return buildSMBErrorResponse(req, smbStatusNameNotFound)
		}
		sid := allocSearchSID(conn)
		storeSearchHandle(conn, sid, nil, 0, pattern, searchAttrs)
		return buildTransaction2FindFirst2Response(req, sid, 0, true, nil, 0)
	}

	if searchCount > len(matches) {
		searchCount = len(matches)
	}
	data, returned, lastNameOffset := buildFindFirst2BothDirData(matches, searchCount)
	endOfSearch := returned >= len(matches)

	sid := allocSearchSID(conn)
	if endOfSearch {
		storeSearchHandle(conn, sid, nil, returned, pattern, searchAttrs)
	} else {
		dirEntries := make([]fs.DirEntry, 0, len(matches)-returned)
		for _, row := range matches[returned:] {
			dirEntries = append(dirEntries, dirEntryFromFileInfo{name: row.name, info: row.info})
		}
		storeSearchHandle(conn, sid, dirEntries, 0, pattern, searchAttrs)
	}

	return buildTransaction2FindFirst2Response(req, sid, returned, endOfSearch, data, lastNameOffset)
}

func (s *Service) handleTransaction2FindNext2(req []byte, conn *connState, params []byte) []byte {
	if len(params) < 12 {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	sid := binary.LittleEndian.Uint16(params[0:2])
	searchCount := int(binary.LittleEndian.Uint16(params[2:4]))
	if searchCount <= 0 {
		searchCount = 1
	}
	if searchCount > 256 {
		searchCount = 256
	}
	infoLevel := binary.LittleEndian.Uint16(params[4:6])
	if infoLevel != findInfoLevelFileBothDir {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}
	flags := binary.LittleEndian.Uint16(params[10:12])
	resumeName := trans2ResumeNameFromFindNext2(params)

	if conn == nil {
		return buildTransaction2FindFirst2Response(req, sid, 0, true, nil, 0)
	}

	conn.mu.Lock()
	h, ok := conn.searches[sid]
	if !ok || h == nil {
		conn.mu.Unlock()
		return buildSMBErrorResponse(req, smbStatusNoMoreFiles)
	}
	start := h.idx
	if flags&findFlagContinueFromLast == 0 && resumeName != "" {
		for i := 0; i < len(h.entries); i++ {
			if strings.EqualFold(h.entries[i].Name(), resumeName) {
				start = i + 1
				break
			}
		}
	}
	entries := h.entries
	conn.mu.Unlock()

	if start >= len(entries) {
		if flags&findFlagCloseAfterRequest != 0 || flags&findFlagCloseAtEOS != 0 {
			conn.mu.Lock()
			delete(conn.searches, sid)
			conn.mu.Unlock()
		}
		return buildSMBErrorResponse(req, smbStatusNoMoreFiles)
	}

	rows := make([]findFirst2Row, 0, searchCount)
	idx := start
	for idx < len(entries) && len(rows) < searchCount {
		info, err := entries[idx].Info()
		if err == nil {
			rows = append(rows, findFirst2Row{name: entries[idx].Name(), info: info})
		}
		idx++
	}

	data, returned, lastNameOffset := buildFindFirst2BothDirData(rows, len(rows))
	endOfSearch := idx >= len(entries)

	conn.mu.Lock()
	if flags&findFlagCloseAfterRequest != 0 || (flags&findFlagCloseAtEOS != 0 && endOfSearch) {
		delete(conn.searches, sid)
	} else if hs, ok := conn.searches[sid]; ok && hs != nil {
		hs.idx = idx
	}
	conn.mu.Unlock()

	return buildTransaction2FindFirst2Response(req, sid, returned, endOfSearch, data, lastNameOffset)
}

func trans2ResumeNameFromFindNext2(params []byte) string {
	if len(params) <= 12 {
		return ""
	}
	raw := params[12:]
	if i := bytes.IndexByte(raw, 0); i >= 0 {
		raw = raw[:i]
	}
	return strings.TrimSpace(string(raw))
}

func parseSMBPathAllowEmpty(req []byte) (string, bool) {
	bytesArea, ok := smbBytesArea(req)
	if !ok || len(bytesArea) == 0 {
		return "", false
	}

	rest := bytesArea
	if len(rest) > 0 && rest[0] == 0x04 {
		rest = rest[1:]
	}
	if nulIdx := bytes.IndexByte(rest, 0); nulIdx >= 0 {
		path := strings.TrimSpace(string(rest[:nulIdx]))
		path = strings.TrimLeft(path, "\\")
		return path, true
	}
	return "", false
}

func resolveExistingPath(fsys fsReadDirStat, rootPath, smbPath string) (string, error) {
	clean := strings.TrimLeft(strings.TrimSpace(smbPath), "\\")
	if clean == "" {
		if rootPath != "" {
			return rootPath, nil
		}
		return ".", nil
	}

	direct := smbJoinPath(rootPath, clean)
	if _, err := fsys.Stat(direct); err == nil {
		return direct, nil
	}

	parts := strings.Split(clean, "\\")
	curr := smbJoinPath(rootPath, "")
	if curr == "" {
		curr = "."
	}

	for _, part := range parts {
		if part == "" {
			continue
		}
		entries, err := fsys.ReadDir(curr)
		if err != nil {
			return "", err
		}
		match := findBestComponentMatch(part, entries)
		if match == "" {
			return "", fs.ErrNotExist
		}
		curr = filepath.Join(curr, match)
	}
	return curr, nil
}

func findBestComponentMatch(component string, entries []fs.DirEntry) string {
	for _, e := range entries {
		if strings.EqualFold(e.Name(), component) {
			return e.Name()
		}
	}

	normTarget := normalizePathToken(component)
	if normTarget == "" {
		return ""
	}
	candidates := make([]string, 0, 4)
	for _, e := range entries {
		n := normalizePathToken(e.Name())
		if n == normTarget {
			return e.Name()
		}
		if strings.HasPrefix(n, normTarget) {
			candidates = append(candidates, e.Name())
		}
	}
	if len(candidates) == 1 {
		return candidates[0]
	}
	return ""
}

func nameMatchesClientPattern(name, pattern string) bool {
	if strings.ContainsAny(pattern, "*?") {
		return matchesPattern(name, pattern)
	}
	return strings.EqualFold(name, pattern)
}

func normalizePathToken(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(s)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func trans2PathFromParams(params []byte) string {
	raw := params[12:]
	if i := bytes.IndexByte(raw, 0); i >= 0 {
		raw = raw[:i]
	}
	path := strings.TrimSpace(string(raw))
	path = strings.TrimLeft(path, "\\")
	return path
}

func splitSearchPattern(pattern string) (dirPath, filePattern string) {
	last := strings.LastIndex(pattern, "\\")
	if last < 0 {
		return "", pattern
	}
	return pattern[:last], pattern[last+1:]
}

func smbJoinPath(root, rel string) string {
	root = strings.TrimSpace(root)
	rel = strings.TrimSpace(strings.TrimLeft(rel, "\\"))
	if root == "" {
		return rel
	}
	if rel == "" {
		return root
	}
	return filepath.Join(root, strings.ReplaceAll(rel, "\\", string(filepath.Separator)))
}

func allocSearchSID(conn *connState) uint16 {
	if conn == nil {
		return 1
	}
	conn.mu.Lock()
	defer conn.mu.Unlock()
	conn.nextSID++
	if conn.nextSID == 0 {
		conn.nextSID++
	}
	return conn.nextSID
}

func storeSearchHandle(conn *connState, sid uint16, entries []fs.DirEntry, idx int, pattern string, attrs uint16) {
	if conn == nil {
		return
	}
	conn.mu.Lock()
	if conn.searches == nil {
		conn.searches = map[uint16]*searchHandle{}
	}
	conn.searches[sid] = &searchHandle{entries: entries, idx: idx, pattern: pattern, attrs: attrs}
	conn.mu.Unlock()
}

func buildFindFirst2BothDirData(matches []findFirst2Row, maxEntries int) ([]byte, int, uint16) {
	if maxEntries <= 0 || len(matches) == 0 {
		return nil, 0, 0
	}
	var data bytes.Buffer
	lastNameOffset := uint16(0)
	returned := 0

	for i := 0; i < len(matches) && returned < maxEntries; i++ {
		row := matches[i]
		nameBytes := []byte(row.name)
		recordLen := findBothFixedBytes + len(nameBytes)
		pad := (4 - (recordLen % 4)) % 4
		nextOffset := uint32(recordLen + pad)
		if returned == maxEntries-1 || i == len(matches)-1 {
			nextOffset = 0
		}

		recStart := data.Len()
		fileNameOffset := recStart + findBothFixedBytes
		if fileNameOffset > 0xFFFF {
			break
		}
		lastNameOffset = uint16(fileNameOffset)

		rec := make([]byte, recordLen+pad)
		binary.LittleEndian.PutUint32(rec[0:4], nextOffset)

		ft := fileTimeFromModTime(row.info.ModTime())
		binary.LittleEndian.PutUint64(rec[8:16], ft)
		binary.LittleEndian.PutUint64(rec[16:24], ft)
		binary.LittleEndian.PutUint64(rec[24:32], ft)
		binary.LittleEndian.PutUint64(rec[32:40], ft)

		size := uint64(0)
		if !row.info.IsDir() {
			size = uint64(row.info.Size())
		}
		binary.LittleEndian.PutUint64(rec[40:48], size)
		binary.LittleEndian.PutUint64(rec[48:56], allocSizeFor(size, row.info.IsDir()))
		binary.LittleEndian.PutUint32(rec[56:60], uint32(extFileAttrs(row.info)))
		binary.LittleEndian.PutUint32(rec[60:64], uint32(len(nameBytes)))
		copy(rec[94:94+len(nameBytes)], nameBytes)

		data.Write(rec)
		returned++
	}

	return data.Bytes(), returned, lastNameOffset
}

func allocSizeFor(size uint64, isDir bool) uint64 {
	if isDir || size == 0 {
		return 0
	}
	const cluster = 4096
	return ((size + cluster - 1) / cluster) * cluster
}

func extFileAttrs(info fs.FileInfo) uint16 {
	attrs := uint16(0)
	if info.IsDir() {
		attrs |= FileAttributeDirectory
	} else {
		attrs |= FileAttributeArchive
	}
	if info.Mode().Perm()&0o222 == 0 {
		attrs |= FileAttributeReadOnly
	}
	return attrs
}

func fileTimeFromModTime(t time.Time) uint64 {
	if t.IsZero() {
		return windowsFiletimeOffset
	}
	ns := t.UTC().UnixNano()
	if ns < 0 {
		return windowsFiletimeOffset
	}
	return uint64(ns/100) + windowsFiletimeOffset
}

func buildTransaction2FindFirst2Response(req []byte, sid uint16, searchCount int, endOfSearch bool, data []byte, lastNameOffset uint16) []byte {
	if len(req) < smbHeaderLen || string(req[0:4]) != "\xffSMB" {
		return nil
	}

	const paramLen = 10
	dataLen := len(data)
	paramOffset := smbHeaderLen + 1 + 20 + 2
	dataOffset := paramOffset + paramLen
	totalLen := dataOffset + dataLen

	out := make([]byte, totalLen)
	copy(out[:smbHeaderLen], req[:smbHeaderLen])
	binary.LittleEndian.PutUint32(out[smbOffStatus:smbOffStatus+4], smbStatusSuccess)
	out[smbOffFlags] |= 0x80

	out[smbHeaderLen] = 10
	w := out[smbHeaderLen+1:]
	binary.LittleEndian.PutUint16(w[0:2], paramLen)
	binary.LittleEndian.PutUint16(w[2:4], uint16(dataLen))
	binary.LittleEndian.PutUint16(w[6:8], paramLen)
	binary.LittleEndian.PutUint16(w[8:10], uint16(paramOffset))
	binary.LittleEndian.PutUint16(w[12:14], uint16(dataLen))
	if dataLen > 0 {
		binary.LittleEndian.PutUint16(w[14:16], uint16(dataOffset))
	}
	binary.LittleEndian.PutUint16(w[20:22], uint16(paramLen+dataLen))

	p := out[paramOffset:]
	binary.LittleEndian.PutUint16(p[0:2], sid)
	binary.LittleEndian.PutUint16(p[2:4], uint16(searchCount))
	if endOfSearch {
		binary.LittleEndian.PutUint16(p[4:6], 1)
	}
	binary.LittleEndian.PutUint16(p[6:8], 0)
	binary.LittleEndian.PutUint16(p[8:10], lastNameOffset)

	if dataLen > 0 {
		copy(out[dataOffset:], data)
	}
	return out
}

type dirEntryFromFileInfo struct {
	name string
	info fs.FileInfo
}

func (d dirEntryFromFileInfo) Name() string               { return d.name }
func (d dirEntryFromFileInfo) IsDir() bool                { return d.info.IsDir() }
func (d dirEntryFromFileInfo) Type() fs.FileMode          { return d.info.Mode().Type() }
func (d dirEntryFromFileInfo) Info() (fs.FileInfo, error) { return d.info, nil }
