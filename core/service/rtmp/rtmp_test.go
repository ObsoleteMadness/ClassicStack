package rtmp

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

	mu        sync.Mutex
	unicast   []ddp.Datagram
	broadcast []ddp.Datagram
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
func (p *fakePort) Multicast([]byte, ddp.Datagram) {}
func (p *fakePort) Broadcast(d ddp.Datagram) {
	p.mu.Lock()
	p.broadcast = append(p.broadcast, d)
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

// TestRangeRequestReply: an RTMP Request(FuncRequest) from a client on the port's network gets
// a Data reply carrying the port's network/node and extended range tuple.
func TestRangeRequestReply(t *testing.T) {
	r := startedRouter(t)
	p := newFakePort("EtherTalk", 10, 0x80, 10, 12)
	if err := r.Attach(p); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	svc := NewRespondingService(r, nil)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("svc Start: %v", err)
	}
	defer svc.Stop(context.Background())

	// Request from node 0x81 on network 10, addressed to the router's RTMP socket.
	svc.Inbound(ddp.Datagram{
		Hops: 0, DestNetwork: 10, SrcNetwork: 10, DestNode: 0x80, SrcNode: 0x81,
		DestSocket: SAS, SrcSocket: SAS, DDPType: DDPTypeRequest, Data: []byte{FuncRequest},
	}, p)

	got := p.waitUnicast(1)
	if len(got) != 1 {
		t.Fatalf("got %d unicast replies, want 1", len(got))
	}
	d := got[0]
	if d.DDPType != DDPTypeData {
		t.Errorf("reply DDPType = %d, want %d (Data)", d.DDPType, DDPTypeData)
	}
	// Data: network(2) node-id-len(1)=8 node(1) then extended tuple.
	if len(d.Data) < 4 || d.Data[0] != 0x00 || d.Data[1] != 10 || d.Data[2] != 8 || d.Data[3] != 0x80 {
		t.Fatalf("reply header wrong, want net=10 idlen=8 node=0x80, got %v", d.Data)
	}
	// Extended tuple: networkMin(2)=10, 0x80, networkMax(2)=12, version.
	if len(d.Data) != 10 || d.Data[6] != 0x80 || d.Data[7] != 0x00 || d.Data[8] != 12 || d.Data[9] != Version {
		t.Errorf("extended tuple wrong: %v", d.Data)
	}
}

// TestDataFoldLearnsRoute: an RTMP Data packet from a neighbour adds a learned route for the
// advertised remote network.
func TestDataFoldLearnsRoute(t *testing.T) {
	r := startedRouter(t)
	p := newFakePort("EtherTalk", 10, 0x80, 10, 10) // non-extended local network
	if err := r.Attach(p); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	svc := NewRespondingService(r, nil)
	_ = svc.Start(context.Background())
	defer svc.Stop(context.Background())

	// RTMP Data from sender net 10 node 0x81: header (net=10, idlen=8, node=0x81), own tuple
	// (0,0,version) for a non-extended sender, then a neighbour tuple for network 50 distance 1.
	data := []byte{
		0x00, 10, 8, 0x81, // sender header
		0x00, 0x00, Version, // sender's own (non-extended) tuple
		0x00, 50, 0x01, // neighbour: network 50, distance 1 (non-extended)
	}
	svc.Inbound(ddp.Datagram{
		DestNetwork: 10, SrcNetwork: 10, DestNode: 0x80, SrcNode: 0x81,
		DestSocket: SAS, SrcSocket: SAS, DDPType: DDPTypeData, Data: data,
	}, p)

	// Poll the routing table for the learned route.
	var e *router.RoutingTableEntry
	for range 2000 {
		e, _ = r.RoutingTable().GetByNetwork(50)
		if e != nil {
			break
		}
		runtime.Gosched()
		time.Sleep(time.Millisecond)
	}
	if e == nil {
		t.Fatalf("network 50 not learned from RTMP Data")
	}
	if e.Distance != 2 {
		t.Errorf("learned distance = %d, want 2 (advertised 1 + 1 hop)", e.Distance)
	}
	if e.NextNode != 0x81 || e.NextNetwork != 10 {
		t.Errorf("learned next hop = %d.%d, want 10.0x81", e.NextNetwork, e.NextNode)
	}
}
