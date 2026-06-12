package smb

import (
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
	p := make([]byte, 12)
	bp.PutLE16(p[2:4], uint16(searchCount))
	bp.PutLE16(p[4:6], flags)
	bp.PutLE16(p[6:8], infoFileBothDirInfo)
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
		names = append(names, string(rec[94:94+nameLen]))
		if next == 0 {
			break
		}
		pos += next
	}
	return names, sid, endOfSearch
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
