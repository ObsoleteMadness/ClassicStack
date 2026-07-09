package netbios

import (
	"context"
	"testing"

	portnetbeui "github.com/ObsoleteMadness/ClassicStack/core/port/netbeui"
	nbf "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbeui"
	protocol "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbios"
	netbeui "github.com/ObsoleteMadness/ClassicStack/core/router/netbeui"
)

// compile-time assertion: the exported Engine satisfies the core/router/netbeui
// mini-router's NameHandler and SessionHandler, so compose registers it directly.
var (
	_ netbeui.NameHandler    = (*Engine)(nil)
	_ netbeui.SessionHandler = (*Engine)(nil)
)

// recordingPort is a netbeui.Port that records every frame the mini-router sends,
// so a test can assert the NBF replies the engine produced. It is the fake link
// the engine's FrameSender (the mini-router) writes through.
type recordingPort struct {
	sent      []sentFrame
	broadcast []*nbf.Frame
	cb        portnetbeui.DeliveryCallback
}

type sentFrame struct {
	dst   [6]byte
	frame *nbf.Frame
}

func (p *recordingPort) SetDeliveryCallback(cb portnetbeui.DeliveryCallback) {
	p.cb = cb
}
func (p *recordingPort) Send(dst [6]byte, f *nbf.Frame) error {
	p.sent = append(p.sent, sentFrame{dst, f})
	return nil
}
func (p *recordingPort) SendBroadcast(f *nbf.Frame) error {
	p.broadcast = append(p.broadcast, f)
	return nil
}

// lastSent returns the most recent directed frame of the given command, or nil.
func (p *recordingPort) lastSent(cmd uint8) *nbf.Frame {
	for i := len(p.sent) - 1; i >= 0; i-- {
		if p.sent[i].frame.Command == cmd {
			return p.sent[i].frame
		}
	}
	return nil
}

// echoConsumer is a SessionConsumer whose circuits echo each served message back
// with a marker prefix, so a test can prove the reassembled SMB message reached
// the consumer and the response travelled back over the circuit.
type echoConsumer struct {
	opened     int
	closed     int
	last       []byte
	lastClient string       // client label the most recent NewConn was opened with
	circuit    *echoCircuit // the most recently opened circuit (for asserting server push)
}

type echoCircuit struct {
	c    *echoConsumer
	push func([]byte) // captured server-push writer, for asserting async delivery
}

func (e *echoConsumer) NewConn(client string) SessionCircuit {
	e.opened++
	e.lastClient = client
	ec := &echoCircuit{c: e}
	e.circuit = ec
	return ec
}
func (ec *echoCircuit) ServeMessage(req []byte) []byte {
	ec.c.last = append([]byte(nil), req...)
	return append([]byte("R:"), req...)
}
func (ec *echoCircuit) SetPushWriter(w func([]byte)) { ec.push = w }
func (ec *echoCircuit) Close()                       { ec.c.closed++ }

// establishCircuit drives a CALL through to an active circuit and returns the
// local/remote session numbers and the peer MAC, leaving the circuit ready for
// data. It mirrors a real client: NAME_QUERY (CALL) → NAME_RECOGNIZED →
// SESSION_INITIALIZE → SESSION_CONFIRM.
func establishCircuit(t *testing.T, r *netbeui.Router, port *recordingPort, name protocol.Name, callerNum uint8) (localNum, remoteNum uint8, peer [6]byte) {
	t.Helper()
	peer = [6]byte{0x02, 0, 0, 0, 0, 0x01}

	// NAME_QUERY (CALL): caller session number in Data2 low byte.
	clientName := protocol.NewName("CLIENT", protocol.NameTypeWorkstation)
	nq := &nbf.Frame{Command: nbf.CmdNameQuery, Data2: uint16(callerNum), RspCorrelator: 0x1234}
	copy(nq.DestinationName[:], name[:])
	copy(nq.SourceName[:], clientName[:])
	r.Inbound(peer, nbf.NetBIOSMulticastMAC, nq)

	nr := port.lastSent(nbf.CmdNameRecognized)
	if nr == nil {
		t.Fatal("no NAME_RECOGNIZED sent for CALL")
	}
	localNum = uint8(nr.Data2 & 0xFF)
	if localNum == 0 {
		t.Fatal("NAME_RECOGNIZED carried session number 0")
	}

	// SESSION_INITIALIZE to the granted local number.
	si := &nbf.Frame{Command: nbf.CmdSessionInitialize, DestNumber: localNum, SourceNumber: callerNum}
	r.Inbound(peer, peer, si)
	if port.lastSent(nbf.CmdSessionConfirm) == nil {
		t.Fatal("no SESSION_CONFIRM sent after SESSION_INITIALIZE")
	}
	return localNum, callerNum, peer
}

// newWiredEngine builds a NetBIOS service claiming "CLASSICSTACK", an NBF engine
// bound to a fresh mini-router with a recording port, the engine registered as
// the router's name + session handler, and the echo consumer installed.
func newWiredEngine(t *testing.T) (*Service, *netbeui.Router, *recordingPort, *echoConsumer) {
	t.Helper()
	svc := NewService(nil, "CLASSICSTACK")
	consumer := &echoConsumer{}
	svc.SetSessionConsumer(consumer)

	r := netbeui.NewRouter(nil)
	port := &recordingPort{}
	r.AddPort(port)

	eng := svc.NewNBFEngine(r)
	if err := r.RegisterSession(eng); err != nil {
		t.Fatalf("RegisterSession: %v", err)
	}
	for _, n := range svc.localNames() {
		if err := r.RegisterName([16]byte(n), eng); err != nil {
			t.Fatalf("RegisterName %q: %v", n.String(), err)
		}
	}
	return svc, r, port, consumer
}

// TestNBF_CallEstablishesCircuit proves a CALL for our name is answered with
// NAME_RECOGNIZED + SESSION_CONFIRM and brings a circuit up.
func TestNBF_CallEstablishesCircuit(t *testing.T) {
	_, r, port, _ := newWiredEngine(t)
	name := protocol.NewName("CLASSICSTACK", protocol.NameTypeFileServer)
	establishCircuit(t, r, port, name, 5)
}

// TestNBF_CallForForeignNameIgnored proves a CALL for a name we do not own
// produces no NAME_RECOGNIZED.
func TestNBF_CallForForeignNameIgnored(t *testing.T) {
	_, r, port, _ := newWiredEngine(t)
	foreign := protocol.NewName("SOMEONELSE", protocol.NameTypeFileServer)
	nq := &nbf.Frame{Command: nbf.CmdNameQuery, Data2: 5}
	copy(nq.DestinationName[:], foreign[:])
	r.Inbound([6]byte{0x02}, nbf.NetBIOSMulticastMAC, nq)
	if port.lastSent(nbf.CmdNameRecognized) != nil {
		t.Fatal("NAME_RECOGNIZED sent for a foreign name")
	}
}

// TestNBF_LocateQueryIsAnswered proves the broadcast-locate phase of a Windows CALL
// — a NAME_QUERY carrying Local Session No. 0 ("FIND.NAME request") for our name —
// is answered with a NAME_RECOGNIZED (Data2 ss = 0, no circuit allocated) rather than
// dropped. This is the NT 3.51 netbeui.pcap regression: without this reply the client
// never learns the name exists and never proceeds to the unicast CALL.
func TestNBF_LocateQueryIsAnswered(t *testing.T) {
	_, r, port, _ := newWiredEngine(t)
	name := protocol.NewName("CLASSICSTACK", protocol.NameTypeFileServer)
	client := protocol.NewName("CLIENT", protocol.NameTypeWorkstation)

	nq := &nbf.Frame{Command: nbf.CmdNameQuery, Data2: 0, RspCorrelator: 0x000b}
	copy(nq.DestinationName[:], name[:])
	copy(nq.SourceName[:], client[:])
	r.Inbound([6]byte{0x02, 0, 0, 0, 0, 0x01}, nbf.NetBIOSMulticastMAC, nq)

	nr := port.lastSent(nbf.CmdNameRecognized)
	if nr == nil {
		t.Fatal("no NAME_RECOGNIZED sent for a session-0 locate query")
	}
	if nr.XmitCorrelator != nq.RspCorrelator {
		t.Errorf("XmitCorrelator = %#04x, want the query's RspCorrelator %#04x", nr.XmitCorrelator, nq.RspCorrelator)
	}
	if nr.Data2&0xFF != 0 {
		t.Errorf("locate NAME_RECOGNIZED Data2 ss = %d, want 0 (no session)", nr.Data2&0xFF)
	}
	if protocol.Name(nr.DestinationName) != client {
		t.Errorf("NAME_RECOGNIZED dest = %q, want the querier %q", protocol.Name(nr.DestinationName).String(), client.String())
	}
	if protocol.Name(nr.SourceName) != name {
		t.Errorf("NAME_RECOGNIZED source = %q, want our name %q", protocol.Name(nr.SourceName).String(), name.String())
	}
}

// TestNBF_DataDeliversToConsumerAndReplies proves a DATA_ONLY_LAST message on an
// established circuit is ACKed, served to the consumer, and the response sent
// back as a DATA_ONLY_LAST.
func TestNBF_DataDeliversToConsumerAndReplies(t *testing.T) {
	_, r, port, consumer := newWiredEngine(t)
	name := protocol.NewName("CLASSICSTACK", protocol.NameTypeFileServer)
	localNum, remoteNum, peer := establishCircuit(t, r, port, name, 7)

	msg := []byte("\xffSMBhello")
	dol := &nbf.Frame{Command: nbf.CmdDataOnlyLast, DestNumber: localNum, SourceNumber: remoteNum, Payload: msg}
	r.Inbound(peer, peer, dol)

	if consumer.opened != 1 {
		t.Fatalf("consumer opened %d circuits, want 1", consumer.opened)
	}
	if string(consumer.last) != string(msg) {
		t.Fatalf("consumer saw %q, want %q", consumer.last, msg)
	}
	// The circuit is opened with the requesting client's MAC as the label so the
	// SMB session-tracking view can attribute the session to that client.
	if want := nbfClientLabel(peer); consumer.lastClient != want {
		t.Errorf("NewConn client label = %q, want %q", consumer.lastClient, want)
	}
	if port.lastSent(nbf.CmdDataAck) == nil {
		t.Error("no DATA_ACK sent for DATA_ONLY_LAST")
	}
	reply := port.lastSent(nbf.CmdDataOnlyLast)
	if reply == nil {
		t.Fatal("no DATA_ONLY_LAST response sent")
	}
	if want := append([]byte("R:"), msg...); string(reply.Payload) != string(want) {
		t.Fatalf("response payload %q, want %q", reply.Payload, want)
	}
	// The response must address the caller's session number.
	if reply.DestNumber != remoteNum || reply.SourceNumber != localNum {
		t.Errorf("response session nums dst=%d src=%d, want dst=%d src=%d",
			reply.DestNumber, reply.SourceNumber, remoteNum, localNum)
	}
}

// TestNBF_ServerPushDeliversUnsolicitedFrame proves the §10d server-push seam: the
// engine installs a push writer on the circuit, and invoking it sends an unsolicited
// DATA_ONLY_LAST to the circuit's peer (the path an async NOTIFY_CHANGE completion
// takes), addressed with the circuit's own session numbers.
func TestNBF_ServerPushDeliversUnsolicitedFrame(t *testing.T) {
	_, r, port, consumer := newWiredEngine(t)
	name := protocol.NewName("CLASSICSTACK", protocol.NameTypeFileServer)
	localNum, remoteNum, peer := establishCircuit(t, r, port, name, 7)

	// One data frame opens the circuit (and installs the push writer).
	r.Inbound(peer, peer, &nbf.Frame{Command: nbf.CmdDataOnlyLast, DestNumber: localNum, SourceNumber: remoteNum, Payload: []byte("\xffSMBx")})
	if consumer.circuit == nil || consumer.circuit.push == nil {
		t.Fatal("engine did not install a server-push writer on the circuit")
	}

	// Invoke the push writer with an unsolicited frame.
	port.sent = nil
	consumer.circuit.push([]byte("\xffSMBnotify"))

	pushed := port.lastSent(nbf.CmdDataOnlyLast)
	if pushed == nil {
		t.Fatal("server push did not send a DATA_ONLY_LAST")
	}
	if string(pushed.Payload) != "\xffSMBnotify" {
		t.Fatalf("pushed payload = %q, want the notify frame", pushed.Payload)
	}
	if pushed.DestNumber != remoteNum || pushed.SourceNumber != localNum {
		t.Errorf("push session nums dst=%d src=%d, want dst=%d src=%d", pushed.DestNumber, pushed.SourceNumber, remoteNum, localNum)
	}
}

// TestNBF_SegmentedMessageReassembled proves a DATA_FIRST_MIDDLE + DATA_ONLY_LAST
// pair is reassembled into one message before reaching the consumer.
func TestNBF_SegmentedMessageReassembled(t *testing.T) {
	_, r, port, consumer := newWiredEngine(t)
	name := protocol.NewName("CLASSICSTACK", protocol.NameTypeFileServer)
	localNum, remoteNum, peer := establishCircuit(t, r, port, name, 9)

	r.Inbound(peer, peer, &nbf.Frame{Command: nbf.CmdDataFirstMiddle, DestNumber: localNum, SourceNumber: remoteNum, Payload: []byte("\xffSMBpart-one;")})
	r.Inbound(peer, peer, &nbf.Frame{Command: nbf.CmdDataOnlyLast, DestNumber: localNum, SourceNumber: remoteNum, Payload: []byte("part-two")})

	if want := "\xffSMBpart-one;part-two"; string(consumer.last) != want {
		t.Fatalf("reassembled message %q, want %q", consumer.last, want)
	}
}

// TestNBF_SessionEndClosesConn proves SESSION_END closes the consumer circuit so
// open handles do not leak, and a duplicate SESSION_END is harmless.
func TestNBF_SessionEndClosesConn(t *testing.T) {
	_, r, port, consumer := newWiredEngine(t)
	name := protocol.NewName("CLASSICSTACK", protocol.NameTypeFileServer)
	localNum, remoteNum, peer := establishCircuit(t, r, port, name, 11)

	// One message opens the circuit's consumer conn.
	r.Inbound(peer, peer, &nbf.Frame{Command: nbf.CmdDataOnlyLast, DestNumber: localNum, SourceNumber: remoteNum, Payload: []byte("\xffSMBx")})
	if consumer.opened != 1 {
		t.Fatalf("opened %d, want 1", consumer.opened)
	}

	r.Inbound(peer, peer, &nbf.Frame{Command: nbf.CmdSessionEnd, DestNumber: localNum, SourceNumber: remoteNum})
	if consumer.closed != 1 {
		t.Fatalf("closed %d after SESSION_END, want 1", consumer.closed)
	}
	// Duplicate SESSION_END is a no-op (circuit already gone).
	r.Inbound(peer, peer, &nbf.Frame{Command: nbf.CmdSessionEnd, DestNumber: localNum, SourceNumber: remoteNum})
	if consumer.closed != 1 {
		t.Fatalf("duplicate SESSION_END closed again: closed=%d", consumer.closed)
	}
}

// TestNBF_StopTearsDownCircuits proves Service.Stop closes any open consumer
// circuits through the engine, so a server shutdown does not leak handles.
func TestNBF_StopTearsDownCircuits(t *testing.T) {
	svc, r, port, consumer := newWiredEngine(t)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	name := protocol.NewName("CLASSICSTACK", protocol.NameTypeFileServer)
	localNum, remoteNum, peer := establishCircuit(t, r, port, name, 13)
	r.Inbound(peer, peer, &nbf.Frame{Command: nbf.CmdDataOnlyLast, DestNumber: localNum, SourceNumber: remoteNum, Payload: []byte("\xffSMBx")})

	if err := svc.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if consumer.closed != 1 {
		t.Fatalf("Stop closed %d circuits, want 1", consumer.closed)
	}
}

// countSent returns how many directed frames of the given command were sent.
func (p *recordingPort) countSent(cmd uint8) int {
	n := 0
	for _, s := range p.sent {
		if s.frame.Command == cmd {
			n++
		}
	}
	return n
}

// TestNBF_NoReceiveHoldsReplyUntilContinue proves the NBF flow-control window: a peer
// that sends NO_RECEIVE before our reply blocks the circuit, so the DATA_ONLY_LAST
// response is held (only the DATA_ACK for the request goes out); RECEIVE_CONTINUE then
// flushes the queued response. Matches the legacy over_netbeui transport.
func TestNBF_NoReceiveHoldsReplyUntilContinue(t *testing.T) {
	_, r, port, _ := newWiredEngine(t)
	name := protocol.NewName("CLASSICSTACK", protocol.NameTypeFileServer)
	localNum, remoteNum, peer := establishCircuit(t, r, port, name, 21)

	// Peer closes its receive window before we would reply.
	r.Inbound(peer, peer, &nbf.Frame{Command: nbf.CmdNoReceive, DestNumber: localNum, SourceNumber: remoteNum})

	// A request arrives: it is ACKed, served, but the reply must be held.
	msg := []byte("\xffSMBq")
	r.Inbound(peer, peer, &nbf.Frame{Command: nbf.CmdDataOnlyLast, DestNumber: localNum, SourceNumber: remoteNum, Payload: msg})
	if port.lastSent(nbf.CmdDataAck) == nil {
		t.Fatal("no DATA_ACK sent for the request")
	}
	if port.countSent(nbf.CmdDataOnlyLast) != 0 {
		t.Fatal("reply DATA_ONLY_LAST was sent while the receive window was closed")
	}

	// Window reopens: the held reply flushes.
	r.Inbound(peer, peer, &nbf.Frame{Command: nbf.CmdReceiveContinue, DestNumber: localNum, SourceNumber: remoteNum})
	reply := port.lastSent(nbf.CmdDataOnlyLast)
	if reply == nil {
		t.Fatal("RECEIVE_CONTINUE did not flush the held reply")
	}
	if want := append([]byte("R:"), msg...); string(reply.Payload) != string(want) {
		t.Fatalf("flushed reply payload %q, want %q", reply.Payload, want)
	}
	if reply.DestNumber != remoteNum || reply.SourceNumber != localNum {
		t.Errorf("flushed reply nums dst=%d src=%d, want dst=%d src=%d", reply.DestNumber, reply.SourceNumber, remoteNum, localNum)
	}
}

// TestNBF_ReceiveOutstandingRetransmitsLast proves a RECEIVE_OUTSTANDING makes the
// engine retransmit the last session frame it sent (the peer missed it). Matches the
// legacy over_netbeui handleReceiveOutstanding.
func TestNBF_ReceiveOutstandingRetransmitsLast(t *testing.T) {
	_, r, port, _ := newWiredEngine(t)
	name := protocol.NewName("CLASSICSTACK", protocol.NameTypeFileServer)
	localNum, remoteNum, peer := establishCircuit(t, r, port, name, 23)

	// Drive one request→reply so the engine records a last-sent frame.
	r.Inbound(peer, peer, &nbf.Frame{Command: nbf.CmdDataOnlyLast, DestNumber: localNum, SourceNumber: remoteNum, Payload: []byte("\xffSMBz")})
	before := port.countSent(nbf.CmdDataOnlyLast)
	if before == 0 {
		t.Fatal("no reply frame recorded to retransmit")
	}

	// Peer asks for the last frame again.
	r.Inbound(peer, peer, &nbf.Frame{Command: nbf.CmdReceiveOutstanding, DestNumber: localNum, SourceNumber: remoteNum})
	if got := port.countSent(nbf.CmdDataOnlyLast); got != before+1 {
		t.Fatalf("RECEIVE_OUTSTANDING sent %d DATA_ONLY_LAST total, want %d (one retransmit)", got, before+1)
	}
	last := port.lastSent(nbf.CmdDataOnlyLast)
	if want := []byte("R:\xffSMBz"); string(last.Payload) != string(want) {
		t.Fatalf("retransmit payload %q, want %q", last.Payload, want)
	}
}

// recordingDatagramConsumer captures datagrams handed up by the engine.
type recordingDatagramConsumer struct{ got []Datagram }

func (c *recordingDatagramConsumer) HandleDatagram(d Datagram) { c.got = append(c.got, d) }

// TestNBF_StatusQueryAnswered proves a STATUS_QUERY (NODE.STATUS) for one of our
// names is answered with a STATUS_RESPONSE carrying the local name table.
func TestNBF_StatusQueryAnswered(t *testing.T) {
	_, r, port, _ := newWiredEngine(t)
	name := protocol.NewName("CLASSICSTACK", protocol.NameTypeFileServer)

	sq := &nbf.Frame{Command: nbf.CmdStatusQuery, Data2: 1024, RspCorrelator: 0x55}
	copy(sq.DestinationName[:], name[:])
	client := protocol.NewName("CLIENT", protocol.NameTypeWorkstation)
	copy(sq.SourceName[:], client[:])
	peer := [6]byte{0x02, 0, 0, 0, 0, 0x09}
	r.Inbound(peer, peer, sq)

	resp := port.lastSent(nbf.CmdStatusResponse)
	if resp == nil {
		t.Fatal("no STATUS_RESPONSE sent")
	}
	// Length field (low 14 bits of Data2) must be a whole number of 18-byte entries
	// and cover the two names CLASSICSTACK claims (file-server + workstation).
	n := int(resp.Data2 & statusLenMask)
	if n == 0 || n%statusEntryLen != 0 {
		t.Fatalf("STATUS_RESPONSE length %d not a multiple of %d", n, statusEntryLen)
	}
	if len(resp.Payload) != n {
		t.Fatalf("payload %d bytes, Data2 length %d", len(resp.Payload), n)
	}
	// The reply must address the querier and source from the queried name.
	if protocol.Name(resp.DestinationName) != client {
		t.Errorf("STATUS_RESPONSE dst = %q, want %q", protocol.Name(resp.DestinationName).String(), client.String())
	}
}

// TestNBF_StatusQueryForeignNameIgnored proves a STATUS_QUERY for a name we do not
// own produces no STATUS_RESPONSE.
func TestNBF_StatusQueryForeignNameIgnored(t *testing.T) {
	_, r, port, _ := newWiredEngine(t)
	foreign := protocol.NewName("ELSEWHERE", protocol.NameTypeFileServer)
	sq := &nbf.Frame{Command: nbf.CmdStatusQuery, Data2: 1024}
	copy(sq.DestinationName[:], foreign[:])
	r.Inbound([6]byte{0x02}, [6]byte{0x02}, sq)
	if port.lastSent(nbf.CmdStatusResponse) != nil {
		t.Fatal("STATUS_RESPONSE sent for a foreign name")
	}
}

// TestNBF_StatusQueryTruncation proves a small advertised buffer truncates the
// name table to whole entries and sets the more/too-big flags.
func TestNBF_StatusQueryTruncation(t *testing.T) {
	_, r, port, _ := newWiredEngine(t)
	name := protocol.NewName("CLASSICSTACK", protocol.NameTypeFileServer)
	sq := &nbf.Frame{Command: nbf.CmdStatusQuery, Data2: statusEntryLen} // room for exactly one entry
	copy(sq.DestinationName[:], name[:])
	r.Inbound([6]byte{0x02}, [6]byte{0x02}, sq)

	resp := port.lastSent(nbf.CmdStatusResponse)
	if resp == nil {
		t.Fatal("no STATUS_RESPONSE sent")
	}
	if resp.Data2&statusFlagMore == 0 {
		t.Error("expected the more-data flag set on a truncated table")
	}
	if got := int(resp.Data2 & statusLenMask); got != statusEntryLen {
		t.Fatalf("truncated length = %d, want %d (one entry)", got, statusEntryLen)
	}
}

// TestNBF_DatagramDeliveredToConsumer proves a directed datagram is decoded to
// names + payload and handed to the installed DatagramConsumer.
func TestNBF_DatagramDeliveredToConsumer(t *testing.T) {
	svc, r, _, _ := newWiredEngine(t)
	dc := &recordingDatagramConsumer{}
	svc.SetDatagramConsumer(dc)

	name := protocol.NewName("CLASSICSTACK", protocol.NameTypeFileServer)
	src := protocol.NewName("BROWSER", protocol.NameTypeWorkstation)
	dg := &nbf.Frame{Command: nbf.CmdDatagram, Payload: []byte("mailslot-data")}
	copy(dg.DestinationName[:], name[:])
	copy(dg.SourceName[:], src[:])
	r.Inbound([6]byte{0x02}, [6]byte{0x02}, dg)

	if len(dc.got) != 1 {
		t.Fatalf("consumer got %d datagrams, want 1", len(dc.got))
	}
	d := dc.got[0]
	if d.Source != src || d.Destination != name {
		t.Errorf("datagram names src=%q dst=%q", d.Source.String(), d.Destination.String())
	}
	if string(d.Payload) != "mailslot-data" || d.Broadcast {
		t.Errorf("datagram payload=%q broadcast=%v", d.Payload, d.Broadcast)
	}
}

// TestNBF_DatagramDroppedWithoutConsumer proves a datagram with no consumer wired
// is dropped cleanly (no panic, no reply).
func TestNBF_DatagramDroppedWithoutConsumer(t *testing.T) {
	_, r, port, _ := newWiredEngine(t)
	name := protocol.NewName("CLASSICSTACK", protocol.NameTypeFileServer)
	dg := &nbf.Frame{Command: nbf.CmdDatagramBroadcast, Payload: []byte("x")}
	copy(dg.DestinationName[:], name[:])
	r.Inbound([6]byte{0x02}, [6]byte{0x02}, dg)
	if len(port.sent) != 0 || len(port.broadcast) != 0 {
		t.Fatal("a datagram without a consumer produced wire traffic")
	}
}
