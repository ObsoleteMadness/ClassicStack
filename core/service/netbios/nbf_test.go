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
	opened int
	closed int
	last   []byte
}

type echoCircuit struct {
	c *echoConsumer
}

func (e *echoConsumer) NewConn() SessionCircuit {
	e.opened++
	return &echoCircuit{c: e}
}
func (ec *echoCircuit) ServeMessage(req []byte) []byte {
	ec.c.last = append([]byte(nil), req...)
	return append([]byte("R:"), req...)
}
func (ec *echoCircuit) Close() { ec.c.closed++ }

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
