package macip

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
	"github.com/ObsoleteMadness/ClassicStack/core/router"
)

// fakeServiceRouter records Reply/Route and serves empty tables.
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

// fakeEgress records outbound IP packets and exposes the inbound callback.
type fakeEgress struct {
	mu      sync.Mutex
	out     [][]byte
	inbound func([]byte)
}

func (e *fakeEgress) SendIP(packet []byte) error {
	e.mu.Lock()
	e.out = append(e.out, append([]byte(nil), packet...))
	e.mu.Unlock()
	return nil
}
func (e *fakeEgress) SetInbound(cb func([]byte)) { e.inbound = cb }
func (e *fakeEgress) sentCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.out)
}

func testConfig() Config {
	return Config{
		GatewayIP:  IPv4{192, 168, 100, 1},
		Network:    IPv4{192, 168, 100, 0},
		Nameserver: IPv4{192, 168, 100, 1},
		Broadcast:  IPv4{192, 168, 100, 255},
		SubnetMask: IPv4{255, 255, 255, 0},
		HostCount:  254,
		Zone:       []byte("MyZone"),
	}
}

func TestPoolAssignReuseAndRange(t *testing.T) {
	p := newIPPool(IPv4{192, 168, 100, 0}, 254)
	ip1, ok := p.assign(IPv4{}, 10, 5)
	if !ok || ip1 != (IPv4{192, 168, 100, 1}) {
		t.Fatalf("first assign = %v ok=%v, want 192.168.100.1", ip1, ok)
	}
	// Same endpoint reuses its lease.
	ip1b, _ := p.assign(IPv4{}, 10, 5)
	if ip1b != ip1 {
		t.Errorf("reassign for same endpoint = %v, want %v", ip1b, ip1)
	}
	// A different endpoint gets the next slot.
	ip2, _ := p.assign(IPv4{}, 10, 6)
	if ip2 != (IPv4{192, 168, 100, 2}) {
		t.Errorf("second endpoint assign = %v, want .2", ip2)
	}
	// Reverse lookup works both ways.
	if n, node, ok := p.lookupByIP(ip2); !ok || n != 10 || node != 6 {
		t.Errorf("lookupByIP(%v) = %d.%d ok=%v, want 10.6", ip2, n, node, ok)
	}
}

// TestATPConfigAssign: an ATP TReq (func=assign) gets a TResp carrying an assigned IP.
func TestATPConfigAssign(t *testing.T) {
	fr := newFakeRouter()
	svc := New(fr, nil, nil, testConfig(), nil)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer svc.Stop(context.Background())

	// ATP TReq: ctrl(TReq) bitmap tid(2) + userdata(version2 pad2 function4).
	data := []byte{atpFuncTReq, 0x00, 0x12, 0x34, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, macIPFuncAssign}
	svc.Inbound(ddp.Datagram{
		SrcNetwork: 10, SrcNode: 5, SrcSocket: Socket,
		DestSocket: Socket, DDPType: ddpTypeATP, Data: data,
	}, nil)

	got := fr.waitReplies(1)
	if len(got) != 1 {
		t.Fatalf("got %d replies, want 1", len(got))
	}
	r := got[0]
	if r.ddpType != ddpTypeATP {
		t.Errorf("reply ddpType = %d, want ATP", r.ddpType)
	}
	if r.data[0] != (atpFuncTResp|atpEOM) || r.data[2] != 0x12 || r.data[3] != 0x34 {
		t.Errorf("TResp header wrong: %x", r.data[:4])
	}
	// Assigned IP at resp[12:16] should be the first pool address.
	if got := r.data[12:16]; got[0] != 192 || got[1] != 168 || got[2] != 100 || got[3] != 1 {
		t.Errorf("assigned IP = %v, want 192.168.100.1", got)
	}
}

// TestMacIPDataToEgress: a DDP-22 IP packet for an off-pool destination is sent
// to the IP egress.
func TestMacIPDataToEgress(t *testing.T) {
	fr := newFakeRouter()
	eg := &fakeEgress{}
	svc := New(fr, nil, eg, testConfig(), nil)
	_ = svc.Start(context.Background())
	defer svc.Stop(context.Background())

	// Minimal 20-byte IPv4 header: src 192.168.100.9 → dst 8.8.8.8.
	pkt := make([]byte, 20)
	pkt[0] = 0x45
	copy(pkt[12:16], []byte{192, 168, 100, 9})
	copy(pkt[16:20], []byte{8, 8, 8, 8})
	svc.Inbound(ddp.Datagram{
		SrcNetwork: 10, SrcNode: 9, SrcSocket: Socket,
		DestSocket: Socket, DDPType: ddpTypeMacIP, Data: pkt,
	}, nil)

	for range 2000 {
		if eg.sentCount() >= 1 {
			break
		}
		runtime.Gosched()
		time.Sleep(time.Millisecond)
	}
	if eg.sentCount() != 1 {
		t.Fatalf("egress got %d packets, want 1", eg.sentCount())
	}
}

// TestInboundIPRoutedToClient: an inbound IP packet from the egress for a leased
// client is wrapped in DDP-22 and routed to that AppleTalk node.
func TestInboundIPRoutedToClient(t *testing.T) {
	fr := newFakeRouter()
	eg := &fakeEgress{}
	svc := New(fr, nil, eg, testConfig(), nil)
	_ = svc.Start(context.Background())
	defer svc.Stop(context.Background())

	// Lease .1 to AT 10.5 by issuing an assign request.
	data := []byte{atpFuncTReq, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, macIPFuncAssign}
	svc.Inbound(ddp.Datagram{
		SrcNetwork: 10, SrcNode: 5, SrcSocket: Socket,
		DestSocket: Socket, DDPType: ddpTypeATP, Data: data,
	}, nil)
	_ = fr.waitReplies(1)

	// Inbound IP from egress destined for 192.168.100.1 (AT 10.5).
	pkt := make([]byte, 20)
	pkt[0] = 0x45
	copy(pkt[12:16], []byte{8, 8, 8, 8})
	copy(pkt[16:20], []byte{192, 168, 100, 1})
	if eg.inbound == nil {
		t.Fatal("egress inbound callback not installed")
	}
	eg.inbound(pkt)

	routes := fr.waitRoutes(1)
	if len(routes) != 1 {
		t.Fatalf("got %d routes, want 1", len(routes))
	}
	out := routes[0]
	if out.DestNetwork != 10 || out.DestNode != 5 || out.DDPType != ddpTypeMacIP {
		t.Errorf("routed DDP = %d.%d type=%d, want 10.5 type=22", out.DestNetwork, out.DestNode, out.DDPType)
	}
}
