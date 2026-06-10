package zip

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
	"github.com/ObsoleteMadness/ClassicStack/core/router"
)

// fakePort is a RoutedPort that records sent datagrams, for driving the real router in tests.
type fakePort struct {
	name           string
	network        uint16
	node           uint8
	netMin, netMax uint16

	mu      sync.Mutex
	unicast []ddp.Datagram
}

func newFakePort(name string, network uint16, node uint8, netMin, netMax uint16) *fakePort {
	return &fakePort{name: name, network: network, node: node, netMin: netMin, netMax: netMax}
}

func (p *fakePort) Name() string                   { return p.name }
func (p *fakePort) Start(context.Context) error    { return nil }
func (p *fakePort) Stop(context.Context) error     { return nil }
func (p *fakePort) Network() uint16                { return p.network }
func (p *fakePort) Node() uint8                    { return p.node }
func (p *fakePort) NetworkMin() uint16             { return p.netMin }
func (p *fakePort) NetworkMax() uint16             { return p.netMax }
func (p *fakePort) Broadcast(ddp.Datagram)         {}
func (p *fakePort) Multicast([]byte, ddp.Datagram) {}
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

// TestGetMyZoneReply: an ATP GetMyZone for the source network is answered with the network's
// default zone.
func TestGetMyZoneReply(t *testing.T) {
	r := startedRouter(t)
	p := newFakePort("EtherTalk", 10, 0x80, 10, 10)
	if err := r.Attach(p); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	nmax := uint16(10)
	if err := r.Zones().AddNetworksToZone([]byte("Engineering"), 10, &nmax); err != nil {
		t.Fatalf("AddNetworksToZone: %v", err)
	}

	svc := NewRespondingService(r, nil)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("svc Start: %v", err)
	}
	defer svc.Stop(context.Background())

	// ATP TReq carrying GetMyZone: ctrl=TReq, bitmap=1, tid, fn=GetMyZone, zero, 0,0.
	tid := uint16(0x1234)
	svc.Inbound(ddp.Datagram{
		DestNetwork: 10, SrcNetwork: 10, DestNode: 0x80, SrcNode: 0x81,
		DestSocket: SAS, SrcSocket: 250, DDPType: ATPDDPType,
		Data: []byte{ATPFuncTReq, 1, byte(tid >> 8), byte(tid), ATPGetMyZone, 0, 0, 0},
	}, p)

	got := p.waitUnicast(1)
	if len(got) != 1 {
		t.Fatalf("got %d replies, want 1", len(got))
	}
	d := got[0]
	if d.DDPType != ATPDDPType {
		t.Errorf("reply DDPType = %d, want %d (ATP)", d.DDPType, ATPDDPType)
	}
	// ATP TResp header (8) then zone-len + zone.
	if len(d.Data) < 9 {
		t.Fatalf("reply too short: %v", d.Data)
	}
	if d.Data[0] != (ATPFuncTResp | ATPEOM) {
		t.Errorf("reply ctrl = 0x%02x, want TResp|EOM", d.Data[0])
	}
	if be16(d.Data[2:4]) != tid {
		t.Errorf("reply tid = %d, want %d", be16(d.Data[2:4]), tid)
	}
	zlen := int(d.Data[8])
	if 9+zlen > len(d.Data) || string(d.Data[9:9+zlen]) != "Engineering" {
		t.Errorf("reply zone = %q, want Engineering", d.Data[9:])
	}
}

// TestZipReplyCommitsZone: a ZIP Reply tuple for a known network adds the zone to the table.
func TestZipReplyCommitsZone(t *testing.T) {
	r := startedRouter(t)
	via := newFakePort("EtherTalk", 10, 0x80, 10, 10)
	if err := r.Attach(via); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	// A learned remote network 50 must exist for the ZIP Reply to attach a zone to.
	r.RoutingTable().Consider(&router.RoutingTableEntry{
		NetworkMin: 50, NetworkMax: 50, Distance: 1, Port: via, NextNetwork: 10, NextNode: 0x81,
	})

	svc := NewRespondingService(r, nil)
	_ = svc.Start(context.Background())
	defer svc.Stop(context.Background())

	// ZIP Reply: func, pad, then tuple [network(2)=50, len, zone...].
	zone := []byte("Marketing")
	data := []byte{FuncReply, 0, 0x00, 50, byte(len(zone))}
	data = append(data, zone...)
	svc.Inbound(ddp.Datagram{
		DestNetwork: 10, SrcNetwork: 10, DestNode: 0x80, SrcNode: 0x81,
		DestSocket: SAS, SrcSocket: SAS, DDPType: DDPType, Data: data,
	}, via)

	// Poll the zone table.
	for range 2000 {
		nets := r.Zones().NetworksInZone(zone)
		if len(nets) > 0 {
			return // committed
		}
		runtime.Gosched()
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("ZIP Reply did not commit zone %q to network 50", zone)
}
