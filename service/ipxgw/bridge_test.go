//go:build ipxgw || all

package ipxgw

import (
	"bytes"
	"encoding/hex"
	"sync"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/capture"
	"github.com/ObsoleteMadness/ClassicStack/port"
	portipx "github.com/ObsoleteMadness/ClassicStack/port/ipx"
	"github.com/ObsoleteMadness/ClassicStack/protocol/ddp"
	"github.com/ObsoleteMadness/ClassicStack/protocol/ipx"
	"github.com/ObsoleteMadness/ClassicStack/protocol/macipx"
	routeripx "github.com/ObsoleteMadness/ClassicStack/router/ipx"
	"github.com/ObsoleteMadness/ClassicStack/service/zip"
)

// recordingPort is a portipx.Port test double that records every Send.
type recordingPort struct {
	mu   sync.Mutex
	sent []*ipx.Datagram
	cb   portipx.DeliveryCallback
}

func (p *recordingPort) Start() error { return nil }
func (p *recordingPort) Stop() error  { return nil }
func (p *recordingPort) Send(d *ipx.Datagram) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sent = append(p.sent, d)
	return nil
}
func (p *recordingPort) SetDeliveryCallback(cb portipx.DeliveryCallback) {
	p.mu.Lock()
	p.cb = cb
	p.mu.Unlock()
}
func (p *recordingPort) SetCaptureSink(_ capture.Sink) {}

// fakeATRouter is the minimal slice of service.DatagramRouter the gateway
// touches. It records every Reply/Route call so the test can assert what
// went onto the AppleTalk wire.
type fakeATRouter struct {
	mu      sync.Mutex
	replies []reply
	routed  []ddp.Datagram
}

type reply struct {
	to      ddp.Datagram
	ddpType uint8
	data    []byte
}

func (r *fakeATRouter) Route(d ddp.Datagram, _ bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.routed = append(r.routed, d)
	return nil
}

func (r *fakeATRouter) Reply(d ddp.Datagram, _ port.Port, ddpType uint8, data []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.replies = append(r.replies, reply{to: d, ddpType: ddpType, data: append([]byte(nil), data...)})
}

func (r *fakeATRouter) PortsList() []port.Port { return nil }
func (r *fakeATRouter) Zones() [][]byte        { return nil }

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("hex: %v", err)
	}
	return b
}

// TestBridge_RegisterClaimsIPXNode confirms that handling a 0x20
// register-request both sends a 0x21 reply and claims the assigned IPX
// node on the IPX router so inbound IPX for that node will reach the
// gateway.
func TestBridge_RegisterClaimsIPXNode(t *testing.T) {
	nbp := zip.NewNameInformationService()
	svc := NewWithConfig(nbp, nil, Config{})

	ipxRouter := routeripx.NewRouter()
	// Identity-set is not strictly needed for the node-handler path,
	// but mirrors how the real wiring runs.
	ipxRouter.SetIdentity([4]byte{0, 0, 0, 0x02}, [6]byte{0xDE, 0xAD, 0xBE, 0xEF, 0xCA, 0xFE})
	svc.SetIPXRouter(ipxRouter)

	at := &fakeATRouter{}
	svc.router = at // bypass Start: tests do not need NBP registration

	// Client register-request (opcode 0x20).
	d := ddp.Datagram{
		SourceNetwork:     1,
		SourceNode:        1,
		SourceSocket:      Socket,
		DestinationSocket: Socket,
		DDPType:           macipx.DDPProtocol,
		Data:              mustHex(t, "20000200000001"),
	}
	svc.Inbound(d, nil)

	// One 0x23 reply must have been emitted assigning the canonical
	// IPX node for AT 1.1, i.e. 7a:00:00:00:01:01.
	at.mu.Lock()
	if len(at.replies) != 1 {
		at.mu.Unlock()
		t.Fatalf("replies = %d, want 1", len(at.replies))
	}
	got := at.replies[0]
	at.mu.Unlock()
	want := mustHex(t, "23000200000001000101")
	if !bytes.Equal(got.data, want) {
		t.Fatalf("reply data = %x, want %x", got.data, want)
	}

	// The IPX router must now accept traffic addressed to that node.
	assigned := [6]byte{0x7A, 0, 0, 0, 0x01, 0x01}
	probe := &ipx.Datagram{
		DstNet:  [4]byte{0, 0, 0, 0x02},
		DstNode: assigned,
		DstSock: [2]byte{0x40, 0x00},
	}
	// Re-route through the IPX router's Inbound — the gateway's
	// HandleNodeDatagram should pick it up and Route a DDP frame back
	// to the original client.
	ipxRouter.Inbound(probe)

	at.mu.Lock()
	defer at.mu.Unlock()
	if len(at.routed) != 1 {
		t.Fatalf("routed = %d, want 1 (inbound IPX → DDP)", len(at.routed))
	}
	out := at.routed[0]
	if out.DestinationNetwork != 1 || out.DestinationNode != 1 || out.DestinationSocket != Socket {
		t.Fatalf("out DDP dst = %d.%d:%d, want 1.1:%d",
			out.DestinationNetwork, out.DestinationNode, out.DestinationSocket, Socket)
	}
	if out.DDPType != macipx.DDPProtocol {
		t.Fatalf("out DDP type = 0x%02x, want 0x4E", out.DDPType)
	}
	if len(out.Data) == 0 || out.Data[0] != byte(macipx.OpcodeData) {
		t.Fatalf("out payload missing opcode 0x00: %x", out.Data)
	}
}

// TestBridge_EncapsulatedIPXForwarded confirms that an inbound MacIPX
// data frame is decoded and handed to the IPX router's Send path
// unchanged. The gateway must NOT rewrite the IPX source network — the
// client populates it itself (typically the operator-configured IPX
// network number such as 0x00000010), and overwriting it would break
// the conversation.
func TestBridge_EncapsulatedIPXForwarded(t *testing.T) {
	nbp := zip.NewNameInformationService()
	svc := NewWithConfig(nbp, nil, Config{})

	ipxRouter := routeripx.NewRouter()
	ipxRouter.SetIdentity([4]byte{0, 0, 0, 0x10}, [6]byte{0xDE, 0xAD, 0xBE, 0xEF, 0xCA, 0xFE})
	port := &recordingPort{}
	ipxRouter.AddPort(port)
	svc.SetIPXRouter(ipxRouter)

	at := &fakeATRouter{}
	svc.router = at

	// Opcode 0x00 wrapping an IPX SAP query from a MacIPX client at
	// AT 3.62. The client's IPX source is already populated: net
	// 0x00000010, node 7a:00:00:00:03:3e (the deterministic encoding
	// from AssignedNodeForDDP).
	d := ddp.Datagram{
		SourceNetwork:     3,
		SourceNode:        0x3E,
		SourceSocket:      Socket,
		DestinationSocket: Socket,
		DDPType:           macipx.DDPProtocol,
		Data:              mustHex(t, "00ffff0022000400000000ffffffffffff0452000000107a000000033e400300030004"),
	}
	svc.Inbound(d, nil)

	port.mu.Lock()
	defer port.mu.Unlock()
	if len(port.sent) != 1 {
		t.Fatalf("ipx Send count = %d, want 1", len(port.sent))
	}
	sent := port.sent[0]
	wantSrcNet := [4]byte{0x00, 0x00, 0x00, 0x10}
	if sent.SrcNet != wantSrcNet {
		t.Fatalf("SrcNet = %x, want %x (gateway must not rewrite)", sent.SrcNet, wantSrcNet)
	}
	wantSrcNode := [6]byte{0x7A, 0, 0, 0, 0x03, 0x3E}
	if sent.SrcNode != wantSrcNode {
		t.Fatalf("SrcNode = %x, want %x", sent.SrcNode, wantSrcNode)
	}
	wantDstSock := [2]byte{0x04, 0x52}
	if sent.DstSock != wantDstSock {
		t.Fatalf("DstSock = %x, want 0452 (SAP)", sent.DstSock)
	}
}

// TestBridge_LogOnlyWhenNoIPXRouter confirms that the gateway still works
// for discovery and address-assignment when no IPX router is attached —
// encapsulated IPX must be silently dropped rather than panicking.
func TestBridge_LogOnlyWhenNoIPXRouter(t *testing.T) {
	nbp := zip.NewNameInformationService()
	svc := NewWithConfig(nbp, nil, Config{})
	at := &fakeATRouter{}
	svc.router = at

	d := ddp.Datagram{
		SourceNetwork:     1,
		SourceNode:        1,
		SourceSocket:      Socket,
		DestinationSocket: Socket,
		DDPType:           macipx.DDPProtocol,
		Data:              mustHex(t, "00ffff0028000100000000ffffffffffff04530000000000000000010140000001ffffffffffffffff"),
	}
	svc.Inbound(d, nil) // must not panic
}

// TestBridge_DataFrameLearnsClient covers the fallback learning path:
// even when the 0x20/0x23 handshake is missed (capture started
// mid-conversation, frames reordered, etc.), the first opcode-0x00
// data frame from a client must be enough for the gateway to:
//  1. Forward the IPX onto the wire.
//  2. Learn the (IPX node → DDP addr) mapping from the IPX source
//     node, claim it on the IPX router, and use it to route an
//     inbound reply back over DDP.
func TestBridge_DataFrameLearnsClient(t *testing.T) {
	nbp := zip.NewNameInformationService()
	svc := NewWithConfig(nbp, nil, Config{})

	ipxRouter := routeripx.NewRouter()
	ipxRouter.SetIdentity([4]byte{0, 0, 0, 0x10}, [6]byte{0xDE, 0xAD, 0xBE, 0xEF, 0xCA, 0xFE})
	port := &recordingPort{}
	ipxRouter.AddPort(port)
	svc.SetIPXRouter(ipxRouter)

	at := &fakeATRouter{}
	svc.router = at

	// Outbound: client (AT 3.62) → gateway, SAP request.
	out := ddp.Datagram{
		SourceNetwork:     3,
		SourceNode:        0x3E,
		SourceSocket:      Socket,
		DestinationSocket: Socket,
		DDPType:           macipx.DDPProtocol,
		Data:              mustHex(t, "00ffff0022000400000000ffffffffffff0452000000107a000000033e400300030004"),
	}
	svc.Inbound(out, nil)

	// Inbound: server reply addressed back to 7a:00:00:00:03:3e on
	// net 0x10.
	reply := &ipx.Datagram{
		Length:  60,
		Type:    0,
		DstNet:  [4]byte{0, 0, 0, 0x10},
		DstNode: [6]byte{0x7A, 0, 0, 0, 0x03, 0x3E},
		DstSock: [2]byte{0x40, 0x03},
		SrcNet:  [4]byte{0, 0, 0x10, 0x7A},
		SrcNode: [6]byte{0, 0, 0, 0, 0, 0x01},
		SrcSock: [2]byte{0x04, 0x52},
		Payload: []byte("hello-sap-reply"),
	}
	ipxRouter.Inbound(reply)

	at.mu.Lock()
	defer at.mu.Unlock()
	if len(at.routed) != 1 {
		t.Fatalf("inbound IPX did not reach the AT side: routed=%d", len(at.routed))
	}
	d2 := at.routed[0]
	if d2.DestinationNetwork != 3 || d2.DestinationNode != 0x3E || d2.DestinationSocket != Socket {
		t.Fatalf("AT dst = %d.%d:%d, want 3.62:%d",
			d2.DestinationNetwork, d2.DestinationNode, d2.DestinationSocket, Socket)
	}
	if d2.DDPType != macipx.DDPProtocol {
		t.Fatalf("DDP type = 0x%02x, want 0x4E", d2.DDPType)
	}
	if len(d2.Data) == 0 || d2.Data[0] != byte(macipx.OpcodeData) {
		t.Fatalf("payload missing opcode 0x00: %x", d2.Data)
	}
}

// TestBridge_BroadcastFanout reproduces a Duke3D-style scenario: a
// MacIPX client registers a listen for socket 0xDEAD via opcode 0x10,
// then a DOS client on the IPX side broadcasts to 0xDEAD looking for
// game peers. The gateway must tunnel that broadcast back to the
// MacIPX client.
//
// Without the broadcast fan-out path, frames addressed to
// ff:ff:ff:ff:ff:ff are dropped by the IPX router (no node handler
// matches, no socket handler is registered for 0xDEAD) and the Mac
// never sees the DOS client's frames.
func TestBridge_BroadcastFanout(t *testing.T) {
	nbp := zip.NewNameInformationService()
	svc := NewWithConfig(nbp, nil, Config{})

	ipxRouter := routeripx.NewRouter()
	ipxRouter.SetIdentity([4]byte{0, 0, 0, 0x10}, [6]byte{0xDE, 0xAD, 0xBE, 0xEF, 0xCA, 0xFE})
	ipxRouter.AddPort(&recordingPort{})
	svc.SetIPXRouter(ipxRouter)

	at := &fakeATRouter{}
	svc.router = at

	// 1) Client (AT 3.62) registers listens for 0x0456 (NetWare
	//    diagnostic) and 0xDEAD (Duke3D) in a single 0x10 frame.
	listen := ddp.Datagram{
		SourceNetwork:     3,
		SourceNode:        0x3E,
		SourceSocket:      Socket,
		DestinationSocket: Socket,
		DDPType:           macipx.DDPProtocol,
		Data:              mustHex(t, "10ffffffffffff0456ffffffffffffdead"),
	}
	svc.Inbound(listen, nil)

	// 2) A DOS client on the IPX wire broadcasts to 0xDEAD looking
	//    for game peers.
	bcast := &ipx.Datagram{
		Length:  40,
		Type:    0,
		DstNet:  [4]byte{0, 0, 0, 0},
		DstNode: routeripx.BroadcastNode,
		DstSock: [2]byte{0xDE, 0xAD},
		SrcNet:  [4]byte{0, 0, 0, 0},
		SrcNode: [6]byte{0x00, 0x00, 0xD8, 0x96, 0x2D, 0x62},
		SrcSock: [2]byte{0xDE, 0xAD},
		Payload: []byte("duke-hello"),
	}
	ipxRouter.Inbound(bcast)

	at.mu.Lock()
	defer at.mu.Unlock()
	if len(at.routed) != 1 {
		t.Fatalf("broadcast was not tunneled to the MacIPX client: routed=%d", len(at.routed))
	}
	d2 := at.routed[0]
	if d2.DestinationNetwork != 3 || d2.DestinationNode != 0x3E {
		t.Fatalf("fanned-out frame addressed to wrong client: %d.%d", d2.DestinationNetwork, d2.DestinationNode)
	}
	if d2.DDPType != macipx.DDPProtocol || len(d2.Data) == 0 || d2.Data[0] != byte(macipx.OpcodeData) {
		t.Fatalf("fanned-out frame not a MacIPX data frame: type=0x%02x payload=%x", d2.DDPType, d2.Data)
	}
}

// TestBridge_BroadcastNotReflectedToSender confirms the gateway does
// not echo a Mac client's own broadcast back at it — the Mac broadcast
// goes out the IPX router and would otherwise loop back through the
// gateway's broadcast handler.
func TestBridge_BroadcastNotReflectedToSender(t *testing.T) {
	nbp := zip.NewNameInformationService()
	svc := NewWithConfig(nbp, nil, Config{})

	ipxRouter := routeripx.NewRouter()
	ipxRouter.SetIdentity([4]byte{0, 0, 0, 0x10}, [6]byte{0xDE, 0xAD, 0xBE, 0xEF, 0xCA, 0xFE})
	ipxRouter.AddPort(&recordingPort{})
	svc.SetIPXRouter(ipxRouter)

	at := &fakeATRouter{}
	svc.router = at

	// Client registers listen for 0xDEAD, then broadcasts on 0xDEAD.
	svc.Inbound(ddp.Datagram{
		SourceNetwork:     3,
		SourceNode:        0x3E,
		SourceSocket:      Socket,
		DestinationSocket: Socket,
		DDPType:           macipx.DDPProtocol,
		Data:              mustHex(t, "10ffffffffffffdead"),
	}, nil)

	// Simulate the broadcast arriving back via the IPX router with
	// the Mac client's own IPX node as SrcNode.
	bcast := &ipx.Datagram{
		Length:  40,
		DstNet:  [4]byte{0, 0, 0, 0},
		DstNode: routeripx.BroadcastNode,
		DstSock: [2]byte{0xDE, 0xAD},
		SrcNet:  [4]byte{0, 0, 0, 0},
		SrcNode: [6]byte{0x7A, 0, 0, 0, 0x03, 0x3E}, // ours
		SrcSock: [2]byte{0xDE, 0xAD},
		Payload: []byte("duke-self-echo"),
	}
	ipxRouter.Inbound(bcast)

	at.mu.Lock()
	defer at.mu.Unlock()
	if len(at.routed) != 0 {
		t.Fatalf("broadcast was reflected back to its originator: routed=%d", len(at.routed))
	}
}
