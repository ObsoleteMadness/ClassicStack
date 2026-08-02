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
