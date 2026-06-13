package netbios

import (
	"context"
	"testing"

	portipx "github.com/ObsoleteMadness/ClassicStack/core/port/ipx"
	ipxproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx"
	protocol "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbios"
	ipxrouter "github.com/ObsoleteMadness/ClassicStack/core/router/ipx"
)

// compile-time assertion: the exported IPXEngine satisfies the core/router/ipx
// mini-router's SocketHandler, so compose registers it directly on socket 0x0455.
var _ ipxrouter.SocketHandler = (*IPXEngine)(nil)

// recordingIPXPort is an ipxrouter.Port that records every datagram the
// mini-router sends, so a test can assert the NB-IPX replies the engine produced.
// It is the fake link the engine's DatagramSender (the mini-router) writes through.
type recordingIPXPort struct {
	sent []*ipxproto.Datagram
	cb   portipx.DeliveryCallback
}

func (p *recordingIPXPort) SetDeliveryCallback(cb portipx.DeliveryCallback) { p.cb = cb }
func (p *recordingIPXPort) Send(_ [6]byte, d *ipxproto.Datagram) error {
	p.sent = append(p.sent, d)
	return nil
}

// lastSentStream returns the most recent datagram whose NB-IPX session header
// carries the given DataStreamType, with its decoded header, or nil.
func (p *recordingIPXPort) lastSentStream(streamType uint8) (*ipxproto.Datagram, *protocol.NBIPXSessionHeader) {
	for i := len(p.sent) - 1; i >= 0; i-- {
		hdr, err := protocol.DecodeSessionHeader(p.sent[i].Payload)
		if err != nil {
			continue
		}
		if hdr.DataStreamType == streamType {
			return p.sent[i], hdr
		}
	}
	return nil, nil
}

// testRouterNode is the IPX node the mini-router presents; inbound datagrams must
// be addressed to it (or broadcast) to pass the router's addressed-to-us filter.
var testRouterNode = [6]byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55}

// testPeer is the remote NB-IPX endpoint a test client drives from.
var (
	testPeerNet  = [4]byte{0, 0, 0, 0}
	testPeerNode = [6]byte{0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f}
	testPeerSock = [2]byte{0x40, 0x00}
)

// newWiredIPXEngine builds a NetBIOS service, an NBIPX engine bound to a fresh IPX
// mini-router with a recording port, the engine registered as the router's
// SocketHandler on the NB-IPX session socket, and the echo consumer installed.
func newWiredIPXEngine(t *testing.T) (*Service, *ipxrouter.Router, *recordingIPXPort, *echoConsumer) {
	t.Helper()
	svc := NewService(nil, "CLASSICSTACK")
	consumer := &echoConsumer{}
	svc.SetSessionConsumer(consumer)

	r := ipxrouter.NewRouter(nil)
	r.SetIdentity(ipxrouter.DefaultNetwork, testRouterNode)
	port := &recordingIPXPort{}
	r.AddPort(port)

	eng := svc.NewIPXEngine(r)
	if err := r.RegisterSocket(NBIPXSessionSocket, eng); err != nil {
		t.Fatalf("RegisterSocket: %v", err)
	}
	return svc, r, port, consumer
}

// sessionDatagram builds an inbound NB-IPX PEP datagram (type 4) addressed to the
// router on the session socket, carrying a session header + body.
func sessionDatagram(hdr *protocol.NBIPXSessionHeader, body []byte) *ipxproto.Datagram {
	payload := append(protocol.EncodeSessionHeader(hdr), body...)
	return &ipxproto.Datagram{
		Type:    protocol.IPXTypePEP,
		DstNet:  ipxrouter.DefaultNetwork,
		DstNode: testRouterNode,
		DstSock: NBIPXSessionSocket,
		SrcNet:  testPeerNet,
		SrcNode: testPeerNode,
		SrcSock: testPeerSock,
		Payload: payload,
	}
}

// establishIPXCircuit drives SESSION_INIT through the mini-router and returns the
// remote connection ID used and the local ID the engine confirmed.
func establishIPXCircuit(t *testing.T, r *ipxrouter.Router, port *recordingIPXPort, remoteID uint16) (localID uint16) {
	t.Helper()
	init := &protocol.NBIPXSessionHeader{
		ConnCtrlFlag:   protocol.NBIPXConnFlagSYS,
		DataStreamType: protocol.NBIPXSessionInit,
		SourceConnID:   remoteID,
	}
	r.Inbound(sessionDatagram(init, nil))

	dg, hdr := port.lastSentStream(protocol.NBIPXSessionConfirm)
	if dg == nil {
		t.Fatal("no SESSION_CONFIRM sent after SESSION_INIT")
	}
	if hdr.DestConnID != remoteID {
		t.Fatalf("SESSION_CONFIRM DestConnID = %#x, want %#x", hdr.DestConnID, remoteID)
	}
	if hdr.SourceConnID == 0 {
		t.Fatal("SESSION_CONFIRM carried local connection ID 0")
	}
	// The reply must be addressed back to the peer.
	if dg.DstNode != testPeerNode || dg.DstSock != testPeerSock {
		t.Fatalf("SESSION_CONFIRM addressed to %x:%v, want peer %x:%v", dg.DstNode, dg.DstSock, testPeerNode, testPeerSock)
	}
	return hdr.SourceConnID
}

// dataDatagram builds a DATA frame (DATA_ONLY_LAST + EOM by default) on the circuit.
func dataDatagram(remoteID uint16, streamType uint8, eom bool, body []byte) *ipxproto.Datagram {
	var flag uint8
	if eom {
		flag = protocol.NBIPXConnFlagEOM
	}
	hdr := &protocol.NBIPXSessionHeader{
		ConnCtrlFlag:   flag,
		DataStreamType: streamType,
		SourceConnID:   remoteID,
		TotalDataLen:   uint16(len(body)),
		DataLen:        uint16(len(body)),
	}
	return sessionDatagram(hdr, body)
}

// TestNBIPX_InitEstablishesCircuit proves a SESSION_INIT is answered with
// SESSION_CONFIRM carrying a non-zero local connection ID, mirroring the remote's.
func TestNBIPX_InitEstablishesCircuit(t *testing.T) {
	_, r, port, _ := newWiredIPXEngine(t)
	establishIPXCircuit(t, r, port, 0x0042)
}

// TestNBIPX_NonPEPIgnored proves a datagram that is not IPX type 4 (PEP) produces
// no reply — the engine owns only the session family.
func TestNBIPX_NonPEPIgnored(t *testing.T) {
	_, r, port, _ := newWiredIPXEngine(t)
	init := &protocol.NBIPXSessionHeader{DataStreamType: protocol.NBIPXSessionInit, SourceConnID: 1}
	dg := sessionDatagram(init, nil)
	dg.Type = protocol.IPXTypeNetBIOS // type 20, not PEP
	r.Inbound(dg)
	if len(port.sent) != 0 {
		t.Fatalf("non-PEP datagram produced %d replies, want 0", len(port.sent))
	}
}

// TestNBIPX_DataDeliversToConsumerAndReplies proves a complete DATA message reaches
// the consumer and the echoed response travels back as a DATA_ONLY_LAST frame.
func TestNBIPX_DataDeliversToConsumerAndReplies(t *testing.T) {
	_, r, port, consumer := newWiredIPXEngine(t)
	remoteID := uint16(0x0007)
	establishIPXCircuit(t, r, port, remoteID)

	msg := []byte("SMBhello")
	r.Inbound(dataDatagram(remoteID, protocol.NBIPXDataOnlyLast, true, msg))

	if consumer.opened != 1 {
		t.Fatalf("consumer opened %d circuits, want 1", consumer.opened)
	}
	if string(consumer.last) != "SMBhello" {
		t.Fatalf("consumer saw %q, want %q", consumer.last, "SMBhello")
	}
	dg, hdr := port.lastSentStream(protocol.NBIPXDataOnlyLast)
	if dg == nil {
		t.Fatal("no DATA reply sent")
	}
	body := dg.Payload[protocol.NBIPXSessionHeaderLen : protocol.NBIPXSessionHeaderLen+int(hdr.DataLen)]
	if string(body) != "R:SMBhello" {
		t.Fatalf("DATA reply body = %q, want %q", body, "R:SMBhello")
	}
	if hdr.ConnCtrlFlag&protocol.NBIPXConnFlagEOM == 0 {
		t.Fatal("DATA reply missing EOM flag")
	}
}

// TestNBIPX_SegmentedMessageReassembled proves DATA_FIRST_MIDDLE segments
// accumulate and DATA_ONLY_LAST completes the message the consumer sees whole.
func TestNBIPX_SegmentedMessageReassembled(t *testing.T) {
	_, r, port, consumer := newWiredIPXEngine(t)
	remoteID := uint16(0x0011)
	establishIPXCircuit(t, r, port, remoteID)

	r.Inbound(dataDatagram(remoteID, protocol.NBIPXDataFirstMiddle, false, []byte("AAAA")))
	r.Inbound(dataDatagram(remoteID, protocol.NBIPXDataFirstMiddle, false, []byte("BBBB")))
	r.Inbound(dataDatagram(remoteID, protocol.NBIPXDataOnlyLast, true, []byte("CCCC")))

	if string(consumer.last) != "AAAABBBBCCCC" {
		t.Fatalf("reassembled message = %q, want %q", consumer.last, "AAAABBBBCCCC")
	}
}

// TestNBIPX_SessionEndClosesConn proves SESSION_END closes the upper-layer conn,
// drops the circuit, and acknowledges with SESSION_END_ACK.
func TestNBIPX_SessionEndClosesConn(t *testing.T) {
	_, r, port, consumer := newWiredIPXEngine(t)
	remoteID := uint16(0x00aa)
	establishIPXCircuit(t, r, port, remoteID)
	// Carry one message so a conn is opened.
	r.Inbound(dataDatagram(remoteID, protocol.NBIPXDataOnlyLast, true, []byte("x")))
	if consumer.opened != 1 {
		t.Fatalf("consumer opened %d, want 1", consumer.opened)
	}

	end := &protocol.NBIPXSessionHeader{
		ConnCtrlFlag:   protocol.NBIPXConnFlagSYS,
		DataStreamType: protocol.NBIPXSessionEnd,
		SourceConnID:   remoteID,
	}
	r.Inbound(sessionDatagram(end, nil))

	if consumer.closed != 1 {
		t.Fatalf("consumer closed %d, want 1", consumer.closed)
	}
	if dg, _ := port.lastSentStream(protocol.NBIPXSessionEndAck); dg == nil {
		t.Fatal("no SESSION_END_ACK sent")
	}
}

// TestNBIPX_StopTearsDownCircuits proves Service.Stop closes the engine's open
// circuits (releasing upper-layer handles) so nothing leaks on shutdown.
func TestNBIPX_StopTearsDownCircuits(t *testing.T) {
	svc, r, port, consumer := newWiredIPXEngine(t)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	remoteID := uint16(0x00bb)
	establishIPXCircuit(t, r, port, remoteID)
	r.Inbound(dataDatagram(remoteID, protocol.NBIPXDataOnlyLast, true, []byte("y")))
	if consumer.opened != 1 {
		t.Fatalf("consumer opened %d, want 1", consumer.opened)
	}

	if err := svc.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if consumer.closed != 1 {
		t.Fatalf("Stop closed %d circuits, want 1", consumer.closed)
	}
}
