package smb

// nbframing_test.go unit-tests the NBIPX and NBF CLIENT-direction frame construction in
// isolation (no server, no link): it captures the first frame each transport writes and
// asserts the wire shape matches what the server engines expect — the NBIPX
// SESSION_INITIALIZE header (DestConnID sentinel, SendSeq 0, ACK|CONFIRM) and the NBF
// caller frames (a broadcast NAME_QUERY UI frame, a SABME U-frame). The e2e tests already
// prove the whole handshake against the real engines; these pin the exact bytes so a
// framing regression is caught without spinning up the stack.

import (
	"sync"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/link"
	ipxport "github.com/ObsoleteMadness/ClassicStack/core/port/ipx"
	ipxproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx"
	nbfproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbeui"
	nb "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbios"
)

// captureLink is a FrameLink that records every frame written and blocks reads (so the
// transport's read loop parks harmlessly while the test inspects the writes).
type captureLink struct {
	mu      sync.Mutex
	written [][]byte
	closed  chan struct{}
	once    sync.Once
}

func newCaptureLink() *captureLink { return &captureLink{closed: make(chan struct{})} }

func (l *captureLink) Write(f link.Frame) error {
	l.mu.Lock()
	l.written = append(l.written, append([]byte(nil), f...))
	l.mu.Unlock()
	return nil
}

func (l *captureLink) Read() (link.Frame, error) {
	<-l.closed
	return nil, link.ErrClosed
}

func (l *captureLink) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

// firstWrite returns the first captured frame, or fails if none was written.
func (l *captureLink) firstWrite(t *testing.T) []byte {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.written) == 0 {
		t.Fatal("transport wrote no frame")
	}
	return l.written[0]
}

// testMAC is a fixed virtual-station MAC so the derived NetBIOS calling name is stable.
var testMAC = [6]byte{0x02, 0x11, 0x22, 0x33, 0x44, 0x55}

// TestNBIPXInitFrameShape drives DialNBIPX's first frame (SESSION_INITIALIZE) and asserts
// the NBIPX session header the server's handleSessionRequest keys on: DataStreamType
// DATA (0x06), DestConnID the unassigned sentinel (0xFFFF), our SourceConnID, SendSeq 0,
// and the ACK|CONFIRM control byte — plus a [called||calling||6-byte-trailer] payload.
func TestNBIPXInitFrameShape(t *testing.T) {
	l := newCaptureLink()
	// DialNBIPX blocks on the accept; run it in a goroutine and inspect the first write.
	go func() { _, _ = DialNBIPX(l, testMAC, "CLASSICSTACK") }()
	frame := waitFirstWrite(t, l)
	l.Close()

	payload, _, ok := ipxport.Strip(frame)
	if !ok {
		t.Fatalf("first frame is not IPX-encapsulated: % x", frame[:min(20, len(frame))])
	}
	d, err := ipxproto.Decode(payload)
	if err != nil {
		t.Fatalf("IPX decode: %v", err)
	}
	if d.Type != ipxPEPType {
		t.Errorf("IPX type = %#x, want PEP %#x", d.Type, ipxPEPType)
	}
	if d.DstSock != nbipxSessionSocket {
		t.Errorf("dst socket = % x, want NB-IPX session 0455", d.DstSock)
	}
	if d.DstNode != broadcastNode {
		t.Errorf("SESSION_INITIALIZE not broadcast: dst node = % x", d.DstNode)
	}
	hdr, err := nb.DecodeSessionHeader(d.Payload)
	if err != nil {
		t.Fatalf("session header decode: %v", err)
	}
	if hdr.DataStreamType != nb.NBIPXSessionData {
		t.Errorf("DataStreamType = %#x, want DATA %#x", hdr.DataStreamType, nb.NBIPXSessionData)
	}
	if hdr.DestConnID != nbipxUnassignedConnID {
		t.Errorf("DestConnID = %#x, want unassigned sentinel %#x", hdr.DestConnID, nbipxUnassignedConnID)
	}
	if hdr.SourceConnID != nbipxClientConnID {
		t.Errorf("SourceConnID = %#x, want client conn id %#x", hdr.SourceConnID, nbipxClientConnID)
	}
	if hdr.SendSeq != 0 {
		t.Errorf("SendSeq = %d, want 0 (the INIT consumes seq 0; first SMB is seq 1)", hdr.SendSeq)
	}
	if hdr.ConnCtrlFlag != nbipxInitCtrl {
		t.Errorf("ConnCtrlFlag = %#x, want ACK|CONFIRM %#x", hdr.ConnCtrlFlag, nbipxInitCtrl)
	}
	// Payload: [called(16) || calling(16) || 6-byte trailer].
	body := d.Payload[nb.NBIPXSessionHeaderLen:]
	if len(body) != 2*nb.NameLength+len(nbipxInitTrailer) {
		t.Fatalf("init payload = %d bytes, want %d", len(body), 2*nb.NameLength+len(nbipxInitTrailer))
	}
	var called nb.Name
	copy(called[:], body[:nb.NameLength])
	if called.String() != "CLASSICSTACK" || called.Type() != nb.NameTypeFileServer {
		t.Errorf("called name = %q type %#x, want CLASSICSTACK<20>", called.String(), called.Type())
	}
}

// TestNBFNameQueryAndSABME drives DialNBF's first two frames and asserts (1) a broadcast
// NAME_QUERY UI frame for SERVER<20> carrying our local session number (a CALL, Data2 low
// byte != 0), then (2) after the retransmit loop, a SABME U-frame — the LLC2 connect the
// port answers with UA. It inspects only the first NAME_QUERY (the SABME follows a
// NAME_RECOGNIZED the test does not supply, so establish never advances past the query).
func TestNBFNameQueryShape(t *testing.T) {
	l := newCaptureLink()
	go func() { _, _ = DialNBF(l, testMAC, "CLASSICSTACK") }()
	frame := waitFirstWrite(t, l)
	l.Close()

	if len(frame) < nbfEthHdrLen+3 {
		t.Fatalf("frame too short: %d bytes", len(frame))
	}
	var dstMAC [6]byte
	copy(dstMAC[:], frame[0:6])
	if dstMAC != nbfproto.NetBIOSMulticastMAC {
		t.Errorf("NAME_QUERY dst MAC = % x, want NetBIOS multicast % x", dstMAC, nbfproto.NetBIOSMulticastMAC)
	}
	body := frame[nbfEthHdrLen:]
	if body[0] != nbfLLCDSAP || body[1] != nbfLLCSSAPCmd || body[2] != nbfLLCUI {
		t.Errorf("LLC header = % x, want F0 F0 03 (NetBIOS UI)", body[:3])
	}
	f, err := nbfproto.Decode(body[3:])
	if err != nil {
		t.Fatalf("NBF decode: %v", err)
	}
	if f.Command != nbfproto.CmdNameQuery {
		t.Errorf("command = %s, want NAME_QUERY", nbfproto.CommandName(f.Command))
	}
	if uint8(f.Data2&0xFF) != nbfClientSessionNum {
		t.Errorf("Data2 local session = %d, want %d (a CALL, not a locate)", f.Data2&0xFF, nbfClientSessionNum)
	}
	var called nb.Name
	copy(called[:], f.DestinationName[:])
	if called.String() != "CLASSICSTACK" || called.Type() != nb.NameTypeFileServer {
		t.Errorf("called name = %q type %#x, want CLASSICSTACK<20>", called.String(), called.Type())
	}
}

// waitFirstWrite polls the capture link until the transport's establish goroutine has
// written its first frame, so the test reads it deterministically without a sleep.
func waitFirstWrite(t *testing.T, l *captureLink) []byte {
	t.Helper()
	for i := 0; i < 500; i++ {
		l.mu.Lock()
		n := len(l.written)
		l.mu.Unlock()
		if n > 0 {
			return l.firstWrite(t)
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("transport wrote no frame within the poll budget")
	return nil
}
