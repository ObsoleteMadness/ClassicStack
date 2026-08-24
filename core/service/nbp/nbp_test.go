package nbp

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
	protonbp "github.com/ObsoleteMadness/ClassicStack/core/protocol/nbp"
	"github.com/ObsoleteMadness/ClassicStack/core/router"
)

// fakePort is a RoutedPort that records sent datagrams, for driving the real router.
type fakePort struct {
	name           string
	network        uint16
	node           uint8
	netMin, netMax uint16

	mu        sync.Mutex
	unicast   []ddp.Datagram
	broadcast []ddp.Datagram
	multicast []ddp.Datagram
}

func newFakePort(name string, network uint16, node uint8, netMin, netMax uint16) *fakePort {
	return &fakePort{name: name, network: network, node: node, netMin: netMin, netMax: netMax}
}

func (p *fakePort) Name() string                { return p.name }
func (p *fakePort) Start(context.Context) error { return nil }
func (p *fakePort) Stop(context.Context) error  { return nil }
func (p *fakePort) Network() uint16             { return p.network }
func (p *fakePort) Node() uint8                 { return p.node }
func (p *fakePort) NetworkMin() uint16          { return p.netMin }
func (p *fakePort) NetworkMax() uint16          { return p.netMax }
func (p *fakePort) Broadcast(d ddp.Datagram) {
	p.mu.Lock()
	p.broadcast = append(p.broadcast, d)
	p.mu.Unlock()
}
func (p *fakePort) Multicast(_ []byte, d ddp.Datagram) {
	p.mu.Lock()
	p.multicast = append(p.multicast, d)
	p.mu.Unlock()
}
func (p *fakePort) Unicast(network uint16, node uint8, d ddp.Datagram) {
	d.DestNetwork = network
	d.DestNode = node
	p.mu.Lock()
	p.unicast = append(p.unicast, d)
	p.mu.Unlock()
}

func (p *fakePort) waitUnicast(n int) []ddp.Datagram {
	for range 2000 {
		p.mu.Lock()
		got := len(p.unicast)
		p.mu.Unlock()
		if got >= n {
			break
		}
		runtime.Gosched()
		time.Sleep(time.Millisecond)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]ddp.Datagram(nil), p.unicast...)
}

func startedRouter(t *testing.T) *router.RouterImpl {
	t.Helper()
	r := router.New(nil)
	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("router Start: %v", err)
	}
	return r
}

// buildLkUp builds a single-tuple NBP LkUp packet for obj:typ@zone, with the
// querier addressed at node/socket on enumerator 0.
func buildLkUp(nbpID, node, socket byte, obj, typ, zone string) []byte {
	out := []byte{(protonbp.CtrlLkUp << 4) | 1, nbpID, 0, 0, node, socket, 0}
	out = append(out, byte(len(obj)))
	out = append(out, obj...)
	out = append(out, byte(len(typ)))
	out = append(out, typ...)
	out = append(out, byte(len(zone)))
	out = append(out, zone...)
	return out
}

// TestLkUpRepliesForRegisteredName: a LkUp matching a registered name yields a
// LkUp-Rply unicast back to the querier carrying the registered socket.
func TestLkUpRepliesForRegisteredName(t *testing.T) {
	r := startedRouter(t)
	p := newFakePort("EtherTalk", 10, 0x80, 10, 10)
	if err := r.Attach(p); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	svc := New(r, nil)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("svc Start: %v", err)
	}
	defer svc.Stop(context.Background())

	svc.RegisterName([]byte("MyMac"), []byte("AFPServer"), []byte("MyZone"), 0xFB)

	// Query from node 0x81 on network 10 for =:AFPServer@MyZone.
	svc.Inbound(ddp.Datagram{
		DestNetwork: 10, SrcNetwork: 10, DestNode: 0x80, SrcNode: 0x81,
		DestSocket: Socket, SrcSocket: Socket, DDPType: DDPType,
		Data: buildLkUp(0x07, 0x81, Socket, "=", "AFPServer", "MyZone"),
	}, p)

	got := p.waitUnicast(1)
	if len(got) != 1 {
		t.Fatalf("got %d replies, want 1", len(got))
	}
	d := got[0]
	if d.DDPType != DDPType {
		t.Errorf("reply DDPType = %d, want %d", d.DDPType, DDPType)
	}
	pkt, err := protonbp.ParsePacket(d.Data)
	if err != nil {
		t.Fatalf("parse reply: %v", err)
	}
	if pkt.Function != protonbp.CtrlLkUpRply {
		t.Errorf("reply func = %d, want LkUpRply", pkt.Function)
	}
	if pkt.Tuple.Socket != 0xFB {
		t.Errorf("reply socket = %d, want 0xFB (registered)", pkt.Tuple.Socket)
	}
	if string(pkt.Tuple.Object) != "MyMac" {
		t.Errorf("reply object = %q, want MyMac", pkt.Tuple.Object)
	}
}

// TestLkUpNoMatchNoReply: a LkUp for an unregistered name produces nothing.
func TestLkUpNoMatchNoReply(t *testing.T) {
	r := startedRouter(t)
	p := newFakePort("EtherTalk", 10, 0x80, 10, 10)
	_ = r.Attach(p)
	svc := New(r, nil)
	_ = svc.Start(context.Background())
	defer svc.Stop(context.Background())

	svc.Inbound(ddp.Datagram{
		DestNetwork: 10, SrcNetwork: 10, DestNode: 0x80, SrcNode: 0x81,
		DestSocket: Socket, SrcSocket: Socket, DDPType: DDPType,
		Data: buildLkUp(0x07, 0x81, Socket, "=", "Nope", "MyZone"),
	}, p)

	// Give the worker a chance, then assert no unicast happened.
	for range 50 {
		runtime.Gosched()
		time.Sleep(time.Millisecond)
	}
	if got := p.waitUnicast(0); len(got) != 0 {
		t.Errorf("got %d replies for unregistered name, want 0", len(got))
	}
}

// TestLkUpAnyObjectEchoes: an any-object registration (netboot's BootServer)
// matches a LkUp for an arbitrary object of its type, and the reply tuple
// echoes the requested object rather than the registered one.
func TestLkUpAnyObjectEchoes(t *testing.T) {
	r := startedRouter(t)
	p := newFakePort("LToUDP", 10, 0x80, 10, 10)
	if err := r.Attach(p); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	svc := New(r, nil)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("svc Start: %v", err)
	}
	defer svc.Stop(context.Background())

	svc.RegisterNameAnyObject([]byte("0000"), []byte("BootServer"), []byte("*"), 10)

	// The booting ROM looks up its PRAM serverNum in hex — "BABE" here.
	svc.Inbound(ddp.Datagram{
		DestNetwork: 10, SrcNetwork: 10, DestNode: 0x80, SrcNode: 0x81,
		DestSocket: Socket, SrcSocket: Socket, DDPType: DDPType,
		Data: buildLkUp(0x07, 0x81, 10, "BABE", "BootServer", "*"),
	}, p)

	got := p.waitUnicast(1)
	if len(got) != 1 {
		t.Fatalf("got %d replies, want 1", len(got))
	}
	pkt, err := protonbp.ParsePacket(got[0].Data)
	if err != nil {
		t.Fatalf("parse reply: %v", err)
	}
	if string(pkt.Tuple.Object) != "BABE" {
		t.Errorf("reply object = %q, want the echoed BABE", pkt.Tuple.Object)
	}
	if string(pkt.Tuple.Type) != "BootServer" || pkt.Tuple.Socket != 10 {
		t.Errorf("reply tuple = %q socket %d", pkt.Tuple.Type, pkt.Tuple.Socket)
	}

	// A wildcard-object query falls back to the registered object name.
	svc.Inbound(ddp.Datagram{
		DestNetwork: 10, SrcNetwork: 10, DestNode: 0x80, SrcNode: 0x81,
		DestSocket: Socket, SrcSocket: Socket, DDPType: DDPType,
		Data: buildLkUp(0x08, 0x81, 10, "=", "BootServer", "*"),
	}, p)
	got = p.waitUnicast(2)
	if len(got) != 2 {
		t.Fatalf("got %d replies, want 2", len(got))
	}
	pkt, err = protonbp.ParsePacket(got[1].Data)
	if err != nil {
		t.Fatalf("parse reply: %v", err)
	}
	if string(pkt.Tuple.Object) != "0000" {
		t.Errorf("wildcard reply object = %q, want the registered 0000", pkt.Tuple.Object)
	}

	// An unrelated type still gets nothing.
	svc.Inbound(ddp.Datagram{
		DestNetwork: 10, SrcNetwork: 10, DestNode: 0x80, SrcNode: 0x81,
		DestSocket: Socket, SrcSocket: Socket, DDPType: DDPType,
		Data: buildLkUp(0x09, 0x81, 10, "BABE", "AFPServer", "*"),
	}, p)
	for range 50 {
		runtime.Gosched()
		time.Sleep(time.Millisecond)
	}
	if got := p.waitUnicast(2); len(got) != 2 {
		t.Errorf("unrelated type matched an any-object entry: %d replies", len(got))
	}
}

func (p *fakePort) waitBroadcast(n int) []ddp.Datagram {
	for range 2000 {
		p.mu.Lock()
		got := len(p.broadcast)
		p.mu.Unlock()
		if got >= n {
			break
		}
		runtime.Gosched()
		time.Sleep(time.Millisecond)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]ddp.Datagram(nil), p.broadcast...)
}

// buildLkUpRply builds a single-tuple LkUp-Rply for obj:typ@zone resolving to
// network.node:socket, carrying the given NBP id.
func buildLkUpRply(nbpID byte, network uint16, node, socket byte, obj, typ, zone string) []byte {
	out := []byte{(protonbp.CtrlLkUpRply << 4) | 1, nbpID, byte(network >> 8), byte(network), node, socket, 0}
	out = append(out, byte(len(obj)))
	out = append(out, obj...)
	out = append(out, byte(len(typ)))
	out = append(out, typ...)
	out = append(out, byte(len(zone)))
	out = append(out, zone...)
	return out
}

// TestLookupCollectsReplies: a self-originated Lookup broadcasts a BrRq on every port and
// collects the LkUp-Rply tuples that come back (delivered by NBP id), de-duplicated.
func TestLookupCollectsReplies(t *testing.T) {
	r := startedRouter(t)
	p := newFakePort("EtherTalk", 10, 0x80, 10, 10)
	if err := r.Attach(p); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	svc := New(r, nil)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("svc Start: %v", err)
	}
	defer svc.Stop(context.Background())

	// Run the lookup on a goroutine; it broadcasts a BrRq then waits its window.
	type result struct{ ents []NBPEntity }
	resc := make(chan result, 1)
	go func() {
		ents := svc.LookupTimeout([]byte("="), []byte("IPADDRESS"), []byte("*"), 500*time.Millisecond)
		resc <- result{ents}
	}()

	// Capture the BrRq the service broadcast so we can echo its NBP id back in a reply.
	bc := p.waitBroadcast(1)
	if len(bc) == 0 {
		t.Fatal("Lookup did not broadcast a BrRq")
	}
	brreq, err := protonbp.ParsePacket(bc[0].Data)
	if err != nil {
		t.Fatalf("parse BrRq: %v", err)
	}
	id := brreq.NBPID

	// Feed two replies (one duplicate) as if two hosts answered the reregistration search.
	svc.Inbound(ddp.Datagram{
		DestNetwork: 10, SrcNetwork: 10, DestNode: 0x80, SrcNode: 0x81,
		DestSocket: Socket, SrcSocket: Socket, DDPType: DDPType,
		Data: buildLkUpRply(id, 10, 0x05, Socket, "192.168.1.2", "IPADDRESS", "MyZone"),
	}, p)
	svc.Inbound(ddp.Datagram{
		DestNetwork: 10, SrcNetwork: 10, DestNode: 0x80, SrcNode: 0x82,
		DestSocket: Socket, SrcSocket: Socket, DDPType: DDPType,
		Data: buildLkUpRply(id, 10, 0x05, Socket, "192.168.1.2", "IPADDRESS", "MyZone"), // dup
	}, p)
	svc.Inbound(ddp.Datagram{
		DestNetwork: 10, SrcNetwork: 10, DestNode: 0x80, SrcNode: 0x83,
		DestSocket: Socket, SrcSocket: Socket, DDPType: DDPType,
		Data: buildLkUpRply(id, 10, 0x06, Socket, "192.168.1.3", "IPADDRESS", "MyZone"),
	}, p)

	res := <-resc
	if len(res.ents) != 2 {
		t.Fatalf("Lookup returned %d entities, want 2 (deduped)", len(res.ents))
	}
	// Both discovered addresses must be present with their responder net.node.
	found := map[string]NBPEntity{}
	for _, e := range res.ents {
		found[string(e.Object)] = e
	}
	if e, ok := found["192.168.1.2"]; !ok || e.Node != 0x05 || e.Network != 10 {
		t.Errorf(".2 entity = %+v, want net 10 node 5", e)
	}
	if e, ok := found["192.168.1.3"]; !ok || e.Node != 0x06 {
		t.Errorf(".3 entity = %+v, want node 6", e)
	}
}

// TestLookupNotRunningReturnsNil: Lookup before Start (or after Stop) returns nil, not a
// hang.
func TestLookupNotRunningReturnsNil(t *testing.T) {
	svc := New(startedRouter(t), nil)
	if ents := svc.Lookup([]byte("="), []byte("IPADDRESS"), []byte("*")); ents != nil {
		t.Errorf("Lookup while stopped = %v, want nil", ents)
	}
}

func (p *fakePort) waitMulticast(n int) []ddp.Datagram {
	for range 2000 {
		p.mu.Lock()
		got := len(p.multicast)
		p.mu.Unlock()
		if got >= n {
			break
		}
		runtime.Gosched()
		time.Sleep(time.Millisecond)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]ddp.Datagram(nil), p.multicast...)
}

// buildBrRq builds a single-tuple NBP BrRq packet for obj:typ@zone, with the
// querier addressed at node/socket on enumerator 0.
func buildBrRq(nbpID, node, socket byte, obj, typ, zone string) []byte {
	out := []byte{(protonbp.CtrlBrRq << 4) | 1, nbpID, 0, 0, node, socket, 0}
	out = append(out, byte(len(obj)))
	out = append(out, obj...)
	out = append(out, byte(len(typ)))
	out = append(out, typ...)
	out = append(out, byte(len(zone)))
	out = append(out, zone...)
	return out
}

// TestBrRqResolvesWildcardZoneInReRoutedLkUp: a BrRq with zone=* arriving on a port whose
// network sits in exactly one zone must be re-broadcast as a LkUp carrying that resolved
// zone name, not the literal "*" — otherwise responders on other member networks echo "*"
// back, and a zone-scoped Chooser/Finder query (which asks for a real zone name) never
// matches those replies. Regression for the resolved routeZone not being threaded into
// buildCommonPayload.
func TestBrRqResolvesWildcardZoneInReRoutedLkUp(t *testing.T) {
	r := startedRouter(t)
	p10 := newFakePort("EtherTalk10", 10, 0x80, 10, 10)
	p20 := newFakePort("EtherTalk20", 20, 0x80, 20, 20)
	if err := r.Attach(p10); err != nil {
		t.Fatalf("Attach p10: %v", err)
	}
	if err := r.Attach(p20); err != nil {
		t.Fatalf("Attach p20: %v", err)
	}
	nmax10 := uint16(10)
	if err := r.Zones().AddNetworksToZone([]byte("ZoneA"), 10, &nmax10); err != nil {
		t.Fatalf("AddNetworksToZone ZoneA: %v", err)
	}
	nmax20 := uint16(20)
	if err := r.Zones().AddNetworksToZone([]byte("ZoneB"), 20, &nmax20); err != nil {
		t.Fatalf("AddNetworksToZone ZoneB: %v", err)
	}

	svc := New(r, nil)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("svc Start: %v", err)
	}
	defer svc.Stop(context.Background())

	// A Chooser-style BrRq arrives on p10 asking for zone=* (its own, single-zone network).
	svc.Inbound(ddp.Datagram{
		DestNetwork: 10, SrcNetwork: 10, DestNode: 0x80, SrcNode: 0x81,
		DestSocket: Socket, SrcSocket: Socket, DDPType: DDPType,
		Data: buildBrRq(0x07, 0x81, Socket, "=", "AFPServer", "*"),
	}, p10)

	got := p10.waitMulticast(1)
	if len(got) != 1 {
		t.Fatalf("got %d multicasts on p10, want 1", len(got))
	}
	pkt, err := protonbp.ParsePacket(got[0].Data)
	if err != nil {
		t.Fatalf("parse re-broadcast LkUp: %v", err)
	}
	if pkt.Function != protonbp.CtrlLkUp {
		t.Errorf("re-broadcast func = %d, want LkUp", pkt.Function)
	}
	if string(pkt.Tuple.Zone) != "ZoneA" {
		t.Errorf("re-broadcast zone = %q, want resolved \"ZoneA\" (not the literal wildcard)", pkt.Tuple.Zone)
	}
}

// TestRegisterUnregister verifies the name table mutates and dedups by entity.
func TestRegisterUnregister(t *testing.T) {
	svc := New(startedRouter(t), nil)
	svc.RegisterName([]byte("A"), []byte("T"), []byte("Z"), 1)
	svc.RegisterName([]byte("a"), []byte("t"), []byte("z"), 2) // case-insensitive update
	if names := svc.Names(); len(names) != 1 || names[0].Socket != 2 {
		t.Fatalf("expected 1 name with updated socket 2, got %+v", names)
	}
	svc.UnregisterName([]byte("A"), []byte("T"), []byte("Z"))
	if names := svc.Names(); len(names) != 0 {
		t.Errorf("expected 0 names after unregister, got %d", len(names))
	}
}
