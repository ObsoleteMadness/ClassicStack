package over_netbeui

import (
	"encoding/binary"
	"sync"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/capture"
	netbeuiport "github.com/ObsoleteMadness/ClassicStack/port/netbeui"
	nbfproto "github.com/ObsoleteMadness/ClassicStack/protocol/netbeui"
	protocol "github.com/ObsoleteMadness/ClassicStack/protocol/netbios"
)

// --- Mock port ---

type sentFrame struct {
	dstMAC [6]byte
	frame  *nbfproto.Frame
}

type mockPort struct {
	mu        sync.Mutex
	sent      []sentFrame
	cb        netbeuiport.DeliveryCallback
	sourceMAC [6]byte
	started   bool
}


func (m *mockPort) Start() error {
	m.mu.Lock()
	m.started = true
	m.mu.Unlock()
	return nil
}

func (m *mockPort) Stop() error {
	m.mu.Lock()
	m.started = false
	m.mu.Unlock()
	return nil
}

func (m *mockPort) Send(dstMAC [6]byte, frame *nbfproto.Frame) error {
	m.mu.Lock()
	m.sent = append(m.sent, sentFrame{dstMAC: dstMAC, frame: frame})
	m.mu.Unlock()
	return nil
}

func (m *mockPort) SendBroadcast(frame *nbfproto.Frame) error {
	return m.Send(nbfproto.NetBIOSMulticastMAC, frame)
}

func (m *mockPort) SetSourceMAC(mac [6]byte) {
	m.mu.Lock()
	m.sourceMAC = mac
	m.mu.Unlock()
}

func (m *mockPort) SetDeliveryCallback(cb netbeuiport.DeliveryCallback) {
	m.mu.Lock()
	m.cb = cb
	m.mu.Unlock()
}

func (m *mockPort) SetCaptureSink(_ capture.Sink) {}

func (m *mockPort) deliverFrame(srcMAC, dstMAC [6]byte, frame *nbfproto.Frame) {
	m.mu.Lock()
	cb := m.cb
	m.mu.Unlock()
	if cb != nil {
		cb(srcMAC, dstMAC, frame)
	}
}

func (m *mockPort) sentFrames() []sentFrame {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]sentFrame, len(m.sent))
	copy(out, m.sent)
	return out
}

func (m *mockPort) clearSent() {
	m.mu.Lock()
	m.sent = nil
	m.mu.Unlock()
}

// --- Mock command handler ---

type mockHandler struct {
	mu        sync.Mutex
	sessions  []*protocol.SessionPacket
	datagrams []*protocol.Datagram
}

func (h *mockHandler) HandleSession(pkt *protocol.SessionPacket) error {
	h.mu.Lock()
	h.sessions = append(h.sessions, pkt)
	h.mu.Unlock()
	return nil
}

func (h *mockHandler) HandleDatagram(d *protocol.Datagram) error {
	h.mu.Lock()
	h.datagrams = append(h.datagrams, d)
	h.mu.Unlock()
	return nil
}

func (h *mockHandler) receivedSessions() []*protocol.SessionPacket {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]*protocol.SessionPacket, len(h.sessions))
	copy(out, h.sessions)
	return out
}

func (h *mockHandler) receivedDatagrams() []*protocol.Datagram {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]*protocol.Datagram, len(h.datagrams))
	copy(out, h.datagrams)
	return out
}

// --- Test helpers ---

var (
	localMAC  = [6]byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55}
	remoteMAC = [6]byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF}
)

func testName(s string) protocol.Name {
	return protocol.NewName(s, 0x00)
}

func newTestTransport() (*transport, *mockPort) {
	mock := &mockPort{}
	tp := NewTransport(mock, localMAC).(*transport)
	return tp, mock
}

// --- Tests ---

func TestNameClaim_NoConflict(t *testing.T) {
	tp, _ := newTestTransport()
	if err := tp.Start(nil); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tp.Stop()

	name := testName("MYSERVER")

	// SendName blocks for defaultTransmitCount * defaultTransmitTimeout.
	// Override for test speed by inserting directly.
	entry := tp.names.Add(name, false)
	if entry == nil {
		t.Fatal("name table Add returned nil")
	}

	// Simulate: no ADD_NAME_RESPONSE arrives → promote to registered.
	tp.names.SetState(name, nameStateRegistered)

	got := tp.names.Lookup(name)
	if got == nil || got.State != nameStateRegistered {
		t.Fatalf("expected registered state, got %v", got)
	}
}

func TestNameClaim_Conflict(t *testing.T) {
	tp, mock := newTestTransport()
	if err := tp.Start(nil); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tp.Stop()

	name := testName("MYSERVER")
	entry := tp.names.Add(name, false)
	if entry == nil {
		t.Fatal("name table Add returned nil")
	}

	// Inject ADD_NAME_RESPONSE from remote → conflict.
	resp := &nbfproto.Frame{
		Command:        nbfproto.CmdAddNameResponse,
		Data1:          0x00,
		XmitCorrelator: 0x0001,
	}
	copy(resp.DestinationName[:], name[:])
	copy(resp.SourceName[:], name[:])
	mock.deliverFrame(remoteMAC, localMAC, resp)

	time.Sleep(10 * time.Millisecond) // let handler run

	got := tp.names.Lookup(name)
	if got == nil || got.State != nameStateConflict {
		t.Fatalf("expected conflict state, got %v", got)
	}
}

func TestAddNameQuery_Responds_WithConflict(t *testing.T) {
	tp, mock := newTestTransport()
	if err := tp.Start(nil); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tp.Stop()

	// Register a local name.
	name := testName("MYSERVER")
	tp.names.Add(name, false)
	tp.names.SetState(name, nameStateRegistered)

	// Inject an ADD_NAME_QUERY from a remote node trying to claim our name.
	query := &nbfproto.Frame{
		Command:       nbfproto.CmdAddNameQuery,
		RspCorrelator: 0x4242,
	}
	copy(query.SourceName[:], name[:])
	mock.deliverFrame(remoteMAC, nbfproto.NetBIOSMulticastMAC, query)

	time.Sleep(10 * time.Millisecond)

	sent := mock.sentFrames()
	if len(sent) == 0 {
		t.Fatal("expected ADD_NAME_RESPONSE to be sent, got none")
	}
	resp := sent[0]
	if resp.frame.Command != nbfproto.CmdAddNameResponse {
		t.Fatalf("expected CmdAddNameResponse (0x%02X), got 0x%02X",
			nbfproto.CmdAddNameResponse, resp.frame.Command)
	}
	if resp.dstMAC != remoteMAC {
		t.Fatalf("expected response directed to remote MAC, got %v", resp.dstMAC)
	}
	if resp.frame.XmitCorrelator != 0x4242 {
		t.Fatalf("XmitCorrelator = 0x%04X, want 0x4242", resp.frame.XmitCorrelator)
	}
}

func TestNameQuery_NameRecognized(t *testing.T) {
	tp, mock := newTestTransport()
	if err := tp.Start(nil); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tp.Stop()

	name := testName("MYSERVER")
	tp.names.Add(name, false)
	tp.names.SetState(name, nameStateRegistered)

	// Inject NAME_QUERY with session request (callerSession > 0).
	query := &nbfproto.Frame{
		Command:       nbfproto.CmdNameQuery,
		Data2:         0x0001, // caller's session # = 1
		RspCorrelator: 0xBEEF,
	}
	copy(query.DestinationName[:], name[:])
	callerName := testName("CLIENT")
	copy(query.SourceName[:], callerName[:])
	mock.deliverFrame(remoteMAC, nbfproto.NetBIOSMulticastMAC, query)

	time.Sleep(10 * time.Millisecond)

	sent := mock.sentFrames()
	if len(sent) == 0 {
		t.Fatal("expected NAME_RECOGNIZED, got none")
	}
	nr := sent[0]
	if nr.frame.Command != nbfproto.CmdNameRecognized {
		t.Fatalf("command = 0x%02X, want NAME_RECOGNIZED (0x%02X)",
			nr.frame.Command, nbfproto.CmdNameRecognized)
	}
	if nr.dstMAC != remoteMAC {
		t.Fatal("expected directed to remote MAC")
	}
	// Session number should be non-zero (assigned from session table).
	sessionNum := uint8(nr.frame.Data2 & 0xFF)
	if sessionNum == 0 {
		t.Fatal("expected non-zero session number in NAME_RECOGNIZED")
	}
	if nr.frame.XmitCorrelator != 0xBEEF {
		t.Fatalf("XmitCorrelator = 0x%04X, want 0xBEEF", nr.frame.XmitCorrelator)
	}
}

func TestSessionEstablishment(t *testing.T) {
	tp, mock := newTestTransport()
	if err := tp.Start(nil); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tp.Stop()

	name := testName("MYSERVER")
	tp.names.Add(name, false)
	tp.names.SetState(name, nameStateRegistered)

	// Step 1: NAME_QUERY → NAME_RECOGNIZED
	query := &nbfproto.Frame{
		Command:       nbfproto.CmdNameQuery,
		Data2:         0x0001,
		RspCorrelator: 0x1111,
	}
	copy(query.DestinationName[:], name[:])
	mock.deliverFrame(remoteMAC, nbfproto.NetBIOSMulticastMAC, query)
	time.Sleep(10 * time.Millisecond)

	sent := mock.sentFrames()
	if len(sent) != 1 || sent[0].frame.Command != nbfproto.CmdNameRecognized {
		t.Fatal("expected NAME_RECOGNIZED")
	}
	localNum := uint8(sent[0].frame.Data2 & 0xFF)
	rspCorr := sent[0].frame.RspCorrelator
	mock.clearSent()

	// Step 2: SESSION_INITIALIZE → SESSION_CONFIRM
	init := &nbfproto.Frame{
		Command:        nbfproto.CmdSessionInitialize,
		XmitCorrelator: rspCorr,
		RspCorrelator:  0x2222,
		DestNumber:     localNum,
		SourceNumber:   0x05, // remote's session number
	}
	mock.deliverFrame(remoteMAC, localMAC, init)
	time.Sleep(10 * time.Millisecond)

	sent = mock.sentFrames()
	if len(sent) != 1 || sent[0].frame.Command != nbfproto.CmdSessionConfirm {
		t.Fatal("expected SESSION_CONFIRM")
	}
	confirm := sent[0].frame
	if confirm.DestNumber != 0x05 {
		t.Fatalf("SESSION_CONFIRM DestNumber = %d, want 5", confirm.DestNumber)
	}
	if confirm.SourceNumber != localNum {
		t.Fatalf("SESSION_CONFIRM SourceNumber = %d, want %d", confirm.SourceNumber, localNum)
	}

	// Verify session is active.
	sess := tp.sessions.Lookup(remoteMAC, localNum)
	if sess == nil {
		t.Fatal("session not found in table")
	}
	if sess.state != sessionStateActive {
		t.Fatalf("session state = %d, want active (%d)", sess.state, sessionStateActive)
	}
}

func TestDataOnlyLast_DeliveryAndAck(t *testing.T) {
	tp, mock := newTestTransport()
	handler := &mockHandler{}
	tp.SetCommandHandler(handler)
	if err := tp.Start(nil); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tp.Stop()

	// Set up an active session.
	sess := tp.sessions.Create(remoteMAC)
	sess.remoteNum = 0x05
	sess.state = sessionStateActive

	payload := []byte("SMB data goes here")
	data := &nbfproto.Frame{
		Command:        nbfproto.CmdDataOnlyLast,
		RspCorrelator:  0x3333,
		DestNumber:     sess.localNum,
		SourceNumber:   sess.remoteNum,
		Payload:        payload,
	}
	mock.deliverFrame(remoteMAC, localMAC, data)
	time.Sleep(10 * time.Millisecond)

	// Verify DATA_ACK was sent.
	sent := mock.sentFrames()
	if len(sent) == 0 {
		t.Fatal("expected DATA_ACK, got none")
	}
	ack := sent[0]
	if ack.frame.Command != nbfproto.CmdDataAck {
		t.Fatalf("command = 0x%02X, want DATA_ACK (0x%02X)",
			ack.frame.Command, nbfproto.CmdDataAck)
	}
	if ack.frame.XmitCorrelator != 0x3333 {
		t.Fatalf("DATA_ACK XmitCorrelator = 0x%04X, want 0x3333", ack.frame.XmitCorrelator)
	}
	if ack.dstMAC != remoteMAC {
		t.Fatal("DATA_ACK not directed to remote MAC")
	}

	// Verify handler received the session packet.
	pkts := handler.receivedSessions()
	if len(pkts) != 1 {
		t.Fatalf("expected 1 session packet, got %d", len(pkts))
	}
	if string(pkts[0].Payload) != string(payload) {
		t.Fatalf("payload mismatch: %q", pkts[0].Payload)
	}
}

func TestSessionEnd(t *testing.T) {
	tp, mock := newTestTransport()
	if err := tp.Start(nil); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tp.Stop()

	sess := tp.sessions.Create(remoteMAC)
	sess.remoteNum = 0x05
	sess.state = sessionStateActive
	localNum := sess.localNum

	end := &nbfproto.Frame{
		Command:    nbfproto.CmdSessionEnd,
		DestNumber: localNum,
	}
	mock.deliverFrame(remoteMAC, localMAC, end)
	time.Sleep(10 * time.Millisecond)

	if tp.sessions.Lookup(remoteMAC, localNum) != nil {
		t.Fatal("session should have been removed from table")
	}
}

func TestDatagramDelivery(t *testing.T) {
	tp, mock := newTestTransport()
	handler := &mockHandler{}
	tp.SetCommandHandler(handler)
	if err := tp.Start(nil); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tp.Stop()

	src := testName("CLIENT")
	dst := testName("MYSERVER")

	dgram := &nbfproto.Frame{
		Command: nbfproto.CmdDatagram,
		Payload: []byte("datagram payload"),
	}
	copy(dgram.DestinationName[:], dst[:])
	copy(dgram.SourceName[:], src[:])
	mock.deliverFrame(remoteMAC, nbfproto.NetBIOSMulticastMAC, dgram)
	time.Sleep(10 * time.Millisecond)

	rcvd := handler.receivedDatagrams()
	if len(rcvd) != 1 {
		t.Fatalf("expected 1 datagram, got %d", len(rcvd))
	}
	if rcvd[0].Source != src {
		t.Errorf("Source = %q, want %q", rcvd[0].Source.String(), src.String())
	}
	if rcvd[0].Destination != dst {
		t.Errorf("Destination = %q, want %q", rcvd[0].Destination.String(), dst.String())
	}
}

func TestNameNumber1(t *testing.T) {
	mac := [6]byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF}
	n := nameNumber1(mac)
	// First 10 bytes should be zero.
	for i := 0; i < 10; i++ {
		if n[i] != 0 {
			t.Fatalf("byte %d = 0x%02X, want 0x00", i, n[i])
		}
	}
	// Last 6 bytes = MAC.
	if [6]byte(n[10:16]) != mac {
		t.Fatalf("MAC portion mismatch")
	}
}

func TestNameTable_GroupNameNoConflict(t *testing.T) {
	nt := newNameTable()
	name := testName("WORKGROUP")

	entry := nt.Add(name, true)
	if entry == nil {
		t.Fatal("expected entry, got nil")
	}
	if !entry.IsGroup {
		t.Fatal("expected group flag")
	}
}

func TestNameTable_DuplicateAddReturnsNil(t *testing.T) {
	nt := newNameTable()
	name := testName("MYSERVER")

	e1 := nt.Add(name, false)
	if e1 == nil {
		t.Fatal("first add should succeed")
	}

	e2 := nt.Add(name, false)
	if e2 != nil {
		t.Fatal("duplicate add should return nil")
	}
}

func TestSessionTable_AllocWraparound(t *testing.T) {
	st := newSessionTable()

	// Exhaust 1..254
	for i := 0; i < 254; i++ {
		mac := [6]byte{byte(i), 0, 0, 0, 0, 0}
		st.Create(mac)
	}

	// Next allocation should wrap to 1.
	mac := [6]byte{0xFF, 0, 0, 0, 0, 0}
	sess := st.Create(mac)
	if sess.localNum != 1 {
		t.Fatalf("expected wraparound to 1, got %d", sess.localNum)
	}
}

func TestIsSessionCommand_Consistency(t *testing.T) {
	// Verify the discriminator matches the spec boundary.
	for cmd := uint8(0x00); cmd <= 0x13; cmd++ {
		if nbfproto.IsSessionCommand(cmd) {
			t.Errorf("0x%02X should not be session", cmd)
		}
	}
	for cmd := uint8(0x14); cmd <= 0x1F; cmd++ {
		if !nbfproto.IsSessionCommand(cmd) {
			t.Errorf("0x%02X should be session", cmd)
		}
	}
}

var _ = binary.LittleEndian
