package zip

import (
	"context"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
	"github.com/ObsoleteMadness/ClassicStack/core/router"
)

// SendingName is the component/section key for the ZIP sending service.
const SendingName = "ZIP-Send"

// defaultSendInterval is the ZIP query period (every 10 s): the router asks for zone names of
// any network range still missing from the zone information table.
const defaultSendInterval = 10 * time.Second

// SendingService periodically queries for the zones of networks whose zones the zone
// information table does not yet know. It binds no socket — it is a timer-only component.
type SendingService struct {
	rtr      router.ServiceRouter
	interval time.Duration

	mu      sync.Mutex
	running bool
	stop    chan struct{}
	wg      sync.WaitGroup
}

// NewSendingService builds the ZIP sender bound to its router.
func NewSendingService(rtr router.ServiceRouter) *SendingService {
	return &SendingService{rtr: rtr, interval: defaultSendInterval}
}

// Name returns the component name.
func (s *SendingService) Name() string { return SendingName }

// Socket returns 0: the sender binds no socket (timer only).
func (s *SendingService) Socket() uint8 { return 0 }

// Inbound is a no-op: the sender does not receive datagrams.
func (s *SendingService) Inbound(ddp.Datagram, router.RoutedPort) {}

// Start launches the query ticker. Idempotent (§3).
func (s *SendingService) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return nil
	}
	s.running = true
	s.stop = make(chan struct{})
	s.wg.Add(1)
	go s.run(ctx, s.stop)
	return nil
}

// Stop halts the ticker. Safe after a partial Start (§3) and idempotent.
func (s *SendingService) Stop(ctx context.Context) error {
	_ = ctx
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	close(s.stop)
	s.mu.Unlock()
	s.wg.Wait()
	return nil
}

func (s *SendingService) run(ctx context.Context, stop chan struct{}) {
	defer s.wg.Done()
	t := time.NewTicker(s.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-t.C:
			s.queryMissingZones()
		}
	}
}

// queryMissingZones sends a ZIP Query for every routing-table network whose zones are unknown:
// broadcast for a directly-connected network, unicast to the next hop for a learned one.
func (s *SendingService) queryMissingZones() {
	for _, item := range s.rtr.RoutingTable().Entries() {
		e := item.Entry
		z, err := s.rtr.Zones().ZonesInNetworkRange(e.NetworkMin, &e.NetworkMax)
		if err == nil && len(z) > 0 {
			continue // already know this range's zones
		}
		if e.Port == nil || e.Port.Node() == 0 || e.Port.Network() == 0 {
			continue
		}
		data := []byte{FuncQuery, 1, byte(e.NetworkMin >> 8), byte(e.NetworkMin)}
		if e.Distance == 0 {
			e.Port.Broadcast(ddp.Datagram{
				DestNetwork: 0, SrcNetwork: e.Port.Network(), DestNode: 0xFF, SrcNode: e.Port.Node(),
				DestSocket: SAS, SrcSocket: SAS, DDPType: DDPType, Data: data,
			})
		} else {
			e.Port.Unicast(e.NextNetwork, e.NextNode, ddp.Datagram{
				DestNetwork: e.NextNetwork, SrcNetwork: e.Port.Network(), DestNode: e.NextNode, SrcNode: e.Port.Node(),
				DestSocket: SAS, SrcSocket: SAS, DDPType: DDPType, Data: data,
			})
		}
	}
}
