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

// TestPoolAllocatesOldestFreedSlot: once every slot has been used, a new assignment reuses
// the slot freed LONGEST ago (draft §3.8.2 "oldest unused entry"), not the lowest index.
// (A never-used slot carries the zero freedAt, which is older still, so this rule is
// observable once the pool has no never-used slack — the realistic reuse case.)
func TestPoolAllocatesOldestFreedSlot(t *testing.T) {
	q := newIPPool(IPv4{10, 0, 0, 0}, 5) // slots: 0 net, 1 gw, 2/3/4 assignable
	x2, _, _ := q.assign(IPv4{}, 10, 2)  // .2
	x3, _, _ := q.assign(IPv4{}, 10, 3)  // .3
	x4, _, _ := q.assign(IPv4{}, 10, 4)  // .4
	if x2 != (IPv4{10, 0, 0, 2}) || x3 != (IPv4{10, 0, 0, 3}) || x4 != (IPv4{10, 0, 0, 4}) {
		t.Fatalf("initial leases = %v %v %v", x2, x3, x4)
	}
	// Free .4 first (oldest freedAt), then .3.
	q.release(x4, 10, 4)
	time.Sleep(2 * time.Millisecond)
	q.release(x3, 10, 3)

	// A new endpoint must reuse .4 (freed longest ago), not the lower-indexed .3.
	got, fresh, ok := q.assign(IPv4{}, 20, 20)
	if !ok || !fresh {
		t.Fatalf("reassign failed: ok=%v fresh=%v", ok, fresh)
	}
	if got != x4 {
		t.Errorf("reassigned = %v, want the oldest-freed %v (.4)", got, x4)
	}
}

// TestPoolSkipsConflictedAddress: an address a probe marked as in-use (noteConflict) is not
// handed out until its conflict record ages out; noteConflict also frees the tentative slot.
func TestPoolSkipsConflictedAddress(t *testing.T) {
	p := newIPPool(IPv4{10, 0, 0, 0}, 5) // assignable .2 .3 .4
	// Claim .2 tentatively then mark it conflicted (as the pre-assign probe would).
	ip, _, _ := p.assign(IPv4{}, 10, 2)
	if ip != (IPv4{10, 0, 0, 2}) {
		t.Fatalf("first assign = %v, want .2", ip)
	}
	p.noteConflict(ip) // .2 is really held by someone else
	// Next assignment must skip .2 and take .3.
	ip2, _, ok := p.assign(IPv4{}, 10, 2)
	if !ok || ip2 == ip {
		t.Fatalf("assign after conflict = %v ok=%v, want a non-.2 address", ip2, ok)
	}
	// An explicit request for the conflicted .2 is refused too (falls through).
	ip3, _, ok := p.assign(IPv4{10, 0, 0, 2}, 30, 30)
	if !ok || ip3 == (IPv4{10, 0, 0, 2}) {
		t.Fatalf("requesting conflicted .2 yielded %v ok=%v; must be refused", ip3, ok)
	}
}

// TestPoolConfirmMissEvictsAfterLimit: a lease is reclaimed only after confirmMissLimit
// consecutive missed confirms; a hit (or data traffic) in between resets the counter.
func TestPoolConfirmMissEvictsAfterLimit(t *testing.T) {
	p := newIPPool(IPv4{10, 0, 0, 0}, 10)
	ip, _, _ := p.assign(IPv4{}, 10, 5)

	// Four misses (limit 5) — not yet evicted.
	for i := range 4 {
		if p.confirmMiss(ip, 10, 5, 5) {
			t.Fatalf("evicted after %d misses, want survive until 5", i+1)
		}
	}
	// A hit resets the counter.
	p.confirmHit(ip, 10, 5)
	// Now it takes another full 5 misses.
	for i := range 4 {
		if p.confirmMiss(ip, 10, 5, 5) {
			t.Fatalf("evicted after reset+%d misses, want survive", i+1)
		}
	}
	if !p.confirmMiss(ip, 10, 5, 5) {
		t.Fatal("5th miss after reset should evict")
	}
	// The slot is now free again.
	if _, _, ok := p.lookupByIP(ip); ok {
		t.Errorf("lease %v still present after eviction", ip)
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

// atpReq builds a MacIP ATP TReq DDP payload: 8-byte ATP header (ctrl, bitmap, tid,
// 4 user bytes) followed by the macip_req control struct (version 1, pad, function).
// The ATP user bytes default to zero unless overridden via user.
func atpReq(tid uint16, function byte, user ...byte) []byte {
	var u [4]byte
	copy(u[:], user)
	return []byte{
		atpFuncTReq, 0x00, byte(tid >> 8), byte(tid), // ATP: ctrl, bitmap, tid
		u[0], u[1], u[2], u[3], // ATP user bytes
		0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, function, // macip_req_control: version, pad, function
	}
}

// respFN reads the 32-bit MacIP function code from a config reply (macip_req_control at
// offset atpHeaderLen+4).
func respFN(data []byte) uint32 {
	o := atpHeaderLen + 4
	return uint32(data[o])<<24 | uint32(data[o+1])<<16 | uint32(data[o+2])<<8 | uint32(data[o+3])
}

// TestATPConfigAssign: an ATP TReq (func=assign) gets a TResp carrying an assigned IP.
func TestATPConfigAssign(t *testing.T) {
	fr := newFakeRouter()
	svc := New(fr, nil, nil, testConfig(), nil)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer svc.Stop(context.Background())

	// ATP TReq with a K-STAR-style 0x08 in the last ATP user byte (issue #17); it must
	// round-trip untouched in the reply's user bytes.
	svc.Inbound(ddp.Datagram{
		SrcNetwork: 10, SrcNode: 5, SrcSocket: Socket,
		DestSocket: Socket, DDPType: ddpTypeATP, Data: atpReq(0x1234, macIPFuncAssign, 0, 0, 0, 0x08),
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
	// The reply's ATP user bytes: version stamped into the first two (Apple IP Gateway
	// behaviour macipgw copied), K-STAR's 0x08 preserved in the last.
	if r.data[4] != 0x00 || r.data[5] != macIPVersion || r.data[7] != 0x08 {
		t.Errorf("ATP user bytes = %x, want version in [4:6] and 0x08 in [7]", r.data[4:8])
	}
	// Assigned IP at respIPOff should be the first assignable pool address (.2): .0 is
	// the network address and .1 is the gateway, both reserved.
	if got := r.data[respIPOff : respIPOff+4]; got[0] != 192 || got[1] != 168 || got[2] != 100 || got[3] != 2 {
		t.Errorf("assigned IP = %v, want 192.168.100.2", got)
	}
	// The reply must be byte-length-compatible with macipgw: 8-byte ATP header +
	// 41-byte MacIP data (control 8 + 32 data fields + 1 NUL error byte).
	if len(r.data) != atpHeaderLen+configUserLen {
		t.Errorf("reply length = %d, want %d (macipgw success len)", len(r.data), atpHeaderLen+configUserLen)
	}
	if fn := respFN(r.data); fn != macIPFuncAssign {
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
	svc.Inbound(ddp.Datagram{
		SrcNetwork: 10, SrcNode: 5, SrcSocket: Socket,
		DestSocket: Socket, DDPType: ddpTypeATP, Data: atpReq(0x1234, 99),
	}, nil)

	got := fr.waitReplies(1)
	if len(got) != 1 {
		t.Fatalf("got %d replies, want 1", len(got))
	}
	r := got[0]
	// Function field must be MACIP_ERROR (0xFFFFFFFF).
	if fn := respFN(r.data); fn != 0xFFFFFFFF {
		t.Errorf("reply function = %#x, want 0xFFFFFFFF (MACIP_ERROR)", fn)
	}
	// An error reply still carries the full config block with a zeroed first IP address
	// (issue #17); the error string is appended after it.
	if ip := r.data[respIPOff : respIPOff+4]; ip[0]|ip[1]|ip[2]|ip[3] != 0 {
		t.Errorf("error reply first IP = %v, want all zeros", ip)
	}
	if len(r.data) < respErrOff+len(errNoOp) {
		t.Fatalf("reply too short (%d) to carry error string", len(r.data))
	}
	if got := string(r.data[respErrOff : respErrOff+len(errNoOp)]); got != errNoOp {
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
	svc.Inbound(ddp.Datagram{SrcNetwork: 10, SrcNode: 5, SrcSocket: Socket, DestSocket: Socket, DDPType: ddpTypeATP, Data: atpReq(0x0001, macIPFuncAssign)}, nil)
	first := fr.waitReplies(1)
	if len(first) != 1 {
		t.Fatalf("first assign: got %d replies", len(first))
	}

	// Second endpoint finds the pool exhausted → MACIP_ERROR / errNoIP.
	svc.Inbound(ddp.Datagram{SrcNetwork: 10, SrcNode: 6, SrcSocket: Socket, DestSocket: Socket, DDPType: ddpTypeATP, Data: atpReq(0x0002, macIPFuncAssign)}, nil)
	all := fr.waitReplies(2)
	if len(all) != 2 {
		t.Fatalf("got %d replies, want 2", len(all))
	}
	r := all[1]
	if fn := respFN(r.data); fn != 0xFFFFFFFF {
		t.Errorf("exhausted reply function = %#x, want MACIP_ERROR", fn)
	}
	if got := string(r.data[respErrOff : respErrOff+len(errNoIP)]); got != errNoIP {
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
	svc.Inbound(ddp.Datagram{
		SrcNetwork: 10, SrcNode: 5, SrcSocket: Socket,
		DestSocket: Socket, DDPType: ddpTypeATP, Data: atpReq(0x0001, macIPFuncAssign),
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

	svc.Inbound(ddp.Datagram{
		SrcNetwork: 10, SrcNode: 5, SrcSocket: Socket,
		DestSocket: Socket, DDPType: ddpTypeATP, Data: atpReq(0xABCD, macIPFuncAssign),
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
	if ip := r.data[respIPOff : respIPOff+4]; ip[0] != 10 || ip[1] != 0 || ip[2] != 0 || ip[3] != 77 {
		t.Errorf("assigned IP = %v, want 10.0.0.77 (from egress)", ip)
	}
	if ns := r.data[respNSOff : respNSOff+4]; ns[0] != 10 || ns[1] != 0 || ns[2] != 0 || ns[3] != 1 {
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

	svc.Inbound(ddp.Datagram{
		SrcNetwork: 10, SrcNode: 5, SrcSocket: Socket,
		DestSocket: Socket, DDPType: ddpTypeATP, Data: atpReq(0xABCD, macIPFuncAssign),
	}, nil)

	got := fr.waitReplies(1)
	if len(got) != 1 {
		t.Fatalf("got %d replies, want 1 — NAT mode must fall back to the static pool and reply", len(got))
	}
	if eg.calls != 0 {
		t.Errorf("AssignIP was called %d times; core must not delegate to an inactive assigner", eg.calls)
	}
	// The reply must carry a real static-pool address (network base 192.168.100.0 + 1).
	if ip := got[0].data[respIPOff : respIPOff+4]; ip[0] != 192 || ip[1] != 168 || ip[2] != 100 {
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

	svc.Inbound(ddp.Datagram{
		SrcNetwork: 10, SrcNode: 5, SrcSocket: Socket,
		DestSocket: Socket, DDPType: ddpTypeATP, Data: atpReq(0x0001, macIPFuncAssign),
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

// rrPort is a minimal RoutedPort for the NBP integration tests: it records broadcasts (so
// the test can read the BrRq's NBP id) and unicasts (so it can read a routed TResp) and
// swallows the rest.
type rrPort struct {
	mu    sync.Mutex
	bcst  []ddp.Datagram
	ucast []ddp.Datagram
}

func (p *rrPort) Name() string                       { return "rr" }
func (p *rrPort) Start(context.Context) error        { return nil }
func (p *rrPort) Stop(context.Context) error         { return nil }
func (p *rrPort) Network() uint16                    { return 10 }
func (p *rrPort) Node() uint8                        { return 0x80 }
func (p *rrPort) NetworkMin() uint16                 { return 10 }
func (p *rrPort) NetworkMax() uint16                 { return 10 }
func (p *rrPort) Multicast(_ []byte, _ ddp.Datagram) {}
func (p *rrPort) Unicast(network uint16, node uint8, d ddp.Datagram) {
	d.DestNetwork = network
	d.DestNode = node
	p.mu.Lock()
	p.ucast = append(p.ucast, d)
	p.mu.Unlock()
}
func (p *rrPort) Broadcast(d ddp.Datagram) {
	p.mu.Lock()
	p.bcst = append(p.bcst, d)
	p.mu.Unlock()
}

// waitTResp returns the assigned IP from the first MacIP ATP TResp routed to the given node,
// or the zero IP if none arrives in time. It reads the config reply's first IP field.
func (p *rrPort) waitTRespIP(node uint8) IPv4 {
	for range 800 {
		p.mu.Lock()
		uc := append([]ddp.Datagram(nil), p.ucast...)
		p.mu.Unlock()
		for _, d := range uc {
			if d.DDPType == ddpTypeATP && d.DestNode == node && len(d.Data) >= respIPOff+4 {
				var ip IPv4
				copy(ip[:], d.Data[respIPOff:respIPOff+4])
				return ip
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	return IPv4{}
}
func (p *rrPort) waitBroadcast(n int) []ddp.Datagram {
	for range 3000 {
		p.mu.Lock()
		got := len(p.bcst)
		p.mu.Unlock()
		if got >= n {
			break
		}
		runtime.Gosched()
		time.Sleep(time.Millisecond)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]ddp.Datagram(nil), p.bcst...)
}

// rrLkUpRply builds a single-tuple IPADDRESS LkUp-Rply carrying the given NBP id, from a
// host at net 10, the given node, on socket 72.
func rrLkUpRply(nbpID byte, node byte, ip string) []byte {
	out := []byte{(3 << 4) | 1, nbpID, 0, 10, node, Socket, 0} // 3 = CtrlLkUpRply; network 10
	out = append(out, byte(len(ip)))
	out = append(out, ip...)
	out = append(out, byte(len(nbpTypeIPAddress)))
	out = append(out, nbpTypeIPAddress...)
	out = append(out, byte(len("MyZone")))
	out = append(out, "MyZone"...)
	return out
}

// TestReregistrationSeedsPool: on startup the gateway searches "=:IPADDRESS@*" and seeds
// its pool with any discovered in-range address so it is not reassigned (spec §3.7). A
// discovered out-of-range address is ignored; the gateway's own IP is skipped.
func TestReregistrationSeedsPool(t *testing.T) {
	r := router.New(nil)
	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("router Start: %v", err)
	}
	p := &rrPort{}
	if err := r.Attach(p); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	names := nbp.New(r, nil)
	if err := names.Start(context.Background()); err != nil {
		t.Fatalf("nbp Start: %v", err)
	}
	defer names.Stop(context.Background())

	svc := New(r, names, nil, testConfig(), nil)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer svc.Stop(context.Background())

	// The reregistration goroutine broadcasts the =:IPADDRESS@* BrRq; capture its NBP id.
	var id byte
	got := p.waitBroadcast(1)
	for _, d := range got {
		if len(d.Data) >= 2 && d.Data[0]>>4 == 1 { // CtrlBrRq
			id = d.Data[1]
		}
	}
	if len(got) == 0 {
		t.Fatal("reregistration did not broadcast a BrRq")
	}

	// A live host holds 192.168.100.50 (in range) at node 9; another answers with an
	// out-of-range address; a third answers with the gateway's own IP.
	names.Inbound(ddp.Datagram{
		DestNetwork: 10, SrcNetwork: 10, DestNode: 0x80, SrcNode: 9,
		DestSocket: Socket, SrcSocket: Socket, DDPType: nbp.DDPType,
		Data: rrLkUpRply(id, 9, "192.168.100.50"),
	}, p)
	names.Inbound(ddp.Datagram{
		DestNetwork: 10, SrcNetwork: 10, DestNode: 0x80, SrcNode: 11,
		DestSocket: Socket, SrcSocket: Socket, DDPType: nbp.DDPType,
		Data: rrLkUpRply(id, 11, "10.9.9.9"), // out of the 192.168.100.0 pool
	}, p)
	names.Inbound(ddp.Datagram{
		DestNetwork: 10, SrcNetwork: 10, DestNode: 0x80, SrcNode: 12,
		DestSocket: Socket, SrcSocket: Socket, DDPType: nbp.DDPType,
		Data: rrLkUpRply(id, 12, "192.168.100.1"), // the gateway's own IP
	}, p)

	// Wait for reregistration to finish seeding (the Lookup window is ~2s).
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if svc.OwnsIP(IPv4{192, 168, 100, 50}) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// The in-range discovered address is now leased to node 9 and never handed out.
	if !svc.OwnsIP(IPv4{192, 168, 100, 50}) {
		t.Fatalf("192.168.100.50 not seeded from reregistration")
	}
	if n, node, ok := svc.pool.lookupByIP(IPv4{192, 168, 100, 50}); !ok || n != 10 || node != 9 {
		t.Errorf("seeded lease = %d.%d ok=%v, want 10.9", n, node, ok)
	}
	// The out-of-range and gateway addresses must NOT have been seeded into the static pool.
	if svc.OwnsIP(IPv4{10, 9, 9, 9}) {
		t.Errorf("out-of-range 10.9.9.9 was seeded; must be ignored")
	}
	// Its IPADDRESS name should have been (re)registered by us too.
	if got := ipAddressNBPNames(names); !got["192.168.100.50"] {
		t.Errorf("reregistered IPADDRESS names = %v, want 192.168.100.50", got)
	}
}

// TestPreAssignProbeSkipsLiveDuplicate: when a live host already holds the first candidate
// address (answers its IPADDRESS NBP lookup), the gateway skips it and assigns another
// (draft §3.8.2: assigned addresses are resolved via NBP ARP first).
func TestPreAssignProbeSkipsLiveDuplicate(t *testing.T) {
	r := router.New(nil)
	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("router Start: %v", err)
	}
	p := &rrPort{}
	if err := r.Attach(p); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	names := nbp.New(r, nil)
	if err := names.Start(context.Background()); err != nil {
		t.Fatalf("nbp Start: %v", err)
	}
	defer names.Stop(context.Background())

	// A "live host" holds 192.168.100.2 (the first candidate the gateway would hand out). We
	// simulate it by watching the port for the gateway's IPADDRESS probe BrRq and injecting a
	// LkUp-Rply for .2 from node 99 (≠ the requesting client) — a genuine conflict — using the
	// probe's own NBP id so it reaches the waiting Lookup. The real router does not loop a
	// reply back to the originating node's services, so replies must be injected via Inbound.
	stopResp := make(chan struct{})
	defer close(stopResp)
	go func() {
		seen := 0
		for {
			select {
			case <-stopResp:
				return
			default:
			}
			p.mu.Lock()
			bc := append([]ddp.Datagram(nil), p.bcst...)
			p.mu.Unlock()
			for ; seen < len(bc); seen++ {
				d := bc[seen]
				if len(d.Data) < 8 || d.Data[0]>>4 != 1 { // not a BrRq
					continue
				}
				// object begins at Data[8] with a length prefix at Data[7].
				objLen := int(d.Data[7])
				if len(d.Data) < 8+objLen || string(d.Data[8:8+objLen]) != "192.168.100.2" {
					continue
				}
				names.Inbound(ddp.Datagram{
					DestNetwork: 10, SrcNetwork: 10, DestNode: 0x80, SrcNode: 99,
					DestSocket: Socket, SrcSocket: Socket, DDPType: nbp.DDPType,
					Data: rrLkUpRply(d.Data[1], 99, "192.168.100.2"),
				}, p)
			}
			time.Sleep(200 * time.Microsecond)
		}
	}()

	svc := New(r, names, nil, testConfig(), nil)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer svc.Stop(context.Background())

	// New client 10.7 asks for any address. The first candidate .2 is held by node 99, so
	// the gateway must probe, skip it, and hand out .3.
	svc.Inbound(ddp.Datagram{
		SrcNetwork: 10, SrcNode: 7, SrcSocket: Socket,
		DestSocket: Socket, DDPType: ddpTypeATP, Data: atpReq(0x0001, macIPFuncAssign),
	}, p)

	// Read the assigned IP from the ATP TResp routed back to node 7. It must NOT be the
	// live-duplicate .2 (which the probe rejects) and must be a real, owned pool address.
	assigned := p.waitTRespIP(7)
	if assigned.IsZero() {
		t.Fatal("client 10.7 never received a TResp")
	}
	if assigned == (IPv4{192, 168, 100, 2}) {
		t.Fatalf("gateway assigned the live-duplicate .2; expected it to skip to another address")
	}
	if !svc.OwnsIP(assigned) {
		t.Errorf("assigned %v not owned", assigned)
	}
}

// ipAddressNBPNames returns the set of IPADDRESS object names currently registered.
func ipAddressNBPNames(names *nbp.Service) map[string]bool {
	out := map[string]bool{}
	for _, reg := range names.Names() {
		if string(reg.Type) == nbpTypeIPAddress {
			out[string(reg.Object)] = true
		}
	}
	return out
}

// TestParseDottedIPv4 covers the NBP-object → IPv4 parse used by reregistration.
func TestParseDottedIPv4(t *testing.T) {
	ok := []struct {
		in   string
		want IPv4
	}{
		{"0.0.0.0", IPv4{0, 0, 0, 0}},
		{"192.168.1.2", IPv4{192, 168, 1, 2}},
		{"255.255.255.255", IPv4{255, 255, 255, 255}},
		{"10.0.0.77", IPv4{10, 0, 0, 77}},
	}
	for _, c := range ok {
		got, gotOK := parseDottedIPv4([]byte(c.in))
		if !gotOK || got != c.want {
			t.Errorf("parseDottedIPv4(%q) = %v ok=%v, want %v true", c.in, got, gotOK, c.want)
		}
	}
	bad := []string{"", "1.2.3", "1.2.3.4.5", "256.0.0.1", "1..2.3", "1.2.3.", ".1.2.3", "a.b.c.d", "1.2.3.4 ", "-1.2.3.4"}
	for _, in := range bad {
		if got, gotOK := parseDottedIPv4([]byte(in)); gotOK {
			t.Errorf("parseDottedIPv4(%q) = %v true, want !ok", in, got)
		}
	}
}

// TestLeaseRegistersIPADDRESSName: a static assignment publishes an IPADDRESS NBP name for
// the leased address (spec §3.2.4.3), and it is withdrawn when the lease expires.
func TestLeaseRegistersIPADDRESSName(t *testing.T) {
	fr := newFakeRouter()
	names := nbp.New(fr, nil)
	svc := New(fr, names, nil, testConfig(), nil)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer svc.Stop(context.Background())

	svc.Inbound(ddp.Datagram{
		SrcNetwork: 10, SrcNode: 5, SrcSocket: Socket,
		DestSocket: Socket, DDPType: ddpTypeATP, Data: atpReq(0x0001, macIPFuncAssign),
	}, nil)
	_ = fr.waitReplies(1)

	// The first assignable address is 192.168.100.2 (see TestPoolAssignReuseAndRange).
	if got := ipAddressNBPNames(names); !got["192.168.100.2"] {
		t.Fatalf("IPADDRESS names = %v, want 192.168.100.2 registered", got)
	}

	// Force the lease to expire and drive the pool sweep directly; the name must be gone.
	for _, ip := range svc.pool.expire() {
		svc.unregisterLeaseName(ip)
	}
	// (Nothing evicted yet — lease is fresh. Simulate age by zeroing lastSeen.)
	svc.pool.mu.Lock()
	for i := range svc.pool.entries {
		if svc.pool.entries[i].used {
			svc.pool.entries[i].lastSeen = time.Now().Add(-2 * leaseDuration)
		}
	}
	svc.pool.mu.Unlock()
	for _, ip := range svc.pool.expire() {
		svc.unregisterLeaseName(ip)
	}
	if got := ipAddressNBPNames(names); got["192.168.100.2"] {
		t.Errorf("IPADDRESS name 192.168.100.2 still registered after expiry: %v", got)
	}
}
