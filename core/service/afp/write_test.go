package afp

import (
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/asp"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/atp"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
)

// recordingPort is a RoutedPort that captures the datagrams the service sends via
// Unicast — the server-initiated aspDataWrite TReq of a two-phase write. Node()
// answers the originator the test addresses replies to.
type recordingPort struct {
	fakePort
	sent []ddp.Datagram
}

func (p *recordingPort) Unicast(_ uint16, _ uint8, d ddp.Datagram) {
	p.sent = append(p.sent, d)
}

// openForkRW logs in, opens "Share", and opens the data fork of path read/write,
// returning the session id and fork ref.
func openForkRW(t *testing.T, svc *Service, r *fakeRouter, from *recordingPort, path string) (sessID uint8, forkRef uint16) {
	t.Helper()
	sessID = login(t, svc, r)

	r.reset()
	openVol := []byte{cmdOpenVol, 0}
	openVol = putBE16(openVol, volBitmapID)
	openVol = putPString(openVol, []byte("Share"))
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncCommand, sessID, 3), openVol)), from)
	if got := int32(respUserData(r.lastReply())); got != afpNoErr {
		t.Fatalf("OpenVol result = %d, want 0", got)
	}
	volID := be16(respPayload(r.lastReply())[2:4])

	openFork := []byte{cmdOpenFork, forkFlagData}
	openFork = putBE16(openFork, volID)
	openFork = putBE32(openFork, 2) // dirID root
	openFork = putBE16(openFork, 0)
	openFork = putBE16(openFork, accessRead|accessWrite)
	openFork = append(openFork, PathTypeUTF8Names)
	openFork = append(openFork, []byte(path)...)
	r.reset()
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncCommand, sessID, 4), openFork)), from)
	if got := int32(respUserData(r.lastReply())); got != afpNoErr {
		t.Fatalf("OpenFork result = %d, want 0", got)
	}
	return sessID, be16(respPayload(r.lastReply())[2:4])
}

// fpWriteHeader builds the 12-byte FPWrite header carried in a phase-1 aspWrite:
// cmd(1) flag(1) forkRef(2) offset(4) reqCount(4), with no inline data.
func fpWriteHeader(forkRef uint16, offset, reqCount uint32) []byte {
	h := []byte{cmdWrite, 0x00}
	h = putBE16(h, forkRef)
	h = putBE32(h, offset)
	h = putBE32(h, reqCount)
	return h
}

// TestTwoPhaseWrite_DataPath drives a full two-phase ASPWrite: phase-1 aspWrite
// (header only) → the service's server-initiated aspDataWrite TReq → the
// workstation's TResp carrying the data → the phase-3 reply. It then reads the
// fork back to prove the data landed.
func TestTwoPhaseWrite_DataPath(t *testing.T) {
	svc, r := newRunningService(t)
	vol := svc.Volumes()[0]
	mustCreate(t, vol, "doc.txt") // seeds "data"

	from := &recordingPort{}
	sessID, forkRef := openForkRW(t, svc, r, from, "doc.txt")

	payload := []byte("two phase payload")
	from.sent = nil
	r.reset()

	// Phase 1: aspWrite with the FPWrite header only (no data).
	seq := uint16(9)
	header := fpWriteHeader(forkRef, 0, uint32(len(payload)))
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncWrite, sessID, seq), header)), from)

	// The service must have sent exactly one aspDataWrite TReq to the WSS.
	if len(from.sent) != 1 {
		t.Fatalf("aspDataWrite TReqs sent = %d, want 1", len(from.sent))
	}
	dw := from.sent[0]
	if dw.DestSocket != 200 { // the WSS the test client opened the session from
		t.Errorf("aspDataWrite DestSocket = %d, want 200 (WSS)", dw.DestSocket)
	}
	dh, err := atp.Decode(dw.Data)
	if err != nil {
		t.Fatalf("decode aspDataWrite: %v", err)
	}
	if dh.FuncCode() != atp.FuncTReq {
		t.Errorf("aspDataWrite func = %#x, want TReq", dh.FuncCode())
	}
	if fn := uint8(dh.UserData >> 24); fn != asp.SPFuncWriteContinue {
		t.Errorf("aspDataWrite SPFunc = %d, want %d", fn, asp.SPFuncWriteContinue)
	}
	if sid := uint8(dh.UserData >> 16); sid != sessID {
		t.Errorf("aspDataWrite session = %d, want %d", sid, sessID)
	}
	if s := uint16(dh.UserData); s != seq {
		t.Errorf("aspDataWrite seq = %d, want %d", s, seq)
	}
	if bsz := be16(dw.Data[atp.HeaderSize:]); int(bsz) != len(payload) {
		t.Errorf("aspDataWrite bufferSize = %d, want %d", bsz, len(payload))
	}
	// No phase-3 reply has been produced yet — the data has not arrived.
	if len(r.replies) != 0 {
		t.Fatalf("got %d replies before data arrived, want 0", len(r.replies))
	}

	// Phase 2b: the workstation answers the aspDataWrite TReq with the data as an
	// EOM TResp echoing the server's transaction id.
	svc.Inbound(dataResponse(dh.TransID, payload), from)

	// Phase 3: the reply to the *original* aspWrite reports lastWritten.
	if len(r.replies) != 1 {
		t.Fatalf("phase-3 replies = %d, want 1", len(r.replies))
	}
	if got := int32(respUserData(r.lastReply())); got != afpNoErr {
		t.Fatalf("two-phase write result = %d, want 0", got)
	}
	if last := be32(respPayload(r.lastReply())[0:4]); int(last) != len(payload) {
		t.Fatalf("lastWritten = %d, want %d", last, len(payload))
	}

	// The data is readable from the fork.
	read := []byte{cmdRead, 0x00}
	read = putBE16(read, forkRef)
	read = putBE32(read, 0)
	read = putBE32(read, uint32(len(payload)))
	code, got := sendCmd(t, svc, r, sessID, 20, read)
	if code != afpNoErr {
		t.Fatalf("Read result = %d, want 0", code)
	}
	if string(got) != string(payload) {
		t.Fatalf("read back = %q, want %q", got, payload)
	}
}

// TestTwoPhaseWrite_MultiPacketData proves the data path reassembles a write that
// spans several aspDataWrite TResp packets (the service accumulates by sequence
// and completes on EOM).
func TestTwoPhaseWrite_MultiPacketData(t *testing.T) {
	svc, r := newRunningService(t)
	vol := svc.Volumes()[0]
	mustCreate(t, vol, "big.bin")

	from := &recordingPort{}
	sessID, forkRef := openForkRW(t, svc, r, from, "big.bin")

	// A payload larger than one ATP packet (578) so the data spans two TResps.
	payload := make([]byte, atp.MaxATPData+100)
	for i := range payload {
		payload[i] = byte(i)
	}
	from.sent = nil
	r.reset()

	header := fpWriteHeader(forkRef, 0, uint32(len(payload)))
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncWrite, sessID, 1), header)), from)
	if len(from.sent) != 1 {
		t.Fatalf("aspDataWrite TReqs = %d, want 1", len(from.sent))
	}
	tid, _ := atp.Decode(from.sent[0].Data)

	// Two TResp packets: seq 0 (not EOM) and seq 1 (EOM).
	svc.Inbound(dataResponseSeq(tid.TransID, 0, false, payload[:atp.MaxATPData]), from)
	if len(r.replies) != 0 {
		t.Fatalf("reply produced before EOM, want 0 got %d", len(r.replies))
	}
	svc.Inbound(dataResponseSeq(tid.TransID, 1, true, payload[atp.MaxATPData:]), from)
	if len(r.replies) != 1 {
		t.Fatalf("phase-3 replies = %d, want 1", len(r.replies))
	}
	if last := be32(respPayload(r.lastReply())[0:4]); int(last) != len(payload) {
		t.Fatalf("lastWritten = %d, want %d", last, len(payload))
	}

	// Read it all back straight from the fork engine (a multi-packet FPRead reply
	// would be split across ATP packets the fake router records separately, which
	// is orthogonal to what this test proves).
	f, err := vol.FS().OpenFork("big.bin", fs.DataFork, 0)
	if err != nil {
		t.Fatalf("OpenFork: %v", err)
	}
	defer f.Close()
	got := make([]byte, len(payload))
	n, _ := f.ReadAt(got, 0)
	if n != len(payload) || string(got) != string(payload) {
		t.Fatalf("fork contents mismatch (read %d of %d bytes)", n, len(payload))
	}
}

// TestTwoPhaseWrite_ZeroLength proves a zero-reqCount FPWrite completes inline
// without a data round-trip (no aspDataWrite is sent).
func TestTwoPhaseWrite_ZeroLength(t *testing.T) {
	svc, r := newRunningService(t)
	vol := svc.Volumes()[0]
	mustCreate(t, vol, "empty.txt")

	from := &recordingPort{}
	sessID, forkRef := openForkRW(t, svc, r, from, "empty.txt")
	from.sent = nil
	r.reset()

	header := fpWriteHeader(forkRef, 0, 0)
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncWrite, sessID, 1), header)), from)
	if len(from.sent) != 0 {
		t.Fatalf("zero-length write sent %d aspDataWrite TReqs, want 0", len(from.sent))
	}
	if len(r.replies) != 1 {
		t.Fatalf("zero-length write replies = %d, want 1", len(r.replies))
	}
	if got := int32(respUserData(r.lastReply())); got != afpNoErr {
		t.Fatalf("zero-length write result = %d, want 0", got)
	}
}

// dataResponse builds a single EOM TResp datagram carrying write data, echoing
// the server's transaction id (as the workstation's .XPP driver does).
func dataResponse(transID uint16, data []byte) ddp.Datagram {
	return dataResponseSeq(transID, 0, true, data)
}

// dataResponseSeq builds one TResp packet at the given sequence number, EOM flag,
// and payload, addressed from the client WSS back to the AFP socket.
func dataResponseSeq(transID uint16, seq uint8, eom bool, data []byte) ddp.Datagram {
	control := uint8(atp.TRESP)
	if eom {
		control |= atp.EOM
	}
	h := atp.Header{Control: control, Bitmap: seq, TransID: transID}
	frame := append(h.Encode(nil), data...)
	return ddp.Datagram{
		DestNetwork: 1, SrcNetwork: 1,
		DestNode: 2, SrcNode: 10,
		DestSocket: 251, SrcSocket: 200,
		DDPType: atp.DDPType,
		Data:    frame,
	}
}
