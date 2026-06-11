package afp

import (
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/protocol/asp"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/atp"
)

// openDT logs in, opens the volume, and opens the Desktop database, returning the
// session id, volume id, and the DTRefNum FPOpenDT handed out.
func openDT(t *testing.T, svc *Service, r *fakeRouter) (sessID uint8, volID uint16, dtRef uint16) {
	t.Helper()
	sessID, volID = openVolForFork(t, svc, r)

	openDT := []byte{cmdOpenDT, 0}
	openDT = putBE16(openDT, volID)
	code, reply := sendCmd(t, svc, r, sessID, 5, openDT)
	if code != afpNoErr {
		t.Fatalf("OpenDT result = %d, want 0", code)
	}
	dtRef = be16(reply[0:2])
	if dtRef == 0 {
		t.Fatalf("OpenDT returned DTRefNum 0, want non-zero")
	}
	return sessID, volID, dtRef
}

// commentPath builds a (DTRefNum, dirID, pathType, pstring path) request prefix
// for the comment commands.
func commentPath(cmd uint8, dtRef uint16, dirID uint32, name string) []byte {
	b := []byte{cmd, 0}
	b = putBE16(b, dtRef)
	b = putBE32(b, dirID)
	b = append(b, PathTypeUTF8Names)
	b = putPString(b, []byte(name))
	return b
}

// TestDesktop_OpenCloseDT proves FPOpenDT hands out a usable ref and FPCloseDT
// invalidates it (a second close fails).
func TestDesktop_OpenCloseDT(t *testing.T) {
	svc, r := newRunningService(t)
	sessID, _, dtRef := openDT(t, svc, r)

	closeDT := []byte{cmdCloseDT, 0}
	closeDT = putBE16(closeDT, dtRef)
	code, _ := sendCmd(t, svc, r, sessID, 6, closeDT)
	if code != afpNoErr {
		t.Fatalf("CloseDT result = %d, want 0", code)
	}
	// Closing the same ref again is a parameter error (it is gone).
	code, _ = sendCmd(t, svc, r, sessID, 7, closeDT)
	if code != afpErrParamErr {
		t.Fatalf("second CloseDT result = %d, want %d", code, afpErrParamErr)
	}
}

// TestDesktop_CommentRoundTrip proves Add/Get/Remove comment ride the fork seam:
// a comment is stored, read back, then cleared (after which Get reports
// kFPItemNotFound). It also verifies the comment travels with the file's
// metadata (readable directly through the volume's FS).
func TestDesktop_CommentRoundTrip(t *testing.T) {
	svc, r := newRunningService(t)
	vol := svc.Volumes()[0]
	mustCreate(t, vol, "noted.txt")

	sessID, _, dtRef := openDT(t, svc, r)
	comment := []byte("a Finder comment")

	// FPAddComment: path pstring then (even-aligned) comment pstring.
	add := commentPath(cmdAddComment, dtRef, 2, "noted.txt")
	if len(add)%2 != 0 {
		add = append(add, 0) // pad to even before the comment pstring
	}
	add = putPString(add, comment)
	if code, _ := sendCmd(t, svc, r, sessID, 6, add); code != afpNoErr {
		t.Fatalf("AddComment result = %d, want 0", code)
	}
	// The comment landed in the file's metadata container (fork seam).
	if c, ok := vol.FS().ReadComment("noted.txt"); !ok || string(c) != string(comment) {
		t.Fatalf("comment via FS = %q ok=%v, want %q", c, ok, comment)
	}

	// FPGetComment returns it as a pstring.
	get := commentPath(cmdGetComment, dtRef, 2, "noted.txt")
	code, reply := sendCmd(t, svc, r, sessID, 7, get)
	if code != afpNoErr {
		t.Fatalf("GetComment result = %d, want 0", code)
	}
	got, _, ok := pString(reply, 0)
	if !ok || string(got) != string(comment) {
		t.Fatalf("GetComment = %q, want %q", got, comment)
	}

	// FPRemoveComment clears it; GetComment then reports item-not-found.
	rem := commentPath(cmdRemoveComment, dtRef, 2, "noted.txt")
	if code, _ := sendCmd(t, svc, r, sessID, 8, rem); code != afpNoErr {
		t.Fatalf("RemoveComment result = %d, want 0", code)
	}
	code, _ = sendCmd(t, svc, r, sessID, 9, get)
	if code != afpErrItemNotFound {
		t.Fatalf("GetComment after remove = %d, want %d", code, afpErrItemNotFound)
	}
}

// TestDesktop_GetCommentMissing proves a file with no comment reports
// kFPItemNotFound rather than an empty success.
func TestDesktop_GetCommentMissing(t *testing.T) {
	svc, r := newRunningService(t)
	vol := svc.Volumes()[0]
	mustCreate(t, vol, "plain.txt")

	sessID, _, dtRef := openDT(t, svc, r)
	get := commentPath(cmdGetComment, dtRef, 2, "plain.txt")
	if code, _ := sendCmd(t, svc, r, sessID, 6, get); code != afpErrItemNotFound {
		t.Fatalf("GetComment (no comment) = %d, want %d", code, afpErrItemNotFound)
	}
}

// TestDesktop_IconTwoPhaseAddGet drives FPAddIcon over the two-phase ASPWrite path
// (the bitmap is bulk write data) and reads it back with FPGetIcon /
// FPGetIconInfo.
func TestDesktop_IconTwoPhaseAddGet(t *testing.T) {
	svc, r := newRunningService(t)
	from := &recordingPort{}

	// Use the recordingPort path so OpenVol/OpenDT capture the WSS for the
	// server-initiated aspDataWrite. openVolForFork uses a plain fakePort, so log
	// in + open vol + DT through `from` directly.
	sessID := login(t, svc, r)
	openVol := []byte{cmdOpenVol, 0}
	openVol = putBE16(openVol, volBitmapID)
	openVol = putPString(openVol, []byte("Share"))
	r.reset()
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncCommand, sessID, 3), openVol)), from)
	volID := be16(respPayload(r.lastReply())[2:4])

	openDT := []byte{cmdOpenDT, 0}
	openDT = putBE16(openDT, volID)
	r.reset()
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncCommand, sessID, 4), openDT)), from)
	dtRef := be16(respPayload(r.lastReply())[0:2])

	creator := [4]byte{'A', 'P', 'P', 'L'}
	fileType := [4]byte{'T', 'E', 'X', 'T'}
	iconType := uint8(1)
	tag := uint32(0xCAFE)
	bitmap := make([]byte, 256) // ICN# is 256 bytes; arbitrary content here
	for i := range bitmap {
		bitmap[i] = byte(i)
	}

	// Phase 1: aspWrite carrying the FPAddIcon header (no bitmap yet).
	header := fpAddIconHeader(dtRef, creator, fileType, iconType, tag, uint16(len(bitmap)))
	from.sent = nil
	r.reset()
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncWrite, sessID, 9), header)), from)
	if len(from.sent) != 1 {
		t.Fatalf("FPAddIcon aspDataWrite TReqs = %d, want 1", len(from.sent))
	}
	dh, _ := atp.Decode(from.sent[0].Data)
	if bsz := be16(from.sent[0].Data[atp.HeaderSize:]); int(bsz) != len(bitmap) {
		t.Fatalf("aspDataWrite bufferSize = %d, want %d", bsz, len(bitmap))
	}
	if len(r.replies) != 0 {
		t.Fatalf("FPAddIcon replied before data arrived, want 0 got %d", len(r.replies))
	}

	// Phase 2b: the bitmap arrives as an EOM TResp; phase 3 replies success.
	svc.Inbound(dataResponse(dh.TransID, bitmap), from)
	if len(r.replies) != 1 {
		t.Fatalf("FPAddIcon phase-3 replies = %d, want 1", len(r.replies))
	}
	if got := int32(respUserData(r.lastReply())); got != afpNoErr {
		t.Fatalf("FPAddIcon result = %d, want 0", got)
	}

	// FPGetIcon returns the stored bitmap.
	getIcon := []byte{cmdGetIcon, 0}
	getIcon = putBE16(getIcon, dtRef)
	getIcon = append(getIcon, creator[:]...)
	getIcon = append(getIcon, fileType[:]...)
	getIcon = append(getIcon, iconType, 0)
	getIcon = putBE16(getIcon, uint16(len(bitmap)))
	code, gotIcon := sendCmd(t, svc, r, sessID, 10, getIcon)
	if code != afpNoErr {
		t.Fatalf("GetIcon result = %d, want 0", code)
	}
	if string(gotIcon) != string(bitmap) {
		t.Fatalf("GetIcon bitmap mismatch (%d bytes)", len(gotIcon))
	}

	// FPGetIconInfo (index 1) reports the tag, type, icon type, and size.
	getInfo := []byte{cmdGetIconInfo, 0}
	getInfo = putBE16(getInfo, dtRef)
	getInfo = append(getInfo, creator[:]...)
	getInfo = putBE16(getInfo, 1)
	code, info := sendCmd(t, svc, r, sessID, 11, getInfo)
	if code != afpNoErr {
		t.Fatalf("GetIconInfo result = %d, want 0", code)
	}
	if be32(info[0:4]) != tag {
		t.Errorf("GetIconInfo tag = %#x, want %#x", be32(info[0:4]), tag)
	}
	if string(info[4:8]) != string(fileType[:]) {
		t.Errorf("GetIconInfo fileType = %q, want %q", info[4:8], fileType[:])
	}
	if info[8] != iconType {
		t.Errorf("GetIconInfo iconType = %d, want %d", info[8], iconType)
	}
	if sz := be16(info[10:12]); int(sz) != len(bitmap) {
		t.Errorf("GetIconInfo size = %d, want %d", sz, len(bitmap))
	}

	// A second icon index past the end reports item-not-found.
	getInfo2 := []byte{cmdGetIconInfo, 0}
	getInfo2 = putBE16(getInfo2, dtRef)
	getInfo2 = append(getInfo2, creator[:]...)
	getInfo2 = putBE16(getInfo2, 2)
	if code, _ := sendCmd(t, svc, r, sessID, 12, getInfo2); code != afpErrItemNotFound {
		t.Fatalf("GetIconInfo index 2 = %d, want %d", code, afpErrItemNotFound)
	}
}

// TestDesktop_GetIconMissing proves FPGetIcon for an unregistered icon reports
// kFPItemNotFound.
func TestDesktop_GetIconMissing(t *testing.T) {
	svc, r := newRunningService(t)
	sessID, _, dtRef := openDT(t, svc, r)

	getIcon := []byte{cmdGetIcon, 0}
	getIcon = putBE16(getIcon, dtRef)
	getIcon = append(getIcon, 'X', 'X', 'X', 'X')
	getIcon = append(getIcon, 'T', 'E', 'X', 'T')
	getIcon = append(getIcon, 1, 0)
	getIcon = putBE16(getIcon, 256)
	if code, _ := sendCmd(t, svc, r, sessID, 6, getIcon); code != afpErrItemNotFound {
		t.Fatalf("GetIcon (missing) = %d, want %d", code, afpErrItemNotFound)
	}
}

// TestDesktop_APPLRoundTrip proves Add/Get/Remove APPL: a mapping to a real file
// is registered, fetched back by index (with file params), and removed (after
// which Get reports kFPItemNotFound).
func TestDesktop_APPLRoundTrip(t *testing.T) {
	svc, r := newRunningService(t)
	vol := svc.Volumes()[0]
	mustCreate(t, vol, "TeachText")

	sessID, _, dtRef := openDT(t, svc, r)
	creator := [4]byte{'t', 't', 'x', 't'}
	tag := uint32(0x1234)

	// FPAddAPPL: DTRefNum dirID creator tag pathType pstring(path).
	add := []byte{cmdAddAPPL, 0}
	add = putBE16(add, dtRef)
	add = putBE32(add, 2) // dirID root
	add = append(add, creator[:]...)
	add = putBE32(add, tag)
	add = append(add, PathTypeUTF8Names)
	add = append(add, []byte("TeachText")...) // resolveCatalogPath reads rest-of-block
	if code, _ := sendCmd(t, svc, r, sessID, 6, add); code != afpNoErr {
		t.Fatalf("AddAPPL result = %d, want 0", code)
	}

	// FPGetAPPL index 0: bitmap echoed, tag returned, file params packed.
	getAppl := []byte{cmdGetAPPL, 0}
	getAppl = putBE16(getAppl, dtRef)
	getAppl = append(getAppl, creator[:]...)
	getAppl = putBE16(getAppl, 0) // index
	getAppl = putBE16(getAppl, fdBitmapLongName)
	code, reply := sendCmd(t, svc, r, sessID, 7, getAppl)
	if code != afpNoErr {
		t.Fatalf("GetAPPL result = %d, want 0", code)
	}
	if be16(reply[0:2]) != fdBitmapLongName {
		t.Errorf("GetAPPL bitmap = %#x, want %#x", be16(reply[0:2]), fdBitmapLongName)
	}
	if be32(reply[2:6]) != tag {
		t.Errorf("GetAPPL tag = %#x, want %#x", be32(reply[2:6]), tag)
	}
	// The packed file params follow; the LongName offset points at "TeachText".
	params := reply[6:]
	nameOff := int(be16(params[0:2]))
	name, _, ok := pString(params, nameOff)
	if !ok || string(name) != "TeachText" {
		t.Errorf("GetAPPL packed name = %q, want TeachText", name)
	}

	// FPRemoveAPPL drops it; GetAPPL then reports item-not-found.
	rem := []byte{cmdRemoveAPPL, 0}
	rem = putBE16(rem, dtRef)
	rem = putBE32(rem, 2)
	rem = append(rem, creator[:]...)
	rem = append(rem, PathTypeUTF8Names)
	rem = append(rem, []byte("TeachText")...) // resolveCatalogPath reads rest-of-block
	if code, _ := sendCmd(t, svc, r, sessID, 8, rem); code != afpNoErr {
		t.Fatalf("RemoveAPPL result = %d, want 0", code)
	}
	if code, _ := sendCmd(t, svc, r, sessID, 9, getAppl); code != afpErrItemNotFound {
		t.Fatalf("GetAPPL after remove = %d, want %d", code, afpErrItemNotFound)
	}
}

// fpAddIconHeader builds the 20-byte FPAddIcon header carried in a phase-1
// aspWrite: cmd(1) pad(1) DTRefNum(2) creator(4) type(4) iconType(1) pad(1)
// tag(4) size(2), with no inline bitmap.
func fpAddIconHeader(dtRef uint16, creator, fileType [4]byte, iconType uint8, tag uint32, size uint16) []byte {
	h := []byte{cmdAddIcon, 0x00}
	h = putBE16(h, dtRef)
	h = append(h, creator[:]...)
	h = append(h, fileType[:]...)
	h = append(h, iconType, 0)
	h = putBE32(h, tag)
	h = putBE16(h, size)
	return h
}
