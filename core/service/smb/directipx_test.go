package smb

import (
	"testing"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
	portipx "github.com/ObsoleteMadness/ClassicStack/core/port/ipx"
	ipxproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx"
	protocol "github.com/ObsoleteMadness/ClassicStack/core/protocol/smb"
	ipxrouter "github.com/ObsoleteMadness/ClassicStack/core/router/ipx"
)

// compile-time assertion: the exported DirectIPX satisfies the core/router/ipx
// mini-router's SocketHandler, so compose registers it directly on socket 0x0550
// with no shim. go list -deps ./core/router/ipx carries no service/smb, so this is
// acyclic.
var _ ipxrouter.SocketHandler = (*DirectIPX)(nil)

// recordingIPXPort is an ipxrouter.Port that records every datagram the mini-router
// sends, so a test can assert the SMB responses the transport produced.
type recordingIPXPort struct {
	sent []*ipxproto.Datagram
	cb   portipx.DeliveryCallback
}

func (p *recordingIPXPort) SetDeliveryCallback(cb portipx.DeliveryCallback) { p.cb = cb }
func (p *recordingIPXPort) SrcMAC() [6]byte {
	return testRouterNode
}
func (p *recordingIPXPort) Send(_ [6]byte, d *ipxproto.Datagram) error {
	p.sent = append(p.sent, d)
	return nil
}

var (
	testRouterNode = [6]byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55}
	testClientNode = [6]byte{0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f}
	testClientSock = [2]byte{0x40, 0x00}
)

// newWiredDirectIPX builds an SMB service with one PUBLIC share, a fresh IPX
// mini-router with a recording port, and the direct-IPX transport registered as
// the router's SocketHandler on socket 0x0550.
func newWiredDirectIPX(t *testing.T) (*Service, *ipxrouter.Router, *recordingIPXPort) {
	t.Helper()
	svc := &Service{shares: []*Share{newTestShare(t)}}
	r := ipxrouter.NewRouter(nil)
	r.SetIdentity(ipxrouter.DefaultNetwork, testRouterNode)
	port := &recordingIPXPort{}
	r.AddPort(port)

	tr := svc.NewDirectIPX(r)
	if err := r.RegisterSocket(DirectSMBSocket, tr); err != nil {
		t.Fatalf("RegisterSocket: %v", err)
	}
	return svc, r, port
}

// directIPXDatagram wraps an SMB message in a PEP (type 4) datagram addressed to
// the router on the direct-SMB socket, from the test client endpoint.
func directIPXDatagram(smb []byte) *ipxproto.Datagram {
	return &ipxproto.Datagram{
		Type:    ipxPEPType,
		DstNet:  ipxrouter.DefaultNetwork,
		DstNode: testRouterNode,
		DstSock: DirectSMBSocket,
		SrcNet:  ipxrouter.DefaultNetwork,
		SrcNode: testClientNode,
		SrcSock: testClientSock,
		Payload: smb,
	}
}

// negotiateMsg builds a NEGOTIATE request offering NT LM 0.12.
func negotiateMsg() []byte {
	dialects := append([]byte{0x02}, []byte(protocol.DialectNTLM)...)
	dialects = append(dialects, 0)
	return smbReq(protocol.CommandNegotiate, 0, 0, 0, nil, dialects)
}

// lastSent returns the most recent datagram the port sent, or nil.
func (p *recordingIPXPort) lastSent() *ipxproto.Datagram {
	if len(p.sent) == 0 {
		return nil
	}
	return p.sent[len(p.sent)-1]
}

// TestDirectIPX_NegotiateAllocatesCID proves a NEGOTIATE over direct-IPX is
// answered (reply flag set) and the response carries a non-zero server-assigned
// CID in the SMB header SecurityFeatures field, addressed back to the client.
func TestDirectIPX_NegotiateAllocatesCID(t *testing.T) {
	_, r, port := newWiredDirectIPX(t)
	r.Inbound(directIPXDatagram(negotiateMsg()))

	dg := port.lastSent()
	if dg == nil {
		t.Fatal("no response sent for NEGOTIATE")
	}
	if dg.DstNode != testClientNode || dg.DstSock != testClientSock {
		t.Fatalf("response addressed to %x:%v, want client %x:%v", dg.DstNode, dg.DstSock, testClientNode, testClientSock)
	}
	resp := dg.Payload
	h := respHeader(t, resp)
	if h.Command != protocol.CommandNegotiate {
		t.Fatalf("response command = %#x, want NEGOTIATE", h.Command)
	}
	cid := bp.LE16(resp[smbCIDOffset : smbCIDOffset+2])
	if cid == 0 || cid == cidReservedHi {
		t.Fatalf("response CID = %#x, want a non-reserved server-assigned id", cid)
	}
}

// TestDirectIPX_CircuitSharedAcrossMessages proves a second message from the same
// endpoint reuses the same circuit (the smbSession persists), so a TREE_CONNECT
// after SESSION_SETUP rides the same UID — i.e. the transport keeps one Conn per
// endpoint across datagrams.
func TestDirectIPX_CircuitSharedAcrossMessages(t *testing.T) {
	svc, r, _ := newWiredDirectIPX(t)
	r.Inbound(directIPXDatagram(negotiateMsg()))

	// SESSION_SETUP_ANDX from the same endpoint rides the same circuit (the guest
	// path does not parse the word block; WCT=13 is enough to look real).
	setup := smbReq(protocol.CommandSessionSetupAndX, protocol.Flags2NTStatus, 0, 0, make([]byte, 26), nil)
	r.Inbound(directIPXDatagram(setup))

	// One endpoint → exactly one circuit retained.
	tr := svc.closers[0].(*DirectIPX)
	tr.mu.Lock()
	n := len(tr.conns)
	tr.mu.Unlock()
	if n != 1 {
		t.Fatalf("transport holds %d circuits for one endpoint, want 1", n)
	}
}

// TestDirectIPX_EchoMultiResponse proves an SMB_COM_ECHO requesting N responses
// produces N datagrams, each carrying an incrementing SequenceNumber — the
// connectionless-ECHO behaviour the legacy direct-IPX transport implemented.
func TestDirectIPX_EchoMultiResponse(t *testing.T) {
	_, r, port := newWiredDirectIPX(t)

	// ECHO request: WCT=1, EchoCount=3, then a small data payload.
	echoCount := uint16(3)
	words := make([]byte, 2)
	bp.PutLE16(words, echoCount)
	echo := smbReq(protocol.CommandEcho, 0, 0, 0, words, []byte("ping"))
	r.Inbound(directIPXDatagram(echo))

	if len(port.sent) != int(echoCount) {
		t.Fatalf("ECHO count %d produced %d responses, want %d", echoCount, len(port.sent), echoCount)
	}
	for i, dg := range port.sent {
		seq := bp.LE16(dg.Payload[smbWordCountStart+1 : smbWordCountStart+3])
		if seq != uint16(i+1) {
			t.Fatalf("response %d SequenceNumber = %d, want %d", i, seq, i+1)
		}
	}
}

// TestDirectIPX_ResponseIngressDropped proves an SMB *response* (reply bit set)
// arriving on ingress is dropped — only requests are dispatched.
func TestDirectIPX_ResponseIngressDropped(t *testing.T) {
	_, r, port := newWiredDirectIPX(t)
	msg := negotiateMsg()
	msg[smbFlagsOffset] |= smbReplyFlag // mark as a response
	r.Inbound(directIPXDatagram(msg))
	if len(port.sent) != 0 {
		t.Fatalf("a response on ingress produced %d datagrams, want 0", len(port.sent))
	}
}

// TestDirectIPX_NonSMBDropped proves a PEP datagram that is not an SMB message is
// dropped without a reply.
func TestDirectIPX_NonSMBDropped(t *testing.T) {
	_, r, port := newWiredDirectIPX(t)
	r.Inbound(directIPXDatagram([]byte("not-an-smb-frame-but-long-enough-to-pass-length-check-aaaaaaaa")))
	if len(port.sent) != 0 {
		t.Fatalf("a non-SMB datagram produced %d datagrams, want 0", len(port.sent))
	}
}

// TestDirectIPX_StopClosesCircuits proves Service.Stop tears down the transport's
// circuits so no file handles leak on shutdown.
func TestDirectIPX_StopClosesCircuits(t *testing.T) {
	svc, r, _ := newWiredDirectIPX(t)
	if err := svc.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	r.Inbound(directIPXDatagram(negotiateMsg()))

	tr := svc.closers[0].(*DirectIPX)
	tr.mu.Lock()
	before := len(tr.conns)
	tr.mu.Unlock()
	if before == 0 {
		t.Fatal("expected a circuit after NEGOTIATE")
	}

	if err := svc.Stop(t.Context()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	tr.mu.Lock()
	after := len(tr.conns)
	tr.mu.Unlock()
	if after != 0 {
		t.Fatalf("Stop left %d circuits, want 0", after)
	}
}
