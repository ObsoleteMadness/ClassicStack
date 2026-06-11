package afp

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/asp"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/atp"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
	"github.com/ObsoleteMadness/ClassicStack/core/router"
)

// --- fake router surface (Reply records the response datagrams). ---

type fakeRouter struct {
	mu      sync.Mutex
	replies []ddp.Datagram
}

func (f *fakeRouter) Reply(d ddp.Datagram, _ router.RoutedPort, ddpType uint8, data []byte) {
	// Reply echoes the source/dest swap the real router does; for the test we
	// only need the response payload and the swapped src socket.
	f.mu.Lock()
	f.replies = append(f.replies, ddp.Datagram{
		DestNetwork: d.SrcNetwork,
		SrcNetwork:  d.DestNetwork,
		DestNode:    d.SrcNode,
		SrcNode:     d.DestNode,
		DestSocket:  d.SrcSocket,
		SrcSocket:   d.DestSocket,
		DDPType:     ddpType,
		Data:        append([]byte(nil), data...),
	})
	f.mu.Unlock()
}
func (f *fakeRouter) Route(ddp.Datagram, bool) error      { return nil }
func (f *fakeRouter) RoutingTable() *router.RoutingTable  { return nil }
func (f *fakeRouter) Zones() *router.ZoneInformationTable { return nil }
func (f *fakeRouter) Ports() []router.RoutedPort          { return nil }
func (f *fakeRouter) lastReply() ddp.Datagram             { return f.replies[len(f.replies)-1] }
func (f *fakeRouter) reset()                              { f.mu.Lock(); f.replies = nil; f.mu.Unlock() }

// fakePort is a minimal RoutedPort; Reply ignores it, so the methods are stubs.
type fakePort struct{ router.RoutedPort }

func (fakePort) Node() uint8 { return 1 }

// --- request encoders ---

// ddpTo wraps an ATP frame in a DDP datagram addressed to the AFP socket from a
// client at net.node:wss.
func ddpTo(sock uint8, frame []byte) ddp.Datagram {
	return ddp.Datagram{
		DestNetwork: 1, SrcNetwork: 1,
		DestNode: 2, SrcNode: 10,
		DestSocket: sock, SrcSocket: 200,
		DDPType: atp.DDPType,
		Data:    frame,
	}
}

// atpTReq builds an ATP TReq with a single-packet bitmap and the given UserData
// and payload.
func atpTReq(userData uint32, payload []byte) []byte {
	h := atp.Header{Control: atp.TREQ | atp.XO, Bitmap: 0x01, TransID: 7, UserData: userData}
	return append(h.Encode(nil), payload...)
}

// aspUserData packs the ASP UserData (function, session/ws, seq/version).
func aspUserData(fn, b1 uint8, b23 uint16) uint32 {
	return uint32(fn)<<24 | uint32(b1)<<16 | uint32(b23)
}

// respPayload extracts the ASP/AFP reply data from the last reply datagram
// (everything after the ATP header).
func respPayload(d ddp.Datagram) []byte { return d.Data[atp.HeaderSize:] }

// respUserData extracts the ATP UserData (AFP result / ASP reply fields).
func respUserData(d ddp.Datagram) uint32 {
	h, _ := atp.Decode(d.Data)
	return h.UserData
}

// newRunningService builds an AFP service with one memfs volume, binds a fake
// router, and starts it.
func newRunningService(t *testing.T) (*Service, *fakeRouter) {
	t.Helper()
	svc, err := NewWithVolumes(nil, VolumeSpec{
		ID:   1,
		Name: "Share",
		Share: fs.ShareSpec{
			Name: "Share", FSType: "memfs",
			ForkBackend: "appledouble", FilenameCodec: "macroman-utf8",
		},
	})
	if err != nil {
		t.Fatalf("NewWithVolumes: %v", err)
	}
	r := &fakeRouter{}
	svc.SetRouter(r)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return svc, r
}

// login drives ASPGetStatus → OpenSession → FPLogin(guest) and returns the
// session id for follow-on commands.
func login(t *testing.T, svc *Service, r *fakeRouter) uint8 {
	t.Helper()
	from := fakePort{}

	// ASPGetStatus → server-info block.
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncGetStatus, 0, 0), nil)), from)
	if len(r.replies) == 0 {
		t.Fatal("GetStatus produced no reply")
	}

	// OpenSession.
	r.reset()
	openUD := aspUserData(asp.SPFuncOpenSess, 200 /*wss*/, asp.Version)
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(openUD, nil)), from)
	if len(r.replies) == 0 {
		t.Fatal("OpenSession produced no reply")
	}
	openReply := respUserData(r.lastReply())
	sessID := uint8(openReply >> 16)
	if errCode := int16(openReply & 0xFFFF); errCode != asp.SPErrorNoError {
		t.Fatalf("OpenSession error = %d, want 0", errCode)
	}
	if sessID == 0 {
		t.Fatal("OpenSession returned session id 0")
	}

	// FPLogin (guest).
	r.reset()
	loginBlock := []byte{cmdLogin}
	loginBlock = putPString(loginBlock, []byte("AFP2.2"))
	loginBlock = putPString(loginBlock, []byte("No User Authent"))
	cmdUD := aspUserData(asp.SPFuncCommand, sessID, 1)
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(cmdUD, loginBlock)), from)
	if got := int32(respUserData(r.lastReply())); got != uint32From(afpNoErr) {
		t.Fatalf("FPLogin result = %d, want 0", int32(got))
	}
	return sessID
}

func uint32From(c int32) int32 { return int32(uint32(c)) }

func TestDispatch_GetStatusReturnsServerInfo(t *testing.T) {
	svc, r := newRunningService(t)
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncGetStatus, 0, 0), nil)), fakePort{})

	if len(r.replies) != 1 {
		t.Fatalf("got %d replies, want 1", len(r.replies))
	}
	block := respPayload(r.lastReply())
	// The server name "ClassicStack" must be packed as a Pascal string right
	// after the 10-byte header.
	name, _, ok := pString(block, 10)
	if !ok || string(name) != "ClassicStack" {
		t.Fatalf("server name = %q ok=%v, want ClassicStack", name, ok)
	}
}

func TestDispatch_OpenSessionRejectsBadVersion(t *testing.T) {
	svc, r := newRunningService(t)
	badUD := aspUserData(asp.SPFuncOpenSess, 200, 0x0200) // wrong ASP version
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(badUD, nil)), fakePort{})

	reply := respUserData(r.lastReply())
	if errCode := int16(reply & 0xFFFF); errCode != asp.SPErrorBadVersNum {
		t.Fatalf("bad-version OpenSession error = %d, want %d", errCode, asp.SPErrorBadVersNum)
	}
}

func TestDispatch_CommandBeforeLoginDenied(t *testing.T) {
	svc, r := newRunningService(t)
	from := fakePort{}
	// OpenSession but DON'T login.
	openUD := aspUserData(asp.SPFuncOpenSess, 200, asp.Version)
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(openUD, nil)), from)
	sessID := uint8(respUserData(r.lastReply()) >> 16)

	r.reset()
	parmsUD := aspUserData(asp.SPFuncCommand, sessID, 1)
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(parmsUD, []byte{cmdGetSrvrParms})), from)
	if got := int32(respUserData(r.lastReply())); got != afpErrAccessDenied {
		t.Fatalf("GetSrvrParms before login = %d, want %d", got, afpErrAccessDenied)
	}
}

func TestDispatch_LoginGetSrvrParmsOpenVolEnumerate(t *testing.T) {
	svc, r := newRunningService(t)
	from := fakePort{}

	// Seed the volume with two files and a subdir so Enumerate has content.
	vol := svc.Volumes()[0]
	mustCreate(t, vol, "alpha.txt")
	mustCreate(t, vol, "beta.txt")
	if err := vol.FS().CreateDir("subdir"); err != nil {
		t.Fatalf("CreateDir: %v", err)
	}

	sessID := login(t, svc, r)

	// FPGetSrvrParms: the volume "Share" must appear in the list.
	r.reset()
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncCommand, sessID, 2), []byte{cmdGetSrvrParms})), from)
	parms := respPayload(r.lastReply())
	if len(parms) < 5 || parms[4] != 1 {
		t.Fatalf("GetSrvrParms vol count = %v, want 1", parms[4:5])
	}
	volName, _, _ := pString(parms, 6) // skip serverTime(4)+count(1)+flags(1)
	if string(volName) != "Share" {
		t.Fatalf("GetSrvrParms vol name = %q, want Share", volName)
	}

	// FPOpenVol "Share" with the ID bit requested.
	r.reset()
	openVol := []byte{cmdOpenVol, 0}
	openVol = putBE16(openVol, volBitmapID|volBitmapName)
	openVol = putPString(openVol, []byte("Share"))
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncCommand, sessID, 3), openVol)), from)
	if got := int32(respUserData(r.lastReply())); got != afpNoErr {
		t.Fatalf("OpenVol result = %d, want 0", got)
	}
	ovReply := respPayload(r.lastReply())
	gotBitmap := be16(ovReply[0:2])
	if gotBitmap&volBitmapID == 0 {
		t.Fatalf("OpenVol reply bitmap %#x missing ID bit", gotBitmap)
	}
	volID := be16(ovReply[2:4]) // ID is the first param after the bitmap
	if volID != vol.ID() {
		t.Fatalf("OpenVol volID = %d, want %d", volID, vol.ID())
	}

	// FPEnumerate the volume root, requesting LongName + offspring/data lengths.
	r.reset()
	enum := []byte{cmdEnumerate, 0}
	enum = putBE16(enum, volID) // volID
	enum = putBE32(enum, 2)     // dirID = root
	enum = putBE16(enum, fileDirBitmapLongName|fileBitmapDataForkLen)
	enum = putBE16(enum, fileDirBitmapLongName|dirBitmapOffspringCount)
	enum = putBE16(enum, 10)               // reqCount
	enum = putBE16(enum, 1)                // startIndex (1-based)
	enum = putBE16(enum, 4624)             // maxReplySize
	enum = append(enum, PathTypeUTF8Names) // pathType
	// empty pathname → the volume root
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncCommand, sessID, 4), enum)), from)
	if got := int32(respUserData(r.lastReply())); got != afpNoErr {
		t.Fatalf("Enumerate result = %d, want 0", got)
	}
	enReply := respPayload(r.lastReply())
	actual := be16(enReply[4:6])
	if actual != 3 {
		t.Fatalf("Enumerate actualCount = %d, want 3 (alpha.txt, beta.txt, subdir)", actual)
	}
	// Walk the first entry and confirm its LongName decodes back to a real name.
	names := decodeEnumNames(t, enReply[6:], int(actual))
	if !contains(names, "alpha.txt") || !contains(names, "subdir") {
		t.Fatalf("Enumerate names = %v, want alpha.txt + subdir present", names)
	}
}

func TestDispatch_GetFileDirParms(t *testing.T) {
	svc, r := newRunningService(t)
	from := fakePort{}
	vol := svc.Volumes()[0]
	mustCreate(t, vol, "report.doc")

	sessID := login(t, svc, r)

	// Open the volume to get a handle into the session.
	r.reset()
	openVol := []byte{cmdOpenVol, 0}
	openVol = putBE16(openVol, volBitmapID)
	openVol = putPString(openVol, []byte("Share"))
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncCommand, sessID, 3), openVol)), from)
	volID := be16(respPayload(r.lastReply())[2:4])

	// FPGetFileDirParms for "report.doc", asking for LongName + data-fork length.
	r.reset()
	req := []byte{cmdGetFileDirParms, 0}
	req = putBE16(req, volID)
	req = putBE32(req, 2) // dirID root
	req = putBE16(req, fileDirBitmapLongName|fileBitmapDataForkLen)
	req = putBE16(req, fileDirBitmapLongName)
	req = append(req, PathTypeUTF8Names)
	req = append(req, []byte("report.doc")...)
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncCommand, sessID, 4), req)), from)

	if got := int32(respUserData(r.lastReply())); got != afpNoErr {
		t.Fatalf("GetFileDirParms result = %d, want 0", got)
	}
	reply := respPayload(r.lastReply())
	// reply = bitmap(2) isDir(1) pad(1) longName(pstring) dataForkLen(4)
	if reply[2]&isDirFlag != 0 {
		t.Fatalf("report.doc reported as directory")
	}
	name, next, ok := pString(reply, 4)
	if !ok || string(name) != "report.doc" {
		t.Fatalf("GetFileDirParms name = %q, want report.doc", name)
	}
	dataLen := be32(reply[next : next+4])
	if dataLen != 4 { // mustCreate wrote "data"
		t.Fatalf("data-fork length = %d, want 4", dataLen)
	}
}

// decodeEnumNames walks `count` Enumerate entries and pulls each LongName (the
// first packed param in this spine's bitmap order).
func decodeEnumNames(t *testing.T, b []byte, count int) []string {
	t.Helper()
	var names []string
	off := 0
	for range count {
		if off >= len(b) {
			break
		}
		entryLen := int(b[off])
		entry := b[off+1 : off+entryLen]
		// entry = isDir(1) pad(1) then params; first param is the LongName pstring.
		name, _, ok := pString(entry, 2)
		if ok {
			names = append(names, string(name))
		}
		off += entryLen
	}
	return names
}

func contains(ss []string, want string) bool { return slices.Contains(ss, want) }

func mustCreate(t *testing.T, vol *Volume, path string) {
	t.Helper()
	f, err := vol.FS().CreateFile(path)
	if err != nil {
		t.Fatalf("CreateFile %q: %v", path, err)
	}
	_, _ = f.WriteAt([]byte("data"), 0)
	_ = f.Sync()
	_ = f.Close()
}
