package rtmp

import (
	"context"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/protocol/ddp"

	"github.com/ObsoleteMadness/ClassicStack/port"
	"github.com/ObsoleteMadness/ClassicStack/service"
)

type RoutingTableAgingService struct {
	timeout time.Duration
	stop    chan struct{}
	wg      sync.WaitGroup
}

func NewRoutingTableAgingService() *RoutingTableAgingService {
	return &RoutingTableAgingService{timeout: 20 * time.Second, stop: make(chan struct{})}
}

func (s *RoutingTableAgingService) Start(ctx context.Context, router service.Router) error {
	// Narrow to RouteIndex inside the goroutine so the type signature
	// documents the only capability this loop touches.
	idx := service.RouteIndex(router)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		t := time.NewTicker(s.timeout)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.stop:
				return
			case <-t.C:
				idx.RoutingTableAge()
			}
		}
	}()
	return nil
}

func (s *RoutingTableAgingService) Stop() error {
	close(s.stop)
	s.wg.Wait()
	return nil
}
func (s *RoutingTableAgingService) Inbound(_ ddp.Datagram, _ port.Port) {}
