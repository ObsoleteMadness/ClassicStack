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
	trans2SubcommandFindFirst2     = 0x0001
	trans2SubcommandFindNext2      = 0x0002
	trans2SubcommandQueryPathInfo  = 0x0005
	trans2SubcommandQueryFileInfo  = 0x0007
	findInfoLevelFileBothDir       = 0x0104
	findBothFixedBytes             = 94
	findFlagCloseAfterRequest      = 0x0001
	findFlagCloseAtEOS             = 0x0002
	findFlagContinueFromLast       = 0x0008

	// Information levels for QUERY_PATH_INFO / QUERY_FILE_INFO. Numbered
	// per [MS-CIFS] 2.2.6.6 / 2.2.6.8 and [MS-CIFS] 2.2.8.3.
	infoLevelStandard          = 0x0001 // SMB_INFO_STANDARD
	infoLevelQueryEaSize       = 0x0002 // SMB_INFO_QUERY_EA_SIZE
	infoLevelQueryFileBasic    = 0x0101 // SMB_QUERY_FILE_BASIC_INFO
	infoLevelQueryFileStandard = 0x0102 // SMB_QUERY_FILE_STANDARD_INFO
	infoLevelQueryFileEaInfo   = 0x0103 // SMB_QUERY_FILE_EA_INFO
	infoLevelQueryFileNameInfo = 0x0104 // SMB_QUERY_FILE_NAME_INFO  (also FILE_BOTH_DIR for FindFirst2)
	infoLevelQueryFileAllInfo  = 0x0107 // SMB_QUERY_FILE_ALL_INFO
)

type fsReadDirStat interface {
	ReadDir(path string) ([]fs.DirEntry, error)
	Stat(path string) (fs.FileInfo, error)
	ShortName(path string) (string, error)
}

type findFirst2Row struct {
	name      string
	shortName string
	info      fs.FileInfo
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
	case trans2SubcommandQueryFileInfo:
		return s.handleTransaction2QueryFileInfo(req, conn, fsys, params)
	case trans2SubcommandQueryPathInfo:
		rootPath := ""
		s.mu.Lock()
		if slot.shareIdx >= 0 && slot.shareIdx < len(s.shares) {
			rootPath = strings.TrimSpace(s.shares[slot.shareIdx].Path)
		}
		s.mu.Unlock()
		return s.handleTransaction2QueryPathInfo(req, fsys, rootPath, params)
	default:
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}
}

// handleTransaction2QueryFileInfo serves TRANS2_QUERY_FILE_INFORMATION
// (subcommand 0x0007). Per [MS-CIFS] 2.2.6.8.1 the params block is:
//
//	FID(2) InformationLevel(2)
//
// We resolve the FID to its open file, fetch fs.FileInfo, and serialize
// according to the requested level. Win9x typically asks for 0x0101
// (SMB_QUERY_FILE_BASIC_INFO) right after OpenAndX as a sanity check.
func (s *Service) handleTransaction2QueryFileInfo(req []byte, conn *connState, fsys fsReadDirStat, params []byte) []byte {
	if len(params) < 4 {
		return buildSMBErrorResponse(req, smbStatusErrSrvError)
	}
	fid := binary.LittleEndian.Uint16(params[0:2])
	infoLevel := binary.LittleEndian.Uint16(params[2:4])

	conn.mu.Lock()
	handle, ok := conn.fids[fid]
	conn.mu.Unlock()
	if !ok || handle == nil || handle.file == nil {
		return buildSMBErrorResponse(req, smbStatusInvalidHandle)
	}

	info, err := fsys.Stat(handle.path)
	if err != nil {
		return buildSMBErrorResponse(req, smbStatusNameNotFound)
	}

	data, ok := buildQueryInfoData(infoLevel, info)
	if !ok {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}
	return buildTransaction2QueryInfoResponse(req, data)
}

// handleTransaction2QueryPathInfo serves TRANS2_QUERY_PATH_INFORMATION
// (subcommand 0x0005). Per [MS-CIFS] 2.2.6.6.1 the params block is:
//
//	InformationLevel(2) Reserved(4) FileName(SMB_STRING)
//
// The body is the same set of info levels as QueryFileInfo; we share
// the serialization helper.
func (s *Service) handleTransaction2QueryPathInfo(req []byte, fsys fsReadDirStat, rootPath string, params []byte) []byte {
	if len(params) < 6 {
		return buildSMBErrorResponse(req, smbStatusErrSrvError)
	}
	infoLevel := binary.LittleEndian.Uint16(params[0:2])
	// Skip params[2:6] Reserved.
	rawName := params[6:]
	if i := bytes.IndexByte(rawName, 0); i >= 0 {
		rawName = rawName[:i]
	}
	path := strings.TrimLeft(strings.TrimSpace(string(rawName)), "\\")

	resolved, err := resolveExistingPath(fsys, rootPath, path)
	if err != nil {
		return buildSMBErrorResponse(req, smbStatusNameNotFound)
	}
	info, err := fsys.Stat(resolved)
	if err != nil {
		return buildSMBErrorResponse(req, smbStatusNameNotFound)
	}

	data, ok := buildQueryInfoData(infoLevel, info)
	if !ok {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}
	return buildTransaction2QueryInfoResponse(req, data)
}

// buildQueryInfoData serializes fs.FileInfo into the requested info-level
// payload. Returns false if the level is unsupported.
//
// Layouts per [MS-CIFS] 2.2.8.3:
//   - 0x0101 SMB_QUERY_FILE_BASIC_INFO     — 40 bytes (4 FILETIMEs + attrs + reserved)
//   - 0x0102 SMB_QUERY_FILE_STANDARD_INFO  — 24 bytes (alloc, eof, links, delete, dir, pad)
//   - 0x0103 SMB_QUERY_FILE_EA_INFO        — 4 bytes  (EaSize)
//   - 0x0107 SMB_QUERY_FILE_ALL_INFO       — concatenation of the above
func buildQueryInfoData(level uint16, info fs.FileInfo) ([]byte, bool) {
	switch level {
	case infoLevelQueryFileBasic:
		buf := make([]byte, 40)
		ft := fileTimeFromModTime(info.ModTime())
		binary.LittleEndian.PutUint64(buf[0:8], ft)   // CreationTime
		binary.LittleEndian.PutUint64(buf[8:16], ft)  // LastAccessTime
		binary.LittleEndian.PutUint64(buf[16:24], ft) // LastWriteTime
		binary.LittleEndian.PutUint64(buf[24:32], ft) // ChangeTime
		binary.LittleEndian.PutUint32(buf[32:36], uint32(extFileAttrs(info)))
		// buf[36:40] Reserved = 0
		return buf, true
	case infoLevelQueryFileStandard:
		buf := make([]byte, 24)
		size := uint64(0)
		if !info.IsDir() {
			size = uint64(info.Size())
		}
		binary.LittleEndian.PutUint64(buf[0:8], allocSizeFor(size, info.IsDir()))
		binary.LittleEndian.PutUint64(buf[8:16], size)
		binary.LittleEndian.PutUint32(buf[16:20], 1) // NumberOfLinks
		buf[20] = 0                                  // DeletePending
		if info.IsDir() {
			buf[21] = 1
		}
		// buf[22:24] padding
		return buf, true
	case infoLevelQueryFileEaInfo:
		buf := make([]byte, 4) // EaSize = 0
		return buf, true
	case infoLevelQueryFileAllInfo:
		basic, _ := buildQueryInfoData(infoLevelQueryFileBasic, info)
		std, _ := buildQueryInfoData(infoLevelQueryFileStandard, info)
		ea, _ := buildQueryInfoData(infoLevelQueryFileEaInfo, info)
		buf := make([]byte, 0, len(basic)+len(std)+len(ea))
		buf = append(buf, basic...)
		buf = append(buf, std...)
		buf = append(buf, ea...)
		return buf, true
	default:
		return nil, false
	}
}

// buildTransaction2QueryInfoResponse builds a TRANS2 reply carrying a
// 2-byte EaErrorOffset param and the supplied info-level data block.
// Layout matches the existing FindFirst2 response builder; only the
// param contents differ.
func buildTransaction2QueryInfoResponse(req []byte, data []byte) []byte {
	if len(req) < smbHeaderLen || string(req[0:4]) != "\xffSMB" {
		return nil
	}

	const paramLen = 2 // EaErrorOffset only
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
	binary.LittleEndian.PutUint16(w[0:2], paramLen)        // TotalParamCount
	binary.LittleEndian.PutUint16(w[2:4], uint16(dataLen)) // TotalDataCount
	binary.LittleEndian.PutUint16(w[6:8], paramLen)        // ParamCount
	binary.LittleEndian.PutUint16(w[8:10], uint16(paramOffset))
	binary.LittleEndian.PutUint16(w[12:14], uint16(dataLen)) // DataCount
	if dataLen > 0 {
		binary.LittleEndian.PutUint16(w[14:16], uint16(dataOffset))
	}
	binary.LittleEndian.PutUint16(w[20:22], uint16(paramLen+dataLen)) // ByteCount

	// EaErrorOffset = 0
	binary.LittleEndian.PutUint16(out[paramOffset:paramOffset+2], 0)
	if dataLen > 0 {
		copy(out[dataOffset:], data)
	}
	return out
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
		shortName := name
		if s, err := fsys.ShortName(filepath.Join(rootPath, dirPath, name)); err == nil && s != "" {
			shortName = s
		}
		matches = append(matches, findFirst2Row{name: name, shortName: shortName, info: info})
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
		rows := make([]findFirst2Row, 0, len(matches)-returned)
		rows = append(rows, matches[returned:]...)
		storeSearchHandle(conn, sid, rows, 0, pattern, searchAttrs)
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
		return buildTransaction2FindNext2Response(req, 0, true, nil, 0)
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
			if strings.EqualFold(h.entries[i].name, resumeName) {
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
		rows = append(rows, entries[idx])
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

	return buildTransaction2FindNext2Response(req, returned, endOfSearch, data, lastNameOffset)
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

// resolveSMBLeaf resolves the parent of an SMB path strictly and reports
// whether the requested leaf already exists in that parent (matched
// case-insensitively). Returns the host path of the parent directory, the
// matched leaf entry (zero value if no match), and an error only if the
// parent path itself cannot be resolved.
//
// Callers performing existence-or-create operations (mkdir, create file,
// rename target) should prefer this over resolveExistingPath: it never
// silently substitutes a sibling whose name happens to share a prefix.
func resolveSMBLeaf(fsys fsReadDirStat, rootPath, smbPath string) (parentHost, matchedName string, info fs.FileInfo, err error) {
	parentSMB, leaf := splitSMBParent(smbPath)
	if leaf == "" {
		return "", "", nil, fs.ErrInvalid
	}

	parentHost = smbJoinPath(rootPath, parentSMB)
	if parentSMB != "" {
		if resolved, rerr := resolveExistingPath(fsys, rootPath, parentSMB); rerr == nil {
			parentHost = resolved
		}
	}

	entries, derr := fsys.ReadDir(parentHost)
	if derr != nil {
		return parentHost, "", nil, derr
	}
	for _, e := range entries {
		if !strings.EqualFold(e.Name(), leaf) {
			continue
		}
		ei, ierr := e.Info()
		if ierr != nil {
			return parentHost, e.Name(), nil, ierr
		}
		return parentHost, e.Name(), ei, nil
	}
	return parentHost, "", nil, nil
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
		// Use DOS-name-aware matching so legacy clients can resolve
		// mangled forms like "VOLUME68K" to the real "Volume 68k".
		// Callers that must reject prefix-style false positives (mkdir,
		// create) should use resolveSMBLeaf instead, which only matches
		// the final component case-insensitively-exactly.
		match := findDOSLikeComponentMatch(part, entries)
		if match == "" {
			return "", fs.ErrNotExist
		}
		curr = filepath.Join(curr, match)
	}
	return curr, nil
}

// findBestComponentMatch returns the name of the entry matching component
// case-insensitively, or "" if no entry matches. Matching is strict —
// callers that need to resolve DOS-mangled names (e.g. "VOLUME68K" →
// "Volume 68k") should use findDOSLikeComponentMatch instead. Strict
// matching is required for create/collision checks because prefix
// matching lets siblings like "SETUP.cab" masquerade as "setup".
func findBestComponentMatch(component string, entries []fs.DirEntry) string {
	for _, e := range entries {
		if strings.EqualFold(e.Name(), component) {
			return e.Name()
		}
	}
	return ""
}

// findDOSLikeComponentMatch extends findBestComponentMatch with the
// fallback heuristics needed to resolve a DOS-mangled name (uppercase,
// spaces and punctuation stripped, possibly truncated) to its real
// host-filesystem name. Returns the matched entry name or "".
//
// The fallback only fires when no strict match exists. If multiple
// entries normalize-or-prefix-match the request, the result is "" —
// ambiguity must not silently pick a sibling.
func findDOSLikeComponentMatch(component string, entries []fs.DirEntry) string {
	if name := findBestComponentMatch(component, entries); name != "" {
		return name
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

func normalizePathToken(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(s)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func nameMatchesClientPattern(name, pattern string) bool {
	if strings.ContainsAny(pattern, "*?") {
		return matchesPattern(name, pattern)
	}
	return strings.EqualFold(name, pattern)
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



func storeSearchHandle(conn *connState, sid uint16, entries []findFirst2Row, idx int, pattern string, attrs uint16) {
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
		nameBytes := encodeOEM(row.name)
		shortNameBytes := encodeOEM(row.shortName)
		if len(shortNameBytes) > 24 {
			shortNameBytes = shortNameBytes[:24]
		}

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
		
		rec[68] = byte(len(shortNameBytes))
		if len(shortNameBytes) > 24 {
			copy(rec[70:94], shortNameBytes[:24])
		} else {
			copy(rec[70:94], shortNameBytes)
		}

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

// encodeOEM encodes s as the single-byte OEM/ASCII form used on the wire when
// SMB_FLAGS2_UNICODE is not negotiated. Non-ASCII runes are replaced with '?'.
// Legacy clients (Win9x, classic Mac SMB) cannot decode UTF-16, so FIND_FIRST2
// records must use this even though the fixed-area layout is unchanged.
func encodeOEM(s string) []byte {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		if r < 0x80 {
			out = append(out, byte(r))
		} else {
			out = append(out, '?')
		}
	}
	return out
}

func buildTransaction2FindFirst2Response(req []byte, sid uint16, searchCount int, endOfSearch bool, data []byte, lastNameOffset uint16) []byte {
	return buildTransaction2FindResponse(req, true, sid, searchCount, endOfSearch, data, lastNameOffset)
}

func buildTransaction2FindNext2Response(req []byte, searchCount int, endOfSearch bool, data []byte, lastNameOffset uint16) []byte {
	return buildTransaction2FindResponse(req, false, 0, searchCount, endOfSearch, data, lastNameOffset)
}

// buildTransaction2FindResponse encodes a FIND_FIRST2 or FIND_NEXT2 reply.
// The two share the data layout but differ in the response param block:
// FIND_FIRST2 prepends a 2-byte SID (10-byte block); FIND_NEXT2 omits it
// (8-byte block). Mixing them up makes legacy clients parse SearchCount
// as SID and silently drop every record after the first.
func buildTransaction2FindResponse(req []byte, includeSID bool, sid uint16, searchCount int, endOfSearch bool, data []byte, lastNameOffset uint16) []byte {
	if len(req) < smbHeaderLen || string(req[0:4]) != "\xffSMB" {
		return nil
	}

	paramLen := 8
	if includeSID {
		paramLen = 10
	}
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
	binary.LittleEndian.PutUint16(w[0:2], uint16(paramLen))
	binary.LittleEndian.PutUint16(w[2:4], uint16(dataLen))
	binary.LittleEndian.PutUint16(w[6:8], uint16(paramLen))
	binary.LittleEndian.PutUint16(w[8:10], uint16(paramOffset))
	binary.LittleEndian.PutUint16(w[12:14], uint16(dataLen))
	if dataLen > 0 {
		binary.LittleEndian.PutUint16(w[14:16], uint16(dataOffset))
	}
	binary.LittleEndian.PutUint16(w[20:22], uint16(paramLen+dataLen))

	p := out[paramOffset:]
	off := 0
	if includeSID {
		binary.LittleEndian.PutUint16(p[off:off+2], sid)
		off += 2
	}
	binary.LittleEndian.PutUint16(p[off:off+2], uint16(searchCount))
	off += 2
	if endOfSearch {
		binary.LittleEndian.PutUint16(p[off:off+2], 1)
	}
	off += 2
	binary.LittleEndian.PutUint16(p[off:off+2], 0)
	off += 2
	binary.LittleEndian.PutUint16(p[off:off+2], lastNameOffset)

	if dataLen > 0 {
		copy(out[dataOffset:], data)
	}
	return out
}

