package ipxgw

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	portipx "github.com/ObsoleteMadness/ClassicStack/core/port/ipx"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
	protoipx "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/macipx"
	ripproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/rip"
	"github.com/ObsoleteMadness/ClassicStack/core/router"
	routeripx "github.com/ObsoleteMadness/ClassicStack/core/router/ipx"
	"github.com/ObsoleteMadness/ClassicStack/core/service/rip"
)

// fakeServiceRouter records Reply/Route calls and serves empty tables.
type fakeServiceRouter struct {
	mu      sync.Mutex
	replies []replyCall
	routes  []ddp.Datagram
	zit     *router.ZoneInformationTable
	rt      *router.RoutingTable
}

type replyCall struct {
	d       ddp.Datagram
	ddpType uint8
	data    []byte
}

func newFakeRouter() *fakeServiceRouter {
	zit := router.NewZoneInformationTable()
	return &fakeServiceRouter{zit: zit, rt: router.NewRoutingTable(zit, nil)}
}

func (f *fakeServiceRouter) Reply(d ddp.Datagram, _ router.RoutedPort, ddpType uint8, data []byte) {
	f.mu.Lock()
	f.replies = append(f.replies, replyCall{d: d, ddpType: ddpType, data: append([]byte(nil), data...)})
	f.mu.Unlock()
}
func (f *fakeServiceRouter) Route(d ddp.Datagram, _ bool) error {
	f.mu.Lock()
	f.routes = append(f.routes, d)
	f.mu.Unlock()
	return nil
}
func (f *fakeServiceRouter) RoutingTable() *router.RoutingTable  { return f.rt }
func (f *fakeServiceRouter) Zones() *router.ZoneInformationTable { return f.zit }
func (f *fakeServiceRouter) Ports() []router.RoutedPort          { return nil }

func (f *fakeServiceRouter) waitReplies(n int) []replyCall {
	for range 2000 {
		f.mu.Lock()
		got := len(f.replies)
		f.mu.Unlock()
		if got >= n {
			break
		}
		runtime.Gosched()
		time.Sleep(time.Millisecond)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]replyCall(nil), f.replies...)
}

func (f *fakeServiceRouter) waitRoutes(n int) []ddp.Datagram {
	for range 2000 {
		f.mu.Lock()
		got := len(f.routes)
		f.mu.Unlock()
		if got >= n {
			break
		}
		runtime.Gosched()
		time.Sleep(time.Millisecond)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ddp.Datagram(nil), f.routes...)
}

// fakeIPXPort drives the IPX mini-router and records sent datagrams.
type fakeIPXPort struct {
	mu   sync.Mutex
	cb   portipx.DeliveryCallback
	sent []*protoipx.Datagram
}

func (p *fakeIPXPort) SetDeliveryCallback(cb portipx.DeliveryCallback) { p.cb = cb }
func (p *fakeIPXPort) SrcMAC() [6]byte {
	return [6]byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF}
}
func (p *fakeIPXPort) Send(_ [6]byte, d *protoipx.Datagram) error {
	p.mu.Lock()
	p.sent = append(p.sent, d)
	p.mu.Unlock()
	return nil
}
func (p *fakeIPXPort) waitSent(n int) []*protoipx.Datagram {
	for range 2000 {
		p.mu.Lock()
		got := len(p.sent)
		p.mu.Unlock()
		if got >= n {
			break
		}
		runtime.Gosched()
		time.Sleep(time.Millisecond)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]*protoipx.Datagram(nil), p.sent...)
}

// TestRegisterReplyAssignsNode: an opcode-0x20 register request gets a 0x23 reply
// carrying the node synthesized from the client's DDP address.
func TestRegisterReplyAssignsNode(t *testing.T) {
	fr := newFakeRouter()
	svc := New(fr, nil, nil, nil)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer svc.Stop(context.Background())

	req := [6]byte{0x00, 0x02, 0x00, 0x00, 0x00, 0x01}
	frame := append([]byte{byte(macipx.OpcodeRegisterReq)}, req[:]...)
	svc.Inbound(ddp.Datagram{
		SrcNetwork: 3, SrcNode: 62, SrcSocket: macipx.Socket,
		DestSocket: macipx.Socket, DDPType: macipx.DDPProtocol, Data: frame,
	}, nil)

	got := fr.waitReplies(1)
	if len(got) != 1 {
		t.Fatalf("got %d replies, want 1", len(got))
	}
	if got[0].ddpType != macipx.DDPProtocol {
		t.Errorf("reply ddpType = %d, want MacIPX", got[0].ddpType)
	}
	node, err := macipx.DecodeRegisterReply(got[0].data[1:])
	if err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	want := macipx.AssignedNodeForDDP(3, 62) // 7a:00:00:00:03:3e
	if node != want {
		t.Errorf("assigned node = %x, want %x", node, want)
	}
}

// TestEncapsulatedIPXForwarded: an opcode-0x00 data frame is decoded and injected
// into the attached IPX mini-router (which sends it on its port).
func TestEncapsulatedIPXForwarded(t *testing.T) {
	fr := newFakeRouter()
	ipxr := routeripx.NewRouter(nil)
	ipxr.SetIdentity([4]byte{0, 0, 0, 0x10}, [6]byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF})
	port := &fakeIPXPort{}
	ipxr.AddPort(port)

	svc := New(fr, nil, nil, nil)
	svc.SetIPXRouter(ipxr)
	_ = svc.Start(context.Background())
	defer svc.Stop(context.Background())

	// Build an IPX datagram from the client to a native peer.
	dg := &protoipx.Datagram{
		Type:    4,
		DstNet:  [4]byte{0, 0, 0, 0x10},
		DstNode: [6]byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66},
		DstSock: [2]byte{0x04, 0x51},
		SrcNet:  [4]byte{0, 0, 0, 0x10},
		SrcNode: macipx.AssignedNodeForDDP(1, 1),
		SrcSock: [2]byte{0x40, 0x00},
		Payload: []byte{0xDE, 0xAD},
	}
	ipxBytes, err := dg.Encode(nil)
	if err != nil {
		t.Fatalf("encode IPX: %v", err)
	}
	frame := macipx.EncodeData(ipxBytes)
	svc.Inbound(ddp.Datagram{
		SrcNetwork: 1, SrcNode: 1, SrcSocket: macipx.Socket,
		DestSocket: macipx.Socket, DDPType: macipx.DDPProtocol, Data: frame,
	}, nil)

	sent := port.waitSent(1)
	if len(sent) != 1 {
		t.Fatalf("IPX port got %d sends, want 1", len(sent))
	}
	if sent[0].DstSock != [2]byte{0x04, 0x51} {
		t.Errorf("forwarded dst socket = %x, want 0451", sent[0].DstSock)
	}
}

// TestInboundIPXTunneledToClient: the IPX router delivers a datagram addressed to
// an assigned node; the gateway re-encapsulates it and routes it over DDP.
func TestInboundIPXTunneledToClient(t *testing.T) {
	fr := newFakeRouter()
	ipxr := routeripx.NewRouter(nil)
	ipxr.SetIdentity([4]byte{0, 0, 0, 0x10}, [6]byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF})
	ipxr.AddPort(&fakeIPXPort{})

	svc := New(fr, nil, nil, nil)
	svc.SetIPXRouter(ipxr)
	_ = svc.Start(context.Background())
	defer svc.Stop(context.Background())

	// Learn the client via a register request so the node is claimed.
	clientNode := macipx.AssignedNodeForDDP(5, 9)
	req := [6]byte{0x00, 0x02, 0x00, 0x00, 0x00, 0x01}
	svc.Inbound(ddp.Datagram{
		SrcNetwork: 5, SrcNode: 9, SrcSocket: macipx.Socket,
		DestSocket: macipx.Socket, DDPType: macipx.DDPProtocol,
		Data: append([]byte{byte(macipx.OpcodeRegisterReq)}, req[:]...),
	}, nil)
	_ = fr.waitReplies(1)

	// Inbound IPX from a native peer addressed to the client's node.
	in := &protoipx.Datagram{
		Type:    4,
		DstNet:  [4]byte{0, 0, 0, 0x10},
		DstNode: clientNode,
		DstSock: [2]byte{0x45, 0x00},
		SrcNet:  [4]byte{0, 0, 0, 0x10},
		SrcNode: [6]byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66},
		SrcSock: [2]byte{0x04, 0x51},
		Payload: []byte{0x01, 0x02},
	}
	svc.HandleNodeDatagram(in)

	routes := fr.waitRoutes(1)
	if len(routes) != 1 {
		t.Fatalf("got %d routes, want 1", len(routes))
	}
	out := routes[0]
	if out.DestNetwork != 5 || out.DestNode != 9 || out.DestSocket != macipx.Socket {
		t.Errorf("tunneled DDP dst = %d.%d:%d, want 5.9:%d", out.DestNetwork, out.DestNode, out.DestSocket, macipx.Socket)
	}
	op, _, err := macipx.DecodeFrame(out.Data)
	if err != nil || op != macipx.OpcodeData {
		t.Errorf("tunneled frame opcode = 0x%02x (err %v), want OpcodeData", byte(op), err)
	}
}

// TestRIPReplyConveysConfiguredNetwork reproduces the LToUDP-netboot scenario
// (captures/ltoudp-netboot.pcap): a MacIPX gateway with a configured ipx_network
// but NO native IPX port. Right after the register handshake the Mac broadcasts a
// RIP Request ("what network am I on?"). With a RIP responder owning the gateway's
// network wired onto the same mini-router, the gateway tunnels the request into the
// router's local dispatch, the responder answers, and the answer is tunnelled back
// to the Mac over DDP advertising the configured network — so ipx_network actually
// reaches the client. Regression guard: before the fix nothing answered the RIP
// request and the Mac stayed on IPX net 0.
func TestRIPReplyConveysConfiguredNetwork(t *testing.T) {
	const configuredNet uint32 = 0x03
	netBytes := [4]byte{0x00, 0x00, 0x00, 0x03}

	fr := newFakeRouter()
	// Mini-router with NO port (log-only / LToUDP-only deployment). Compose sets the
	// router's wire network from the gateway's ipx_network, so RIP replies carry it
	// as their IPX SrcNet (not the default 0) — mirror that here.
	ipxr := routeripx.NewRouter(nil)
	ipxr.SetIdentity(netBytes, [6]byte{})

	// RIP responder owning the gateway's network, on socket 0x0453 — exactly how
	// compose/runtime wires it when the gateway is present.
	responder := rip.New(ipxr)
	responder.SetNetworks(netBytes)
	if err := ipxr.RegisterSocket(ripproto.Socket, responder); err != nil {
		t.Fatalf("register RIP socket: %v", err)
	}

	svc := NewWithConfig(fr, nil, nil, Config{IPXNetwork: configuredNet}, nil)
	svc.SetIPXRouter(ipxr) // claims client nodes; registers broadcast handler
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer svc.Stop(context.Background())

	// 1) Register handshake so the gateway claims the Mac's assigned node.
	clientAT := struct {
		net  uint16
		node uint8
	}{net: 1, node: 1}
	req := [6]byte{0x00, 0x02, 0x00, 0x00, 0x00, 0x01}
	svc.Inbound(ddp.Datagram{
		SrcNetwork: clientAT.net, SrcNode: clientAT.node, SrcSocket: macipx.Socket,
		DestSocket: macipx.Socket, DDPType: macipx.DDPProtocol,
		Data: append([]byte{byte(macipx.OpcodeRegisterReq)}, req[:]...),
	}, nil)
	_ = fr.waitReplies(1) // the 0x23 register reply (node only, no network — per spec)

	// 2) The Mac broadcasts a RIP Request to net 0 (as in the capture): src node is
	//    its assigned node, dst is broadcast on socket 0x0453, asking about net 0.
	clientNode := macipx.AssignedNodeForDDP(clientAT.net, clientAT.node)
	// The real Mac (capture frame 31) asks with the wildcard network
	// (0xFFFFFFFF) — "tell me every route you know" — not net 0.
	ripReq := (&ripproto.Packet{
		Operation: ripproto.OpRequest,
		Entries:   []ripproto.Entry{{Network: ripproto.NetworkWildcard, Hops: 0xFFFF, Ticks: 0xFFFF}},
	}).Marshal(nil)
	ripDG := &protoipx.Datagram{
		Type:    ripproto.IPXType,
		DstNet:  [4]byte{},
		DstNode: routeripx.BroadcastNode,
		DstSock: ripproto.Socket,
		SrcNet:  [4]byte{},
		SrcNode: clientNode,
		SrcSock: [2]byte{0x40, 0x00},
		Payload: ripReq,
	}
	ripBytes, err := ripDG.Encode(nil)
	if err != nil {
		t.Fatalf("encode RIP request: %v", err)
	}
	svc.Inbound(ddp.Datagram{
		SrcNetwork: clientAT.net, SrcNode: clientAT.node, SrcSocket: macipx.Socket,
		DestSocket: macipx.Socket, DDPType: macipx.DDPProtocol,
		Data: macipx.EncodeData(ripBytes),
	}, nil)

	// 3) Expect a DDP route back to the Mac carrying an encapsulated RIP Response
	//    that advertises the configured network. (waitRoutes(1): the tunnelled reply.)
	routes := fr.waitRoutes(1)
	if len(routes) == 0 {
		t.Fatal("no RIP reply tunnelled back to the Mac (client would stay on IPX net 0)")
	}
	out := routes[len(routes)-1]
	if out.DestNetwork != uint16(clientAT.net) || out.DestNode != clientAT.node {
		t.Errorf("RIP reply DDP dst = %d.%d, want %d.%d", out.DestNetwork, out.DestNode, clientAT.net, clientAT.node)
	}
	op, payload, err := macipx.DecodeFrame(out.Data)
	if err != nil || op != macipx.OpcodeData {
		t.Fatalf("reply frame opcode = 0x%02x (err %v), want OpcodeData", byte(op), err)
	}
	replyDG, err := protoipx.Decode(payload)
	if err != nil {
		t.Fatalf("decode tunnelled IPX: %v", err)
	}
	if replyDG.SrcSock != ripproto.Socket {
		t.Errorf("reply src socket = %x, want RIP 0453", replyDG.SrcSock)
	}
	// The IPX header SrcNet must carry the configured network, not 0 — a MacIPX
	// client that reads it as its network would otherwise stay on net 0 ("network
	// appears as 0x0"). Guards the router-identity wiring.
	if replyDG.SrcNet != netBytes {
		t.Errorf("reply IPX SrcNet = %v, want %v", replyDG.SrcNet, netBytes)
	}
	pkt, err := ripproto.Unmarshal(replyDG.Payload)
	if err != nil {
		t.Fatalf("unmarshal RIP reply: %v", err)
	}
	if pkt.Operation != ripproto.OpResponse {
		t.Errorf("RIP op = %#04x, want Response", pkt.Operation)
	}
	found := false
	for _, e := range pkt.Entries {
		if e.Network == netBytes {
			found = true
		}
	}
	if !found {
		t.Errorf("RIP reply did not advertise configured network %v; entries=%+v", netBytes, pkt.Entries)
	}
}
