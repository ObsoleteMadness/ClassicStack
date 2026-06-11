package afp

import (
	"testing"
)

// catSearchReq builds an FPCatSearch command block: a partial- or full-name
// search over volID, resuming from cursorIndex (0 = new search), asking for up to
// reqMatches results with the given file/dir result bitmaps. The spec1 record
// carries a single name field — a 2-byte offset pointer to a Pascal-string name
// in its tail — which is the shape the Finder's "Find File" sends.
func catSearchReq(volID uint16, reqMatches int, cursorIndex uint32, fileBitmap, dirBitmap uint16, partial bool, name string) []byte {
	b := []byte{cmdCatSearch, 0}
	b = putBE16(b, volID)
	b = putBE32(b, uint32(reqMatches))
	b = putBE32(b, 0) // reserved
	var cursor [16]byte
	cursor[12] = byte(cursorIndex >> 24)
	cursor[13] = byte(cursorIndex >> 16)
	cursor[14] = byte(cursorIndex >> 8)
	cursor[15] = byte(cursorIndex)
	b = append(b, cursor[:]...)
	b = putBE16(b, fileBitmap)
	b = putBE16(b, dirBitmap)

	reqBitmap := catSearchBitFullName
	if partial {
		reqBitmap = catSearchBitPartialName
	}
	b = putBE32(b, reqBitmap)

	// spec1: a parameter block keyed by reqBitmap. With only the name bit set it is
	// a 2-byte name-offset pointer followed by the Pascal-string name in the tail.
	// The offset is measured from the start of the spec block: 2 (the pointer).
	spec1 := putBE16(nil, 2)
	spec1 = putPString(spec1, []byte(name))
	b = putBE16(b, uint16(len(spec1)))
	b = append(b, spec1...)

	// spec2: empty (no ranged fields).
	b = putBE16(b, 0)
	return b
}

// catSearchNames walks a CatSearch reply's ResultsRecord area and returns the
// long names it carries. Each record is StructLength(1) fileDir(1) then a
// parameter block whose LongName field is a 2-byte offset (from the start of the
// parameter block, i.e. just after the fileDir byte) to a Pascal string. The test
// requests fdBitmapLongName as the first (and here only addressed) field, so the
// LongName offset pointer is the first packed field.
func catSearchNames(t *testing.T, reply []byte) []string {
	t.Helper()
	// reply: CatalogPosition(16) fileBitmap(2) dirBitmap(2) actualCount(4) records.
	if len(reply) < 24 {
		t.Fatalf("CatSearch reply too short: %d bytes", len(reply))
	}
	count := int(be32(reply[20:24]))
	var names []string
	off := 24
	for range count {
		if off >= len(reply) {
			break
		}
		structLen := int(reply[off])
		if structLen == 0 || off+structLen > len(reply) {
			break
		}
		rec := reply[off : off+structLen]
		// rec[0]=len, rec[1]=fileDir, rec[2:4]=LongName offset into the param block.
		// The param block begins at rec[2] (after len+fileDir), and the offset is
		// measured from there.
		paramBlock := rec[2:]
		nameOff := int(be16(paramBlock[0:2]))
		if nameOff < len(paramBlock) {
			n := int(paramBlock[nameOff])
			if nameOff+1+n <= len(paramBlock) {
				names = append(names, string(paramBlock[nameOff+1:nameOff+1+n]))
			}
		}
		off += structLen
	}
	return names
}

// TestCatSearch_PartialNameAcrossTree proves a partial-name search finds matches
// at any depth in the catalog (the Finder "Find File" behaviour): the walk
// descends into subdirectories, not just the volume root.
func TestCatSearch_PartialNameAcrossTree(t *testing.T) {
	svc, r := newRunningService(t)
	vol := svc.Volumes()[0]
	mustCreate(t, vol, "report-jan.txt")
	if err := vol.FS().CreateDir("sub"); err != nil {
		t.Fatalf("CreateDir: %v", err)
	}
	mustCreate(t, vol, "sub/report-feb.txt")
	mustCreate(t, vol, "sub/notes.txt")

	sessID, volID := openVolForFork(t, svc, r)

	req := catSearchReq(volID, 50, 0, fdBitmapLongName|fileBitmapFileNum, 0, true, "report")
	code, reply := sendCmd(t, svc, r, sessID, 9, req)
	// Last page (all results fit) → kFPEOFErr per AFP/Netatalk convention.
	if code != afpErrEOFErr && code != afpNoErr {
		t.Fatalf("CatSearch result = %d, want EOFErr(%d) or NoErr(0)", code, afpErrEOFErr)
	}
	names := catSearchNames(t, reply)
	if !contains(names, "report-jan.txt") || !contains(names, "report-feb.txt") {
		t.Fatalf("CatSearch names = %v, want report-jan.txt + report-feb.txt", names)
	}
	if contains(names, "notes.txt") {
		t.Fatalf("CatSearch names = %v, must not include non-matching notes.txt", names)
	}
}

// TestCatSearch_FullNameExact proves a full-name search matches only the exact
// name (case-insensitively), not substrings.
func TestCatSearch_FullNameExact(t *testing.T) {
	svc, r := newRunningService(t)
	vol := svc.Volumes()[0]
	mustCreate(t, vol, "alpha.txt")
	mustCreate(t, vol, "alpha.txt.bak")

	sessID, volID := openVolForFork(t, svc, r)

	req := catSearchReq(volID, 50, 0, fdBitmapLongName, 0, false /*full*/, "ALPHA.TXT")
	_, reply := sendCmd(t, svc, r, sessID, 9, req)
	names := catSearchNames(t, reply)
	if !contains(names, "alpha.txt") {
		t.Fatalf("CatSearch full-name names = %v, want alpha.txt", names)
	}
	if contains(names, "alpha.txt.bak") {
		t.Fatalf("CatSearch full-name names = %v, must not include alpha.txt.bak (substring)", names)
	}
}

// TestCatSearch_Paged proves a search whose results exceed reqMatches returns a
// continuation cursor (result NoErr) and that resuming from it yields the rest
// without repeats.
func TestCatSearch_Paged(t *testing.T) {
	svc, r := newRunningService(t)
	vol := svc.Volumes()[0]
	mustCreate(t, vol, "hit-a.txt")
	mustCreate(t, vol, "hit-b.txt")
	mustCreate(t, vol, "hit-c.txt")

	sessID, volID := openVolForFork(t, svc, r)

	// Page 1: ask for 2 of the 3 "hit-" files.
	req := catSearchReq(volID, 2, 0, fdBitmapLongName, 0, true, "hit-")
	code, reply := sendCmd(t, svc, r, sessID, 9, req)
	if code != afpNoErr {
		t.Fatalf("CatSearch page 1 result = %d, want NoErr (more pages follow)", code)
	}
	page1 := catSearchNames(t, reply)
	if len(page1) != 2 {
		t.Fatalf("CatSearch page 1 = %v, want 2 names", page1)
	}
	cursorIndex := be32(reply[12:16])
	if cursorIndex == 0 {
		t.Fatalf("CatSearch page 1 cursor index = 0, want a continuation index")
	}

	// Page 2: resume from the cursor; should get the remaining hit and finish.
	req2 := catSearchReq(volID, 2, cursorIndex, fdBitmapLongName, 0, true, "hit-")
	code, reply = sendCmd(t, svc, r, sessID, 10, req2)
	if code != afpErrEOFErr {
		t.Fatalf("CatSearch page 2 result = %d, want EOFErr (last page)", code)
	}
	page2 := catSearchNames(t, reply)
	all := append(append([]string{}, page1...), page2...)
	for _, want := range []string{"hit-a.txt", "hit-b.txt", "hit-c.txt"} {
		if !contains(all, want) {
			t.Fatalf("paged CatSearch missing %q; got pages %v + %v", want, page1, page2)
		}
	}
	// No repeats across pages.
	for _, n := range page2 {
		if contains(page1, n) {
			t.Fatalf("CatSearch page 2 repeats %q from page 1", n)
		}
	}
}
