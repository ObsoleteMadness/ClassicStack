package macip

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
	"github.com/ObsoleteMadness/ClassicStack/core/router"
	"github.com/ObsoleteMadness/ClassicStack/core/service/nbp"
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

// assigningEgress is a fakeEgress that also implements AddressAssigner (the DHCP-relay
// shape): it returns a fixed config for any node so we can drive the async-assign path.
type assigningEgress struct {
	fakeEgress
	assignIP IPv4
	ns       IPv4
	router   IPv4 // DHCP-supplied gateway (option 3); zero = none
	calls    int
	mu2      sync.Mutex
}

func (e *assigningEgress) AssignerActive() bool { return true }

func (e *assigningEgress) AssignIP(_ uint16, _ uint8, _ IPv4) (AssignedConfig, bool) {
	e.mu2.Lock()
	e.calls++
	e.mu2.Unlock()
	return AssignedConfig{IP: e.assignIP, Nameserver: e.ns, Router: e.router}, true
}

// reportingEgress is a fakeEgress that also implements GatewayReporter, so the service
// adopts its reported on-subnet gateway when its own GatewayIP is unset.
type reportingEgress struct {
	fakeEgress
	gw IPv4
}

func (e *reportingEgress) GatewayIP() IPv4 { return e.gw }

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
	ip1, fresh, ok := p.assign(IPv4{}, 10, 5)
	// First assignable host is base+2 (.2): index 0 = network address, index 1 = the
	// gateway (base+1), both reserved. Handing out .1 would collide with the gateway's
	// own IPGATEWAY identity.
	if !ok || !fresh || ip1 != (IPv4{192, 168, 100, 2}) {
		t.Fatalf("first assign = %v fresh=%v ok=%v, want 192.168.100.2 fresh", ip1, fresh, ok)
	}
	// Same endpoint reuses its lease (not a fresh allocation).
	ip1b, fresh, _ := p.assign(IPv4{}, 10, 5)
	if ip1b != ip1 || fresh {
		t.Errorf("reassign for same endpoint = %v fresh=%v, want %v not-fresh", ip1b, fresh, ip1)
	}
	// A different endpoint gets the next slot.
	ip2, fresh, _ := p.assign(IPv4{}, 10, 6)
	if !fresh || ip2 != (IPv4{192, 168, 100, 3}) {
		t.Errorf("second endpoint assign = %v fresh=%v, want .3 fresh", ip2, fresh)
	}
	// Reverse lookup works both ways.
	if n, node, ok := p.lookupByIP(ip2); !ok || n != 10 || node != 6 {
		t.Errorf("lookupByIP(%v) = %d.%d ok=%v, want 10.6", ip2, n, node, ok)
	}
}

// TestPoolNeverLeasesGatewayOrNetworkAddr is the regression for the observed bug where
// a Mac was leased 192.168.100.1 — the exact address the gateway advertises as its own
// IPGATEWAY identity. The pool must reserve both the network address (base) and the
// gateway (base+1); the first lease is base+2, and neither reserved address is ever
// returned no matter how many endpoints assign or which IP they request.
func TestPoolNeverLeasesGatewayOrNetworkAddr(t *testing.T) {
	base := IPv4{192, 168, 100, 0}
	gw := IPv4{192, 168, 100, 1}
	p := newIPPool(base, 254)

	// Exhaust a good chunk of the pool; the gateway/network addresses must never appear.
	for i := range 200 {
		ip, _, ok := p.assign(IPv4{}, uint16(10+i/250), uint8(1+i%250))
		if !ok {
			t.Fatalf("assign %d failed unexpectedly", i)
		}
		if ip == base || ip == gw {
			t.Fatalf("assign handed out reserved address %v", ip)
		}
	}
	// An explicit request for the gateway IP must be refused (fall through to a free slot).
	ip, _, ok := p.assign(gw, 99, 99)
	if !ok || ip == gw {
		t.Fatalf("requesting the gateway IP yielded %v ok=%v; must not lease the gateway", ip, ok)
	}
}

// TestLearnSourceStaticMac: a Mac that never leased from the pool (static IP in range)
// becomes reachable once we snoop its source IP↔AT binding — mirrors the original
// macipgw arp_set() on every inbound IP packet. The learned address is claimed in the
// pool so subsequent assign() never hands it out.
func TestLearnSourceStaticMac(t *testing.T) {
	p := newIPPool(IPv4{192, 168, 100, 0}, 254)
	staticIP := IPv4{192, 168, 100, 200} // in-range but unleased
	if _, _, ok := p.lookupByIP(staticIP); ok {
		t.Fatal("static IP resolvable before any traffic — unexpected")
	}
	if !p.learnSource(staticIP, 10, 7) {
		t.Fatal("expected to learn a new binding")
	}
	n, node, ok := p.lookupByIP(staticIP)
	if !ok || n != 10 || node != 7 {
		t.Fatalf("lookupByIP after learn = %d.%d ok=%v, want 10.7", n, node, ok)
	}
	// Re-learning the same binding is a no-op (returns false) but refreshes liveness.
	if p.learnSource(staticIP, 10, 7) {
		t.Error("re-learning an identical binding should return false")
	}
}

// TestLearnSourceMarksAddressTaken: a learned in-range IP must not be handed out by
// a later assign() to a different Mac.
func TestLearnSourceMarksAddressTaken(t *testing.T) {
	p := newIPPool(IPv4{192, 168, 100, 0}, 254)
	learned := IPv4{192, 168, 100, 2} // would otherwise be the first assignable slot
	if !p.learnSource(learned, 10, 7) {
		t.Fatal("expected to learn binding")
	}
	ip, fresh, ok := p.assign(IPv4{}, 10, 8)
	if !ok || !fresh {
		t.Fatalf("assign after learn failed: ok=%v fresh=%v", ok, fresh)
	}
	if ip == learned {
		t.Fatalf("assign handed out learned address %v to a different Mac", ip)
	}
	if ip != (IPv4{192, 168, 100, 3}) {
		t.Fatalf("assign = %v, want next free .3 (learned .2 taken)", ip)
	}
	// Explicit request for the learned IP must also be refused.
	ip, _, ok = p.assign(learned, 10, 9)
	if !ok {
		t.Fatal("assign with requested learned IP should fall through to another slot")
	}
	if ip == learned {
		t.Fatalf("requested learned IP was leased: %v", ip)
	}
}

// TestLearnSourceNeverShadowsStaticLease: a snoop must not override an authoritative
// static-pool lease at the same IP index.
func TestLearnSourceNeverShadowsStaticLease(t *testing.T) {
	p := newIPPool(IPv4{192, 168, 100, 0}, 254)
	ip, _, ok := p.assign(IPv4{}, 10, 5) // leases the first host (192.168.100.2) to 10.5
	if !ok {
		t.Fatal("assign failed")
	}
	// A stray packet claims that same IP from a different endpoint — must be ignored.
	if p.learnSource(ip, 20, 9) {
		t.Error("snoop must not override a static-pool lease")
	}
	if n, node, _ := p.lookupByIP(ip); n != 10 || node != 5 {
		t.Fatalf("static lease was overwritten: %d.%d, want 10.5", n, node)
	}
}

// TestLearnSourceRepointsEndpoint: if an endpoint's source IP changes, the binding
// re-points and the stale IP no longer resolves.
func TestLearnSourceRepointsEndpoint(t *testing.T) {
	p := newIPPool(IPv4{192, 168, 100, 0}, 254)
	oldIP := IPv4{10, 0, 0, 50}
	newIP := IPv4{10, 0, 0, 51}
	p.learnSource(oldIP, 10, 8)
	if !p.learnSource(newIP, 10, 8) {
		t.Fatal("expected re-point to be recorded")
	}
	if _, _, ok := p.lookupByIP(oldIP); ok {
		t.Error("stale IP still resolves after re-point")
	}
	if n, node, ok := p.lookupByIP(newIP); !ok || n != 10 || node != 8 {
		t.Fatalf("new IP lookup = %d.%d ok=%v, want 10.8", n, node, ok)
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
	// Assigned IP at resp[12:16] should be the first assignable pool address (.2): .0 is
	// the network address and .1 is the gateway, both reserved.
	if got := r.data[12:16]; got[0] != 192 || got[1] != 168 || got[2] != 100 || got[3] != 2 {
		t.Errorf("assigned IP = %v, want 192.168.100.2", got)
	}
	// The reply must be byte-length-compatible with macipgw: 4-byte ATP header +
	// 41-byte MacIP user-data (control 8 + 32 data fields + 1 NUL error byte).
	if len(r.data) != 4+configUserLen {
		t.Errorf("reply length = %d, want %d (macipgw success len)", len(r.data), 4+configUserLen)
	}
	// Function field at resp[8:12] is MACIP_ASSIGN (1), big-endian.
	if fn := uint32(r.data[8])<<24 | uint32(r.data[9])<<16 | uint32(r.data[10])<<8 | uint32(r.data[11]); fn != macIPFuncAssign {
		t.Errorf("reply function = %d, want MACIP_ASSIGN", fn)
	}
}

// TestATPConfigUnknownFunction: an unrecognised function code gets a MACIP_ERROR reply
// carrying "Unknown Operation." — matching macipgw's switch default arm.
func TestATPConfigUnknownFunction(t *testing.T) {
	fr := newFakeRouter()
	svc := New(fr, nil, nil, testConfig(), nil)
	_ = svc.Start(context.Background())
	defer svc.Stop(context.Background())

	// function = 99 (unknown).
	data := []byte{atpFuncTReq, 0x00, 0x12, 0x34, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 99}
	svc.Inbound(ddp.Datagram{
		SrcNetwork: 10, SrcNode: 5, SrcSocket: Socket,
		DestSocket: Socket, DDPType: ddpTypeATP, Data: data,
	}, nil)

	got := fr.waitReplies(1)
	if len(got) != 1 {
		t.Fatalf("got %d replies, want 1", len(got))
	}
	r := got[0]
	// Function field must be MACIP_ERROR (0xFFFFFFFF).
	fn := uint32(r.data[8])<<24 | uint32(r.data[9])<<16 | uint32(r.data[10])<<8 | uint32(r.data[11])
	if fn != 0xFFFFFFFF {
		t.Errorf("reply function = %#x, want 0xFFFFFFFF (MACIP_ERROR)", fn)
	}
	// Error string begins at resp offset 4 + 8 + 32 = 44.
	errStart := 4 + configCtrlLen + configFieldsLen
	if len(r.data) < errStart+len(errNoOp) {
		t.Fatalf("reply too short (%d) to carry error string", len(r.data))
	}
	if got := string(r.data[errStart : errStart+len(errNoOp)]); got != errNoOp {
		t.Errorf("error string = %q, want %q", got, errNoOp)
	}
}

// TestATPConfigPoolExhausted: when the static pool has no free address, the reply is
// MACIP_ERROR with "No Address Available." rather than a bogus 0.0.0.0 lease.
func TestATPConfigPoolExhausted(t *testing.T) {
	fr := newFakeRouter()
	cfg := testConfig()
	cfg.HostCount = 3 // index 0 = network addr, 1 = gateway (both reserved), 2 = the only assignable slot
	svc := New(fr, nil, nil, cfg, nil)
	_ = svc.Start(context.Background())
	defer svc.Stop(context.Background())

	// First endpoint takes the only slot.
	data1 := []byte{atpFuncTReq, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, macIPFuncAssign}
	svc.Inbound(ddp.Datagram{SrcNetwork: 10, SrcNode: 5, SrcSocket: Socket, DestSocket: Socket, DDPType: ddpTypeATP, Data: data1}, nil)
	first := fr.waitReplies(1)
	if len(first) != 1 {
		t.Fatalf("first assign: got %d replies", len(first))
	}

	// Second endpoint finds the pool exhausted → MACIP_ERROR / errNoIP.
	data2 := []byte{atpFuncTReq, 0x00, 0x00, 0x02, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, macIPFuncAssign}
	svc.Inbound(ddp.Datagram{SrcNetwork: 10, SrcNode: 6, SrcSocket: Socket, DestSocket: Socket, DDPType: ddpTypeATP, Data: data2}, nil)
	all := fr.waitReplies(2)
	if len(all) != 2 {
		t.Fatalf("got %d replies, want 2", len(all))
	}
	r := all[1]
	fn := uint32(r.data[8])<<24 | uint32(r.data[9])<<16 | uint32(r.data[10])<<8 | uint32(r.data[11])
	if fn != 0xFFFFFFFF {
		t.Errorf("exhausted reply function = %#x, want MACIP_ERROR", fn)
	}
	errStart := 4 + configCtrlLen + configFieldsLen
	if got := string(r.data[errStart : errStart+len(errNoIP)]); got != errNoIP {
		t.Errorf("error string = %q, want %q", got, errNoIP)
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

	// Lease the first host (.2) to AT 10.5 by issuing an assign request.
	data := []byte{atpFuncTReq, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, macIPFuncAssign}
	svc.Inbound(ddp.Datagram{
		SrcNetwork: 10, SrcNode: 5, SrcSocket: Socket,
		DestSocket: Socket, DDPType: ddpTypeATP, Data: data,
	}, nil)
	_ = fr.waitReplies(1)

	// Inbound IP from egress destined for 192.168.100.2 (AT 10.5).
	pkt := make([]byte, 20)
	pkt[0] = 0x45
	copy(pkt[12:16], []byte{8, 8, 8, 8})
	copy(pkt[16:20], []byte{192, 168, 100, 2})
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

// TestATPConfigAssignViaEgress: when the egress implements AddressAssigner (DHCP
// relay), an ATP TReq is answered with the egress-supplied address and config
// (not a static-pool address), the lease is recorded, and OwnsIP reports it.
func TestATPConfigAssignViaEgress(t *testing.T) {
	fr := newFakeRouter()
	eg := &assigningEgress{assignIP: IPv4{10, 0, 0, 77}, ns: IPv4{10, 0, 0, 1}}
	svc := New(fr, nil, eg, testConfig(), nil)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer svc.Stop(context.Background())

	data := []byte{atpFuncTReq, 0x00, 0xAB, 0xCD, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, macIPFuncAssign}
	svc.Inbound(ddp.Datagram{
		SrcNetwork: 10, SrcNode: 5, SrcSocket: Socket,
		DestSocket: Socket, DDPType: ddpTypeATP, Data: data,
	}, nil)

	got := fr.waitReplies(1)
	if len(got) != 1 {
		t.Fatalf("got %d replies, want 1", len(got))
	}
	r := got[0]
	if r.data[2] != 0xAB || r.data[3] != 0xCD {
		t.Errorf("TResp tid wrong: %x", r.data[:4])
	}
	// The egress-assigned IP and nameserver must be reflected, not the static defaults.
	if ip := r.data[12:16]; ip[0] != 10 || ip[1] != 0 || ip[2] != 0 || ip[3] != 77 {
		t.Errorf("assigned IP = %v, want 10.0.0.77 (from egress)", ip)
	}
	if ns := r.data[16:20]; ns[0] != 10 || ns[1] != 0 || ns[2] != 0 || ns[3] != 1 {
		t.Errorf("nameserver = %v, want 10.0.0.1 (from egress)", ns)
	}
	// The external lease must be recorded so OwnsIP / inbound routing find it.
	if !svc.OwnsIP(IPv4{10, 0, 0, 77}) {
		t.Error("OwnsIP(10.0.0.77) = false, want true after egress assignment")
	}
	if eg.calls != 1 {
		t.Errorf("AssignIP calls = %d, want 1", eg.calls)
	}
}

// inactiveAssignerEgress mimics the NAT/bridge egress: it structurally implements
// AddressAssigner (it HAS an AssignIP method) but is not actively sourcing addresses
// (AssignerActive == false), so AssignIP would always fail. The core must NOT delegate
// to it and must fall back to the static pool.
type inactiveAssignerEgress struct {
	fakeEgress
	calls int
}

func (e *inactiveAssignerEgress) AssignerActive() bool { return false }

func (e *inactiveAssignerEgress) AssignIP(_ uint16, _ uint8, _ IPv4) (AssignedConfig, bool) {
	e.calls++
	return AssignedConfig{}, false // no DHCP → always fails
}

// TestATPConfigNATFallsBackToStaticPool is the regression for the NAT-mode bug: the
// NAT/bridge egress carries an AssignIP method for all modes but only performs DHCP when
// relay is enabled. Before the AssignerActive gate, core delegated to it in NAT mode too,
// AssignIP returned ok=false, and the "do not reply" contract silently swallowed EVERY
// config request — the Mac's socket-72 TReq got no TResp and it never obtained an IP
// (observed on the wire: repeated requests to socket 72, zero replies). With the gate,
// core sees AssignerActive()==false, uses the static pool, and replies.
func TestATPConfigNATFallsBackToStaticPool(t *testing.T) {
	fr := newFakeRouter()
	eg := &inactiveAssignerEgress{}
	svc := New(fr, nil, eg, testConfig(), nil)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer svc.Stop(context.Background())

	data := []byte{atpFuncTReq, 0x00, 0xAB, 0xCD, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, macIPFuncAssign}
	svc.Inbound(ddp.Datagram{
		SrcNetwork: 10, SrcNode: 5, SrcSocket: Socket,
		DestSocket: Socket, DDPType: ddpTypeATP, Data: data,
	}, nil)

	got := fr.waitReplies(1)
	if len(got) != 1 {
		t.Fatalf("got %d replies, want 1 — NAT mode must fall back to the static pool and reply", len(got))
	}
	if eg.calls != 0 {
		t.Errorf("AssignIP was called %d times; core must not delegate to an inactive assigner", eg.calls)
	}
	// The reply must carry a real static-pool address (network base 192.168.100.0 + 1).
	if ip := got[0].data[12:16]; ip[0] != 192 || ip[1] != 168 || ip[2] != 100 {
		t.Errorf("assigned IP = %v, want a 192.168.100.x static-pool address", ip)
	}
}

// TestDHCPRouterAdoptedAsGateway: in DHCP-relay mode the DHCP-supplied router (option
// 3), which is on the client's real LAN subnet, is adopted as the advertised IPGATEWAY
// identity and re-registered via NBP — replacing the configured (off-subnet) GatewayIP.
// This is the fix for MacTCP being handed an off-subnet gateway and refusing to route
// off-net (ping to the internet timing out).
func TestDHCPRouterAdoptedAsGateway(t *testing.T) {
	fr := newFakeRouter()
	names := nbp.New(fr, nil)
	// Lease is on 192.168.0.0/24 (real LAN); the configured GatewayIP is 192.168.100.1
	// (off-subnet, from testConfig). The DHCP router is 192.168.0.1.
	eg := &assigningEgress{assignIP: IPv4{192, 168, 0, 106}, ns: IPv4{192, 168, 0, 1}, router: IPv4{192, 168, 0, 1}}
	svc := New(fr, names, eg, testConfig(), nil)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer svc.Stop(context.Background())

	// The startup NBP registration is under the configured (off-subnet) gateway.
	if got := gatewayNBPName(names); got != "192.168.100.1" {
		t.Fatalf("startup IPGATEWAY name = %q, want 192.168.100.1", got)
	}

	data := []byte{atpFuncTReq, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, macIPFuncAssign}
	svc.Inbound(ddp.Datagram{
		SrcNetwork: 10, SrcNode: 5, SrcSocket: Socket,
		DestSocket: Socket, DDPType: ddpTypeATP, Data: data,
	}, nil)
	if got := fr.waitReplies(1); len(got) != 1 {
		t.Fatalf("got %d replies, want 1", len(got))
	}

	// After the lease, the advertised gateway must be the DHCP router (on-subnet with
	// the client), and the stale off-subnet name must be gone.
	if got := gatewayNBPName(names); got != "192.168.0.1" {
		t.Fatalf("IPGATEWAY name after DHCP = %q, want 192.168.0.1 (adopted DHCP router)", got)
	}
}

// TestEgressGatewayAdoptedAtStart: when GatewayIP is unset (gateway_ip blank in bridge
// mode) the service adopts the egress-reported on-subnet gateway at Start, so the
// IPGATEWAY NBP name is the real gateway (192.168.0.1) rather than 0.0.0.0 — the
// regression that left MacTCP with a 0.0.0.0 gateway that refused to send.
func TestEgressGatewayAdoptedAtStart(t *testing.T) {
	fr := newFakeRouter()
	names := nbp.New(fr, nil)
	eg := &reportingEgress{gw: IPv4{192, 168, 0, 1}}
	cfg := testConfig()
	cfg.GatewayIP = IPv4{} // blank gateway_ip
	svc := New(fr, names, eg, cfg, nil)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer svc.Stop(context.Background())

	if got := gatewayNBPName(names); got != "192.168.0.1" {
		t.Fatalf("IPGATEWAY name = %q, want 192.168.0.1 (adopted from egress)", got)
	}
}

// TestConfiguredGatewayNotOverriddenByEgress: a configured GatewayIP wins over the
// egress-reported one (the operator's explicit choice is authoritative).
func TestConfiguredGatewayNotOverriddenByEgress(t *testing.T) {
	fr := newFakeRouter()
	names := nbp.New(fr, nil)
	eg := &reportingEgress{gw: IPv4{192, 168, 0, 1}}
	svc := New(fr, names, eg, testConfig(), nil) // testConfig GatewayIP = 192.168.100.1
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer svc.Stop(context.Background())

	if got := gatewayNBPName(names); got != "192.168.100.1" {
		t.Fatalf("IPGATEWAY name = %q, want configured 192.168.100.1 (egress must not override)", got)
	}
}

// gatewayNBPName returns the single IPGATEWAY object name currently registered, or ""
// (fails the caller's assertion) if there is not exactly one.
func gatewayNBPName(names *nbp.Service) string {
	var found string
	n := 0
	for _, reg := range names.Names() {
		if string(reg.Type) == "IPGATEWAY" {
			found = string(reg.Object)
			n++
		}
	}
	if n != 1 {
		return ""
	}
	return found
}
