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
	inbox   chan []byte
	closed  chan struct{}
	once    sync.Once
}

func newCaptureLink() *captureLink {
	return &captureLink{closed: make(chan struct{}), inbox: make(chan []byte, 4)}
}

func (l *captureLink) Write(f link.Frame) error {
	l.mu.Lock()
	l.written = append(l.written, append([]byte(nil), f...))
	l.mu.Unlock()
	return nil
}

func (l *captureLink) Read() (link.Frame, error) {
	select {
	case f := <-l.inbox:
		return f, nil
	case <-l.closed:
		return nil, link.ErrClosed
	}
}

func (l *captureLink) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (l *captureLink) inject(f []byte) {
	select {
	case l.inbox <- append([]byte(nil), f...):
	case <-l.closed:
	}
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

// TestNBIPXInitFrameShape drives DialNBIPX's locate-then-INIT: the first write is a
// type-20 Find-name for SERVER<20>; after a NAME_RECOGNIZED the next write is a
// SESSION_INITIALIZE (DestConnID sentinel, SendSeq 0, ACK|CONFIRM) sent UNICAST to
// the node that answered the locate — the golden Win98↔Win98 ordering.
func TestNBIPXInitFrameShape(t *testing.T) {
	l := newCaptureLink()
	go func() { _, _ = DialNBIPX(l, testMAC, "CLASSICSTACK") }()
	find := waitNthWrite(t, l, 1)

	payload, _, ok := ipxport.Strip(find)
	if !ok {
		t.Fatalf("Find-name frame is not IPX-encapsulated: % x", find[:min(20, len(find))])
	}
	d, err := ipxproto.Decode(payload)
	if err != nil {
		t.Fatalf("Find-name IPX decode: %v", err)
	}
	if d.Type != nb.IPXTypeNetBIOS {
		t.Errorf("Find-name IPX type = %#x, want type-20 %#x", d.Type, nb.IPXTypeNetBIOS)
	}
	if d.DstNode != ipxproto.BroadcastNode {
		t.Errorf("Find-name not broadcast: dst node = % x", d.DstNode)
	}
	pkt, err := nb.DecodeNameService(d.Payload)
	if err != nil {
		t.Fatalf("Find-name decode: %v", err)
	}
	if pkt.DataStreamType != nb.NBIPXFindName {
		t.Errorf("Find-name DataStreamType = %#x, want %#x", pkt.DataStreamType, nb.NBIPXFindName)
	}
	if pkt.Name.String() != "CLASSICSTACK" || pkt.Name.Type() != nb.NameTypeFileServer {
		t.Errorf("Find-name = %q type %#x, want CLASSICSTACK<20>", pkt.Name.String(), pkt.Name.Type())
	}

	serverMAC := [6]byte{0x00, 0x86, 0xb0, 0xae, 0x29, 0x6f}
	l.inject(nameRecognizedFrame(t, "CLASSICSTACK", serverMAC, testMAC))
	init := waitNthWrite(t, l, 2)
	l.Close()

	payload, _, ok = ipxport.Strip(init)
	if !ok {
		t.Fatalf("INIT frame is not IPX-encapsulated: % x", init[:min(20, len(init))])
	}
	d, err = ipxproto.Decode(payload)
	if err != nil {
		t.Fatalf("INIT IPX decode: %v", err)
	}
	if d.Type != ipxproto.TypePEP {
		t.Errorf("INIT IPX type = %#x, want PEP %#x", d.Type, ipxproto.TypePEP)
	}
	if d.DstSock != nbipxSessionSocket {
		t.Errorf("dst socket = % x, want NB-IPX session 0455", d.DstSock)
	}
	if d.DstNode != serverMAC {
		t.Errorf("SESSION_INITIALIZE dst node = % x, want the located holder % x", d.DstNode, serverMAC)
	}
	hdr, err := nb.DecodeSessionHeader(d.Payload)
	if err != nil {
		t.Fatalf("session header decode: %v", err)
	}
	if hdr.DataStreamType != nb.NBIPXSessionData {
		t.Errorf("DataStreamType = %#x, want DATA %#x", hdr.DataStreamType, nb.NBIPXSessionData)
	}
	if hdr.DestConnID != nb.NBIPXUnassignedConnID {
		t.Errorf("DestConnID = %#x, want unassigned sentinel %#x", hdr.DestConnID, nb.NBIPXUnassignedConnID)
	}
	if hdr.SourceConnID == 0 {
		t.Errorf("SourceConnID = 0, want a non-zero client circuit id")
	}
	if hdr.SendSeq != 0 {
		t.Errorf("SendSeq = %d, want 0 (the INIT consumes seq 0; first SMB is seq 1)", hdr.SendSeq)
	}
	if hdr.ConnCtrlFlag != nbipxInitCtrl {
		t.Errorf("ConnCtrlFlag = %#x, want ACK|CONFIRM %#x", hdr.ConnCtrlFlag, nbipxInitCtrl)
	}
	body := d.Payload[nb.NBIPXSessionHeaderLen:]
	if len(body) != 2*nb.NameLength+len(nbipxInitTrailer) {
		t.Fatalf("init payload = %d bytes, want %d", len(body), 2*nb.NameLength+len(nbipxInitTrailer))
	}
	// [SOURCE][DESTINATION]: our own calling name first, the server's called name
	// second — golden capture spec/captures/nbipx-win98.pcap frame 65. Emitting these
	// the other way round is why no real Win98 NWLink peer ever answered our INIT.
	var calling, called nb.Name
	copy(calling[:], body[:nb.NameLength])
	copy(called[:], body[nb.NameLength:2*nb.NameLength])
	if called.String() != "CLASSICSTACK" || called.Type() != nb.NameTypeFileServer {
		t.Errorf("called name = %q type %#x, want CLASSICSTACK<20> in the DESTINATION slot",
			called.String(), called.Type())
	}
	if calling.Type() != nb.NameTypeWorkstation {
		t.Errorf("calling name = %q type %#x, want a workstation name in the SOURCE slot",
			calling.String(), calling.Type())
	}
	if calling.String() == "CLASSICSTACK" {
		t.Error("SOURCE slot holds the called name — the name pair is inverted")
	}
}

func nameRecognizedFrame(t *testing.T, server string, serverMAC, clientMAC [6]byte) []byte {
	t.Helper()
	own := nb.NewName(server, nb.NameTypeWorkstation)
	queried := nb.NewName(server, nb.NameTypeFileServer)
	d := &ipxproto.Datagram{
		Type:    nb.IPXTypePEP,
		DstNode: clientMAC,
		DstSock: nbipxSessionSocket,
		SrcNode: serverMAC,
		SrcSock: nbipxSessionSocket,
		Payload: nb.EncodeNameRecognized(own, "WORKGROUP", queried),
	}
	ipxBytes, err := d.Encode(nil)
	if err != nil {
		t.Fatalf("NAME_RECOGNIZED encode: %v", err)
	}
	return ipxport.DefaultFrameType.Encapsulate(clientMAC, serverMAC, ipxBytes)
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
	if body[0] != nbfproto.LLCDSAP || body[1] != nbfproto.LLCSSAPCommand || body[2] != nbfproto.LLCCtrlUI {
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

func waitNthWrite(t *testing.T, l *captureLink, n int) []byte {
	t.Helper()
	for i := 0; i < 500; i++ {
		l.mu.Lock()
		got := len(l.written)
		var frame []byte
		if got >= n {
			frame = append([]byte(nil), l.written[n-1]...)
		}
		l.mu.Unlock()
		if got >= n {
			return frame
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("transport wrote %d frames, want at least %d", n-1, n)
	return nil
}

// waitFirstWrite polls the capture link until the transport's establish goroutine has
// written its first frame, so the test reads it deterministically without a sleep.
func waitFirstWrite(t *testing.T, l *captureLink) []byte {
	t.Helper()
	return waitNthWrite(t, l, 1)
}

func TestNBIPXClientConnIDsAreUnique(t *testing.T) {
	a := nextNBIPXClientConnID()
	b := nextNBIPXClientConnID()
	if a == 0 || b == 0 {
		t.Fatalf("allocated 0 (%d, %d)", a, b)
	}
	if a == b {
		t.Fatalf("consecutive Dials allocated the same SourceConnID %#x", a)
	}
}

// TestNBIPXInitBroadcastsWhenLocateFails proves the INIT falls back to broadcast
// when Find-name located nobody. The unicast form (TestNBIPXInitFrameShape) matches
// the golden Win98 open, but a server that never answers a locate can still only be
// reached by broadcasting the call — so the fallback must survive.
func TestNBIPXInitBroadcastsWhenLocateFails(t *testing.T) {
	l := newCaptureLink()
	go func() { _, _ = DialNBIPX(l, testMAC, "CLASSICSTACK") }()

	// No NAME_RECOGNIZED is injected, so findName times out and establish()
	// proceeds with haveServer still false. The Find-name retransmits come first;
	// scan the writes for the first PEP-typed frame, which is the INIT.
	deadline := time.Now().Add(nbipxFindNameWindow + 2*time.Second)
	var init *ipxproto.Datagram
	for n := 1; time.Now().Before(deadline) && init == nil; n++ {
		frame := waitNthWrite(t, l, n)
		payload, _, ok := ipxport.Strip(frame)
		if !ok {
			continue
		}
		d, err := ipxproto.Decode(payload)
		if err != nil || d.Type != ipxproto.TypePEP {
			continue
		}
		init = d
	}
	l.Close()
	if init == nil {
		t.Fatal("no SESSION_INITIALIZE observed after the locate timed out")
	}
	if init.DstNode != ipxproto.BroadcastNode {
		t.Errorf("unlocated SESSION_INITIALIZE dst node = % x, want broadcast", init.DstNode)
	}
}
