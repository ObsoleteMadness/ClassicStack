package smb

import (
	stdfs "io/fs"
	"strings"
	"time"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"

	protocol "github.com/ObsoleteMadness/ClassicStack/core/protocol/smb"
)

// --- SMB_COM_SEARCH (0x81): the CORE-dialect directory-browse the TRANS2
// FIND_FIRST2/FIND_NEXT2 path replaced for NT clients but which MS-DOS LAN
// Manager and Windows for Workgroups 3.11 still use. It is paged like FIND: the
// first request carries a filename pattern; a continuation carries an empty
// filename and a 21-byte resume key copied verbatim from the previous reply's
// last record. We pack our SID into the resume key's ServerState block and store
// the remaining rows under that SID in the session's searchHandle map (the same
// mechanism FIND_FIRST2 uses), so a continuation resumes where it left off. When
// the rows are exhausted we answer STATUS_NO_MORE_FILES (→ ERRDOS/ERRnofiles),
// which signals end-of-search to the client. Ported from the legacy
// service/smb command_fs_search.go, re-expressed over the §9 share seam. ---

const coreSearchRecordLen = 43 // 21 resume key + 1 attr + 4 time/date + 4 size + 13 name

// coreSearchResumeTag is a server-defined sanity byte placed at resume-key byte 0.
// The client treats the ServerState block (bytes 0..16) as opaque, so any value
// works; a distinctive tag aids debugging.
const coreSearchResumeTag = 0x81

// handleSearch answers SMB_COM_SEARCH (0x81). Request words (WCT=2):
// MaxCount(2) SearchAttributes(2); the byte area carries BufferFormat(0x04)
// FileName NUL and, on a continuation, BufferFormat(0x05) ResumeKeyLength(2)
// ResumeKey[21].
func (s *Service) handleSearch(sess *smbSession, h protocol.Header, req []byte) []byte {
	sh, st := s.treeFor(sess, h)
	if st != statusSuccess {
		return errResponse(h, st)
	}
	words, area, ok := reqBody(req)
	if !ok || len(words) < 4 {
		return errResponse(h, statusNotSupported)
	}
	// MaxCount: [MS-CIFS] §2.2.4.58.1 calls it a session-wide limit, but WfW 3.11
	// sends MaxCount=1 on the initial request and MaxCount=20 on continuations —
	// only per-response semantics fit that, matching real CIFS servers and the
	// legacy service. See spec/errata.md "SMB_COM_SEARCH MaxCount".
	maxCount := int(bp.LE16(words[0:2]))
	if maxCount <= 0 {
		maxCount = 1
	}
	attrs := bp.LE16(words[2:4])

	resumeKey, hasResume := parseSearchResumeKey(area)

	// ClientState (bytes 17..20 of the resume key) is opaque to us and MUST be
	// echoed back unmodified in every response ([MS-CIFS] §2.2.4.58.1).
	var clientState [4]byte
	if hasResume {
		copy(clientState[:], resumeKey[17:21])
	}

	pattern, _ := coreSearchPattern(area)
	isContinuation := hasResume && pattern == ""

	if isContinuation {
		sid := bp.LE16(resumeKey[13:15])
		shndl, ok := sess.search(sid)
		if !ok {
			return errResponse(h, statusNoMoreFiles)
		}
		sess.mu.Lock()
		rows := shndl.rows
		sess.mu.Unlock()
		if len(rows) == 0 {
			sess.dropSearch(sid)
			return errResponse(h, statusNoMoreFiles)
		}
		batch, remaining := sliceCoreBatch(rows, maxCount)
		sess.mu.Lock()
		shndl.rows = remaining
		sess.mu.Unlock()
		if len(remaining) == 0 {
			sess.dropSearch(sid)
		}
		return buildCoreSearchResponse(h, sh, batch, sid, clientState)
	}

	dirStore, filePattern, st := s.resolveSearchPath(sh, coreSearchPathBytes(area, h.Flags2), h.Flags2)
	if st != statusSuccess {
		return errResponse(h, statusNoMoreFiles)
	}
	rows, st := s.listCoreDir(sh, dirStore, filePattern, attrs)
	if st != statusSuccess || len(rows) == 0 {
		return errResponse(h, statusNoMoreFiles)
	}

	batch, remaining := sliceCoreBatch(rows, maxCount)
	sid := sess.allocSID(&searchHandle{rows: remaining, flags2: h.Flags2})
	if len(remaining) == 0 {
		sess.dropSearch(sid)
	}
	return buildCoreSearchResponse(h, sh, batch, sid, clientState)
}

// coreSearchPathBytes returns the raw filename bytes of a SEARCH request's byte
// area as a resolvable path area (a leading 0x04 SMB_FORMAT_ASCII byte, then the
// NUL-terminated name), suitable for resolveSearchPath. On a continuation (no
// filename) it yields an empty area so the caller lists the share root.
func coreSearchPathBytes(area []byte, flags2 uint16) []byte {
	// The SEARCH byte area is BufferFormat(0x04) FileName NUL [BufferFormat(0x05)
	// ResumeKeyLength ResumeKey]. extractWirePath already understands the 0x04
	// prefix, so hand it the area unchanged; it stops at the first NUL.
	if len(area) == 0 || area[0] != 0x04 {
		return nil
	}
	return area
}

// coreSearchPattern extracts the filename pattern from a SEARCH byte area, with
// leading path separators trimmed. An empty result marks a continuation request.
func coreSearchPattern(area []byte) (string, bool) {
	raw, _, ok := extractWirePath(area, 0) // SEARCH names are always OEM/ASCII
	if !ok {
		return "", false
	}
	return strings.TrimLeft(string(raw), "\\"), true
}

// parseSearchResumeKey returns the 21-byte resume-key block from a SEARCH byte
// area, if present. Layout: BufferFormat(0x04) FileName NUL BufferFormat(0x05)
// ResumeKeyLength(2) ResumeKey[21].
func parseSearchResumeKey(area []byte) ([]byte, bool) {
	if len(area) == 0 || area[0] != 0x04 {
		return nil, false
	}
	rest := area[1:]
	nul := indexByte(rest, 0)
	if nul < 0 {
		return nil, false
	}
	rest = rest[nul+1:]
	if len(rest) < 3 || rest[0] != 0x05 {
		return nil, false
	}
	rkLen := int(bp.LE16(rest[1:3]))
	if rkLen != 21 || len(rest) < 3+rkLen {
		return nil, false
	}
	return rest[3 : 3+21], true
}

// listCoreDir reads dirStore and returns the entries matching the wildcard
// pattern and the SearchAttributes filter, as findRows. Directories are included
// only when the attribute filter sets ATTR_DIRECTORY (0x0010); a set ATTR_VOLUME
// (0x0008) means "volume label only", which we do not expose, so it matches
// nothing ([MS-CIFS] §2.2.4.58.1).
func (s *Service) listCoreDir(sh *Share, dirStore, pattern string, attrs uint16) ([]findRow, uint32) {
	if attrs&attrVolume != 0 {
		return nil, statusSuccess // volume label only — none to return
	}
	entries, err := sh.FS().ReadDir(dirStore)
	if err != nil {
		return nil, statusObjectPathNotFound
	}
	rows := make([]findRow, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.IsDir() && attrs&attrDirectory == 0 {
			continue
		}
		name := e.Name()
		full := name
		if dirStore != "" {
			full = dirStore + "/" + name
		}
		short := name
		if sn, err := sh.FS().ShortName(full); err == nil && sn != "" {
			short = sn
		}
		// Match against both the short (8.3) name and the long name, so a client
		// that browses with an 8.3 pattern still finds long-named files.
		if !wildcardMatch(short, pattern) && !wildcardMatch(name, pattern) {
			continue
		}
		rows = append(rows, findRow{name: name, shortName: short, store: full, info: info})
	}
	return rows, statusSuccess
}

// sliceCoreBatch splits rows into the next batch of at most maxCount and the
// remaining rows (destructively paged, matching the searchHandle model).
func sliceCoreBatch(rows []findRow, maxCount int) (batch, remaining []findRow) {
	if maxCount > len(rows) {
		maxCount = len(rows)
	}
	return rows[:maxCount], append([]findRow(nil), rows[maxCount:]...)
}

// buildCoreSearchResponse encodes a SMB_COM_SEARCH reply (WCT=1: Count(2); byte
// area: BufferFormat(0x05) DataLength(2) DirectoryInformationData). Each entry is
// a 43-byte record: 21-byte resume key, 1-byte attributes, 4-byte DOS
// LastWriteTime+Date, 4-byte file size, 13-byte 8.3 name.
//
// Resume-key layout ([MS-CIFS] §2.2.4.58.1): byte 0 Reserved; bytes 1..16
// ServerState (opaque) — we pack the 8.3 base name in 1..8 and the SID in 13..14;
// bytes 17..20 ClientState (echoed verbatim). All private state lives in
// ServerState so the client's ClientState is never clobbered.
func buildCoreSearchResponse(h protocol.Header, sh *Share, entries []findRow, sid uint16, clientState [4]byte) []byte {
	data := make([]byte, 0, len(entries)*coreSearchRecordLen)
	for _, entry := range entries {
		var rec [coreSearchRecordLen]byte

		rec[0] = coreSearchResumeTag
		base, _ := splitDOSName(strings.ToUpper(entry.shortName))
		if len(base) > 8 {
			base = base[:8]
		}
		copy(rec[1:9], "        ")
		copy(rec[1:9], base)
		bp.PutLE16(rec[13:15], sid)
		copy(rec[17:21], clientState[:])

		rec[21] = byte(coreSearchAttrs(sh, entry))
		bp.PutLE32(rec[22:26], dosTimeDate(entry.info.ModTime()))
		bp.PutLE32(rec[26:30], coreSearchSize(entry.info))
		copy(rec[30:43], formatSearchFileName(entry.shortName))

		data = append(data, rec[:]...)
	}

	w := make([]byte, 2)
	bp.PutLE16(w[0:2], uint16(len(entries))) // Count

	area := make([]byte, 3+len(data))
	area[0] = 0x05 // BufferFormat = Variable Block
	bp.PutLE16(area[1:3], uint16(len(data)))
	copy(area[3:], data)

	return reply(h, statusSuccess, 1, w, area)
}

// coreSearchAttrs returns the low-byte DOS FileAttributes for a SEARCH record.
// Only the low-byte bits (0x01..0x20) fit the 1-byte FileAttributes field; the
// share's persisted attribute store is consulted so Hidden/System/ReadOnly bits
// the host cannot represent are still reported.
func coreSearchAttrs(sh *Share, entry findRow) uint16 {
	return sh.AttrsFor(entry.store, entry.info) & 0x00FF
}

// coreSearchSize returns a file's size clamped to the 32-bit FileSize field.
func coreSearchSize(info stdfs.FileInfo) uint32 {
	if info.IsDir() {
		return 0
	}
	size := info.Size()
	if size < 0 {
		return 0
	}
	if size > 0xFFFFFFFF {
		return 0xFFFFFFFF
	}
	return uint32(size)
}

// formatSearchFileName returns the 13-byte FileName field for a SEARCH record.
// [MS-CIFS] §2.2.4.58.2 space-pads to 12 chars + NUL, but WfW 3.11 treats every
// byte before the first NUL as the filename, so we NUL-pad. See spec/errata.md
// "SMB_COM_SEARCH FileName padding".
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
	return out // remaining bytes already zero
}

// splitDOSName splits a name into its 8.3 base and extension at the first dot.
func splitDOSName(s string) (base, ext string) {
	if dot := strings.IndexByte(s, '.'); dot >= 0 {
		return s[:dot], s[dot+1:]
	}
	return s, ""
}

// dosTimeDate packs a Go time into the 32-bit DOS date+time SEARCH replies use
// (low 16 bits = time, high 16 bits = date). Dates before the 1980 DOS epoch are
// clamped to 1980-01-01.
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
