package ipx

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/capture"
	"github.com/ObsoleteMadness/ClassicStack/port/ipx"
	protocol "github.com/ObsoleteMadness/ClassicStack/protocol/ipx"
)

// fakeHandler captures the last datagram delivered to it.
type fakeHandler struct {
	mu   sync.Mutex
	last *protocol.Datagram
	hits atomic.Int32
}

func (f *fakeHandler) HandleDatagram(d *protocol.Datagram) {
	f.mu.Lock()
	f.last = d
	f.mu.Unlock()
	f.hits.Add(1)
}

// fakePort captures Send calls and exposes a SetDeliveryCallback hook
// the test can drive directly.
type fakePort struct {
	mu   sync.Mutex
	sent []*protocol.Datagram
	cb   ipx.DeliveryCallback
}

func (p *fakePort) Start() error { return nil }
func (p *fakePort) Stop() error  { return nil }
func (p *fakePort) Send(d *protocol.Datagram) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sent = append(p.sent, d)
	return nil
}
func (p *fakePort) SetDeliveryCallback(cb ipx.DeliveryCallback) {
	p.mu.Lock()
	p.cb = cb
	p.mu.Unlock()
}
func (p *fakePort) SetCaptureSink(_ capture.Sink) {}

func ours() ([4]byte, [6]byte) {
	return [4]byte{0xCA, 0xFE, 0xF0, 0x0D}, [6]byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x42}
}

func TestRouterAcceptsAddressedToUs(t *testing.T) {
	r := NewRouter()
	net, node := ours()
	r.SetIdentity(net, node)

	h := &fakeHandler{}
	if err := r.RegisterSocket([2]byte{0x04, 0x53}, h); err != nil {
		t.Fatalf("RegisterSocket: %v", err)
	}

	d := &protocol.Datagram{
		DstNet:  net,
		DstNode: node,
		DstSock: [2]byte{0x04, 0x53},
	}
	r.Inbound(d)
	if h.hits.Load() != 1 {
		t.Fatalf("expected 1 dispatch, got %d", h.hits.Load())
	}
}

func TestRouterAcceptsBroadcastNode(t *testing.T) {
	r := NewRouter()
	net, node := ours()
	r.SetIdentity(net, node)

	h := &fakeHandler{}
	_ = r.RegisterSocket([2]byte{0x04, 0x52}, h) // SAP

	d := &protocol.Datagram{
		DstNet:  net,
		DstNode: BroadcastNode,
		DstSock: [2]byte{0x04, 0x52},
	}
	r.Inbound(d)
	if h.hits.Load() != 1 {
		t.Fatalf("broadcast not delivered")
	}
}

func TestRouterAcceptsZeroNetwork(t *testing.T) {
	// Network=0 ("local segment, unknown") is accepted because some
	// clients send name-claim broadcasts that way before learning the
	// network number.
	r := NewRouter()
	net, node := ours()
	r.SetIdentity(net, node)

	h := &fakeHandler{}
	_ = r.RegisterSocket([2]byte{0x04, 0x55}, h)

	d := &protocol.Datagram{
		DstNet:  [4]byte{}, // zero
		DstNode: BroadcastNode,
		DstSock: [2]byte{0x04, 0x55},
	}
	r.Inbound(d)
	if h.hits.Load() != 1 {
		t.Fatalf("zero-network broadcast not delivered")
	}
}

func TestRouterRejectsForeignNetwork(t *testing.T) {
	r := NewRouter()
	net, node := ours()
	r.SetIdentity(net, node)

	h := &fakeHandler{}
	_ = r.RegisterSocket([2]byte{0x04, 0x53}, h)

	d := &protocol.Datagram{
		DstNet:  [4]byte{0xAA, 0xBB, 0xCC, 0xDD}, // not ours
		DstNode: node,
		DstSock: [2]byte{0x04, 0x53},
	}
	r.Inbound(d)
	if h.hits.Load() != 0 {
		t.Fatalf("foreign network was accepted")
	}
}

func TestRouterRejectsForeignNode(t *testing.T) {
	r := NewRouter()
	net, node := ours()
	r.SetIdentity(net, node)

	h := &fakeHandler{}
	_ = r.RegisterSocket([2]byte{0x04, 0x53}, h)

	d := &protocol.Datagram{
		DstNet:  net,
		DstNode: [6]byte{0xDE, 0xAD, 0xBE, 0xEF, 0xCA, 0xFE}, // not us, not broadcast
		DstSock: [2]byte{0x04, 0x53},
	}
	r.Inbound(d)
	if h.hits.Load() != 0 {
		t.Fatalf("foreign-node packet was accepted")
	}
}

func TestRouterRejectsUnregisteredSocket(t *testing.T) {
	r := NewRouter()
	net, node := ours()
	r.SetIdentity(net, node)

	h := &fakeHandler{}
	_ = r.RegisterSocket([2]byte{0x04, 0x53}, h)

	d := &protocol.Datagram{
		DstNet:  net,
		DstNode: node,
		DstSock: [2]byte{0x04, 0x52}, // not the one we registered
	}
	r.Inbound(d)
	if h.hits.Load() != 0 {
		t.Fatalf("unregistered-socket dispatch happened")
	}
}

func TestSendFillsZeroSourceFields(t *testing.T) {
	r := NewRouter()
	net, node := ours()
	r.SetIdentity(net, node)

	port := &fakePort{}
	r.AddPort(port)

	d := &protocol.Datagram{
		DstNet:  [4]byte{0x00, 0x00, 0x00, 0x01},
		DstNode: BroadcastNode,
		DstSock: [2]byte{0x04, 0x52},
	}
	if err := r.Send(d); err != nil {
		t.Fatalf("Send: %v", err)
	}
	port.mu.Lock()
	defer port.mu.Unlock()
	if len(port.sent) != 1 {
		t.Fatalf("send count: got %d want 1", len(port.sent))
	}
	got := port.sent[0]
	if got.SrcNet != net {
		t.Fatalf("SrcNet: got %x want %x", got.SrcNet, net)
	}
	if got.SrcNode != node {
		t.Fatalf("SrcNode: got %x want %x", got.SrcNode, node)
	}
}

func TestSendPreservesPreSetSourceFields(t *testing.T) {
	r := NewRouter()
	net, node := ours()
	r.SetIdentity(net, node)

	port := &fakePort{}
	r.AddPort(port)

	pre := &protocol.Datagram{
		SrcNet:  [4]byte{0x11, 0x22, 0x33, 0x44}, // not zero
		SrcNode: [6]byte{0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA},
		DstNode: BroadcastNode,
	}
	if err := r.Send(pre); err != nil {
		t.Fatalf("Send: %v", err)
	}
	got := port.sent[0]
	if got.SrcNet == net || got.SrcNode == node {
		t.Fatalf("Send overwrote pre-set source fields")
	}
}

func TestSendWithNoPort(t *testing.T) {
	r := NewRouter()
	if err := r.Send(&protocol.Datagram{}); err == nil {
		t.Fatal("expected error sending with no attached port")
	}
}

func TestRegisterSocketRejectsDuplicates(t *testing.T) {
	r := NewRouter()
	h := &fakeHandler{}
	if err := r.RegisterSocket([2]byte{0x04, 0x53}, h); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	if err := r.RegisterSocket([2]byte{0x04, 0x53}, h); err == nil {
		t.Fatal("duplicate Register accepted")
	}
}

// fakeNodeHandler is the NodeHandler counterpart of fakeHandler.
type fakeNodeHandler struct {
	mu   sync.Mutex
	last *protocol.Datagram
	hits atomic.Int32
}

func (f *fakeNodeHandler) HandleNodeDatagram(d *protocol.Datagram) {
	f.mu.Lock()
	f.last = d
	f.mu.Unlock()
	f.hits.Add(1)
}

func TestRegisterNodeDispatch(t *testing.T) {
	// A node-scoped handler receives traffic addressed to a node that
	// is *not* the router's own — the MacIPX gateway claims a pool of
	// assigned client nodes this way.
	r := NewRouter()
	net, node := ours()
	r.SetIdentity(net, node)

	claimed := [6]byte{0x00, 0x00, 0x00, 0x00, 0x01, 0x01} // MacIPX-style
	nh := &fakeNodeHandler{}
	if err := r.RegisterNode(claimed, nh); err != nil {
		t.Fatalf("RegisterNode: %v", err)
	}

	d := &protocol.Datagram{
		DstNet:  net,
		DstNode: claimed,
		DstSock: [2]byte{0x40, 0x00},
	}
	r.Inbound(d)
	if nh.hits.Load() != 1 {
		t.Fatalf("node handler not invoked: %d", nh.hits.Load())
	}
}

func TestRegisterNodeTakesPrecedenceOverSocket(t *testing.T) {
	r := NewRouter()
	net, node := ours()
	r.SetIdentity(net, node)

	sh := &fakeHandler{}
	_ = r.RegisterSocket([2]byte{0x04, 0x53}, sh)

	claimed := [6]byte{0x00, 0x00, 0x00, 0x00, 0x01, 0x01}
	nh := &fakeNodeHandler{}
	_ = r.RegisterNode(claimed, nh)

	d := &protocol.Datagram{
		DstNet:  net,
		DstNode: claimed,
		DstSock: [2]byte{0x04, 0x53}, // matches the socket handler too
	}
	r.Inbound(d)
	if nh.hits.Load() != 1 || sh.hits.Load() != 0 {
		t.Fatalf("dispatch precedence wrong: node=%d socket=%d",
			nh.hits.Load(), sh.hits.Load())
	}
}

func TestBroadcastHandlerRuns(t *testing.T) {
	r := NewRouter()
	net, node := ours()
	r.SetIdentity(net, node)

	bh := &fakeNodeHandler{}
	if err := r.RegisterBroadcast(bh); err != nil {
		t.Fatalf("RegisterBroadcast: %v", err)
	}

	d := &protocol.Datagram{
		DstNet:  net,
		DstNode: BroadcastNode,
		DstSock: [2]byte{0xDE, 0xAD},
	}
	r.Inbound(d)
	if bh.hits.Load() != 1 {
		t.Fatalf("broadcast handler not invoked: %d", bh.hits.Load())
	}
}

func TestBroadcastDoesNotDisplaceSocketHandler(t *testing.T) {
	// SAP responds to broadcast queries; the gateway is also a
	// broadcast listener. Both must run for the same frame.
	r := NewRouter()
	net, node := ours()
	r.SetIdentity(net, node)

	sh := &fakeHandler{}
	_ = r.RegisterSocket([2]byte{0x04, 0x52}, sh) // SAP

	bh := &fakeNodeHandler{}
	_ = r.RegisterBroadcast(bh)

	d := &protocol.Datagram{
		DstNet:  net,
		DstNode: BroadcastNode,
		DstSock: [2]byte{0x04, 0x52},
	}
	r.Inbound(d)
	if sh.hits.Load() != 1 {
		t.Fatalf("socket handler missed: %d", sh.hits.Load())
	}
	if bh.hits.Load() != 1 {
		t.Fatalf("broadcast handler missed: %d", bh.hits.Load())
	}
}

func TestUnregisterBroadcastIsIdempotent(t *testing.T) {
	r := NewRouter()
	bh := &fakeNodeHandler{}
	_ = r.RegisterBroadcast(bh)
	r.UnregisterBroadcast()
	r.UnregisterBroadcast() // must not panic

	net, node := ours()
	r.SetIdentity(net, node)
	d := &protocol.Datagram{
		DstNet:  net,
		DstNode: BroadcastNode,
		DstSock: [2]byte{0xDE, 0xAD},
	}
	r.Inbound(d)
	if bh.hits.Load() != 0 {
		t.Fatalf("broadcast handler ran after UnregisterBroadcast: %d", bh.hits.Load())
	}
}

func TestUnregisterNodeIsIdempotent(t *testing.T) {
	r := NewRouter()
	claimed := [6]byte{0x00, 0x00, 0x00, 0x00, 0x01, 0x01}
	nh := &fakeNodeHandler{}
	_ = r.RegisterNode(claimed, nh)
	r.UnregisterNode(claimed)
	r.UnregisterNode(claimed) // second call must not panic

	// After unregister the router must no longer accept traffic for
	// that node.
	net, node := ours()
	r.SetIdentity(net, node)
	d := &protocol.Datagram{
		DstNet:  net,
		DstNode: claimed,
		DstSock: [2]byte{0x40, 0x00},
	}
	r.Inbound(d)
	if nh.hits.Load() != 0 {
		t.Fatalf("handler invoked after UnregisterNode")
	}
}

func TestNewRouterDefaults(t *testing.T) {
	r := NewRouter()
	if r.Network() != DefaultNetwork {
		t.Fatalf("default network: got %x want %x", r.Network(), DefaultNetwork)
	}
	var zeroNode [6]byte
	if r.Node() != zeroNode {
		t.Fatalf("default node: got %x want zero", r.Node())
	}
}
