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
func (p *recordingIPXPort) SrcMAC() [6]byte { return [6]byte{} }

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
	for _, sock := range [][2]byte{NBIPXSessionSocket, NBIPXNameQuerySocket, NBIPXDatagramSocket, NBIPXNameSocket} {
		if err := r.RegisterSocket(sock, eng); err != nil {
			t.Fatalf("RegisterSocket(%v): %v", sock, err)
		}
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

// sessionRequestBody builds the [called-name || calling-name || trailer] payload a
// client sends in its session-request DATA frame (ERRATA captures/ipx.pcap frame 23).
func sessionRequestBody() []byte {
	called := protocol.NewName("CLASSICSTACK", protocol.NameTypeFileServer)
	calling := protocol.NewName("WIN98", protocol.NameTypeWorkstation)
	body := make([]byte, 0, 2*protocol.NameLength+6)
	body = append(body, called[:]...)
	body = append(body, calling[:]...)
	body = append(body, 0xa0, 0x05, 0x25, 0x00, 0x0d, 0x00) // observed capability trailer
	return body
}

// establishIPXCircuit drives the NB-IPX session request (a DATA frame with the
// unassigned-DestConnID sentinel) through the mini-router and returns the local
// connection ID the engine assigned in its accept. (ERRATA: there is no separate
// SESSION_INIT stream type — establishment rides DATA 0x06; see nbipx.go.)
func establishIPXCircuit(t *testing.T, r *ipxrouter.Router, port *recordingIPXPort, remoteID uint16) (localID uint16) {
	t.Helper()
	body := sessionRequestBody()
	req := &protocol.NBIPXSessionHeader{
		ConnCtrlFlag:   protocol.NBIPXConnFlagACK | protocol.NBIPXConnFlagEOM,
		DataStreamType: protocol.NBIPXSessionData,
		SourceConnID:   remoteID,
		DestConnID:     0xFFFF, // unassigned: this is a session request
		TotalDataLen:   uint16(len(body)),
		DataLen:        uint16(len(body)),
	}
	r.Inbound(sessionDatagram(req, body))

	dg, hdr := port.lastSentStream(protocol.NBIPXSessionData)
	if dg == nil {
		t.Fatal("no session-accept (DATA) sent after session request")
	}
	if hdr.DestConnID != remoteID {
		t.Fatalf("accept DestConnID = %#x, want %#x", hdr.DestConnID, remoteID)
	}
	if hdr.SourceConnID == 0 {
		t.Fatal("accept carried local connection ID 0")
	}
	// The accept is the NBIPX SESSION_CONFIRM: a Win98/WfW client only advances to
	// SMB when it carries ConnCtrlFlag SYS|CONFIRM and RecvSeq 1 (captures/ipx.pcap
	// frame 367). A bare-SYS/RecvSeq-0 accept is treated as unconfirmed and the
	// client retransmits SESSION_INITIALIZE forever (frames 331-340).
	if hdr.ConnCtrlFlag&protocol.NBIPXConnFlagCONFIRM == 0 {
		t.Fatalf("accept ConnCtrlFlag = %#x, missing CONFIRM bit (%#x)", hdr.ConnCtrlFlag, protocol.NBIPXConnFlagCONFIRM)
	}
	if hdr.ConnCtrlFlag&protocol.NBIPXConnFlagSYS == 0 {
		t.Fatalf("accept ConnCtrlFlag = %#x, missing SYS bit", hdr.ConnCtrlFlag)
	}
	if hdr.RecvSeq != protocol.NBIPXSessionAcceptRecvSeq {
		t.Fatalf("accept RecvSeq = %d, want %d", hdr.RecvSeq, protocol.NBIPXSessionAcceptRecvSeq)
	}
	// The reply must be addressed back to the peer.
	if dg.DstNode != testPeerNode || dg.DstSock != testPeerSock {
		t.Fatalf("accept addressed to %x:%v, want peer %x:%v", dg.DstNode, dg.DstSock, testPeerNode, testPeerSock)
	}
	return hdr.SourceConnID
}

// dataDatagram builds a DATA frame (stream 0x06, EOM per the flag) on an open
// circuit, carrying an SMB message body. seq is the frame's SendSeq: the session
// request consumed the client's seq 0, so a client's first data frame is seq 1 and
// every data frame (fragments included) consumes one (sequencing ERRATA on
// protocol.NBIPXSessionHeader; ipx.pcap 2026-07-10 frame 275).
func dataDatagram(remoteID, seq uint16, eom bool, body []byte) *ipxproto.Datagram {
	flag := uint8(0)
	if eom {
		flag = protocol.NBIPXConnFlagEOM
	}
	hdr := &protocol.NBIPXSessionHeader{
		ConnCtrlFlag:   flag,
		DataStreamType: protocol.NBIPXSessionData,
		SourceConnID:   remoteID,
		DestConnID:     1, // a real (assigned) DestConnID marks this a message, not a request
		SendSeq:        seq,
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

// TestNBIPX_AcceptHeaderMatchesCapture pins the SESSION_CONFIRM header the engine
// emits to the working WFW-IPX server's accept (captures/ipx.pcap frame 367). With
// the client's SourceConnID = 0x0a and the engine's first allocated local ID = 1,
// the 18-byte header must be SYS|CONFIRM (0x81), DATA (0x06), SourceConnID 1,
// DestConnID 0x0a, TotalDataLen/DataLen = the swapped-name accept length, RecvSeq 1.
// (Frame 367's own IDs were 9/0x0a; only the local-ID value differs by allocation.)
func TestNBIPX_AcceptHeaderMatchesCapture(t *testing.T) {
	_, r, port, _ := newWiredIPXEngine(t)
	localID := establishIPXCircuit(t, r, port, 0x000a)

	_, hdr := port.lastSentStream(protocol.NBIPXSessionData)
	if hdr.ConnCtrlFlag != protocol.NBIPXConnFlagSYS|protocol.NBIPXConnFlagCONFIRM {
		t.Fatalf("accept ConnCtrlFlag = %#x, want %#x (SYS|CONFIRM, cf. frame 367 = 0x81)",
			hdr.ConnCtrlFlag, protocol.NBIPXConnFlagSYS|protocol.NBIPXConnFlagCONFIRM)
	}
	if hdr.DataStreamType != protocol.NBIPXSessionData {
		t.Fatalf("accept DataStreamType = %#x, want DATA %#x", hdr.DataStreamType, protocol.NBIPXSessionData)
	}
	if hdr.SourceConnID != localID {
		t.Fatalf("accept SourceConnID = %#x, want assigned local ID %#x", hdr.SourceConnID, localID)
	}
	if hdr.DestConnID != 0x000a {
		t.Fatalf("accept DestConnID = %#x, want echoed remote ID 0x0a", hdr.DestConnID)
	}
	if hdr.RecvSeq != protocol.NBIPXSessionAcceptRecvSeq {
		t.Fatalf("accept RecvSeq = %d, want %d (frame 367)", hdr.RecvSeq, protocol.NBIPXSessionAcceptRecvSeq)
	}
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
	r.Inbound(dataDatagram(remoteID, 1, true, msg))

	if consumer.opened != 1 {
		t.Fatalf("consumer opened %d circuits, want 1", consumer.opened)
	}
	if string(consumer.last) != "SMBhello" {
		t.Fatalf("consumer saw %q, want %q", consumer.last, "SMBhello")
	}
	// The accept and the data reply both use stream 0x06; take the last one.
	dg, hdr := port.lastSentStream(protocol.NBIPXSessionData)
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

	r.Inbound(dataDatagram(remoteID, 1, false, []byte("AAAA")))
	r.Inbound(dataDatagram(remoteID, 2, false, []byte("BBBB")))
	r.Inbound(dataDatagram(remoteID, 3, true, []byte("CCCC")))

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
	r.Inbound(dataDatagram(remoteID, 1, true, []byte("x")))
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
	r.Inbound(dataDatagram(remoteID, 1, true, []byte("y")))
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

// TestNBIPX_EmitDatagramSendsNMPIMailslot proves Service.SendDatagram fans a
// connectionless NetBIOS datagram to the NBIPX engine, which emits it as an NMPI
// MailslotSend (opcode 0xFC) IPX type-20 broadcast on the datagram socket (0x0553),
// carrying the source/destination names + payload — the browser's HostAnnounce /
// election egress over IPX.
func TestNBIPX_EmitDatagramSendsNMPIMailslot(t *testing.T) {
	svc, _, port, _ := newWiredIPXEngine(t)

	src := protocol.NewName("CLASSICSTACK", protocol.NameTypeWorkstation)
	dst := protocol.NewName("WORKGROUP", protocol.NameTypeGroup)
	payload := []byte("\xffSMB-mailslot-browse-frame")
	if err := svc.SendDatagram(Datagram{Source: src, Destination: dst, Payload: payload, Broadcast: true}); err != nil {
		t.Fatalf("SendDatagram: %v", err)
	}

	if len(port.sent) != 1 {
		t.Fatalf("SendDatagram emitted %d IPX datagrams, want 1", len(port.sent))
	}
	dg := port.sent[0]
	if dg.Type != protocol.IPXTypeNetBIOS {
		t.Errorf("IPX type = %#x, want NetBIOS(0x14)", dg.Type)
	}
	if dg.DstSock != ([2]byte{0x05, 0x53}) {
		t.Errorf("dst socket = % x, want 0553 (NB-IPX datagram)", dg.DstSock)
	}
	if dg.DstNode != ([6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}) {
		t.Errorf("dst node = % x, want broadcast", dg.DstNode)
	}

	nmpi, err := protocol.DecodeNMPIPacket(dg.Payload)
	if err != nil {
		t.Fatalf("DecodeNMPIPacket: %v", err)
	}
	if nmpi.Opcode != protocol.NMPIOpMailslotSend {
		t.Errorf("NMPI opcode = %#x, want MailslotSend(0xFC)", nmpi.Opcode)
	}
	if nmpi.SourceName.String() != "CLASSICSTACK" || nmpi.RequestedName.String() != "WORKGROUP" {
		t.Errorf("NMPI names src=%q dst=%q", nmpi.SourceName.String(), nmpi.RequestedName.String())
	}
	if nmpi.NameType != protocol.NMPINameTypeWorkgroup {
		t.Errorf("NMPI name type = %#x, want workgroup (group dest)", nmpi.NameType)
	}
	if string(nmpi.Payload) != string(payload) {
		t.Errorf("NMPI payload = %q, want %q", nmpi.Payload, payload)
	}
}

// TestNBIPX_SessionRequestRetransmitKeepsCircuit proves a second SESSION_INITIALIZE
// before any data (the client's 500ms INIT retransmit) re-accepts the same local ID
// and RecvSeq 1 — it is not treated as a reconnect.
func TestNBIPX_SessionRequestRetransmitKeepsCircuit(t *testing.T) {
	_, r, port, _ := newWiredIPXEngine(t)
	remoteID := uint16(0x0001)
	first := establishIPXCircuit(t, r, port, remoteID)
	second := establishIPXCircuit(t, r, port, remoteID)
	if first != second {
		t.Fatalf("retransmit allocated local ID %#x, want existing %#x", second, first)
	}
	_, hdr := port.lastSentStream(protocol.NBIPXSessionData)
	if hdr.RecvSeq != protocol.NBIPXSessionAcceptRecvSeq || hdr.SendSeq != 0 {
		t.Fatalf("re-accept SendSeq/RecvSeq = %d/%d, want 0/1", hdr.SendSeq, hdr.RecvSeq)
	}
}

// TestNBIPX_ReconnectReplacesUsedCircuit proves a new SESSION_INITIALIZE from a
// station that already carried data (same SourceConnID, DestConnID 0xFFFF) tears
// down the old circuit and accepts a fresh one with RecvSeq 1 — the self-talk
// reconnect that previously reused sendSeq 6 and left SMB Negotiate unanswered.
func TestNBIPX_ReconnectReplacesUsedCircuit(t *testing.T) {
	_, r, port, consumer := newWiredIPXEngine(t)
	remoteID := uint16(0x0001)
	firstID := establishIPXCircuit(t, r, port, remoteID)
	r.Inbound(dataDatagram(remoteID, 1, true, []byte("first")))
	if consumer.opened != 1 {
		t.Fatalf("opened %d, want 1 after first data", consumer.opened)
	}

	secondID := establishIPXCircuit(t, r, port, remoteID)
	if secondID == firstID {
		t.Fatalf("reconnect reused local ID %#x, want a new circuit", firstID)
	}
	if consumer.closed != 1 {
		t.Fatalf("closed %d, want 1 (old circuit torn down)", consumer.closed)
	}
	_, hdr := port.lastSentStream(protocol.NBIPXSessionData)
	if hdr.RecvSeq != protocol.NBIPXSessionAcceptRecvSeq || hdr.SendSeq != 0 {
		t.Fatalf("reconnect accept SendSeq/RecvSeq = %d/%d, want 0/1", hdr.SendSeq, hdr.RecvSeq)
	}

	r.Inbound(dataDatagram(remoteID, 1, true, []byte("second")))
	if consumer.opened != 2 {
		t.Fatalf("opened %d, want 2 after reconnect data", consumer.opened)
	}
	if string(consumer.last) != "second" {
		t.Fatalf("consumer saw %q, want second (fresh circuit, not a duplicate on the old one)", consumer.last)
	}
}
