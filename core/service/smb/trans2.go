package smb

import (
	stdfs "io/fs"
	"strings"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"

	protocol "github.com/ObsoleteMadness/ClassicStack/core/protocol/smb"
)

// --- SMB_COM_TRANSACTION2 subcommands over the §9 share seam: FIND_FIRST2 /
// FIND_NEXT2 (directory enumeration, the modern dir-listing path Win9x and the
// classic-Mac SMB client use), FIND_CLOSE2, and QUERY_PATH_INFORMATION /
// QUERY_FILE_INFORMATION (stat by path / by FID). The enumeration snapshots the
// directory at FIND_FIRST2 time into a per-session searchHandle and streams it
// across FIND_NEXT2 calls; entry names are packed in the request's wire charset
// via the share codec (UTF-16 for an NT client, OEM/ANSI for a DOS client). ---

const (
	trans2FindFirst2     = 0x0001
	trans2FindNext2      = 0x0002
	trans2QueryPathInfo  = 0x0005
	trans2SetPathInfo    = 0x0006 // TRANS2_SET_PATH_INFORMATION
	trans2QueryFileInfo  = 0x0007
	trans2SetFileInfo    = 0x0008 // TRANS2_SET_FILE_INFORMATION
	infoFileBothDirInfo  = 0x0104 // SMB_FIND_FILE_BOTH_DIRECTORY_INFO
	infoQueryFileBasic   = 0x0101 // SMB_QUERY_FILE_BASIC_INFO
	infoQueryFileStd     = 0x0102 // SMB_QUERY_FILE_STANDARD_INFO
	infoQueryFileEA      = 0x0103 // SMB_QUERY_FILE_EA_INFO
	infoQueryFileAllInfo = 0x0107 // SMB_QUERY_FILE_ALL_INFO
	infoSetFileBasic     = 0x0101 // SMB_SET_FILE_BASIC_INFO (FileBasicInformation)

	findCloseAfterRequest = 0x0001 // SMB_FIND_CLOSE_AFTER_REQUEST
	findCloseAtEOS        = 0x0002 // SMB_FIND_CLOSE_AT_EOS
)

// trans2Request is the parsed TRANS2 sub-request: the subcommand plus its
// parameter and data blocks. The SET_*_INFORMATION subcommands carry the
// information level + target in the params and the FileBasicInfo payload in the
// data block, so both are surfaced.
type trans2Request struct {
	sub    uint16
	params []byte
	data   []byte
}

// parseTransaction2 decodes the SMB_COM_TRANSACTION2 wrapper ([MS-CIFS]
// §2.2.4.46.1): WCT≥14, with ParameterCount/Offset and SetupCount in the words,
// the first setup word being the subcommand. The param block is sliced at its
// header-relative offset.
func parseTransaction2(req []byte) (trans2Request, bool) {
	words, _, ok := reqBody(req)
	if !ok || len(words) < 28 {
		return trans2Request{}, false
	}
	paramCount := int(bp.LE16(words[18:20]))
	paramOffset := int(bp.LE16(words[20:22]))
	setupCount := int(words[26])
	if setupCount < 1 || 28+2*setupCount > len(words) {
		return trans2Request{}, false
	}
	sub := bp.LE16(words[28:30])
	if paramCount < 0 || paramOffset < protocol.HeaderLen || paramOffset+paramCount > len(req) {
		return trans2Request{}, false
	}
	t2 := trans2Request{sub: sub, params: req[paramOffset : paramOffset+paramCount]}
	// The data block (DataCount/DataOffset, words[22:24]/[24:26]) carries the
	// SET_*_INFORMATION payload; surface it when present and in-bounds.
	dataCount := int(bp.LE16(words[22:24]))
	dataOffset := int(bp.LE16(words[24:26]))
	if dataCount > 0 && dataOffset >= protocol.HeaderLen && dataOffset+dataCount <= len(req) {
		t2.data = req[dataOffset : dataOffset+dataCount]
	}
	return t2, true
}

// handleTransaction2 answers SMB_COM_TRANSACTION2 by dispatching its subcommand.
func (s *Service) handleTransaction2(sess *smbSession, h protocol.Header, req []byte) []byte {
	sh, st := s.treeFor(sess, h)
	if st != statusSuccess {
		return errResponse(h, st)
	}
	t2, ok := parseTransaction2(req)
	if !ok {
		return errResponse(h, statusNotSupported)
	}
	switch t2.sub {
	case trans2FindFirst2:
		return s.findFirst2(sess, sh, h, t2.params)
	case trans2FindNext2:
		return s.findNext2(sess, sh, h, t2.params)
	case trans2QueryPathInfo:
		return s.queryPathInfo(sh, h, t2.params)
	case trans2QueryFileInfo:
		return s.queryFileInfo(sess, h, t2.params)
	case trans2SetPathInfo:
		return s.setPathInfo(sh, h, t2.params, t2.data)
	case trans2SetFileInfo:
		return s.setFileInfo(sess, h, t2.params, t2.data)
	default:
		return errResponse(h, statusNotSupported)
	}
}

// setPathInfo serves TRANS2_SET_PATH_INFORMATION. Params ([MS-CIFS] §2.2.6.7.1):
// InformationLevel(2) Reserved(4) FileName(SMB_STRING). The data block holds the
// FileBasicInfo whose attribute word (offset 32) is persisted through the share's
// DOS-attribute store, so a client setting Hidden/System/ReadOnly/Archive sticks
// even on a host filesystem that cannot represent those bits. A zero attribute
// word means "no change" ([MS-FSCC] FileBasicInformation), so it is ignored.
func (s *Service) setPathInfo(sh *Share, h protocol.Header, params, data []byte) []byte {
	if len(params) < 6 {
		return errResponse(h, statusUnsuccessful)
	}
	level := bp.LE16(params[0:2])
	store, st := resolvePath(sh, params[6:], h.Flags2)
	if st != statusSuccess {
		return errResponse(h, st)
	}
	return s.applySetBasicInfo(sh, h, store, level, data)
}

// setFileInfo serves TRANS2_SET_FILE_INFORMATION. Params ([MS-CIFS] §2.2.6.9.1):
// FID(2) InformationLevel(2) Reserved(2). The target is the open handle's store
// path; the data block is the same FileBasicInfo as setPathInfo.
func (s *Service) setFileInfo(sess *smbSession, h protocol.Header, params, data []byte) []byte {
	if len(params) < 4 {
		return errResponse(h, statusUnsuccessful)
	}
	fid := bp.LE16(params[0:2])
	level := bp.LE16(params[2:4])
	hnd, ok := sess.fileByFID(fid)
	if !ok {
		return errResponse(h, statusInvalidHandle)
	}
	return s.applySetBasicInfo(hnd.share, h, hnd.path, level, data)
}

// applySetBasicInfo persists the attribute word from a FileBasicInfo data block
// (the level must be a basic-info level) through the share's DOS-attribute store.
// The FileBasicInfo layout ([MS-FSCC] §2.4.7): Creation/LastAccess/LastWrite/
// Change FILETIME (4×8 bytes) then FileAttributes(4) at offset 32. The timestamps
// are accepted-and-ignored (the host mtime is authoritative); only the attribute
// word is persisted. A reply is an empty TRANS2 info response (success).
func (s *Service) applySetBasicInfo(sh *Share, h protocol.Header, store string, level uint16, data []byte) []byte {
	if level != infoSetFileBasic {
		// Other set-info levels (allocation, disposition, rename) are not modelled;
		// answer success so a client's housekeeping set does not fail the operation.
		return buildTrans2InfoResponse(h, nil)
	}
	if len(data) >= 36 {
		attrs := uint16(bp.LE32(data[32:36]) & 0xFFFF)
		// A zero attribute word means "do not change attributes" (FileBasicInformation).
		if attrs != 0 {
			if err := sh.SetAttrs(store, attrs); err != nil {
				return errResponse(h, statusUnsuccessful)
			}
		}
	}
	return buildTrans2InfoResponse(h, nil)
}

// findFirst2 serves TRANS2_FIND_FIRST2. Params ([MS-CIFS] §2.2.6.2.1):
// SearchAttributes(2) SearchCount(2) Flags(2) InformationLevel(2)
// SearchStorageType(4) FileName(SMB_STRING, the wire-charset search path with a
// trailing wildcard). The directory is resolved through the codec, its entries
// filtered by the wildcard, snapshotted, and the first batch packed.
func (s *Service) findFirst2(sess *smbSession, sh *Share, h protocol.Header, params []byte) []byte {
	if len(params) < 12 {
		return errResponse(h, statusNotSupported)
	}
	searchCount := clampSearchCount(int(bp.LE16(params[2:4])))
	flags := bp.LE16(params[4:6])
	infoLevel := bp.LE16(params[6:8])
	if infoLevel != infoFileBothDirInfo {
		return errResponse(h, statusNotSupported)
	}

	dirStore, pattern, st := s.resolveSearchPath(sh, params[12:], h.Flags2)
	if st != statusSuccess {
		return errResponse(h, st)
	}
	rows, st := s.listDir(sh, dirStore, pattern)
	if st != statusSuccess {
		return errResponse(h, st)
	}

	data, returned, lastNameOff := packFindBothDir(sh, rows, searchCount, h.Flags2)
	endOfSearch := returned >= len(rows)

	sid := sess.allocSID(&searchHandle{rows: nil, flags2: h.Flags2})
	if !endOfSearch {
		sess.mu.Lock()
		sess.searches[sid].rows = append([]findRow(nil), rows[returned:]...)
		sess.mu.Unlock()
	} else if flags&(findCloseAfterRequest|findCloseAtEOS) != 0 {
		sess.dropSearch(sid)
	}

	return buildFindResponse(h, true, sid, returned, endOfSearch, data, lastNameOff)
}

// findNext2 serves TRANS2_FIND_NEXT2. Params ([MS-CIFS] §2.2.6.3.1): SID(2)
// SearchCount(2) InformationLevel(2) ResumeKey(4) Flags(2) FileName(SMB_STRING).
// It streams the next batch from the snapshotted searchHandle.
func (s *Service) findNext2(sess *smbSession, sh *Share, h protocol.Header, params []byte) []byte {
	if len(params) < 12 {
		return errResponse(h, statusNotSupported)
	}
	sid := bp.LE16(params[0:2])
	searchCount := clampSearchCount(int(bp.LE16(params[2:4])))
	infoLevel := bp.LE16(params[4:6])
	if infoLevel != infoFileBothDirInfo {
		return errResponse(h, statusNotSupported)
	}
	flags := bp.LE16(params[10:12])

	shndl, ok := sess.search(sid)
	if !ok {
		return errResponse(h, statusNoMoreFiles)
	}
	sess.mu.Lock()
	rows := shndl.rows
	sess.mu.Unlock()
	if len(rows) == 0 {
		if flags&(findCloseAfterRequest|findCloseAtEOS) != 0 {
			sess.dropSearch(sid)
		}
		return errResponse(h, statusNoMoreFiles)
	}

	data, returned, lastNameOff := packFindBothDir(sh, rows, searchCount, h.Flags2)
	endOfSearch := returned >= len(rows)

	sess.mu.Lock()
	shndl.rows = append([]findRow(nil), rows[returned:]...)
	remaining := len(shndl.rows)
	sess.mu.Unlock()
	if (endOfSearch && flags&findCloseAtEOS != 0) || flags&findCloseAfterRequest != 0 || remaining == 0 && endOfSearch {
		sess.dropSearch(sid)
	}

	return buildFindResponse(h, false, 0, returned, endOfSearch, data, lastNameOff)
}

// resolveSearchPath splits a FIND_FIRST2 wire search path into its directory
// store path and the (store-charset) wildcard pattern of its last element. The
// directory is resolved through the codec; the last element is taken as the
// pattern when it contains a wildcard, else the whole path is the directory and
// the pattern is "*".
func (s *Service) resolveSearchPath(sh *Share, wire []byte, flags2 uint16) (dirStore, pattern string, status uint32) {
	raw, _, ok := extractWirePath(wire, flags2)
	if !ok {
		return "", "*", statusSuccess // empty → list share root
	}
	store, err := sh.ResolvePath(raw, flags2)
	if err != nil {
		return "", "", statusObjectNameInvalid
	}
	parent, leaf := storeParent(store)
	if strings.ContainsAny(leaf, "*?") {
		return parent, leaf, statusSuccess
	}
	// No wildcard in the last element: a path naming a directory lists it; a path
	// naming a file matches just that file in its parent.
	if info, err := sh.FS().Stat(store); err == nil && info.IsDir() {
		return store, "*", statusSuccess
	}
	return parent, leaf, statusSuccess
}

// listDir reads dirStore and returns the entries matching the wildcard pattern as
// findRows (name, derived short name, info), sorted by ReadDir order.
func (s *Service) listDir(sh *Share, dirStore, pattern string) ([]findRow, uint32) {
	entries, err := sh.FS().ReadDir(dirStore)
	if err != nil {
		return nil, statusObjectPathNotFound
	}
	rows := make([]findRow, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if !wildcardMatch(name, pattern) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		short := name
		full := name
		if dirStore != "" {
			full = dirStore + "/" + name
		}
		if sn, err := sh.FS().ShortName(full); err == nil && sn != "" {
			short = sn
		}
		rows = append(rows, findRow{name: name, shortName: short, store: full, info: info})
	}
	return rows, statusSuccess
}

// queryPathInfo serves TRANS2_QUERY_PATH_INFORMATION. Params ([MS-CIFS]
// §2.2.6.6.1): InformationLevel(2) Reserved(4) FileName(SMB_STRING).
func (s *Service) queryPathInfo(sh *Share, h protocol.Header, params []byte) []byte {
	if len(params) < 6 {
		return errResponse(h, statusUnsuccessful)
	}
	infoLevel := bp.LE16(params[0:2])
	store, st := resolvePath(sh, params[6:], h.Flags2)
	if st != statusSuccess {
		return errResponse(h, st)
	}
	info, err := sh.FS().Stat(store)
	if err != nil {
		return errResponse(h, statusObjectNameNotFound)
	}
	data, ok := packQueryInfo(infoLevel, info, sh.AttrsFor(store, info))
	if !ok {
		return errResponse(h, statusNotSupported)
	}
	return buildTrans2InfoResponse(h, data)
}

// queryFileInfo serves TRANS2_QUERY_FILE_INFORMATION. Params ([MS-CIFS]
// §2.2.6.8.1): FID(2) InformationLevel(2). The FID is re-Stat'd live.
func (s *Service) queryFileInfo(sess *smbSession, h protocol.Header, params []byte) []byte {
	if len(params) < 4 {
		return errResponse(h, statusUnsuccessful)
	}
	fid := bp.LE16(params[0:2])
	infoLevel := bp.LE16(params[2:4])
	hnd, ok := sess.fileByFID(fid)
	if !ok {
		return errResponse(h, statusInvalidHandle)
	}
	info, err := hnd.share.FS().Stat(hnd.path)
	if err != nil {
		return errResponse(h, statusObjectNameNotFound)
	}
	data, ok := packQueryInfo(infoLevel, info, hnd.share.AttrsFor(hnd.path, info))
	if !ok {
		return errResponse(h, statusNotSupported)
	}
	return buildTrans2InfoResponse(h, data)
}

// handleFindClose2 answers SMB_COM_FIND_CLOSE2 (0x34): release a search SID.
// Request words (WCT=1): SID(2). Reply WCT=0.
func (s *Service) handleFindClose2(sess *smbSession, h protocol.Header, req []byte) []byte {
	words, _, ok := reqBody(req)
	if !ok || len(words) < 2 {
		return successNoData(h)
	}
	sess.dropSearch(bp.LE16(words[0:2]))
	return successNoData(h)
}

// --- packing ---

// packQueryInfo serializes a FileInfo into the requested QUERY_*_INFO level, or
// (nil,false) for an unsupported level ([MS-CIFS] §2.2.8.3). attrs is the DOS
// attribute word the caller computed store-aware (Share.AttrsFor), so persisted
// Hidden/System bits the host cannot represent are reported.
func packQueryInfo(level uint16, info stdfs.FileInfo, attrs uint16) ([]byte, bool) {
	switch level {
	case infoQueryFileBasic:
		buf := make([]byte, 40)
		ft := fileTime(info.ModTime())
		bp.PutLE64(buf[0:8], ft)   // CreationTime
		bp.PutLE64(buf[8:16], ft)  // LastAccessTime
		bp.PutLE64(buf[16:24], ft) // LastWriteTime
		bp.PutLE64(buf[24:32], ft) // ChangeTime
		bp.PutLE32(buf[32:36], uint32(attrs))
		return buf, true
	case infoQueryFileStd:
		buf := make([]byte, 24)
		size := fileSize(info)
		bp.PutLE64(buf[0:8], allocSize(size, info.IsDir()))
		bp.PutLE64(buf[8:16], size)
		bp.PutLE32(buf[16:20], 1) // NumberOfLinks
		if info.IsDir() {
			buf[21] = 1 // Directory
		}
		return buf, true
	case infoQueryFileEA:
		return make([]byte, 4), true // EaSize = 0
	case infoQueryFileAllInfo:
		basic, _ := packQueryInfo(infoQueryFileBasic, info, attrs)
		std, _ := packQueryInfo(infoQueryFileStd, info, attrs)
		ea, _ := packQueryInfo(infoQueryFileEA, info, attrs)
		out := make([]byte, 0, len(basic)+len(std)+len(ea))
		out = append(out, basic...)
		out = append(out, std...)
		return append(out, ea...), true
	default:
		return nil, false
	}
}

// packFindBothDir packs up to maxEntries findRows as SMB_FIND_FILE_BOTH_DIRECTORY_INFO
// records ([MS-CIFS] §2.2.8.1.7): a 94-byte fixed area then the long file name in
// the request wire charset, each record 4-byte aligned via NextEntryOffset (0 on
// the last). Returns the data block, the count packed, and the offset of the last
// record's FileName field (the resume hint).
func packFindBothDir(sh *Share, rows []findRow, maxEntries int, flags2 uint16) (data []byte, returned int, lastNameOffset uint16) {
	out := make([]byte, 0, 128)
	for i := 0; i < len(rows) && returned < maxEntries; i++ {
		row := rows[i]
		nameWire, err := sh.EncodeName(row.name, flags2)
		if err != nil {
			continue // a name the wire charset cannot represent is skipped, not fatal
		}
		shortWire := shortNameWire(sh, row.shortName, flags2)

		const fixed = 94
		recLen := fixed + len(nameWire)
		pad := (4 - recLen%4) % 4
		recStart := len(out)
		last := i == len(rows)-1 || returned == maxEntries-1
		next := uint32(recLen + pad)
		if last {
			next = 0
		}

		rec := make([]byte, recLen+pad)
		bp.PutLE32(rec[0:4], next)
		ft := fileTime(row.info.ModTime())
		bp.PutLE64(rec[8:16], ft)
		bp.PutLE64(rec[16:24], ft)
		bp.PutLE64(rec[24:32], ft)
		bp.PutLE64(rec[32:40], ft)
		size := fileSize(row.info)
		bp.PutLE64(rec[40:48], size)
		bp.PutLE64(rec[48:56], allocSize(size, row.info.IsDir()))
		bp.PutLE32(rec[56:60], uint32(sh.AttrsFor(row.store, row.info)))
		bp.PutLE32(rec[60:64], uint32(len(nameWire)))
		rec[68] = byte(len(shortWire))
		copy(rec[70:94], shortWire)
		copy(rec[94:], nameWire)

		out = append(out, rec...)
		lastNameOffset = uint16(recStart + fixed)
		returned++
	}
	return out, returned, lastNameOffset
}

// shortNameWire encodes a derived short (8.3) name to the request wire charset,
// truncated to the 24-byte ShortName field. A name the charset cannot represent
// falls back to empty (the field is optional).
func shortNameWire(sh *Share, short string, flags2 uint16) []byte {
	b, err := sh.EncodeName(short, flags2)
	if err != nil || len(b) > 24 {
		if err == nil {
			return b[:24]
		}
		return nil
	}
	return b
}

// fileSize returns a FileInfo's byte size, 0 for a directory.
func fileSize(info stdfs.FileInfo) uint64 {
	if info.IsDir() {
		return 0
	}
	return uint64(info.Size())
}

// clampSearchCount bounds a requested batch size to [1, 256].
func clampSearchCount(n int) int {
	if n <= 0 {
		return 1
	}
	if n > 256 {
		return 256
	}
	return n
}

// buildFindResponse encodes a FIND_FIRST2 / FIND_NEXT2 reply. FIND_FIRST2 prepends
// a 2-byte SID (10-byte param block); FIND_NEXT2 omits it (8-byte). The param
// block is SID? + SearchCount(2) + EndOfSearch(2) + EaErrorOffset(2) +
// LastNameOffset(2); the data block holds the packed records.
func buildFindResponse(h protocol.Header, includeSID bool, sid uint16, count int, endOfSearch bool, data []byte, lastNameOffset uint16) []byte {
	paramLen := 8
	if includeSID {
		paramLen = 10
	}
	p := make([]byte, paramLen)
	off := 0
	if includeSID {
		bp.PutLE16(p[0:2], sid)
		off = 2
	}
	bp.PutLE16(p[off:off+2], uint16(count))
	if endOfSearch {
		bp.PutLE16(p[off+2:off+4], 1)
	}
	bp.PutLE16(p[off+6:off+8], lastNameOffset)
	return buildTrans2Response(h, p, data)
}

// buildTrans2InfoResponse builds a TRANS2 reply with a 2-byte EaErrorOffset param
// and the supplied info-level data block (QUERY_PATH/FILE_INFO).
func buildTrans2InfoResponse(h protocol.Header, data []byte) []byte {
	return buildTrans2Response(h, make([]byte, 2), data)
}

// buildTrans2Response frames an SMB_COM_TRANSACTION2 response (WCT=10) carrying a
// parameter block and a data block, each at its own header-relative offset
// ([MS-CIFS] §2.2.4.46.2).
func buildTrans2Response(h protocol.Header, params, data []byte) []byte {
	rh := responseHeader(h, statusSuccess)
	out := rh.Encode(nil)
	out = append(out, 10) // WCT

	// Layout after WCT: 10 words(20) + BCC(2). Params follow, then data; both are
	// referenced by header-relative offsets and padded to even boundaries.
	wordsLen := 20
	paramOffset := protocol.HeaderLen + 1 + wordsLen + 2
	paramPad := (2 - paramOffset%2) % 2
	paramOffset += paramPad
	dataOffset := paramOffset + len(params)
	dataPad := (2 - dataOffset%2) % 2
	dataOffset += dataPad

	w := make([]byte, wordsLen)
	bp.PutLE16(w[0:2], uint16(len(params)))  // TotalParameterCount
	bp.PutLE16(w[2:4], uint16(len(data)))    // TotalDataCount
	bp.PutLE16(w[6:8], uint16(len(params)))  // ParameterCount
	bp.PutLE16(w[8:10], uint16(paramOffset)) // ParameterOffset
	bp.PutLE16(w[12:14], uint16(len(data)))  // DataCount
	bp.PutLE16(w[14:16], uint16(dataOffset)) // DataOffset
	out = append(out, w...)

	bcc := paramPad + len(params) + dataPad + len(data)
	out = append(out, byte(bcc), byte(bcc>>8))
	for i := 0; i < paramPad; i++ {
		out = append(out, 0)
	}
	out = append(out, params...)
	for i := 0; i < dataPad; i++ {
		out = append(out, 0)
	}
	out = append(out, data...)
	return out
}
