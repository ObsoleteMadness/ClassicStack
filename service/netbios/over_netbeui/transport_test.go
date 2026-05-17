package over_netbeui

import (
	"context"
	"encoding/binary"
	"sync"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/capture"
	netbeuiport "github.com/ObsoleteMadness/ClassicStack/port/netbeui"
	nbfproto "github.com/ObsoleteMadness/ClassicStack/protocol/netbeui"
	protocol "github.com/ObsoleteMadness/ClassicStack/protocol/netbios"
	nb "github.com/ObsoleteMadness/ClassicStack/service/netbios"
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

type mockContextualHandler struct {
	mu             sync.Mutex
	sessionCalls   int
	datagramCalls  int
	lastSessionCtx nb.SessionContext
	lastDgramCtx   nb.DatagramContext
	response       []byte
}

func (h *mockContextualHandler) HandleSession(_ *protocol.SessionPacket) error {
	return nil
}

func (h *mockContextualHandler) HandleDatagram(_ *protocol.Datagram) error {
	return nil
}

func (h *mockContextualHandler) HandleSessionContext(_ *protocol.SessionPacket, ctx nb.SessionContext) (*protocol.SessionPacket, error) {
	h.mu.Lock()
	h.sessionCalls++
	h.lastSessionCtx = ctx
	resp := append([]byte(nil), h.response...)
	h.mu.Unlock()
	if len(resp) == 0 {
		return nil, nil
	}
	return &protocol.SessionPacket{Type: protocol.SessionMessage, Payload: resp}, nil
}

func (h *mockContextualHandler) HandleDatagramContext(_ *protocol.Datagram, ctx nb.DatagramContext) error {
	h.mu.Lock()
	h.datagramCalls++
	h.lastDgramCtx = ctx
	h.mu.Unlock()
	return nil
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
	if err := tp.Start(context.TODO()); err != nil {
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
	if err := tp.Start(context.TODO()); err != nil {
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
	if err := tp.Start(context.TODO()); err != nil {
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
	if err := tp.Start(context.TODO()); err != nil {
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

func TestNameQuery_ReusesSessionForDuplicateCallerSession(t *testing.T) {
	tp, mock := newTestTransport()
	if err := tp.Start(context.TODO()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tp.Stop()

	name := testName("MYSERVER")
	client := testName("CLIENT")
	tp.names.Add(name, false)
	tp.names.SetState(name, nameStateRegistered)

	query1 := &nbfproto.Frame{
		Command:       nbfproto.CmdNameQuery,
		Data2:         0x0007, // caller session = 7
		RspCorrelator: 0x1111,
	}
	copy(query1.DestinationName[:], name[:])
	copy(query1.SourceName[:], client[:])
	mock.deliverFrame(remoteMAC, nbfproto.NetBIOSMulticastMAC, query1)
	time.Sleep(10 * time.Millisecond)

	query2 := &nbfproto.Frame{
		Command:       nbfproto.CmdNameQuery,
		Data2:         0x0007, // same caller session
		RspCorrelator: 0x2222,
	}
	copy(query2.DestinationName[:], name[:])
	copy(query2.SourceName[:], client[:])
	mock.deliverFrame(remoteMAC, nbfproto.NetBIOSMulticastMAC, query2)
	time.Sleep(10 * time.Millisecond)

	sent := mock.sentFrames()
	if len(sent) != 2 {
		t.Fatalf("expected 2 NAME_RECOGNIZED frames, got %d", len(sent))
	}
	n1 := uint8(sent[0].frame.Data2 & 0xFF)
	n2 := uint8(sent[1].frame.Data2 & 0xFF)
	if n1 == 0 || n2 == 0 {
		t.Fatal("expected non-zero session number in NAME_RECOGNIZED")
	}
	if n1 != n2 {
		t.Fatalf("expected reused local session number, got %d and %d", n1, n2)
	}
	if sent[0].frame.RspCorrelator == 0 || sent[1].frame.RspCorrelator == 0 {
		t.Fatal("expected non-zero RspCorrelator in NAME_RECOGNIZED")
	}
	if sent[0].frame.RspCorrelator != sent[1].frame.RspCorrelator {
		t.Fatalf("expected same RspCorrelator, got %d and %d", sent[0].frame.RspCorrelator, sent[1].frame.RspCorrelator)
	}
}

func TestSessionEstablishment(t *testing.T) {
	tp, mock := newTestTransport()
	if err := tp.Start(context.TODO()); err != nil {
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
	if sess.State != sessionStateActive {
		t.Fatalf("session state = %d, want active (%d)", sess.State, sessionStateActive)
	}
}

func TestDataOnlyLast_DeliveryAndAck(t *testing.T) {
	tp, mock := newTestTransport()
	handler := &mockHandler{}
	tp.SetCommandHandler(handler)
	if err := tp.Start(context.TODO()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tp.Stop()

	// Set up an active session.
	sess := tp.sessions.Create(remoteMAC)
	sess.RemoteNum = 0x05
	sess.State = sessionStateActive

	payload := []byte("SMB data goes here")
	data := &nbfproto.Frame{
		Command:        nbfproto.CmdDataOnlyLast,
		RspCorrelator:  0x3333,
		DestNumber:     sess.LocalNum,
		SourceNumber:   sess.RemoteNum,
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

func TestDataOnlyLast_ContextualHandlerResponds(t *testing.T) {
	tp, mock := newTestTransport()
	h := &mockContextualHandler{response: []byte("SMB reply")}
	tp.SetCommandHandler(h)
	if err := tp.Start(context.TODO()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tp.Stop()

	sess := tp.sessions.Create(remoteMAC)
	sess.RemoteNum = 0x05
	sess.State = sessionStateActive

	in := &nbfproto.Frame{
		Command:       nbfproto.CmdDataOnlyLast,
		DestNumber:    sess.LocalNum,
		SourceNumber:  sess.RemoteNum,
		RspCorrelator: 0x1111,
		Payload:       []byte("SMB request"),
	}
	mock.deliverFrame(remoteMAC, localMAC, in)
	time.Sleep(10 * time.Millisecond)

	sent := mock.sentFrames()
	if len(sent) != 2 {
		t.Fatalf("expected DATA_ACK + response DATA_ONLY_LAST, got %d frames", len(sent))
	}
	if sent[0].frame.Command != nbfproto.CmdDataAck {
		t.Fatalf("first frame command = 0x%02X, want DATA_ACK", sent[0].frame.Command)
	}
	if sent[1].frame.Command != nbfproto.CmdDataOnlyLast {
		t.Fatalf("second frame command = 0x%02X, want DATA_ONLY_LAST", sent[1].frame.Command)
	}
	if got := string(sent[1].frame.Payload); got != "SMB reply" {
		t.Fatalf("response payload = %q, want %q", got, "SMB reply")
	}
	if sent[1].frame.DestNumber != sess.RemoteNum {
		t.Fatalf("response DestNumber = %d, want %d", sent[1].frame.DestNumber, sess.RemoteNum)
	}
	if sent[1].frame.SourceNumber != sess.LocalNum {
		t.Fatalf("response SourceNumber = %d, want %d", sent[1].frame.SourceNumber, sess.LocalNum)
	}
}

func TestSendDirectedDatagram_UsesRemoteMAC(t *testing.T) {
	tp, mock := newTestTransport()
	if err := tp.Start(context.TODO()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tp.Stop()

	d := &protocol.Datagram{
		Destination: testName("SERVER"),
		Source:      testName("CLIENT"),
		Payload:     []byte("browse"),
	}
	err := tp.SendDirectedDatagram(d, nb.DatagramEndpoint{Node: remoteMAC})
	if err != nil {
		t.Fatalf("SendDirectedDatagram: %v", err)
	}

	sent := mock.sentFrames()
	if len(sent) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(sent))
	}
	if sent[0].dstMAC != remoteMAC {
		t.Fatalf("dstMAC = %v, want %v", sent[0].dstMAC, remoteMAC)
	}
	if sent[0].frame.Command != nbfproto.CmdDatagram {
		t.Fatalf("command = 0x%02X, want DATAGRAM", sent[0].frame.Command)
	}
}

func TestSendSession_SegmentsByRemoteMaxPayload(t *testing.T) {
	tp, mock := newTestTransport()
	if err := tp.Start(context.TODO()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tp.Stop()

	sess := tp.sessions.Create(remoteMAC)
	sess.RemoteNum = 0x05
	sess.State = sessionStateActive

	key := sessionWireKey(remoteMAC, sess.LocalNum)
	tp.txMu.Lock()
	tp.sessionMaxPayload[key] = 5
	tp.txMu.Unlock()

	pkt := &protocol.SessionPacket{Type: protocol.SessionMessage, Payload: []byte("abcdefghijk")}
	if err := tp.SendSession(pkt); err != nil {
		t.Fatalf("SendSession: %v", err)
	}

	sent := mock.sentFrames()
	if len(sent) != 3 {
		t.Fatalf("expected 3 segmented frames, got %d", len(sent))
	}
	if sent[0].frame.Command != nbfproto.CmdDataFirstMiddle || sent[1].frame.Command != nbfproto.CmdDataFirstMiddle || sent[2].frame.Command != nbfproto.CmdDataOnlyLast {
		t.Fatal("unexpected command sequence for segmented send")
	}
	if got := string(sent[0].frame.Payload); got != "abcde" {
		t.Fatalf("segment0 payload = %q, want %q", got, "abcde")
	}
	if got := string(sent[1].frame.Payload); got != "fghij" {
		t.Fatalf("segment1 payload = %q, want %q", got, "fghij")
	}
	if got := string(sent[2].frame.Payload); got != "k" {
		t.Fatalf("segment2 payload = %q, want %q", got, "k")
	}
}

func TestNoReceive_QueuesUntilReceiveContinue(t *testing.T) {
	tp, mock := newTestTransport()
	if err := tp.Start(context.TODO()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tp.Stop()

	sess := tp.sessions.Create(remoteMAC)
	sess.RemoteNum = 0x05
	sess.State = sessionStateActive

	noRecv := &nbfproto.Frame{
		Command:      nbfproto.CmdNoReceive,
		DestNumber:   sess.LocalNum,
		SourceNumber: sess.RemoteNum,
	}
	mock.deliverFrame(remoteMAC, localMAC, noRecv)

	if err := tp.SendSession(&protocol.SessionPacket{Type: protocol.SessionMessage, Payload: []byte("reply")}); err != nil {
		t.Fatalf("SendSession: %v", err)
	}

	if got := len(mock.sentFrames()); got != 0 {
		t.Fatalf("expected no immediate sends while blocked, got %d", got)
	}

	cont := &nbfproto.Frame{
		Command:      nbfproto.CmdReceiveContinue,
		DestNumber:   sess.LocalNum,
		SourceNumber: sess.RemoteNum,
	}
	mock.deliverFrame(remoteMAC, localMAC, cont)
	time.Sleep(10 * time.Millisecond)

	sent := mock.sentFrames()
	if len(sent) != 1 {
		t.Fatalf("expected 1 frame flushed on RECEIVE_CONTINUE, got %d", len(sent))
	}
	if sent[0].frame.Command != nbfproto.CmdDataOnlyLast {
		t.Fatalf("flushed command = 0x%02X, want DATA_ONLY_LAST", sent[0].frame.Command)
	}
}

func TestReceiveOutstanding_RetransmitsLastFrame(t *testing.T) {
	tp, mock := newTestTransport()
	if err := tp.Start(context.TODO()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tp.Stop()

	sess := tp.sessions.Create(remoteMAC)
	sess.RemoteNum = 0x05
	sess.State = sessionStateActive

	if err := tp.SendSession(&protocol.SessionPacket{Type: protocol.SessionMessage, Payload: []byte("payload")}); err != nil {
		t.Fatalf("SendSession: %v", err)
	}
	mock.clearSent()

	outstanding := &nbfproto.Frame{
		Command:      nbfproto.CmdReceiveOutstanding,
		DestNumber:   sess.LocalNum,
		SourceNumber: sess.RemoteNum,
	}
	mock.deliverFrame(remoteMAC, localMAC, outstanding)
	time.Sleep(10 * time.Millisecond)

	sent := mock.sentFrames()
	if len(sent) != 1 {
		t.Fatalf("expected 1 retransmitted frame, got %d", len(sent))
	}
	if got := string(sent[0].frame.Payload); got != "payload" {
		t.Fatalf("retransmitted payload = %q, want %q", got, "payload")
	}
}

func TestDataOnlyLast_SourceSessionMismatchIgnored(t *testing.T) {
	tp, mock := newTestTransport()
	handler := &mockHandler{}
	tp.SetCommandHandler(handler)
	if err := tp.Start(context.TODO()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tp.Stop()

	sess := tp.sessions.Create(remoteMAC)
	sess.RemoteNum = 0x05
	sess.State = sessionStateActive

	data := &nbfproto.Frame{
		Command:       nbfproto.CmdDataOnlyLast,
		RspCorrelator: 0x3333,
		DestNumber:    sess.LocalNum,
		SourceNumber:  0x06, // mismatched remote session number
		Payload:       []byte("ignored"),
	}
	mock.deliverFrame(remoteMAC, localMAC, data)
	time.Sleep(10 * time.Millisecond)

	if got := mock.sentFrames(); len(got) != 0 {
		t.Fatalf("expected no DATA_ACK for mismatched session number, got %d", len(got))
	}
	if got := handler.receivedSessions(); len(got) != 0 {
		t.Fatalf("expected no delivered session packets, got %d", len(got))
	}
}

func TestDataFirstMiddle_ReassemblesWithFinalSegment(t *testing.T) {
	tp, mock := newTestTransport()
	handler := &mockHandler{}
	tp.SetCommandHandler(handler)
	if err := tp.Start(context.TODO()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tp.Stop()

	sess := tp.sessions.Create(remoteMAC)
	sess.RemoteNum = 0x05
	sess.State = sessionStateActive

	first := &nbfproto.Frame{
		Command:      nbfproto.CmdDataFirstMiddle,
		DestNumber:   sess.LocalNum,
		SourceNumber: sess.RemoteNum,
		Payload:      []byte("first-"),
	}
	last := &nbfproto.Frame{
		Command:       nbfproto.CmdDataOnlyLast,
		RspCorrelator: 0x8888,
		DestNumber:    sess.LocalNum,
		SourceNumber:  sess.RemoteNum,
		Payload:       []byte("last"),
	}
	mock.deliverFrame(remoteMAC, localMAC, first)
	mock.deliverFrame(remoteMAC, localMAC, last)
	time.Sleep(10 * time.Millisecond)

	pkts := handler.receivedSessions()
	if len(pkts) != 1 {
		t.Fatalf("expected 1 reassembled session packet, got %d", len(pkts))
	}
	if got := string(pkts[0].Payload); got != "first-last" {
		t.Fatalf("payload = %q, want %q", got, "first-last")
	}
}

func TestSessionEnd_SourceSessionMismatchIgnored(t *testing.T) {
	tp, mock := newTestTransport()
	if err := tp.Start(context.TODO()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tp.Stop()

	sess := tp.sessions.Create(remoteMAC)
	sess.RemoteNum = 0x05
	sess.State = sessionStateActive
	localNum := sess.LocalNum

	end := &nbfproto.Frame{
		Command:      nbfproto.CmdSessionEnd,
		DestNumber:   localNum,
		SourceNumber: 0x06, // mismatched remote session number
	}
	mock.deliverFrame(remoteMAC, localMAC, end)
	time.Sleep(10 * time.Millisecond)

	if tp.sessions.Lookup(remoteMAC, localNum) == nil {
		t.Fatal("session should be retained on mismatched SESSION_END source number")
	}
}

func TestDataAck_CorrelatorMismatchIgnored(t *testing.T) {
	tp, mock := newTestTransport()
	if err := tp.Start(context.TODO()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tp.Stop()

	sess := tp.sessions.Create(remoteMAC)
	sess.RemoteNum = 0x05
	sess.State = sessionStateActive
	sess.LastXmitCorrelator = 0x2001

	ack := &nbfproto.Frame{
		Command:        nbfproto.CmdDataAck,
		XmitCorrelator: 0x2002, // does not match last outbound correlator
		DestNumber:     sess.LocalNum,
		SourceNumber:   sess.RemoteNum,
	}
	mock.deliverFrame(remoteMAC, localMAC, ack)
	time.Sleep(10 * time.Millisecond)

	if sess.LastXmitCorrelator != 0x2001 {
		t.Fatalf("LastXmitCorrelator = 0x%04X, want 0x2001", sess.LastXmitCorrelator)
	}
}

func TestSessionEnd(t *testing.T) {
	tp, mock := newTestTransport()
	if err := tp.Start(context.TODO()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tp.Stop()

	sess := tp.sessions.Create(remoteMAC)
	sess.RemoteNum = 0x05
	sess.State = sessionStateActive
	localNum := sess.LocalNum

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
	if err := tp.Start(context.TODO()); err != nil {
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

func TestStatusQuery_RegisteredNameResponds(t *testing.T) {
	tp, mock := newTestTransport()
	if err := tp.Start(context.TODO()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tp.Stop()

	server := testName("MYSERVER")
	client := testName("CLIENT")
	tp.names.Add(server, false)
	tp.names.SetState(server, nameStateRegistered)

	query := &nbfproto.Frame{
		Command:       nbfproto.CmdStatusQuery,
		Data2:         1024,
		RspCorrelator: 0x5555,
	}
	copy(query.DestinationName[:], server[:])
	copy(query.SourceName[:], client[:])

	mock.deliverFrame(remoteMAC, nbfproto.NetBIOSMulticastMAC, query)
	time.Sleep(10 * time.Millisecond)

	sent := mock.sentFrames()
	if len(sent) != 1 {
		t.Fatalf("expected 1 STATUS_RESPONSE, got %d", len(sent))
	}
	resp := sent[0]
	if resp.dstMAC != remoteMAC {
		t.Fatal("expected STATUS_RESPONSE directed to querying MAC")
	}
	if resp.frame.Command != nbfproto.CmdStatusResponse {
		t.Fatalf("command = 0x%02X, want STATUS_RESPONSE (0x%02X)",
			resp.frame.Command, nbfproto.CmdStatusResponse)
	}
	if resp.frame.XmitCorrelator != 0x5555 {
		t.Fatalf("XmitCorrelator = 0x%04X, want 0x5555", resp.frame.XmitCorrelator)
	}
	if protocol.Name(resp.frame.SourceName) != server {
		t.Fatalf("SourceName = %q, want %q",
			protocol.Name(resp.frame.SourceName).String(), server.String())
	}
	if protocol.Name(resp.frame.DestinationName) != client {
		t.Fatalf("DestinationName = %q, want %q",
			protocol.Name(resp.frame.DestinationName).String(), client.String())
	}
	if len(resp.frame.Payload) == 0 {
		t.Fatal("expected STATUS_RESPONSE payload with adapter name entries")
	}
	if len(resp.frame.Payload)%18 != 0 {
		t.Fatalf("STATUS_RESPONSE payload length = %d, want multiple of 18", len(resp.frame.Payload))
	}
}

func TestStatusQuery_TruncatesByRequesterBufferLength(t *testing.T) {
	tp, mock := newTestTransport()
	if err := tp.Start(context.TODO()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tp.Stop()

	server := testName("MYSERVER")
	alias := testName("ALIAS")
	tp.names.Add(server, false)
	tp.names.SetState(server, nameStateRegistered)
	tp.names.Add(alias, false)
	tp.names.SetState(alias, nameStateRegistered)

	query := &nbfproto.Frame{
		Command:       nbfproto.CmdStatusQuery,
		Data2:         18, // exactly one entry
		RspCorrelator: 0x6666,
	}
	client := testName("CLIENT")
	copy(query.DestinationName[:], server[:])
	copy(query.SourceName[:], client[:])

	mock.deliverFrame(remoteMAC, nbfproto.NetBIOSMulticastMAC, query)
	time.Sleep(10 * time.Millisecond)

	sent := mock.sentFrames()
	if len(sent) != 1 {
		t.Fatalf("expected 1 STATUS_RESPONSE, got %d", len(sent))
	}
	resp := sent[0].frame
	if len(resp.Payload) != 18 {
		t.Fatalf("payload length = %d, want 18", len(resp.Payload))
	}
	if resp.Data2&0xC000 != 0xC000 {
		t.Fatalf("Data2 truncation bits = 0x%04X, want both high bits set", resp.Data2)
	}
}

func TestStatusQuery_UnknownNameIgnored(t *testing.T) {
	tp, mock := newTestTransport()
	if err := tp.Start(context.TODO()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tp.Stop()

	unknown := testName("UNKNOWN")
	client := testName("CLIENT")
	query := &nbfproto.Frame{Command: nbfproto.CmdStatusQuery}
	copy(query.DestinationName[:], unknown[:])
	copy(query.SourceName[:], client[:])

	mock.deliverFrame(remoteMAC, nbfproto.NetBIOSMulticastMAC, query)
	time.Sleep(10 * time.Millisecond)

	if got := mock.sentFrames(); len(got) != 0 {
		t.Fatalf("expected no STATUS_RESPONSE for unknown name, got %d", len(got))
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
	if sess.LocalNum != 1 {
		t.Fatalf("expected wraparound to 1, got %d", sess.LocalNum)
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
