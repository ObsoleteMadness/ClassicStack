package smb

import (
	"testing"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
	proto "github.com/ObsoleteMadness/ClassicStack/core/protocol/smb"
)

// fakeFindTransport replays a real Win98 directory-paging conversation: FIND_FIRST2
// returns the first batch and a search id (SID); each FIND_NEXT2 returns one more batch
// until the directory is exhausted, at which point Win98 answers with an EMPTY page —
// SearchCount=0, DataCount=0, EndOfSearch=0 (the flag is NOT set; see errata "a
// FIND_NEXT2 page that returns zero entries is end-of-search"). It records the SID each
// FIND_NEXT2 carried so the test can assert the client threads the FIND_FIRST2 SID
// across every page rather than re-reading it (which parsed as 0 and drew ERRDOS/badfid).
type fakeFindTransport struct {
	sid       uint16
	pages     [][]string // remaining FIND_NEXT2 batches (FIND_FIRST2 serves pages[0])
	nextSIDs  []uint16   // SID observed on each FIND_NEXT2 request, in order
	firstDone bool
}

func (t *fakeFindTransport) MaxResponse() int { return maxMessage }
func (t *fakeFindTransport) Close() error     { return nil }

func (t *fakeFindTransport) Send(req []byte) ([]byte, error) {
	h, _ := proto.DecodeHeader(req)
	// Only TRANS2 FIND requests are exercised here; anything else is a test bug.
	wct := int(req[proto.HeaderLen])
	w := req[proto.HeaderLen+1 : proto.HeaderLen+1+2*wct]
	sub := bp.LE16(w[28:30])
	pOff := int(bp.LE16(w[20:22]))
	pLen := int(bp.LE16(w[18:20]))
	params := req[pOff : pOff+pLen]

	switch sub {
	case 0x0001: // FIND_FIRST2
		t.firstDone = true
		names := t.pages[0]
		t.pages = t.pages[1:]
		return buildFindResp(h.MID, true, t.sid, names), nil
	case 0x0002: // FIND_NEXT2 — record the SID it carried (params[0:2])
		t.nextSIDs = append(t.nextSIDs, bp.LE16(params[0:2]))
		if len(t.pages) == 0 {
			// Exhausted: Win98's empty end-of-search page (flag clear).
			return buildFindResp(h.MID, false, t.sid, nil), nil
		}
		names := t.pages[0]
		t.pages = t.pages[1:]
		return buildFindResp(h.MID, false, t.sid, names), nil
	default:
		t.nextSIDs = append(t.nextSIDs, 0xDEAD)
		return buildFindResp(h.MID, false, t.sid, nil), nil
	}
}

// buildFindResp frames a TRANS2 FIND response (WCT=10) with EndOfSearch always CLEAR —
// mirroring Win98, which ends a search only by returning an empty batch. first controls
// whether the parameter block carries the leading SID (FIND_FIRST2 does, FIND_NEXT2 does
// not). names are packed as minimal SMB_FIND_FILE_BOTH_DIRECTORY_INFO records (ASCII).
func buildFindResp(mid uint16, first bool, sid uint16, names []string) []byte {
	h := proto.Header{Command: proto.CommandTransaction2, Status: proto.StatusSuccess, Flags: proto.FlagReply, MID: mid}
	out := h.Encode(nil)

	// Parameter block. FIND_FIRST2: SID SearchCount EndOfSearch EaErrOff LastNameOff (10).
	// FIND_NEXT2: SearchCount EndOfSearch EaErrOff LastNameOff (8).
	var params []byte
	if first {
		params = make([]byte, 10)
		bp.PutLE16(params[0:2], sid)
		bp.PutLE16(params[2:4], uint16(len(names)))
	} else {
		params = make([]byte, 8)
		bp.PutLE16(params[0:2], uint16(len(names)))
	}
	// EndOfSearch is intentionally left 0 in both param blocks (Win98 never sets it).

	data := packBothDirInfo(names)

	const wct = 10
	words := make([]byte, 2*wct)
	// Header-relative offsets: header + WCT byte + words + BCC(2).
	base := proto.HeaderLen + 1 + 2*wct + 2
	pOff := base
	dOff := pOff + len(params)

	bp.PutLE16(words[0:2], uint16(len(params))) // TotalParameterCount
	bp.PutLE16(words[2:4], uint16(len(data)))   // TotalDataCount
	bp.PutLE16(words[6:8], uint16(len(params))) // ParameterCount
	bp.PutLE16(words[8:10], uint16(pOff))       // ParameterOffset
	bp.PutLE16(words[12:14], uint16(len(data))) // DataCount
	bp.PutLE16(words[14:16], uint16(dOff))      // DataOffset

	out = append(out, byte(wct))
	out = append(out, words...)
	area := append(append([]byte(nil), params...), data...)
	out = append(out, byte(len(area)), byte(len(area)>>8))
	out = append(out, area...)
	return out
}

// packBothDirInfo builds a chain of minimal SMB_FIND_FILE_BOTH_DIRECTORY_INFO records
// (94-byte fixed area + ASCII name), NextEntryOffset chaining, 0 terminating.
func packBothDirInfo(names []string) []byte {
	var out []byte
	for i, name := range names {
		rec := make([]byte, 94+len(name))
		bp.PutLE32(rec[60:64], uint32(len(name))) // FileNameLength
		copy(rec[94:], name)
		next := len(rec)
		if i == len(names)-1 {
			next = 0
		}
		bp.PutLE32(rec[0:4], uint32(next)) // NextEntryOffset
		out = append(out, rec...)
	}
	return out
}

// TestReadDirPagesUntilEmptyPage proves ReadDir (1) carries the FIND_FIRST2 search id
// across every FIND_NEXT2 page and (2) stops on an empty page even though the server
// never sets the EndOfSearch flag — the two live Win98 bugs (ERRDOS/badfid on page 3,
// then an infinite FIND_NEXT2 loop). See errata "a FIND_NEXT2 page that returns zero
// entries is end-of-search".
func TestReadDirPagesUntilEmptyPage(t *testing.T) {
	const wantSID = 0x402d
	tr := &fakeFindTransport{
		sid: wantSID,
		pages: [][]string{
			{"AAA", "BBB"}, // FIND_FIRST2 batch
			{"CCC", "DDD"}, // FIND_NEXT2 batch 1
			{"EEE"},        // FIND_NEXT2 batch 2
			// then the empty end-of-search page
		},
	}
	f := New(&Session{tr: tr, builder: proto.Builder{PID: clientPID}})

	entries, err := f.ReadDir("WINDOWS")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	want := []string{"AAA", "BBB", "CCC", "DDD", "EEE"}
	if len(got) != len(want) {
		t.Fatalf("entries = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entries = %v, want %v", got, want)
		}
	}

	// Every FIND_NEXT2 must have carried the FIND_FIRST2 SID — including the one that hit
	// the empty page. Three FIND_NEXT2 requests fire (batch1, batch2, empty).
	if len(tr.nextSIDs) != 3 {
		t.Fatalf("FIND_NEXT2 count = %d, want 3 (loop must terminate on empty page)", len(tr.nextSIDs))
	}
	for i, sid := range tr.nextSIDs {
		if sid != wantSID {
			t.Errorf("FIND_NEXT2 #%d carried SID %#04x, want %#04x", i, sid, wantSID)
		}
	}
}
