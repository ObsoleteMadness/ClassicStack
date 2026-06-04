package router

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/port"
	"github.com/ObsoleteMadness/ClassicStack/protocol/ddp"
	"github.com/ObsoleteMadness/ClassicStack/service"
)

// fakePort is a minimal port.Port for membership tests. It records
// start/stop and reports a fixed directly-connected network range.
type fakePort struct {
	name    string
	netMin  uint16
	netMax  uint16
	started atomic.Bool
	stopped atomic.Bool
}

func (p *fakePort) ShortString() string                  { return p.name }
func (p *fakePort) Start(port.RouterHooks) error         { p.started.Store(true); return nil }
func (p *fakePort) Stop() error                          { p.stopped.Store(true); return nil }
func (p *fakePort) Unicast(uint16, uint8, ddp.Datagram)  {}
func (p *fakePort) Broadcast(ddp.Datagram)               {}
func (p *fakePort) Multicast([]byte, ddp.Datagram)       {}
func (p *fakePort) SetNetworkRange(uint16, uint16) error { return nil }
func (p *fakePort) Network() uint16                      { return p.netMin }
func (p *fakePort) Node() uint8                          { return 1 }
func (p *fakePort) NetworkMin() uint16                   { return p.netMin }
func (p *fakePort) NetworkMax() uint16                   { return p.netMax }
func (p *fakePort) ExtendedNetwork() bool                { return p.netMin != p.netMax }

// fakeService is a minimal service.Service that listens on a fixed socket.
type fakeService struct {
	socket  uint8
	started atomic.Bool
	stopped atomic.Bool
}

func (s *fakeService) Socket() uint8 { return s.socket }
func (s *fakeService) Start(context.Context, service.Router) error {
	s.started.Store(true)
	return nil
}
func (s *fakeService) Stop() error                     { s.stopped.Store(true); return nil }
func (s *fakeService) Inbound(ddp.Datagram, port.Port) {}

func newTestRouter() *Router {
	return New("test", nil, []service.Service{})
}

func TestAddRemoveServiceSocketBookkeeping(t *testing.T) {
	r := newTestRouter()
	svc := &fakeService{socket: 99}

	if err := r.AddService(context.Background(), svc); err != nil {
		t.Fatalf("AddService: %v", err)
	}
	if !svc.started.Load() {
		t.Error("service not started on AddService")
	}
	r.membership.RLock()
	got := r.servicesBySAS[99]
	r.membership.RUnlock()
	if got != svc {
		t.Error("socket 99 not registered to service")
	}

	if err := r.RemoveService(svc); err != nil {
		t.Fatalf("RemoveService: %v", err)
	}
	if !svc.stopped.Load() {
		t.Error("service not stopped on RemoveService")
	}
	r.membership.RLock()
	_, ok := r.servicesBySAS[99]
	r.membership.RUnlock()
	if ok {
		t.Error("socket 99 still registered after RemoveService")
	}
}

func TestRemovePortWithdrawsRoutes(t *testing.T) {
	r := newTestRouter()
	p := &fakePort{name: "fake", netMin: 10, netMax: 12}

	if err := r.AddPort(context.Background(), p); err != nil {
		t.Fatalf("AddPort: %v", err)
	}
	if !p.started.Load() {
		t.Error("port not started on AddPort")
	}

	// Seed a directly-connected route for the port, as RTMP would.
	r.RoutingTable.SetPortRange(p, 10, 12)
	if e, _ := r.RoutingTable.GetByNetwork(11); e == nil {
		t.Fatal("expected route for network 11 after SetPortRange")
	}

	if err := r.RemovePort(p); err != nil {
		t.Fatalf("RemovePort: %v", err)
	}
	if !p.stopped.Load() {
		t.Error("port not stopped on RemovePort")
	}
	if e, _ := r.RoutingTable.GetByNetwork(11); e != nil {
		t.Errorf("route for network 11 still present after RemovePort: %+v", e)
	}
}

func TestConcurrentDispatchDuringMembershipChange(t *testing.T) {
	r := newTestRouter()
	var wg sync.WaitGroup

	// Reader: hammer deliver via the dispatch map while services churn.
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				r.deliver(ddp.Datagram{DestinationSocket: 50}, nil)
			}
		}
	}()

	for range 200 {
		svc := &fakeService{socket: 50}
		if err := r.AddService(context.Background(), svc); err != nil {
			t.Fatalf("AddService: %v", err)
		}
		if err := r.RemoveService(svc); err != nil {
			t.Fatalf("RemoveService: %v", err)
		}
	}
	close(stop)
	wg.Wait()
}
