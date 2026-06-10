package rtmp

import (
	"context"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
	"github.com/ObsoleteMadness/ClassicStack/core/router"
)

// AgingName is the component/section key for the RTMP aging service.
const AgingName = "RTMP-Age"

// defaultAgeInterval is the routing-table aging period (every 20 s, per Inside Macintosh: a
// learned route survives a few missed advertisements before being aged out).
const defaultAgeInterval = 20 * time.Second

// AgingService ticks the routing table's RTMP aging machine, walking learned routes
// Good→Suspect→Bad→Worst→removed. It binds no socket.
type AgingService struct {
	rtr      router.ServiceRouter
	interval time.Duration

	mu      sync.Mutex
	running bool
	stop    chan struct{}
	wg      sync.WaitGroup
}

// NewAgingService builds the RTMP aging service bound to its router.
func NewAgingService(rtr router.ServiceRouter) *AgingService {
	return &AgingService{rtr: rtr, interval: defaultAgeInterval}
}

// Name returns the component name.
func (s *AgingService) Name() string { return AgingName }

// Socket returns 0: the ager binds no socket (timer only).
func (s *AgingService) Socket() uint8 { return 0 }

// Inbound is a no-op: the ager does not receive datagrams.
func (s *AgingService) Inbound(ddp.Datagram, router.RoutedPort) {}

// Start launches the aging ticker. Idempotent (§3).
func (s *AgingService) Start(ctx context.Context) error {
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
func (s *AgingService) Stop(ctx context.Context) error {
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

func (s *AgingService) run(ctx context.Context, stop chan struct{}) {
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
			s.rtr.RoutingTable().Age()
		}
	}
}
