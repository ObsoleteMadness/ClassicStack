package smb

import (
	"strings"
	"testing"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"

	protocol "github.com/ObsoleteMadness/ClassicStack/core/protocol/smb"
)

// trans2Req builds an SMB_COM_TRANSACTION2 request frame carrying one setup word
// (the subcommand) and a parameter block. The TRANS2 word layout places
// ParameterCount/Offset and a SetupCount=1 with the subcommand following.
func trans2Req(tid, sub uint16, params []byte) []byte {
	// 15 words: the standard TRANS2 fixed words (14) + 1 setup word.
	const wordCount = 15
	words := make([]byte, wordCount*2)
	// ParameterCount(words[18:20]) / ParameterOffset(words[20:22]).
	bp.PutLE16(words[18:20], uint16(len(params)))
	// ParameterOffset is header-relative: header(32) + WCT(1) + words + BCC(2).
	paramOffset := protocol.HeaderLen + 1 + wordCount*2 + 2
	bp.PutLE16(words[20:22], uint16(paramOffset))
	words[26] = 1 // SetupCount
	bp.PutLE16(words[28:30], sub)
	return smbReq(protocol.CommandTransaction2, protocol.Flags2NTStatus, tid, 1, words, params)
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

// trans2RespData slices a TRANS2 reply's data block (DataCount/DataOffset from
// the response words) and its parameter count.
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
