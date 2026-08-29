package rtmp

import (
	"context"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
	"github.com/ObsoleteMadness/ClassicStack/core/router"
)

// SendingName is the component/section key for the RTMP sending service.
const SendingName = "RTMP-Send"

// defaultSendInterval is the RTMP advertisement period (every 10 s, per Inside Macintosh).
const defaultSendInterval = 10 * time.Second

// SendingService periodically broadcasts the routing table out every attached, addressed port
// (split-horizon applied). It binds no socket — it is a timer-only component.
type SendingService struct {
	rtr      router.ServiceRouter
	interval time.Duration

	mu      sync.Mutex
	running bool
	stop    chan struct{}
	wg      sync.WaitGroup
}

// NewSendingService builds the RTMP sender bound to its router.
func NewSendingService(rtr router.ServiceRouter) *SendingService {
	return &SendingService{rtr: rtr, interval: defaultSendInterval}
}

// Name returns the component name.
func (s *SendingService) Name() string { return SendingName }

// Socket returns 0: the sender binds no socket (timer only).
func (s *SendingService) Socket() uint8 { return 0 }

// Inbound is a no-op: the sender does not receive datagrams.
func (s *SendingService) Inbound(ddp.Datagram, router.RoutedPort) {}

// Start launches the advertisement ticker. Idempotent (§3).
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
			s.advertise()
		}
	}
}

// advertise broadcasts the routing table on every addressed port.
func (s *SendingService) advertise() {
	for _, p := range s.rtr.Ports() {
		if p.Node() == 0 || p.Network() == 0 {
			continue
		}
		for _, data := range makeRoutingTableDatagramData(s.rtr, p, true) {
			p.Broadcast(ddp.Datagram{
				DestNetwork: 0, SrcNetwork: p.Network(), DestNode: 0xFF, SrcNode: p.Node(),
				DestSocket: SAS, SrcSocket: SAS, DDPType: DDPTypeData, Data: data,
			})
		}
	}
}
