package rtmp

import (
	"time"

	"github.com/pgodw/omnitalk/go/appletalk"
	"github.com/pgodw/omnitalk/go/port"
	"github.com/pgodw/omnitalk/go/service"
)

type RoutingTableAgingService struct {
	timeout time.Duration
	stop    chan struct{}
}

func NewRoutingTableAgingService() *RoutingTableAgingService {
	return &RoutingTableAgingService{timeout: 20 * time.Second, stop: make(chan struct{})}
}

func (s *RoutingTableAgingService) Start(router service.Router) error {
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

func (s *RoutingTableAgingService) Stop() error                               { close(s.stop); return nil }
func (s *RoutingTableAgingService) Inbound(_ appletalk.Datagram, _ port.Port) {}
