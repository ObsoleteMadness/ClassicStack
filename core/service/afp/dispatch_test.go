package afp

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"

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
	routed  []ddp.Datagram // server-initiated sends via Route (aspDataWrite/TRel/tickle/attention)
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
func (f *fakeRouter) Route(d ddp.Datagram, _ bool) error {
	f.mu.Lock()
	f.routed = append(f.routed, d)
	f.mu.Unlock()
	return nil
}
func (f *fakeRouter) RoutingTable() *router.RoutingTable  { return nil }
func (f *fakeRouter) Zones() *router.ZoneInformationTable { return nil }
func (f *fakeRouter) Ports() []router.RoutedPort          { return nil }
func (f *fakeRouter) lastReply() ddp.Datagram             { return f.replies[len(f.replies)-1] }
func (f *fakeRouter) reset() {
	f.mu.Lock()
	f.replies = nil
	f.routed = nil
	f.mu.Unlock()
}

// routedByFunc returns the ATP function code of the routed datagrams, filtered to
// server-initiated ASP frames. Helper for tests asserting aspDataWrite/TRel emission.
func (f *fakeRouter) routedControls() []uint8 {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]uint8, 0, len(f.routed))
	for _, d := range f.routed {
		if h, err := atp.Decode(d.Data); err == nil {
			out = append(out, h.Control)
		}
	}
	return out
}

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

// atpTReqTID is atpTReq with an explicit ATP transaction id, for tests that model
// a workstation retransmitting the same ASP request under a fresh tid.
func atpTReqTID(userData uint32, tid uint16, payload []byte) []byte {
	h := atp.Header{Control: atp.TREQ | atp.XO, Bitmap: 0x01, TransID: tid, UserData: userData}
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

// TestDispatch_OpenVolSignatureIsFixedDirID pins the FPOpenVol volume signature to
// Fixed Directory ID (2). A volume that reports Flat (1) is not mountable by the
// Finder — the regression that blocked mounts.
func TestDispatch_OpenVolSignatureIsFixedDirID(t *testing.T) {
	svc, r := newRunningService(t)
	from := fakePort{}
	sessID := login(t, svc, r)

	r.reset()
	openVol := []byte{cmdOpenVol, 0}
	openVol = bp.AppendBE16(openVol, volBitmapSignature)
	openVol = putPString(openVol, []byte("Share"))
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncCommand, sessID, 2), openVol)), from)
	if got := int32(respUserData(r.lastReply())); got != afpNoErr {
		t.Fatalf("OpenVol result = %d, want 0", got)
	}
	reply := respPayload(r.lastReply())
	bitmap := bp.BE16(reply[0:2])
	if bitmap&volBitmapSignature == 0 {
		t.Fatalf("OpenVol reply bitmap %#x missing Signature bit", bitmap)
	}
	// Signature (bit 1) is the lowest requested bit, so it is the first param.
	if sig := bp.BE16(reply[2:4]); sig != volSignatureFixedDirID {
		t.Fatalf("volume signature = %d, want %d (Fixed Directory ID)", sig, volSignatureFixedDirID)
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
	openVol = bp.AppendBE16(openVol, volBitmapID|volBitmapName)
	openVol = putPString(openVol, []byte("Share"))
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncCommand, sessID, 3), openVol)), from)
	if got := int32(respUserData(r.lastReply())); got != afpNoErr {
		t.Fatalf("OpenVol result = %d, want 0", got)
	}
	ovReply := respPayload(r.lastReply())
	gotBitmap := bp.BE16(ovReply[0:2])
	if gotBitmap&volBitmapID == 0 {
		t.Fatalf("OpenVol reply bitmap %#x missing ID bit", gotBitmap)
	}
	volID := bp.BE16(ovReply[2:4]) // ID is the first param after the bitmap
	if volID != vol.ID() {
		t.Fatalf("OpenVol volID = %d, want %d", volID, vol.ID())
	}

	// FPEnumerate the volume root, requesting LongName + offspring/data lengths.
	r.reset()
	enum := []byte{cmdEnumerate, 0}
	enum = bp.AppendBE16(enum, volID) // volID
	enum = bp.AppendBE32(enum, 2)     // dirID = root
	enum = bp.AppendBE16(enum, fdBitmapLongName|fileBitmapDataForkLen)
	enum = bp.AppendBE16(enum, fdBitmapLongName|dirBitmapOffspring)
	enum = bp.AppendBE16(enum, 10)         // reqCount
	enum = bp.AppendBE16(enum, 1)          // startIndex (1-based)
	enum = bp.AppendBE16(enum, 4624)       // maxReplySize
	enum = append(enum, PathTypeUTF8Names) // pathType
	// empty pathname → the volume root
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncCommand, sessID, 4), enum)), from)
	if got := int32(respUserData(r.lastReply())); got != afpNoErr {
		t.Fatalf("Enumerate result = %d, want 0", got)
	}
	enReply := respPayload(r.lastReply())
	actual := bp.BE16(enReply[4:6])
	if actual != 3 {
		t.Fatalf("Enumerate actualCount = %d, want 3 (alpha.txt, beta.txt, subdir)", actual)
	}
	// Walk the first entry and confirm its LongName decodes back to a real name.
	names := decodeEnumNames(t, enReply[6:], int(actual))
	if !contains(names, "alpha.txt") || !contains(names, "subdir") {
		t.Fatalf("Enumerate names = %v, want alpha.txt + subdir present", names)
	}
}

// TestDispatch_GetFileDirParmsByNameStripsPascalLen reproduces the mount-blocking
// regression seen on the wire (FPGetFileDirParms Did=2 Name=<child> → object not
// found -5018): the request pathname is a Pascal string (length byte + name), and
// the resolver must strip that length byte before decoding. A child that exists is
// resolved by name from the root DID (2).
func TestDispatch_GetFileDirParmsByNameStripsPascalLen(t *testing.T) {
	svc, r := newRunningService(t)
	from := fakePort{}
	vol := svc.Volumes()[0]
	if err := vol.FS().CreateDir("Configuration"); err != nil {
		t.Fatalf("CreateDir: %v", err)
	}
	sessID := login(t, svc, r)

	r.reset()
	openVol := []byte{cmdOpenVol, 0}
	openVol = bp.AppendBE16(openVol, volBitmapID)
	openVol = putPString(openVol, []byte("Share"))
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncCommand, sessID, 3), openVol)), from)
	volID := bp.BE16(respPayload(r.lastReply())[2:4])

	// FPGetFileDirParms Did=2 (root) Name="Configuration" — the exact failing wire
	// shape. It must resolve, not return object-not-found.
	r.reset()
	req := []byte{cmdGetFileDirParms, 0}
	req = bp.AppendBE16(req, volID)
	req = bp.AppendBE32(req, 2) // DID = root
	req = bp.AppendBE16(req, fdBitmapLongName)
	req = bp.AppendBE16(req, fdBitmapLongName|dirBitmapDirID)
	req = append(req, PathTypeUTF8Names)
	req = putPString(req, []byte("Configuration"))
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncCommand, sessID, 4), req)), from)
	if got := int32(respUserData(r.lastReply())); got != afpNoErr {
		t.Fatalf("GetFileDirParms Name=Configuration = %d, want 0 (Pascal length byte not stripped?)", got)
	}
	reply := respPayload(r.lastReply())
	if reply[4]&isDirFlag == 0 {
		t.Fatalf("Configuration not reported as a directory")
	}
}

// TestDispatch_SubdirDIDRoundTrips proves a directory id handed out in a catalog
// reply resolves back to its path on a later request: enumerate the root, read a
// subdir's DirID from the reply, then GetFileDirParms with that DID (empty path)
// and confirm it resolves to the subdir. Without honouring the request DirID this
// silently returned the root — the "no directory enumeration" symptom.
func TestDispatch_SubdirDIDRoundTrips(t *testing.T) {
	svc, r := newRunningService(t)
	from := fakePort{}
	vol := svc.Volumes()[0]
	if err := vol.FS().CreateDir("subdir"); err != nil {
		t.Fatalf("CreateDir: %v", err)
	}
	mustCreate(t, vol, "subdir/inner.txt")
	sessID := login(t, svc, r)

	r.reset()
	openVol := []byte{cmdOpenVol, 0}
	openVol = bp.AppendBE16(openVol, volBitmapID)
	openVol = putPString(openVol, []byte("Share"))
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncCommand, sessID, 3), openVol)), from)
	volID := bp.BE16(respPayload(r.lastReply())[2:4])

	// GetFileDirParms Did=2 Name="subdir" requesting the DirID bit → learn its DID.
	r.reset()
	req := []byte{cmdGetFileDirParms, 0}
	req = bp.AppendBE16(req, volID)
	req = bp.AppendBE32(req, 2)
	req = bp.AppendBE16(req, 0)                               // fileBitmap (n/a, it's a dir)
	req = bp.AppendBE16(req, dirBitmapDirID|fdBitmapLongName) // dirBitmap
	req = append(req, PathTypeUTF8Names)
	req = putPString(req, []byte("subdir"))
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncCommand, sessID, 4), req)), from)
	if got := int32(respUserData(r.lastReply())); got != afpNoErr {
		t.Fatalf("GetFileDirParms subdir = %d, want 0", got)
	}
	// dir reply: fileBitmap(2) dirBitmap(2) type(1) pad(1) params. params bit order:
	// LongName offset(2) then DirID(4).
	params := respPayload(r.lastReply())[6:]
	subdirDID := bp.BE32(params[2:6])
	if subdirDID <= 2 {
		t.Fatalf("subdir DID = %d, want a freshly-minted id > 2", subdirDID)
	}

	// Enumerate that DID (empty path) → must list inner.txt, proving the DID mapped
	// back to the subdir rather than the root.
	r.reset()
	enum := []byte{cmdEnumerate, 0}
	enum = bp.AppendBE16(enum, volID)
	enum = bp.AppendBE32(enum, subdirDID)
	enum = bp.AppendBE16(enum, fdBitmapLongName|fileBitmapDataForkLen)
	enum = bp.AppendBE16(enum, fdBitmapLongName)
	enum = bp.AppendBE16(enum, 10)
	enum = bp.AppendBE16(enum, 1)
	enum = bp.AppendBE16(enum, 4624)
	enum = append(enum, PathTypeUTF8Names)
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncCommand, sessID, 5), enum)), from)
	if got := int32(respUserData(r.lastReply())); got != afpNoErr {
		t.Fatalf("Enumerate subdir DID = %d, want 0", got)
	}
	enReply := respPayload(r.lastReply())
	names := decodeEnumNames(t, enReply[6:], int(bp.BE16(enReply[4:6])))
	if !contains(names, "inner.txt") {
		t.Fatalf("Enumerate subdir names = %v, want inner.txt", names)
	}
}

// TestDispatch_EnumeratePagingSkipsHiddenEntriesWithoutDuplicates pins the paging
// window to the CLIENT-VISIBLE entries. The client asks for the next page at
// startIndex + actCount, counting only what it was handed, so if the server
// indexes into the raw directory listing every hidden ._sidecar it skipped over
// shifts the next page backwards and the client lists the same file twice.
func TestDispatch_EnumeratePagingSkipsHiddenEntriesWithoutDuplicates(t *testing.T) {
	svc, r := newRunningService(t)
	from := fakePort{}

	vol := svc.Volumes()[0]
	// "._aaa.txt" sorts ahead of every visible name, so it lands inside page 1.
	mustCreate(t, vol, "._aaa.txt")
	want := []string{"aaa.txt", "bbb.txt", "ccc.txt", "ddd.txt"}
	for _, name := range want {
		mustCreate(t, vol, name)
	}
	// Guard the premise: the sidecar must really be in the raw listing, else this
	// test would pass without exercising the filter.
	raw, err := vol.Enumerate("")
	if err != nil {
		t.Fatalf("Enumerate: %v", err)
	}
	var rawNames []string
	for _, de := range raw {
		rawNames = append(rawNames, de.Name())
	}
	if !contains(rawNames, "._aaa.txt") {
		t.Skipf("backend hides ._ sidecars from ReadDir (%v); nothing to page past", rawNames)
	}

	sessID := login(t, svc, r)

	r.reset()
	openVol := []byte{cmdOpenVol, 0}
	openVol = bp.AppendBE16(openVol, volBitmapID)
	openVol = putPString(openVol, []byte("Share"))
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncCommand, sessID, 3), openVol)), from)
	volID := bp.BE16(respPayload(r.lastReply())[2:4])

	// Walk the directory two entries at a time, exactly as a client does.
	const pageSize = 2
	var got []string
	startIndex := uint16(1)
	for seq := uint16(4); ; seq++ {
		r.reset()
		enum := []byte{cmdEnumerate, 0}
		enum = bp.AppendBE16(enum, volID)
		enum = bp.AppendBE32(enum, 2) // dirID = root
		enum = bp.AppendBE16(enum, fdBitmapLongName)
		enum = bp.AppendBE16(enum, fdBitmapLongName)
		enum = bp.AppendBE16(enum, pageSize)
		enum = bp.AppendBE16(enum, startIndex)
		enum = bp.AppendBE16(enum, 4624)
		enum = append(enum, PathTypeUTF8Names)
		svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncCommand, sessID, seq), enum)), from)
		res := int32(respUserData(r.lastReply()))
		if res == afpErrObjectNotFnd {
			break // end of directory
		}
		if res != afpNoErr {
			t.Fatalf("Enumerate startIndex=%d = %d, want 0", startIndex, res)
		}
		reply := respPayload(r.lastReply())
		actCount := bp.BE16(reply[4:6])
		if actCount == 0 {
			t.Fatalf("Enumerate startIndex=%d returned actCount 0 with NoErr", startIndex)
		}
		got = append(got, decodeEnumNames(t, reply[6:], int(actCount))...)
		startIndex += actCount
		if len(got) > 4*len(want) {
			t.Fatalf("Enumerate never terminated; names so far = %v", got)
		}
	}

	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("paged Enumerate names = %v, want %v (duplicates mean startIndex was applied to the unfiltered listing)", got, want)
	}
}

// TestDispatch_GetFileDirParmsParentOfRootByVolumeName reproduces the Finder's
// mount probe: FPGetFileDirParms DID=1 (parent-of-root) Name="<volume name>". It
// must resolve to the volume root, not kFPDirNotFound (-5029) — the regression
// that made the volume mount with no name.
func TestDispatch_GetFileDirParmsParentOfRootByVolumeName(t *testing.T) {
	svc, r := newRunningService(t)
	from := fakePort{}
	sessID := login(t, svc, r)

	r.reset()
	openVol := []byte{cmdOpenVol, 0}
	openVol = bp.AppendBE16(openVol, volBitmapID)
	openVol = putPString(openVol, []byte("Share"))
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncCommand, sessID, 3), openVol)), from)
	volID := bp.BE16(respPayload(r.lastReply())[2:4])

	// DID=1 Name="Share" (the volume's own name) → resolves to the root dir.
	r.reset()
	req := []byte{cmdGetFileDirParms, 0}
	req = bp.AppendBE16(req, volID)
	req = bp.AppendBE32(req, 1) // DID = parent-of-root
	req = bp.AppendBE16(req, 0)
	req = bp.AppendBE16(req, fdBitmapLongName|dirBitmapDirID)
	req = append(req, PathTypeUTF8Names)
	req = putPString(req, []byte("Share"))
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncCommand, sessID, 4), req)), from)
	if got := int32(respUserData(r.lastReply())); got != afpNoErr {
		t.Fatalf("GetFileDirParms DID=1 Name=Share = %d, want 0 (parent-of-root volume resolution)", got)
	}
	reply := respPayload(r.lastReply())
	if reply[4]&isDirFlag == 0 {
		t.Fatalf("volume root not reported as a directory")
	}
	// A wrong volume name under DID=1 must be object-not-found, not the root.
	r.reset()
	req2 := []byte{cmdGetFileDirParms, 0}
	req2 = bp.AppendBE16(req2, volID)
	req2 = bp.AppendBE32(req2, 1)
	req2 = bp.AppendBE16(req2, 0)
	req2 = bp.AppendBE16(req2, dirBitmapDirID)
	req2 = append(req2, PathTypeUTF8Names)
	req2 = putPString(req2, []byte("Not The Volume"))
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncCommand, sessID, 5), req2)), from)
	if got := int32(respUserData(r.lastReply())); got != afpErrObjectNotFnd {
		t.Fatalf("GetFileDirParms DID=1 Name=<wrong> = %d, want object-not-found", got)
	}
}

// TestDispatch_GetFileDirParmsRootHasVolumeName reproduces the Finder's
// window-title probe: FPGetFileDirParms DID=2 (the root) with an empty path,
// requesting LongName. The root entry's LongName must carry the CONFIGURED
// volume name, not an empty string — the regression that made the mounted
// volume's root display with no name. Matches main's catalogNameForPath, which
// substitutes the volume name for the root catalog entry.
func TestDispatch_GetFileDirParmsRootHasVolumeName(t *testing.T) {
	svc, r := newRunningService(t)
	from := fakePort{}
	sessID := login(t, svc, r)

	r.reset()
	openVol := []byte{cmdOpenVol, 0}
	openVol = bp.AppendBE16(openVol, volBitmapID)
	openVol = putPString(openVol, []byte("Share"))
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncCommand, sessID, 3), openVol)), from)
	volID := bp.BE16(respPayload(r.lastReply())[2:4])

	// GetFileDirParms Did=2 (root), empty path, LongName requested.
	r.reset()
	req := []byte{cmdGetFileDirParms, 0}
	req = bp.AppendBE16(req, volID)
	req = bp.AppendBE32(req, 2) // DID = root
	req = bp.AppendBE16(req, 0) // fileBitmap (n/a, root is a dir)
	req = bp.AppendBE16(req, fdBitmapLongName|dirBitmapDirID)
	req = append(req, PathTypeUTF8Names)
	req = putPString(req, nil) // empty path → the root itself
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncCommand, sessID, 4), req)), from)
	if got := int32(respUserData(r.lastReply())); got != afpNoErr {
		t.Fatalf("GetFileDirParms root = %d, want 0", got)
	}
	reply := respPayload(r.lastReply())
	// dir reply: fileBitmap(2) dirBitmap(2) type(1) pad(1) params. params bit order:
	// LongName offset(2) then DirID(4); LongName is the lowest requested field, so
	// its offset points into the variable area at params-start + fixedSize.
	params := reply[6:]
	nameOff := int(bp.BE16(params[0:2]))
	name, _, ok := pString(params, nameOff)
	if !ok || string(name) != "Share" {
		t.Fatalf("root LongName = %q (ok=%v), want %q (volume name); empty means the root shows nameless in Finder", name, ok, "Share")
	}
}

// TestDispatch_SetFileDirParmsAcksFinderInfo proves FPSetFileDirParms is answered
// (not -5024) and persists Finder info the client can read back.
func TestDispatch_SetFileDirParmsAcksFinderInfo(t *testing.T) {
	svc, r := newRunningService(t)
	from := fakePort{}
	vol := svc.Volumes()[0]
	mustCreate(t, vol, "doc.txt")
	sessID := login(t, svc, r)

	r.reset()
	openVol := []byte{cmdOpenVol, 0}
	openVol = bp.AppendBE16(openVol, volBitmapID)
	openVol = putPString(openVol, []byte("Share"))
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncCommand, sessID, 3), openVol)), from)
	volID := bp.BE16(respPayload(r.lastReply())[2:4])

	// FPSetFileDirParms Did=2 Name="doc.txt", FinderInfo bit set, followed by the
	// 32-byte Finder info (word-aligned after the Pascal pathname).
	r.reset()
	req := []byte{cmdSetFileDirParms, 0}
	req = bp.AppendBE16(req, volID)
	req = bp.AppendBE32(req, 2)
	req = bp.AppendBE16(req, fdBitmapFinderInfo)
	req = append(req, PathTypeUTF8Names)
	req = putPString(req, []byte("doc.txt")) // nameLen=7 (odd) → params word-aligned
	if len("doc.txt")%2 != 0 {
		req = append(req, 0) // word-align the parameter block, as a real client does
	}
	var fi [32]byte
	copy(fi[:], []byte("TEXTttxt")) // recognisable type/creator
	req = append(req, fi[:]...)
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncCommand, sessID, 4), req)), from)
	if got := int32(respUserData(r.lastReply())); got != afpNoErr {
		t.Fatalf("SetFileDirParms = %d, want 0 (must not be -5024)", got)
	}
	back, ok := vol.FinderInfo("doc.txt")
	if !ok || string(back[:8]) != "TEXTttxt" {
		t.Fatalf("FinderInfo not persisted: back=%q ok=%v", back[:8], ok)
	}
}

// TestDispatch_ServerCallsMapAndMsg proves the mount-time server calls the Finder
// issues are answered (not -5024): FPMapID → owner/group name, FPMapName → id 0,
// FPGetSrvrMsg → empty message echoing the request type.
func TestDispatch_ServerCallsMapAndMsg(t *testing.T) {
	svc, r := newRunningService(t)
	from := fakePort{}
	sessID := login(t, svc, r)

	// FPMapID function 1 (id→user) → "root".
	r.reset()
	mapID := []byte{cmdMapID, 1}
	mapID = bp.AppendBE32(mapID, 0)
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncCommand, sessID, 2), mapID)), from)
	if got := int32(respUserData(r.lastReply())); got != afpNoErr {
		t.Fatalf("MapID = %d, want 0", got)
	}
	if name, _, ok := pString(respPayload(r.lastReply()), 0); !ok || string(name) != "root" {
		t.Fatalf("MapID name = %q, want root", name)
	}

	// FPMapID function 2 (id→group) → "wheel".
	r.reset()
	mapIDg := []byte{cmdMapID, 2}
	mapIDg = bp.AppendBE32(mapIDg, 0)
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncCommand, sessID, 3), mapIDg)), from)
	if name, _, ok := pString(respPayload(r.lastReply()), 0); !ok || string(name) != "wheel" {
		t.Fatalf("MapID group name = %q, want wheel", name)
	}

	// FPMapName → id 0.
	r.reset()
	mapName := []byte{cmdMapName, 3}
	mapName = putPString(mapName, []byte("alice"))
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncCommand, sessID, 4), mapName)), from)
	if got := int32(respUserData(r.lastReply())); got != afpNoErr {
		t.Fatalf("MapName = %d, want 0", got)
	}
	if id := bp.BE32(respPayload(r.lastReply())[0:4]); id != 0 {
		t.Fatalf("MapName id = %d, want 0", id)
	}

	// FPGetSrvrMsg → echoes type, empty message.
	r.reset()
	getMsg := []byte{cmdGetSrvrMsg, 0}
	getMsg = bp.AppendBE16(getMsg, 1) // messageType
	getMsg = bp.AppendBE16(getMsg, 3) // bitmap
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncCommand, sessID, 5), getMsg)), from)
	if got := int32(respUserData(r.lastReply())); got != afpNoErr {
		t.Fatalf("GetSrvrMsg = %d, want 0", got)
	}
	msg := respPayload(r.lastReply())
	if bp.BE16(msg[0:2]) != 1 || msg[4] != 0 {
		t.Fatalf("GetSrvrMsg reply = % x, want type=1 empty message", msg)
	}
}

// atpTReqAllPackets builds a TReq requesting all 8 response packets (bitmap 0xFF),
// as a real client does for a multi-packet reply like a large FPEnumerate.
func atpTReqAllPackets(userData uint32, payload []byte) []byte {
	h := atp.Header{Control: atp.TREQ | atp.XO, Bitmap: 0xFF, TransID: 7, UserData: userData}
	return append(h.Encode(nil), payload...)
}

// TestDispatch_EnumerateHonoursMaxReply is the regression for "volume enumerates
// nothing": FPEnumerate must not pack more than the client's maxReplySize, and its
// ActCount must match the bytes actually delivered. A reply that overflows the
// budget is truncated by the transport, leaving a partial final entry that desyncs
// the client's parse — so it silently discards the whole listing. This drives a
// directory far larger than one reply, reassembles every ATP packet, and asserts
// the stream is self-consistent and fully pages.
func TestDispatch_EnumerateHonoursMaxReply(t *testing.T) {
	svc, r := newRunningService(t)
	from := fakePort{}
	vol := svc.Volumes()[0]
	const nDirs = 40
	for i := 0; i < nDirs; i++ {
		if err := vol.FS().CreateDir(fmt.Sprintf("Directory Number %02d With A Long Name", i)); err != nil {
			t.Fatalf("CreateDir: %v", err)
		}
	}
	sessID := login(t, svc, r)
	r.reset()
	ov := []byte{cmdOpenVol, 0}
	ov = bp.AppendBE16(ov, volBitmapID)
	ov = putPString(ov, []byte("Share"))
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncCommand, sessID, 3), ov)), from)
	volID := bp.BE16(respPayload(r.lastReply())[2:4])

	const maxReply = 4624 // 8 ATP packets — the classic client budget
	total, startIdx := 0, 1
	for page := 0; page < 30; page++ {
		r.reset()
		enum := []byte{cmdEnumerate, 0}
		enum = bp.AppendBE16(enum, volID)
		enum = bp.AppendBE32(enum, 2)
		enum = bp.AppendBE16(enum, 0x077f)
		enum = bp.AppendBE16(enum, 0x137f)
		enum = bp.AppendBE16(enum, 64) // reqCount
		enum = bp.AppendBE16(enum, uint16(startIdx))
		enum = bp.AppendBE16(enum, maxReply)
		enum = append(enum, PathTypeLongNames)
		svc.Inbound(ddpTo(svc.Socket(), atpTReqAllPackets(aspUserData(asp.SPFuncCommand, sessID, uint16(4+page)), enum)), from)

		rc := int32(respUserData(r.lastReply()))
		if rc == afpErrObjectNotFnd {
			break // end of directory
		}
		if rc != afpNoErr {
			t.Fatalf("page %d Enumerate rc=%d", page, rc)
		}
		// Reassemble every ATP response packet into the full AFP reply.
		var full []byte
		for _, d := range r.replies {
			full = append(full, d.Data[atp.HeaderSize:]...)
		}
		if len(full) > maxReply {
			t.Fatalf("page %d: reply %d bytes exceeds maxReplySize %d", page, len(full), maxReply)
		}
		ac := int(bp.BE16(full[4:6]))
		off := 6
		for i := 0; i < ac; i++ {
			if off >= len(full) {
				t.Fatalf("page %d entry %d: stream overrun (ActCount %d exceeds delivered bytes)", page, i, ac)
			}
			ln := int(full[off])
			if ln == 0 {
				t.Fatalf("page %d entry %d: zero-length entry (desync)", page, i)
			}
			off += ln
		}
		if off != len(full) {
			t.Fatalf("page %d: consumed %d != reply len %d (trailing garbage / miscount)", page, off, len(full))
		}
		total += ac
		startIdx += ac
	}
	if total != nDirs {
		t.Fatalf("paged enumeration returned %d entries, want %d", total, nDirs)
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
	openVol = bp.AppendBE16(openVol, volBitmapID)
	openVol = putPString(openVol, []byte("Share"))
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncCommand, sessID, 3), openVol)), from)
	volID := bp.BE16(respPayload(r.lastReply())[2:4])

	// FPGetFileDirParms for "report.doc", asking for LongName + data-fork length.
	r.reset()
	req := []byte{cmdGetFileDirParms, 0}
	req = bp.AppendBE16(req, volID)
	req = bp.AppendBE32(req, 2) // dirID root
	req = bp.AppendBE16(req, fdBitmapLongName|fileBitmapDataForkLen)
	req = bp.AppendBE16(req, fdBitmapLongName)
	req = append(req, PathTypeUTF8Names)
	req = putPString(req, []byte("report.doc"))
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncCommand, sessID, 4), req)), from)

	if got := int32(respUserData(r.lastReply())); got != afpNoErr {
		t.Fatalf("GetFileDirParms result = %d, want 0", got)
	}
	reply := respPayload(r.lastReply())
	// reply = fileBitmap(2) dirBitmap(2) isDir(1) pad(1) <param block>. The param
	// block holds the fixed fields in bit order — LongName's 2-byte offset, then
	// DataForkLen(4) — followed by the variable area the LongName offset points into.
	if reply[4]&isDirFlag != 0 {
		t.Fatalf("report.doc reported as directory")
	}
	params := reply[6:]
	nameOff := int(bp.BE16(params[0:2]))
	dataLen := bp.BE32(params[2:6])
	if dataLen != 4 { // mustCreate wrote "data"
		t.Fatalf("data-fork length = %d, want 4", dataLen)
	}
	name, _, ok := pString(params, nameOff)
	if !ok || string(name) != "report.doc" {
		t.Fatalf("GetFileDirParms name = %q, want report.doc", name)
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
		// A framed entry is [len][type][params]: after the length byte, entry =
		// type(1) then the parameter block (NO pad byte between them — the name
		// offsets are anchored at the start of the params, i.e. byte 2 of the
		// framed entry). The block's first field (LongName is the lowest requested
		// bit here) is a 2-byte offset, measured from the start of the parameter
		// block, to the name pstring in the trailing variable area.
		params := entry[1:]
		nameOff := int(bp.BE16(params[0:2]))
		name, _, ok := pString(params, nameOff)
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
