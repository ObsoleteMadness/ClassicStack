package router

import (
	"context"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
)

// recordingService is a test router.Service that records the datagrams dispatched to it.
type recordingService struct {
	name   string
	socket uint8
	got    []ddp.Datagram
}

func (s *recordingService) Name() string                { return s.name }
func (s *recordingService) Start(context.Context) error { return nil }
func (s *recordingService) Stop(context.Context) error  { return nil }
func (s *recordingService) Socket() uint8               { return s.socket }
func (s *recordingService) Inbound(d ddp.Datagram, _ RoutedPort) {
	s.got = append(s.got, d)
}

func startedRouter(t *testing.T) *RouterImpl {
	t.Helper()
	r := New(nil)
	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return r
}

func TestAttachInstallsConnectedRoute(t *testing.T) {
	r := startedRouter(t)
	p := newFakePort("EtherTalk", 10, 0x80, 10, 12)
	if err := r.Attach(p); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if e, _ := r.RoutingTable().GetByNetwork(11); e == nil || e.Distance != 0 {
		t.Errorf("Attach did not install the connected route for network 11: %+v", e)
	}
	if got := len(r.Ports()); got != 1 {
		t.Errorf("Ports() = %d, want 1", got)
	}
}

func TestAttachToStoppedRouterFails(t *testing.T) {
	r := New(nil) // not started
	if err := r.Attach(newFakePort("EtherTalk", 10, 0x80, 10, 10)); err == nil {
		t.Errorf("Attach to stopped router should fail")
	}
}

func TestDoubleAttachFails(t *testing.T) {
	r := startedRouter(t)
	p := newFakePort("EtherTalk", 10, 0x80, 10, 10)
	if err := r.Attach(p); err != nil {
		t.Fatalf("first Attach: %v", err)
	}
	if err := r.Attach(p); err == nil {
		t.Errorf("second Attach of same port should fail")
	}
}

func TestDetachWithdrawsConnectedRouteImmediately(t *testing.T) {
	r := startedRouter(t)
	p := newFakePort("EtherTalk", 10, 0x80, 10, 12)
	if err := r.Attach(p); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if err := r.Detach(p); err != nil {
		t.Fatalf("Detach: %v", err)
	}
	// §3: no aging delay — the route is gone immediately.
	if e, _ := r.RoutingTable().GetByNetwork(11); e != nil {
		t.Errorf("Detach did not immediately withdraw the connected route: %+v", e)
	}
	if got := len(r.Ports()); got != 0 {
		t.Errorf("Ports() = %d after Detach, want 0", got)
	}
}

func TestDetachUnknownPortFails(t *testing.T) {
	r := startedRouter(t)
	if err := r.Detach(newFakePort("Ghost", 1, 1, 1, 1)); err == nil {
		t.Errorf("Detach of an unattached port should fail")
	}
}

func TestInboundDispatchesToSocketService(t *testing.T) {
	r := startedRouter(t)
	p := newFakePort("EtherTalk", 10, 0x80, 10, 10)
	if err := r.Attach(p); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	svc := &recordingService{name: "AEP", socket: 4}
	r.RegisterService(svc)

	// A datagram addressed to this port's node on socket 4.
	r.Inbound(ddp.Datagram{
		DestNetwork: 10, SrcNetwork: 10, DestNode: 0x80, SrcNode: 0x81,
		DestSocket: 4, SrcSocket: 4, DDPType: 4, Data: []byte{1},
	}, p)

	if len(svc.got) != 1 {
		t.Fatalf("service received %d datagrams, want 1", len(svc.got))
	}
	if svc.got[0].DestSocket != 4 {
		t.Errorf("dispatched datagram dest socket = %d, want 4", svc.got[0].DestSocket)
	}
}

func TestInboundUnknownSocketIsDropped(t *testing.T) {
	r := startedRouter(t)
	p := newFakePort("EtherTalk", 10, 0x80, 10, 10)
	_ = r.Attach(p)
	svc := &recordingService{name: "AEP", socket: 4}
	r.RegisterService(svc)

	// Socket 9 has no service — must not panic, must not deliver.
	r.Inbound(ddp.Datagram{
		DestNetwork: 10, DestNode: 0x80, DestSocket: 9, SrcNode: 0x81, Data: []byte{0},
	}, p)
	if len(svc.got) != 0 {
		t.Errorf("service got a datagram for the wrong socket")
	}
}

func TestUnregisterServiceStopsDispatch(t *testing.T) {
	r := startedRouter(t)
	p := newFakePort("EtherTalk", 10, 0x80, 10, 10)
	_ = r.Attach(p)
	svc := &recordingService{name: "AEP", socket: 4}
	r.RegisterService(svc)
	r.UnregisterService(svc)

	r.Inbound(ddp.Datagram{
		DestNetwork: 10, DestNode: 0x80, DestSocket: 4, SrcNode: 0x81, Data: []byte{1},
	}, p)
	if len(svc.got) != 0 {
		t.Errorf("dispatch continued after UnregisterService")
	}
}

func TestReplyRoutesBackToOriginator(t *testing.T) {
	r := startedRouter(t)
	// Two ports: the request arrives on A; the source lives on A's own network so the reply
	// routes out A (directly connected).
	a := newFakePort("EtherTalk", 10, 0x80, 10, 10)
	_ = r.Attach(a)

	req := ddp.Datagram{
		DestNetwork: 10, SrcNetwork: 10, DestNode: 0x80, SrcNode: 0x81,
		DestSocket: 4, SrcSocket: 4, DDPType: 4, Data: []byte{1},
	}
	r.Reply(req, a, 4, []byte{2, 0xAA})

	if len(a.unicast) != 1 {
		t.Fatalf("reply produced %d unicasts, want 1", len(a.unicast))
	}
	got := a.unicast[0]
	if got.DestNode != 0x81 {
		t.Errorf("reply dest node = %d, want 0x81 (the requester)", got.DestNode)
	}
	if len(got.Data) != 2 || got.Data[0] != 2 {
		t.Errorf("reply payload = %v, want [2 170]", got.Data)
	}
}

func TestInboundFillsSourceNetworkFromPort(t *testing.T) {
	r := startedRouter(t)
	p := newFakePort("EtherTalk", 10, 0x80, 10, 10)
	_ = r.Attach(p)
	svc := &recordingService{name: "AEP", socket: 4}
	r.RegisterService(svc)

	// Datagram with zero networks (LocalTalk-style short header origin): the router fills them
	// from the rx port before delivery.
	r.Inbound(ddp.Datagram{
		DestNetwork: 0, SrcNetwork: 0, DestNode: 0x80, SrcNode: 0x81,
		DestSocket: 4, SrcSocket: 4, DDPType: 4, Data: []byte{1},
	}, p)

	if len(svc.got) != 1 {
		t.Fatalf("service received %d datagrams, want 1", len(svc.got))
	}
	if svc.got[0].SrcNetwork != 10 || svc.got[0].DestNetwork != 10 {
		t.Errorf("source/dest network not filled from port: %+v", svc.got[0])
	}
}

// TestInboundLeavesSourceNetworkZeroOnExtendedPort: on an extended (multi-network,
// AARP-addressed) port, a zero source network is a genuinely unnumbered/startup-range
// client, not shorthand for "this segment" — backfilling it would manufacture a
// network.node nothing has claimed and defeat Reply()'s broadcast-to-unnumbered-client
// fallback. The destination network is still filled (DestNetwork=0 legitimately means
// "my network" regardless of port type).
func TestInboundLeavesSourceNetworkZeroOnExtendedPort(t *testing.T) {
	r := startedRouter(t)
	p := newFakePort("EtherTalk", 10, 0x80, 10, 12) // extended: netMin(10) != netMax(12)
	if err := r.Attach(p); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	svc := &recordingService{name: "AEP", socket: 4}
	r.RegisterService(svc)

	r.Inbound(ddp.Datagram{
		DestNetwork: 0, SrcNetwork: 0, DestNode: 0x80, SrcNode: 0x81,
		DestSocket: 4, SrcSocket: 4, DDPType: 4, Data: []byte{1},
	}, p)

	if len(svc.got) != 1 {
		t.Fatalf("service received %d datagrams, want 1", len(svc.got))
	}
	if svc.got[0].DestNetwork != 10 {
		t.Errorf("dest network = %d, want 10 (filled from port)", svc.got[0].DestNetwork)
	}
	if svc.got[0].SrcNetwork != 0 {
		t.Errorf("src network = %d, want 0 (left unnumbered on an extended port)", svc.got[0].SrcNetwork)
	}
}
