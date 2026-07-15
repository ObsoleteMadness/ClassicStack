package smb

import (
	"bytes"
	"strings"
	"testing"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"

	protocol "github.com/ObsoleteMadness/ClassicStack/core/protocol/smb"
)

// trans2Req builds an SMB_COM_TRANSACTION2 request frame carrying one setup word
// (the subcommand) and a parameter block. The TRANS2 word layout places
// ParameterCount/Offset and a SetupCount=1 with the subcommand following.
func trans2Req(tid, sub uint16, params []byte) []byte {
	return trans2ReqWithData(tid, sub, params, nil)
}

// trans2ReqWithData is trans2Req plus a data block (DataCount/DataOffset,
// words[22:24]/[24:26]) — the SET_PATH_INFO/SET_FILE_INFO shape, whose
// payload (e.g. an SMB_FEA_LIST for SMB_INFO_SET_EAS) rides the data block,
// not the params.
func trans2ReqWithData(tid, sub uint16, params, data []byte) []byte {
	return trans2ReqPartialData(tid, sub, params, data, len(data))
}

// trans2ReqPartialData is trans2ReqWithData with an explicit TotalDataCount:
// when totalData exceeds len(data) the request is an INCOMPLETE primary
// ([MS-CIFS] §2.2.4.46.1) whose remaining data bytes arrive in
// SMB_COM_TRANSACTION2_SECONDARY messages (trans2SecondaryData).
func trans2ReqPartialData(tid, sub uint16, params, data []byte, totalData int) []byte {
	// 15 words: the standard TRANS2 fixed words (14) + 1 setup word.
	const wordCount = 15
	words := make([]byte, wordCount*2)
	bp.PutLE16(words[0:2], uint16(len(params))) // TotalParameterCount
	bp.PutLE16(words[2:4], uint16(totalData))   // TotalDataCount
	// ParameterCount(words[18:20]) / ParameterOffset(words[20:22]).
	bp.PutLE16(words[18:20], uint16(len(params)))
	// ParameterOffset is header-relative: header(32) + WCT(1) + words + BCC(2).
	paramOffset := protocol.HeaderLen + 1 + wordCount*2 + 2
	bp.PutLE16(words[20:22], uint16(paramOffset))
	area := params
	if len(data) > 0 {
		bp.PutLE16(words[22:24], uint16(len(data))) // DataCount
		dataOffset := paramOffset + len(params)
		bp.PutLE16(words[24:26], uint16(dataOffset)) // DataOffset
		area = append(append([]byte(nil), params...), data...)
	}
	words[26] = 1 // SetupCount
	bp.PutLE16(words[28:30], sub)
	return smbReq(protocol.CommandTransaction2, protocol.Flags2NTStatus, tid, 1, words, area)
}

// trans2SecondaryData builds an SMB_COM_TRANSACTION2_SECONDARY request
// ([MS-CIFS] §2.2.4.47.1, WCT=9) carrying one data fragment at dataDisp of a
// transaction whose totals are totalParams/totalData.
func trans2SecondaryData(tid uint16, frag []byte, dataDisp, totalParams, totalData int) []byte {
	const wordCount = 9
	words := make([]byte, wordCount*2)
	bp.PutLE16(words[0:2], uint16(totalParams)) // TotalParameterCount
	bp.PutLE16(words[2:4], uint16(totalData))   // TotalDataCount
	// ParameterCount/Offset/Displacement (words[4:10]) stay 0: params complete.
	bp.PutLE16(words[10:12], uint16(len(frag))) // DataCount
	dataOffset := protocol.HeaderLen + 1 + wordCount*2 + 2
	bp.PutLE16(words[12:14], uint16(dataOffset)) // DataOffset
	bp.PutLE16(words[14:16], uint16(dataDisp))   // DataDisplacement
	bp.PutLE16(words[16:18], 0xFFFF)             // FID: none
	return smbReq(protocol.CommandTransaction2Secondary, protocol.Flags2NTStatus, tid, 1, words, frag)
}

// geaList builds an SMB_GEA_LIST ([MS-CIFS] §2.2.1.2.1) requesting the given
// EA names: ULONG SizeOfListInBytes (counting itself) then per name
// AttributeNameLengthInBytes(1) AttributeName NUL(1).
func geaList(names ...string) []byte {
	size := 4
	for _, n := range names {
		size += 1 + len(n) + 1
	}
	out := make([]byte, 4, size)
	bp.PutLE32(out[0:4], uint32(size))
	for _, n := range names {
		out = append(out, byte(len(n)))
		out = append(out, n...)
		out = append(out, 0)
	}
	return out
}

// findFirst2Params builds a FIND_FIRST2 parameter block for the given search
// path (ANSI, NUL-terminated): SearchAttributes(2) SearchCount(2) Flags(2)
// InformationLevel(2) SearchStorageType(4) FileName.
func findFirst2Params(searchCount int, flags uint16, path string) []byte {
	return findFirst2ParamsLevel(searchCount, flags, infoFileBothDirInfo, path)
}

// findFirst2ParamsLevel is findFirst2Params with an explicit information level.
func findFirst2ParamsLevel(searchCount int, flags, level uint16, path string) []byte {
	p := make([]byte, 12)
	bp.PutLE16(p[2:4], uint16(searchCount))
	bp.PutLE16(p[4:6], flags)
	bp.PutLE16(p[6:8], level)
	p = append(p, []byte(path)...)
	return append(p, 0)
}

// findNext2Params builds a FIND_NEXT2 parameter block: SID(2) SearchCount(2)
// InformationLevel(2) ResumeKey(4) Flags(2) FileName.
func findNext2Params(sid uint16, searchCount int, flags uint16) []byte {
	p := make([]byte, 12)
	bp.PutLE16(p[0:2], sid)
	bp.PutLE16(p[2:4], uint16(searchCount))
	bp.PutLE16(p[4:6], infoFileBothDirInfo)
	bp.PutLE16(p[10:12], flags)
	return append(p, 0) // empty resume filename
}

// findReplyNames walks a FIND_FIRST2/FIND_NEXT2 reply's data block and returns the
// packed long names (ANSI), the SID (FIND_FIRST2 only), and the end-of-search
// flag. includeSID selects the 10- vs 8-byte param block layout.
func findReplyNames(t *testing.T, reply []byte, includeSID bool) (names []string, sid uint16, endOfSearch bool) {
	t.Helper()
	w := reply[protocol.HeaderLen+1:]
	paramOffset := int(bp.LE16(w[8:10]))
	dataCount := int(bp.LE16(w[12:14]))
	dataOffset := int(bp.LE16(w[14:16]))

	p := reply[paramOffset:]
	off := 0
	if includeSID {
		sid = bp.LE16(p[0:2])
		off = 2
	}
	count := int(bp.LE16(p[off : off+2]))
	endOfSearch = bp.LE16(p[off+2:off+4]) != 0

	data := reply[dataOffset : dataOffset+dataCount]
	pos := 0
	for i := 0; i < count; i++ {
		rec := data[pos:]
		next := int(bp.LE32(rec[0:4]))
		nameLen := int(bp.LE32(rec[60:64]))
		// On non-Unicode sessions FileNameLength counts one NUL terminator
		// ([MS-CIFS] §2.2.8.1.7 <167>); strip it like a real client.
		names = append(names, strings.TrimRight(string(rec[94:94+nameLen]), "\x00"))
		if next == 0 {
			break
		}
		pos += next
	}
	return names, sid, endOfSearch
}

// findReplyBlocks returns a find reply's parameter and data blocks.
func findReplyBlocks(t *testing.T, reply []byte) (params, data []byte) {
	t.Helper()
	w := reply[protocol.HeaderLen+1:]
	paramCount := int(bp.LE16(w[6:8]))
	paramOffset := int(bp.LE16(w[8:10]))
	dataCount := int(bp.LE16(w[12:14]))
	dataOffset := int(bp.LE16(w[14:16]))
	return reply[paramOffset : paramOffset+paramCount], reply[dataOffset : dataOffset+dataCount]
}

// TestTrans2_FindBothDirCountsASCIITerminator proves an SMB_FIND_FILE_BOTH_DIRECTORY_INFO
// record on a non-Unicode session NUL-terminates FileName and counts that one NUL
// byte in FileNameLength, the Windows NT server behavior the NT 3.51 redirector
// requires ([MS-CIFS] §2.2.8.1.7 <167>/<168>; netbeui.pcap 2026-07-10 — without
// it NT renders the directory listing empty). ShortNameLength must be non-zero
// and carry a real derived 8.3 alternate name: MetaEngine derivation is always
// on by default (the fix for the DOS/Win16 SMB_COM_SEARCH long-name bug), so a
// long name always gets a distinct short name, unlike the old passthrough
// default this test predates.
func TestTrans2_FindBothDirCountsASCIITerminator(t *testing.T) {
	svc, sess, tid := fsService(t)
	sess.closeFID(createFile(t, svc, sess, tid, "longfilename.txt"))

	reply := svc.Dispatch(sess, trans2Req(tid, trans2FindFirst2, findFirst2Params(100, 0, "*")))
	if h := respHeader(t, reply); h.Status != statusSuccess {
		t.Fatalf("FIND_FIRST2 status = %#x", h.Status)
	}
	_, data := findReplyBlocks(t, reply)

	const name = "longfilename.txt"
	if got := int(bp.LE32(data[60:64])); got != len(name)+1 {
		t.Errorf("FileNameLength = %d, want %d (name + counted NUL)", got, len(name)+1)
	}
	if got := string(data[94 : 94+len(name)]); got != name {
		t.Errorf("FileName = %q, want %q", got, name)
	}
	if data[94+len(name)] != 0 {
		t.Error("FileName is not NUL-terminated")
	}
	shortLen := int(data[68])
	if shortLen == 0 {
		t.Fatal("ShortNameLength = 0, want a derived 8.3 alternate name")
	}
	// ShortName is UTF-16LE regardless of session charset ([MS-CIFS] §2.2.8.1.7).
	shortRaw := data[70 : 70+shortLen]
	var short strings.Builder
	for i := 0; i+1 < len(shortRaw); i += 2 {
		short.WriteByte(shortRaw[i])
	}
	if got := short.String(); got != "LONGFI~1.TXT" {
		t.Errorf("ShortName = %q, want LONGFI~1.TXT", got)
	}
}

// TestTrans2_FindInfoStandardLevel proves FIND_FIRST2 serves the LANMAN2.0
// SMB_INFO_STANDARD level ([MS-CIFS] §2.2.8.1.1) OS/2 LAN Server requests
// (netbeui.pcap 2026-07-10 frames 308/316 — rejecting it produced SYS0318):
// optional ResumeKey, DOS date/time stamps, sizes, attributes, then the name
// with an uncounted NUL terminator (<153>).
func TestTrans2_FindInfoStandardLevel(t *testing.T) {
	svc, sess, tid := fsService(t)
	for _, name := range []string{"alpha.txt", "beta.txt"} {
		sess.closeFID(createFile(t, svc, sess, tid, name))
	}

	req := trans2Req(tid, trans2FindFirst2,
		findFirst2ParamsLevel(100, findReturnResumeKeys, infoStandard, "*"))
	reply := svc.Dispatch(sess, req)
	if h := respHeader(t, reply); h.Status != statusSuccess {
		t.Fatalf("FIND_FIRST2 level 0x0001 status = %#x", h.Status)
	}
	params, data := findReplyBlocks(t, reply)
	if count := bp.LE16(params[2:4]); count != 2 {
		t.Fatalf("SearchCount = %d, want 2", count)
	}
	if eos := bp.LE16(params[4:6]); eos == 0 {
		t.Error("EndOfSearch not set for a single-batch listing")
	}

	// Record: ResumeKey(4) dates/times(12) FileDataSize(4) AllocationSize(4)
	// Attributes(2) FileNameLength(1) FileName + NUL.
	want := map[string]bool{"alpha.txt": true, "beta.txt": true}
	pos := 0
	for i := 0; i < 2; i++ {
		rec := data[pos:]
		nameLen := int(rec[26])
		name := string(rec[27 : 27+nameLen])
		if !want[name] {
			t.Errorf("record %d: unexpected name %q", i, name)
		}
		if rec[27+nameLen] != 0 {
			t.Errorf("record %d: FileName not NUL-terminated", i)
		}
		pos += 27 + nameLen + 1
	}
	if pos != len(data) {
		t.Errorf("data block is %d bytes, records consumed %d", len(data), pos)
	}
}

// TestTrans2_FindInfoQueryEaSizeLevel proves the SMB_INFO_QUERY_EA_SIZE level
// ([MS-CIFS] §2.2.8.1.2): SMB_INFO_STANDARD (here without resume keys) plus a
// zero EaSize before the name length.
func TestTrans2_FindInfoQueryEaSizeLevel(t *testing.T) {
	svc, sess, tid := fsService(t)
	sess.closeFID(createFile(t, svc, sess, tid, "alpha.txt"))

	req := trans2Req(tid, trans2FindFirst2,
		findFirst2ParamsLevel(100, 0, infoQueryEaSize, "*"))
	reply := svc.Dispatch(sess, req)
	if h := respHeader(t, reply); h.Status != statusSuccess {
		t.Fatalf("FIND_FIRST2 level 0x0002 status = %#x", h.Status)
	}
	_, data := findReplyBlocks(t, reply)

	// Record: dates/times(12) FileDataSize(4) AllocationSize(4) Attributes(2)
	// EaSize(4) FileNameLength(1) FileName + NUL.
	if ea := bp.LE32(data[22:26]); ea != 0 {
		t.Errorf("EaSize = %d, want 0", ea)
	}
	const name = "alpha.txt"
	if got := int(data[26]); got != len(name) {
		t.Errorf("FileNameLength = %d, want %d (terminator uncounted)", got, len(name))
	}
	if got := string(data[27 : 27+len(name)]); got != name {
		t.Errorf("FileName = %q, want %q", got, name)
	}
}

// TestShortNameUTF16 proves the BOTH_DIRECTORY_INFO ShortName encoder emits
// UTF-16LE uppercase for a distinct valid 8.3 alternate and nothing otherwise
// (the field is "in Unicode format" regardless of session charset,
// [MS-CIFS] §2.2.8.1.7).
func TestShortNameUTF16(t *testing.T) {
	got := shortNameUTF16("longfilename.txt", "LONGFI~1.TXT")
	want := []byte("L\x00O\x00N\x00G\x00F\x00I\x00~\x001\x00.\x00T\x00X\x00T\x00")
	if string(got) != string(want) {
		t.Errorf("shortNameUTF16 = % x, want % x", got, want)
	}
	for name, short := range map[string]string{
		"alpha.txt":      "alpha.txt",      // identical to long name
		"._1516HBWT.INF": "._1516HBWT.INF", // not 8.3 (10-char base)
		"beta.txt":       "",               // absent
	} {
		if b := shortNameUTF16(name, short); b != nil {
			t.Errorf("shortNameUTF16(%q, %q) = % x, want nil", name, short, b)
		}
	}
}

// TestIs8Dot3 exercises the DOS 8.3 validity checker.
func TestIs8Dot3(t *testing.T) {
	for name, want := range map[string]bool{
		"ALPHA.TXT":     true,
		"alpha":         true,
		"12345678.abc":  true,
		"123456789.txt": false, // 9-char base
		"A.B.C":         false, // two dots
		"READ ME.TXT":   false, // space
		".foo":          false, // empty base
		"FOO.":          false, // empty extension
		"NAME.LONG":     false, // 4-char extension
	} {
		if got := is8dot3(name); got != want {
			t.Errorf("is8dot3(%q) = %v, want %v", name, got, want)
		}
	}
}

// TestTrans2_FindFirst2ListsDirectory proves FIND_FIRST2 with "*" returns every
// file created in the share root in one batch (end-of-search set).
func TestTrans2_FindFirst2ListsDirectory(t *testing.T) {
	svc, sess, tid := fsService(t)
	for _, name := range []string{"alpha.txt", "beta.txt", "gamma.dat"} {
		fid := createFile(t, svc, sess, tid, name)
		sess.closeFID(fid)
	}

	req := trans2Req(tid, trans2FindFirst2, findFirst2Params(100, 0, "*"))
	reply := svc.Dispatch(sess, req)
	if h := respHeader(t, reply); h.Status != statusSuccess {
		t.Fatalf("FIND_FIRST2 status = %#x", h.Status)
	}
	names, _, eos := findReplyNames(t, reply, true)
	if !eos {
		t.Error("FIND_FIRST2 end-of-search not set for a single-batch listing")
	}
	if got := len(names); got != 3 {
		t.Fatalf("FIND_FIRST2 returned %d names %v, want 3", got, names)
	}
	want := map[string]bool{"alpha.txt": true, "beta.txt": true, "gamma.dat": true}
	for _, n := range names {
		if !want[n] {
			t.Errorf("unexpected name %q in listing", n)
		}
	}
}

// TestTrans2_FindFirst2WildcardFilters proves a wildcard pattern restricts the
// listing to matching names.
func TestTrans2_FindFirst2WildcardFilters(t *testing.T) {
	svc, sess, tid := fsService(t)
	for _, name := range []string{"alpha.txt", "beta.txt", "gamma.dat"} {
		sess.closeFID(createFile(t, svc, sess, tid, name))
	}

	req := trans2Req(tid, trans2FindFirst2, findFirst2Params(100, 0, "*.txt"))
	reply := svc.Dispatch(sess, req)
	names, _, _ := findReplyNames(t, reply, true)
	if len(names) != 2 {
		t.Fatalf("*.txt matched %v, want 2", names)
	}
	for _, n := range names {
		if n == "gamma.dat" {
			t.Errorf("*.txt should not match %q", n)
		}
	}
}

// TestTrans2_FindFirst2NextPaginates proves a small SearchCount returns the first
// batch with end-of-search clear, and FIND_NEXT2 streams the remainder.
func TestTrans2_FindFirst2NextPaginates(t *testing.T) {
	svc, sess, tid := fsService(t)
	for _, name := range []string{"f1", "f2", "f3", "f4"} {
		sess.closeFID(createFile(t, svc, sess, tid, name))
	}

	first := svc.Dispatch(sess, trans2Req(tid, trans2FindFirst2, findFirst2Params(2, 0, "*")))
	names, sid, eos := findReplyNames(t, first, true)
	if eos {
		t.Fatal("FIND_FIRST2 end-of-search set despite a partial batch")
	}
	if len(names) != 2 {
		t.Fatalf("first batch %v, want 2", names)
	}

	next := svc.Dispatch(sess, trans2Req(tid, trans2FindNext2, findNext2Params(sid, 100, 0)))
	if h := respHeader(t, next); h.Status != statusSuccess {
		t.Fatalf("FIND_NEXT2 status = %#x", h.Status)
	}
	more, _, eos2 := findReplyNames(t, next, false)
	if !eos2 {
		t.Error("FIND_NEXT2 end-of-search not set after draining the search")
	}
	if len(more) != 2 {
		t.Fatalf("FIND_NEXT2 batch %v, want 2", more)
	}

	// A further FIND_NEXT2 on the drained search → NO_MORE_FILES.
	drained := svc.Dispatch(sess, trans2Req(tid, trans2FindNext2, findNext2Params(sid, 100, 0)))
	if h := respHeader(t, drained); h.Status != statusNoMoreFiles {
		t.Fatalf("drained FIND_NEXT2 status = %#x, want NO_MORE_FILES", h.Status)
	}
}

// TestTrans2_QueryPathInfoBasic proves QUERY_PATH_INFO at the BASIC level returns
// the file's attribute word.
func TestTrans2_QueryPathInfoBasic(t *testing.T) {
	svc, sess, tid := fsService(t)
	sess.closeFID(createFile(t, svc, sess, tid, "q.txt"))

	// Params: InformationLevel(2) Reserved(4) FileName.
	p := make([]byte, 6)
	bp.PutLE16(p[0:2], infoQueryFileBasic)
	p = append(p, []byte("q.txt")...)
	p = append(p, 0)

	reply := svc.Dispatch(sess, trans2Req(tid, trans2QueryPathInfo, p))
	if h := respHeader(t, reply); h.Status != statusSuccess {
		t.Fatalf("QUERY_PATH_INFO status = %#x", h.Status)
	}
	w := reply[protocol.HeaderLen+1:]
	dataOffset := int(bp.LE16(w[14:16]))
	attrs := bp.LE32(reply[dataOffset+32 : dataOffset+36])
	if attrs&uint32(attrArchive) == 0 {
		t.Errorf("BASIC info attrs = %#x, want archive bit", attrs)
	}
}

// TestTrans2_QueryPathInfoStandardDateTimeFieldOrder proves QUERY_PATH_INFO at
// SMB_INFO_STANDARD (0x0001) packs each SMB_DATE/SMB_TIME pair in the spec's
// Date-then-Time wire order ([MS-CIFS] §2.2.8.3.1) with plausible bit values —
// not the swapped fields a `cd, ct := smbServerTimeDate(...)` call-site bug
// produced (smbServerTimeDate returns (smbTime, smbDate), so destructuring it
// date-first silently swapped every timestamp on the wire). Wireshark decoded
// the swapped output as an invalid DOS date ("2046-13-11", month 13) on every
// TRANS2_QUERY_PATH_INFORMATION SMB_INFO_STANDARD reply — including the root
// `\` query OS/2 WPS issues before listing a share — and netbeui.pcap
// 2026-07-15 frame 752 is exactly that: WPS never advanced past the invalid
// root timestamp to enumerate the folder.
func TestTrans2_QueryPathInfoStandardDateTimeFieldOrder(t *testing.T) {
	svc, sess, tid := fsService(t)
	sess.closeFID(createFile(t, svc, sess, tid, "s.txt"))

	p := make([]byte, 6)
	bp.PutLE16(p[0:2], infoStandard)
	p = append(p, []byte("s.txt")...)
	p = append(p, 0)

	reply := svc.Dispatch(sess, trans2Req(tid, trans2QueryPathInfo, p))
	if h := respHeader(t, reply); h.Status != statusSuccess {
		t.Fatalf("QUERY_PATH_INFO(STANDARD) status = %#x", h.Status)
	}
	w := reply[protocol.HeaderLen+1:]
	dataOffset := int(bp.LE16(w[14:16]))
	data := reply[dataOffset:]

	// Layout ([MS-CIFS] §2.2.8.3.1): CreationDate(2) CreationTime(2)
	// LastAccessDate(2) LastAccessTime(2) LastWriteDate(2) LastWriteTime(2) ...
	for _, pair := range []struct {
		name           string
		dateOff, tmOff int
	}{
		{"Creation", 0, 2},
		{"LastAccess", 4, 6},
		{"LastWrite", 8, 10},
	} {
		date := bp.LE16(data[pair.dateOff : pair.dateOff+2])
		tm := bp.LE16(data[pair.tmOff : pair.tmOff+2])
		day := date & 0x1F
		month := (date >> 5) & 0x0F
		hour := (tm >> 11) & 0x1F
		if day == 0 || day > 31 || month == 0 || month > 12 {
			t.Errorf("%s: DOS date bits decode to invalid day=%d month=%d (date word %#04x, time word %#04x) — date/time fields likely swapped", pair.name, day, month, date, tm)
		}
		if hour > 23 {
			t.Errorf("%s: DOS time bits decode to invalid hour=%d (time word %#04x) — date/time fields likely swapped", pair.name, hour, tm)
		}
	}
}

// trans2RespData slices a TRANS2 reply's data block (DataCount/DataOffset from
// the response words) and its parameter count. It does not reassemble a
// chunked (multi-message) response — use trans2RespDataReassembled for a
// caller that must see the whole TotalDataCount, e.g. after an oversized EA
// query has queued continuations on the session (buildTrans2Response).
func trans2RespData(t *testing.T, reply []byte) (data []byte, paramCount int) {
	t.Helper()
	w := reply[protocol.HeaderLen+1:]
	paramCount = int(bp.LE16(w[6:8]))
	dataCount := int(bp.LE16(w[12:14]))
	dataOffset := int(bp.LE16(w[14:16]))
	if dataOffset+dataCount > len(reply) {
		t.Fatalf("TRANS2 data block out of range (off %d count %d len %d)", dataOffset, dataCount, len(reply))
	}
	return reply[dataOffset : dataOffset+dataCount], paramCount
}

// trans2RespDataReassembled reassembles a (possibly chunked) TRANS2 response's
// full data block: the primary reply plus any continuation frames
// buildTrans2Response queued on sess, placed at their DataDisplacement —
// mirroring what a real client's TRANS2 reassembly does ([MS-CIFS]
// §2.2.4.46.2). Drains the session's continuation queue.
func trans2RespDataReassembled(t *testing.T, sess *smbSession, reply []byte) []byte {
	t.Helper()
	w := reply[protocol.HeaderLen+1:]
	totalData := int(bp.LE16(w[2:4]))
	out := make([]byte, totalData)
	place := func(msg []byte) {
		mw := msg[protocol.HeaderLen+1:]
		dataCount := int(bp.LE16(mw[12:14]))
		dataOffset := int(bp.LE16(mw[14:16]))
		dataDisp := int(bp.LE16(mw[16:18]))
		if dataOffset+dataCount > len(msg) || dataDisp+dataCount > len(out) {
			t.Fatalf("TRANS2 fragment data block out of range (off %d count %d disp %d len %d)", dataOffset, dataCount, dataDisp, len(msg))
		}
		copy(out[dataDisp:], msg[dataOffset:dataOffset+dataCount])
	}
	place(reply)
	frames, _ := sess.drainContinuations()
	for _, f := range frames {
		place(f)
	}
	return out
}

// TestTrans2_QueryFSVolumeInfo proves QUERY_FS_INFORMATION at
// SMB_QUERY_FS_VOLUME_INFO (the level NT 3.51 issues right after opening a
// share, netbeui.pcap frame 491) returns the FileFsVolumeInformation structure
// with a Unicode label and no parameter bytes ([MS-CIFS] §2.2.6.4.2/§2.2.8.2.3).
func TestTrans2_QueryFSVolumeInfo(t *testing.T) {
	svc, sess, tid := fsService(t)

	p := make([]byte, 2)
	bp.PutLE16(p[0:2], fsQueryVolumeInfo)
	reply := svc.Dispatch(sess, trans2Req(tid, trans2QueryFSInfo, p))
	if h := respHeader(t, reply); h.Status != statusSuccess {
		t.Fatalf("QUERY_FS_INFO(VOLUME_INFO) status = %#x, want success", h.Status)
	}
	data, params := trans2RespData(t, reply)
	if params != 0 {
		t.Errorf("QUERY_FS_INFO response ParameterCount = %d, want 0", params)
	}
	if len(data) < 18 {
		t.Fatalf("VOLUME_INFO data len = %d, want >= 18", len(data))
	}
	labelSize := int(bp.LE32(data[12:16]))
	if labelSize == 0 || 18+labelSize != len(data) {
		t.Fatalf("VolumeLabelSize = %d, data len = %d, want 18+size", labelSize, len(data))
	}
	// The label is UTF-16LE regardless of the request charset.
	var label []byte
	for i := 18; i+1 < len(data); i += 2 {
		label = append(label, data[i])
		if data[i+1] != 0 {
			t.Fatalf("VolumeLabel not UTF-16LE ASCII: % x", data[18:])
		}
	}
	if string(label) != "PUBLIC" {
		t.Errorf("VolumeLabel = %q, want PUBLIC", label)
	}
}

// TestTrans2_QueryFSInfoLevels proves each period QUERY_FS_INFORMATION level a
// legacy client may request is served with the spec'd structure size.
func TestTrans2_QueryFSInfoLevels(t *testing.T) {
	cases := []struct {
		name    string
		level   uint16
		minLen  int
		exact   bool
		wantLen int
	}{
		{"SMB_INFO_ALLOCATION", fsInfoAllocation, 0, true, 18},
		{"SMB_INFO_VOLUME", fsInfoVolume, 5, false, 0},
		{"SMB_QUERY_FS_SIZE_INFO", fsQuerySizeInfo, 0, true, 24},
		{"SMB_QUERY_FS_DEVICE_INFO", fsQueryDeviceInfo, 0, true, 8},
		{"SMB_QUERY_FS_ATTRIBUTE_INFO", fsQueryAttributeInfo, 12, false, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc, sess, tid := fsService(t)
			p := make([]byte, 2)
			bp.PutLE16(p[0:2], c.level)
			reply := svc.Dispatch(sess, trans2Req(tid, trans2QueryFSInfo, p))
			if h := respHeader(t, reply); h.Status != statusSuccess {
				t.Fatalf("level %#x status = %#x, want success", c.level, h.Status)
			}
			data, _ := trans2RespData(t, reply)
			if c.exact && len(data) != c.wantLen {
				t.Fatalf("data len = %d, want %d", len(data), c.wantLen)
			}
			if !c.exact && len(data) < c.minLen {
				t.Fatalf("data len = %d, want >= %d", len(data), c.minLen)
			}
		})
	}

	// DEVICE_INFO content: FILE_DEVICE_DISK, mounted.
	svc, sess, tid := fsService(t)
	p := make([]byte, 2)
	bp.PutLE16(p[0:2], fsQueryDeviceInfo)
	data, _ := trans2RespData(t, svc.Dispatch(sess, trans2Req(tid, trans2QueryFSInfo, p)))
	if bp.LE32(data[0:4]) != fileDeviceDisk || bp.LE32(data[4:8]) != fileDeviceIsMounted {
		t.Errorf("DEVICE_INFO = %x/%x, want disk/mounted", bp.LE32(data[0:4]), bp.LE32(data[4:8]))
	}

	// An unknown level still refuses.
	bp.PutLE16(p[0:2], 0x01FF)
	reply := svc.Dispatch(sess, trans2Req(tid, trans2QueryFSInfo, p))
	if h := respHeader(t, reply); h.Status == statusSuccess {
		t.Error("unknown FS info level answered success, want error")
	}
}

// TestTrans2_QueryFileNameInfo proves QUERY_FILE_INFO at
// SMB_QUERY_FILE_NAME_INFO (0x0104 — asked by NT 3.51 for the share-root FID,
// netbeui.pcap frame 486) returns FileNameLength + the '\'-rooted name in
// UTF-16LE ([MS-CIFS] §2.2.8.3.9), independent of the request charset.
func TestTrans2_QueryFileNameInfo(t *testing.T) {
	svc, sess, tid := fsService(t)
	fid := createFile(t, svc, sess, tid, "n.txt")

	p := make([]byte, 4)
	bp.PutLE16(p[0:2], fid)
	bp.PutLE16(p[2:4], infoQueryFileName)
	reply := svc.Dispatch(sess, trans2Req(tid, trans2QueryFileInfo, p))
	if h := respHeader(t, reply); h.Status != statusSuccess {
		t.Fatalf("QUERY_FILE_INFO(NAME_INFO) status = %#x, want success", h.Status)
	}
	data, _ := trans2RespData(t, reply)
	want := "\\n.txt"
	if got := int(bp.LE32(data[0:4])); got != 2*len(want) {
		t.Fatalf("FileNameLength = %d, want %d", got, 2*len(want))
	}
	for i, r := range want {
		if data[4+2*i] != byte(r) || data[5+2*i] != 0 {
			t.Fatalf("FileName not UTF-16LE %q: % x", want, data[4:])
		}
	}
}

// TestTrans2_SetEAsQueryAllEAsRoundTrip proves TRANS2_SET_PATH_INFORMATION at
// SMB_INFO_SET_EAS (0x0002) persists an SMB_FEA_LIST through the share's
// MetaEngine EA store, and a later TRANS2_QUERY_PATH_INFORMATION at
// SMB_INFO_QUERY_ALL_EAS (0x0004) returns the same list — the OS/2 Workplace
// Shell path (netbeui.pcap frame 770's ".LONGNAME"/".TYPE"/".CLASSINFO" style
// EAs) this session's WRITE_AND_CLOSE fix depends on for correctness.
func TestTrans2_SetEAsQueryAllEAsRoundTrip(t *testing.T) {
	svc, sess, tid := fsService(t)
	sess.closeFID(createFile(t, svc, sess, tid, "obj.dat"))

	eas := []fs.EA{
		{Name: ".LONGNAME", Value: []byte("A Workplace Object.dat")},
		{Name: ".TYPE", Value: []byte("EAT_ASCII"), NeedEA: true},
	}
	feaList := packFEAList(eas)

	setParams := make([]byte, 6) // InformationLevel(2) Reserved(4)
	bp.PutLE16(setParams[0:2], infoSetEAs)
	setParams = append(setParams, ansiPathArea("obj.dat")...)
	setReply := svc.Dispatch(sess, trans2ReqWithData(tid, trans2SetPathInfo, setParams, feaList))
	if h := respHeader(t, setReply); h.Status != statusSuccess {
		t.Fatalf("SET_PATH_INFO(SET_EAS) status = %#x", h.Status)
	}

	queryParams := make([]byte, 6)
	bp.PutLE16(queryParams[0:2], infoQueryAllEAs)
	queryParams = append(queryParams, ansiPathArea("obj.dat")...)
	queryReply := svc.Dispatch(sess, trans2Req(tid, trans2QueryPathInfo, queryParams))
	if h := respHeader(t, queryReply); h.Status != statusSuccess {
		t.Fatalf("QUERY_PATH_INFO(QUERY_ALL_EAS) status = %#x", h.Status)
	}
	data, _ := trans2RespData(t, queryReply)
	got, ok, _ := parseFEAList(data)
	if !ok {
		t.Fatalf("QUERY_ALL_EAS returned an unparsable SMB_FEA_LIST: % x", data)
	}
	if len(got) != len(eas) {
		t.Fatalf("EA count = %d, want %d", len(got), len(eas))
	}
	for i := range eas {
		if got[i].Name != eas[i].Name || string(got[i].Value) != string(eas[i].Value) || got[i].NeedEA != eas[i].NeedEA {
			t.Errorf("EA %d = %+v, want %+v", i, got[i], eas[i])
		}
	}
}

// eatASCIIValue packs an EA value in FEA2's typed EAT_ASCII envelope (2-byte
// type 0xFFFD LE, 2-byte length LE, then text) — the form OS/2 Workplace
// Shell always writes .LONGNAME in (netbeui.pcap frame 666: `fd ff 17 00
// "This is a new title.exe"`).
func eatASCIIValue(text string) []byte {
	v := make([]byte, 4+len(text))
	bp.PutLE16(v[0:2], eatASCIIMarker)
	bp.PutLE16(v[2:4], uint16(len(text)))
	copy(v[4:], text)
	return v
}

// TestTrans2_LongNameEAListedAndOpenable proves the OS/2 HPFS .LONGNAME
// convention: a file created with an 8.3 host name (e.g. as OS/2 itself would
// on a FAT-mounted volume) whose .LONGNAME EA is later set is (1) reported by
// FIND_FIRST2 under the long name rather than the host name, and (2) openable
// (chained OPEN_ANDX → READ_ANDX) by that long name — mirroring netbeui.pcap
// frame 666 (SET_EAS .LONGNAME) and frames 812/813 (open+read
// "\Really long file name here.COM").
func TestTrans2_LongNameEAListedAndOpenable(t *testing.T) {
	svc, sess, tid := fsService(t)
	sess.closeFID(createFile(t, svc, sess, tid, "TITLE~1.EXE"))

	const long = "This is a new title.exe"
	feaList := packFEAList([]fs.EA{{Name: ".LONGNAME", Value: eatASCIIValue(long)}})
	setParams := make([]byte, 6)
	bp.PutLE16(setParams[0:2], infoSetEAs)
	setParams = append(setParams, ansiPathArea("TITLE~1.EXE")...)
	setReply := svc.Dispatch(sess, trans2ReqWithData(tid, trans2SetPathInfo, setParams, feaList))
	if h := respHeader(t, setReply); h.Status != statusSuccess {
		t.Fatalf("SET_PATH_INFO(SET_EAS .LONGNAME) status = %#x", h.Status)
	}

	// FIND_FIRST2 "*" reports the long name, not the host 8.3 name.
	findReply := svc.Dispatch(sess, trans2Req(tid, trans2FindFirst2, findFirst2Params(100, 0, "*")))
	if h := respHeader(t, findReply); h.Status != statusSuccess {
		t.Fatalf("FIND_FIRST2 status = %#x", h.Status)
	}
	names, _, _ := findReplyNames(t, findReply, true)
	if len(names) != 1 || names[0] != long {
		t.Fatalf("FIND_FIRST2 names = %v, want [%q]", names, long)
	}

	// Chained OPEN_ANDX -> READ_ANDX by the long name succeeds (the FID
	// inheritance fix), proving the long name resolves back to the host file.
	req := openAndXReadAndXBlock(tid, sess.uid, "\\"+long, 0xFFFF)
	openReply := svc.Dispatch(sess, req)
	if h := respHeader(t, openReply); h.Status != statusSuccess {
		t.Fatalf("open-by-longname status = %#x, want success", h.Status)
	}
}

// TestTrans2_SetEAsUpsertsNotReplaces proves that successive
// TRANS2_SET_PATH_INFORMATION SMB_INFO_SET_EAS requests, each carrying only
// ONE new/changed EA, accumulate rather than clobber each other — the OS/2
// Workplace Shell pattern from netbeui.pcap 2026-07-14 (separate SET calls
// for .SUBJECT, then .ICON, then .COMMENTS, then .KEYPHRASES on the same
// file). A naive full-replace SetEAs would leave only the last EA set; a
// subsequent QUERY_ALL_EAS must see all four. Also proves a zero-length
// value DELETES that EA (the OS/2 DosSetPathInfo/DosSetFileInfo convention),
// by re-setting .SUBJECT to empty and confirming it drops out.
func TestTrans2_SetEAsUpsertsNotReplaces(t *testing.T) {
	svc, sess, tid := fsService(t)
	sess.closeFID(createFile(t, svc, sess, tid, "foo.lnk"))

	setOne := func(ea fs.EA) {
		t.Helper()
		p := make([]byte, 6)
		bp.PutLE16(p[0:2], infoSetEAs)
		p = append(p, ansiPathArea("foo.lnk")...)
		reply := svc.Dispatch(sess, trans2ReqWithData(tid, trans2SetPathInfo, p, packFEAList([]fs.EA{ea})))
		if h := respHeader(t, reply); h.Status != statusSuccess {
			t.Fatalf("SET_PATH_INFO(SET_EAS %q) status = %#x", ea.Name, h.Status)
		}
	}
	setOne(fs.EA{Name: ".SUBJECT", Value: []byte("Subject Set")})
	setOne(fs.EA{Name: ".ICON", Value: []byte{0xde, 0xad, 0xbe, 0xef}})
	setOne(fs.EA{Name: ".COMMENTS", Value: []byte("Another Comment")})
	setOne(fs.EA{Name: ".KEYPHRASES", Value: []byte("key phrase set")})

	queryAll := func() []fs.EA {
		t.Helper()
		p := make([]byte, 6)
		bp.PutLE16(p[0:2], infoQueryAllEAs)
		p = append(p, ansiPathArea("foo.lnk")...)
		reply := svc.Dispatch(sess, trans2Req(tid, trans2QueryPathInfo, p))
		if h := respHeader(t, reply); h.Status != statusSuccess {
			t.Fatalf("QUERY_PATH_INFO(QUERY_ALL_EAS) status = %#x", h.Status)
		}
		data, _ := trans2RespData(t, reply)
		got, ok, _ := parseFEAList(data)
		if !ok {
			t.Fatalf("QUERY_ALL_EAS returned an unparsable SMB_FEA_LIST: % x", data)
		}
		return got
	}

	got := queryAll()
	want := map[string]string{
		".SUBJECT":    "Subject Set",
		".ICON":       string([]byte{0xde, 0xad, 0xbe, 0xef}),
		".COMMENTS":   "Another Comment",
		".KEYPHRASES": "key phrase set",
	}
	if len(got) != len(want) {
		t.Fatalf("EA count after 4 single-EA SETs = %d, want %d (got %+v)", len(got), len(want), got)
	}
	for _, e := range got {
		if wantVal, ok := want[e.Name]; !ok || string(e.Value) != wantVal {
			t.Errorf("EA %q = %q, want %q", e.Name, e.Value, wantVal)
		}
	}

	// Re-setting .SUBJECT with an empty value deletes it; the rest survive.
	setOne(fs.EA{Name: ".SUBJECT", Value: nil})
	got = queryAll()
	if len(got) != len(want)-1 {
		t.Fatalf("EA count after deleting .SUBJECT = %d, want %d", len(got), len(want)-1)
	}
	for _, e := range got {
		if e.Name == ".SUBJECT" {
			t.Fatal(".SUBJECT still present after empty-value SET")
		}
	}
}

// TestTrans2_SetEAsCaseInsensitivePath proves TRANS2_SET_PATH_INFORMATION
// SMB_INFO_SET_EAS and a later TRANS2_QUERY_PATH_INFORMATION SMB_INFO_QUERY_ALL_EAS
// see the SAME EAs even when the two requests spell the filename with
// different casing — netbeui.pcap 2026-07-13: OS/2 WPS created
// "foo.lnk", set a .SUBJECT EA (frames 2783/2784), then queried it back as
// "foo.LNK" (frame 2802) and got an empty list, because the store-path keys
// used for the EA lookup didn't case-fold to the same path SMB is caseless
// by convention (Share.ResolvePath now folds through fs.ResolveFold).
func TestTrans2_SetEAsCaseInsensitivePath(t *testing.T) {
	svc, sess, tid := fsService(t)
	sess.closeFID(createFile(t, svc, sess, tid, "foo.lnk"))

	eas := []fs.EA{{Name: ".SUBJECT", Value: []byte("This is a subject")}}
	setParams := make([]byte, 6)
	bp.PutLE16(setParams[0:2], infoSetEAs)
	setParams = append(setParams, ansiPathArea("foo.lnk")...)
	if h := respHeader(t, svc.Dispatch(sess, trans2ReqWithData(tid, trans2SetPathInfo, setParams, packFEAList(eas)))); h.Status != statusSuccess {
		t.Fatalf("SET_PATH_INFO(SET_EAS) status = %#x", h.Status)
	}

	// Query back with different casing, as OS/2 WPS did in the capture.
	queryParams := make([]byte, 6)
	bp.PutLE16(queryParams[0:2], infoQueryAllEAs)
	queryParams = append(queryParams, ansiPathArea("foo.LNK")...)
	queryReply := svc.Dispatch(sess, trans2Req(tid, trans2QueryPathInfo, queryParams))
	if h := respHeader(t, queryReply); h.Status != statusSuccess {
		t.Fatalf("QUERY_PATH_INFO(QUERY_ALL_EAS) status = %#x", h.Status)
	}
	data, _ := trans2RespData(t, queryReply)
	got, ok, _ := parseFEAList(data)
	if !ok {
		t.Fatalf("QUERY_ALL_EAS returned an unparsable SMB_FEA_LIST: % x", data)
	}
	if len(got) != 1 || got[0].Name != ".SUBJECT" || string(got[0].Value) != string(eas[0].Value) {
		t.Fatalf("cross-case EA lookup = %+v, want %+v", got, eas)
	}
}

// TestTrans2_SetEAsCaseInsensitivePathEasFromList is
// TestTrans2_SetEAsCaseInsensitivePath but through SMB_INFO_QUERY_EAS_FROM_LIST
// (0x0003) instead of SMB_INFO_QUERY_ALL_EAS — the exact level and cross-case
// pattern from netbeui.pcap 2026-07-15 frames 1190-1201: OS/2 set a .ICON EA on
// "1516HBWT.cab" then queried EAS_FROM_LIST on "1516HBWT.CAB" and got .ICON
// back as an empty placeholder instead of its real value.
func TestTrans2_SetEAsCaseInsensitivePathEasFromList(t *testing.T) {
	svc, sess, tid := fsService(t)
	sess.closeFID(createFile(t, svc, sess, tid, "1516HBWT.cab"))

	icon := []byte{0xF9, 0xFF, 0x04, 0x00, 0xDE, 0xAD, 0xBE, 0xEF}
	eas := []fs.EA{{Name: ".ICON", Value: icon}}
	setParams := make([]byte, 6)
	bp.PutLE16(setParams[0:2], infoSetEAs)
	setParams = append(setParams, ansiPathArea("1516HBWT.cab")...)
	if h := respHeader(t, svc.Dispatch(sess, trans2ReqWithData(tid, trans2SetPathInfo, setParams, packFEAList(eas)))); h.Status != statusSuccess {
		t.Fatalf("SET_PATH_INFO(SET_EAS) status = %#x", h.Status)
	}

	queryParams := make([]byte, 6)
	bp.PutLE16(queryParams[0:2], infoQueryEasFromList)
	queryParams = append(queryParams, ansiPathArea("1516HBWT.CAB")...)
	queryReply := svc.Dispatch(sess, trans2ReqWithData(tid, trans2QueryPathInfo, queryParams, geaList(".ICON", ".APPTYPE", ".CHECKSUM", ".ASSOCTABLE")))
	if h := respHeader(t, queryReply); h.Status != statusSuccess {
		t.Fatalf("QUERY_PATH_INFO(EAS_FROM_LIST) status = %#x", h.Status)
	}
	data, _ := trans2RespData(t, queryReply)
	got, ok, _ := parseFEAList(data)
	if !ok {
		t.Fatalf("EAS_FROM_LIST returned an unparsable SMB_FEA_LIST: % x", data)
	}
	if len(got) != 4 || got[0].Name != ".ICON" || !bytes.Equal(got[0].Value, icon) {
		t.Fatalf("cross-case EAS_FROM_LIST lookup = %+v, want [.ICON=%x, .APPTYPE=<empty>, .CHECKSUM=<empty>, .ASSOCTABLE=<empty>]", got, icon)
	}
}

// TestTrans2_FindEasFromListLevel proves FIND_FIRST2 at
// SMB_INFO_QUERY_EAS_FROM_LIST (0x0003) embeds the file's real SMB_FEA_LIST
// (not just a size), matching QUERY_PATH_INFO's view of the same EAs.
func TestTrans2_FindEasFromListLevel(t *testing.T) {
	svc, sess, tid := fsService(t)
	sess.closeFID(createFile(t, svc, sess, tid, "tagged.dat"))

	// Two EAs stored; the FIND request's SMB_GEA_LIST names only .ICON, so
	// only .ICON may come back ([MS-CIFS] §2.2.8.1.3 returns the requested
	// names, not the file's whole list — OS/2 WPS probes one name at a time
	// with a 4356-byte buffer, netbeui.pcap 2026-07-14 frame 334).
	eas := []fs.EA{
		{Name: ".ICON", Value: []byte{0xDE, 0xAD, 0xBE, 0xEF}},
		{Name: ".SUBJECT", Value: []byte("unrequested")},
	}
	setParams := make([]byte, 6)
	bp.PutLE16(setParams[0:2], infoSetEAs)
	setParams = append(setParams, ansiPathArea("tagged.dat")...)
	if h := respHeader(t, svc.Dispatch(sess, trans2ReqWithData(tid, trans2SetPathInfo, setParams, packFEAList(eas)))); h.Status != statusSuccess {
		t.Fatalf("SET_PATH_INFO(SET_EAS) status = %#x", h.Status)
	}

	req := trans2ReqWithData(tid, trans2FindFirst2, findFirst2ParamsLevel(100, 0, infoQueryEasFromList, "*"), geaList(".ICON"))
	reply := svc.Dispatch(sess, req)
	if h := respHeader(t, reply); h.Status != statusSuccess {
		t.Fatalf("FIND_FIRST2 level 0x0003 status = %#x", h.Status)
	}
	_, data := findReplyBlocks(t, reply)

	// Record: dates/times(12) FileDataSize(4) AllocationSize(4) Attributes(2)
	// SMB_FEA_LIST(variable) FileNameLength(1) FileName + NUL.
	got, ok, _ := parseFEAList(data[22:])
	if !ok {
		t.Fatalf("embedded SMB_FEA_LIST unparsable: % x", data[22:])
	}
	if len(got) != 1 || got[0].Name != ".ICON" || string(got[0].Value) != string(eas[0].Value) {
		t.Fatalf("embedded EAs = %+v, want only the requested %+v", got, eas[0])
	}
}

// TestTrans2_QueryEasFromListFiltersByName proves TRANS2_QUERY_PATH_INFORMATION
// at SMB_INFO_QUERY_EAS_FROM_LIST honours the request's SMB_GEA_LIST name
// filter ([MS-CIFS] §2.2.8.3.3 — "pairs where the AttributeName field values
// match those that were provided in the request"): the match is
// case-insensitive (OS/2 EA names are caseless), a requested-but-missing name
// still contributes a zero-length placeholder FEA (not nothing — confirmed
// against real IBM Peer traffic, captures/ibm-peer-clients.pcapng frames
// 505/507 and 1428/1432: the server always answers one FEA per requested name,
// EA Data Length 0 for ones the file lacks), and unrequested EAs stay out of
// the response. Honouring the name filter is what keeps OS/2 WPS's tiny
// per-name probes (.ICON1, .SUBJECT — netbeui.pcap 2026-07-14 frame 334) from
// hauling a stored multi-KB .ICON over the client's 4356-byte buffer.
func TestTrans2_QueryEasFromListFiltersByName(t *testing.T) {
	svc, sess, tid := fsService(t)
	sess.closeFID(createFile(t, svc, sess, tid, "probed.dat"))

	eas := []fs.EA{
		{Name: ".SUBJECT", Value: []byte("a subject")},
		{Name: ".ICON", Value: bytes.Repeat([]byte{0xA5}, 6834)},
	}
	setParams := make([]byte, 6)
	bp.PutLE16(setParams[0:2], infoSetEAs)
	setParams = append(setParams, ansiPathArea("probed.dat")...)
	if h := respHeader(t, svc.Dispatch(sess, trans2ReqWithData(tid, trans2SetPathInfo, setParams, packFEAList(eas)))); h.Status != statusSuccess {
		t.Fatalf("SET_PATH_INFO(SET_EAS) status = %#x", h.Status)
	}

	queryFromList := func(names ...string) []fs.EA {
		t.Helper()
		p := make([]byte, 6)
		bp.PutLE16(p[0:2], infoQueryEasFromList)
		p = append(p, ansiPathArea("probed.dat")...)
		reply := svc.Dispatch(sess, trans2ReqWithData(tid, trans2QueryPathInfo, p, geaList(names...)))
		if h := respHeader(t, reply); h.Status != statusSuccess {
			t.Fatalf("QUERY_PATH_INFO(EAS_FROM_LIST %v) status = %#x", names, h.Status)
		}
		data, _ := trans2RespData(t, reply)
		got, ok, _ := parseFEAList(data)
		if !ok {
			t.Fatalf("EAS_FROM_LIST %v returned an unparsable SMB_FEA_LIST: % x", names, data)
		}
		return got
	}

	// Lower-case request must match the upper-case stored name — and must NOT
	// drag the 6834-byte .ICON along.
	got := queryFromList(".subject")
	if len(got) != 1 || got[0].Name != ".SUBJECT" || string(got[0].Value) != "a subject" {
		t.Fatalf("EAS_FROM_LIST(.subject) = %+v, want just .SUBJECT", got)
	}

	// A requested-but-missing name still yields a zero-length placeholder FEA,
	// not an empty list — the real IBM Peer server always answers positionally
	// to the request.
	got = queryFromList(".ICON1")
	if len(got) != 1 || got[0].Name != ".ICON1" || len(got[0].Value) != 0 {
		t.Fatalf("EAS_FROM_LIST(.ICON1) = %+v, want one zero-length placeholder", got)
	}

	// A mixed request (found + not-found) preserves request order and includes
	// a placeholder for the miss.
	got = queryFromList(".SUBJECT", ".NOPE")
	if len(got) != 2 || got[0].Name != ".SUBJECT" || string(got[0].Value) != "a subject" ||
		got[1].Name != ".NOPE" || len(got[1].Value) != 0 {
		t.Fatalf("EAS_FROM_LIST(.SUBJECT,.NOPE) = %+v, want [.SUBJECT=<val>, .NOPE=<empty>]", got)
	}
}

// TestTrans2_SetEAsSecondaryReassembly proves a TRANS2 SET_PATH_INFORMATION
// SMB_INFO_SET_EAS split across a primary + SMB_COM_TRANSACTION2_SECONDARY
// messages reassembles and applies — the OS/2 WPS icon-set path (netbeui.pcap
// 2026-07-14 frames 237-243: a 6848-byte FEA list for a 6834-byte .ICON EA,
// primary DataCount 4240 of TotalDataCount 6848; answering the primary with an
// error instead of the interim response aborts the transfer, frame 243). The
// primary must draw the WCT=0/BCC=0 interim success, mid-transaction
// secondaries no response at all, and the completing secondary the final
// TRANS2 response; the assembled EA must then round-trip byte-for-byte.
func TestTrans2_SetEAsSecondaryReassembly(t *testing.T) {
	svc, sess, tid := fsService(t)
	sess.closeFID(createFile(t, svc, sess, tid, "iconed.dat"))

	icon := make([]byte, 6834)
	for i := range icon {
		icon[i] = byte(i)
	}
	feaList := packFEAList([]fs.EA{{Name: ".ICON", Value: icon}})

	setParams := make([]byte, 6)
	bp.PutLE16(setParams[0:2], infoSetEAs)
	setParams = append(setParams, ansiPathArea("iconed.dat")...)

	// Primary: all params, first 4240 data bytes (the capture's split).
	const firstChunk = 4240
	interim := svc.Dispatch(sess, trans2ReqPartialData(tid, trans2SetPathInfo, setParams, feaList[:firstChunk], len(feaList)))
	if h := respHeader(t, interim); h.Status != statusSuccess {
		t.Fatalf("interim response status = %#x, want success", h.Status)
	}
	if wct := interim[protocol.HeaderLen]; wct != 0 {
		t.Fatalf("interim response WCT = %d, want 0", wct)
	}
	if bcc := bp.LE16(interim[protocol.HeaderLen+1 : protocol.HeaderLen+3]); bcc != 0 {
		t.Fatalf("interim response BCC = %d, want 0", bcc)
	}

	// A mid-transaction secondary gets no response ([MS-CIFS] §2.2.4.47).
	mid := firstChunk + (len(feaList)-firstChunk)/2
	if resp := svc.Dispatch(sess, trans2SecondaryData(tid, feaList[firstChunk:mid], firstChunk, len(setParams), len(feaList))); resp != nil {
		t.Fatalf("mid-transaction secondary drew a response: % x", resp)
	}

	// The completing secondary executes the transaction; its response is a
	// TRANS2 (0x32) response ([MS-CIFS] §2.2.4.46 — all responses of a
	// transaction carry SMB_COM_TRANSACTION2).
	final := svc.Dispatch(sess, trans2SecondaryData(tid, feaList[mid:], mid, len(setParams), len(feaList)))
	fh := respHeader(t, final)
	if fh.Status != statusSuccess {
		t.Fatalf("final response status = %#x, want success", fh.Status)
	}
	if fh.Command != protocol.CommandTransaction2 {
		t.Fatalf("final response command = %#x, want SMB_COM_TRANSACTION2", fh.Command)
	}

	// The assembled 6834-byte .ICON must round-trip byte-for-byte.
	queryParams := make([]byte, 6)
	bp.PutLE16(queryParams[0:2], infoQueryAllEAs)
	queryParams = append(queryParams, ansiPathArea("iconed.dat")...)
	reply := svc.Dispatch(sess, trans2Req(tid, trans2QueryPathInfo, queryParams))
	if h := respHeader(t, reply); h.Status != statusSuccess {
		t.Fatalf("QUERY_PATH_INFO(QUERY_ALL_EAS) status = %#x", h.Status)
	}
	data := trans2RespDataReassembled(t, sess, reply)
	got, ok, _ := parseFEAList(data)
	if !ok {
		t.Fatalf("QUERY_ALL_EAS returned an unparsable SMB_FEA_LIST")
	}
	if len(got) != 1 || got[0].Name != ".ICON" || !bytes.Equal(got[0].Value, icon) {
		t.Fatalf("reassembled .ICON does not round-trip (got %d EAs, first %q len %d)", len(got), got[0].Name, len(got[0].Value))
	}
}

// TestTrans2_ResponseChunkedAtMaxBufferSize proves an oversized TRANS2 response
// (QUERY_ALL_EAS on a file with a large .ICON, mirroring the real server
// behaviour in captures/ibm-peer-clients.pcapng frames 633/637/641) is split
// into multiple SMB_COM_TRANSACTION2 response messages once it exceeds the
// session's maxBufferSize (default 4356 — [MS-CIFS] "MaxBufferSize" spec
// default), each carrying TotalDataCount for the whole reply but only its own
// slice at its own DataDisplacement, and that the primary response ALONE (no
// reassembly) is too small to hold the full data — proving real chunking
// happened, not a lucky single-message fit.
func TestTrans2_ResponseChunkedAtMaxBufferSize(t *testing.T) {
	svc, sess, tid := fsService(t)
	sess.closeFID(createFile(t, svc, sess, tid, "big.dat"))

	icon := make([]byte, 6834)
	for i := range icon {
		icon[i] = byte(i)
	}
	feaList := packFEAList([]fs.EA{{Name: ".ICON", Value: icon}})
	setParams := make([]byte, 6)
	bp.PutLE16(setParams[0:2], infoSetEAs)
	setParams = append(setParams, ansiPathArea("big.dat")...)
	if h := respHeader(t, svc.Dispatch(sess, trans2ReqWithData(tid, trans2SetPathInfo, setParams, feaList))); h.Status != statusSuccess {
		t.Fatalf("SET_PATH_INFO(SET_EAS) status = %#x", h.Status)
	}

	queryParams := make([]byte, 6)
	bp.PutLE16(queryParams[0:2], infoQueryAllEAs)
	queryParams = append(queryParams, ansiPathArea("big.dat")...)
	reply := svc.Dispatch(sess, trans2Req(tid, trans2QueryPathInfo, queryParams))
	if h := respHeader(t, reply); h.Status != statusSuccess {
		t.Fatalf("QUERY_PATH_INFO(QUERY_ALL_EAS) status = %#x", h.Status)
	}

	w := reply[protocol.HeaderLen+1:]
	totalData := int(bp.LE16(w[2:4]))
	primaryDataCount := int(bp.LE16(w[12:14]))
	primaryDataDisp := int(bp.LE16(w[16:18]))
	if totalData <= int(defaultClientMaxBufferSize) {
		t.Fatalf("test fixture too small to force chunking: TotalDataCount = %d", totalData)
	}
	if primaryDataCount >= totalData {
		t.Fatalf("primary response DataCount = %d, TotalDataCount = %d — response was not chunked", primaryDataCount, totalData)
	}
	if primaryDataDisp != 0 {
		t.Fatalf("primary response DataDisplacement = %d, want 0", primaryDataDisp)
	}

	frames, _ := sess.drainContinuations()
	if len(frames) == 0 {
		t.Fatal("no continuation frames queued despite a chunked primary response")
	}
	gotBytes := primaryDataCount
	for i, f := range frames {
		fh := respHeader(t, f)
		if fh.Command != protocol.CommandTransaction2 {
			t.Fatalf("continuation[%d] command = %#x, want SMB_COM_TRANSACTION2", i, fh.Command)
		}
		fw := f[protocol.HeaderLen+1:]
		fTotal := int(bp.LE16(fw[2:4]))
		if fTotal != totalData {
			t.Fatalf("continuation[%d] TotalDataCount = %d, want %d (must match every fragment)", i, fTotal, totalData)
		}
		disp := int(bp.LE16(fw[16:18]))
		if disp != gotBytes {
			t.Fatalf("continuation[%d] DataDisplacement = %d, want %d (contiguous with prior fragments)", i, disp, gotBytes)
		}
		gotBytes += int(bp.LE16(fw[12:14]))
	}
	if gotBytes != totalData {
		t.Fatalf("fragments cover %d bytes, want %d (TotalDataCount)", gotBytes, totalData)
	}
}
