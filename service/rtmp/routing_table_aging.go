package rtmp

import (
	"context"
	"time"

	"github.com/pgodw/omnitalk/protocol/ddp"

	"github.com/pgodw/omnitalk/port"
	"github.com/pgodw/omnitalk/service"
)

type RoutingTableAgingService struct {
	timeout time.Duration
	stop    chan struct{}
}

func NewRoutingTableAgingService() *RoutingTableAgingService {
	return &RoutingTableAgingService{timeout: 20 * time.Second, stop: make(chan struct{})}
}

func (s *RoutingTableAgingService) Start(ctx context.Context, router service.Router) error {
	go func() {
		t := time.NewTicker(s.timeout)
		defer t.Stop()
		for {
			select {
			case <-s.stop:
				return
			case <-t.C:
				router.RoutingTableAge()
			}
		}
	}()
	return nil
}

func (s *RoutingTableAgingService) Stop() error                         { close(s.stop); return nil }
func (s *RoutingTableAgingService) Inbound(_ ddp.Datagram, _ port.Port) {}
