package rtmp

import (
	"time"

	"github.com/pgodw/omnitalk/appletalk"
	"github.com/pgodw/omnitalk/port"
	"github.com/pgodw/omnitalk/service"
)

type SendingService struct {
	timeout time.Duration
	stop    chan struct{}
}

func NewSendingService() *SendingService {
	return &SendingService{timeout: 10 * time.Second, stop: make(chan struct{})}
}

func (s *SendingService) Start(r service.Router) error {
	go func() {
		t := time.NewTicker(s.timeout)
		defer t.Stop()
		for {
			select {
			case <-s.stop:
				return
			case <-t.C:
				for _, p := range r.PortsList() {
					if p.Node() == 0 || p.Network() == 0 {
						continue
					}
					for _, data := range makeRoutingTableDatagramData(r, p, true) {
						p.Broadcast(appletalk.Datagram{
							DestinationNetwork: 0, SourceNetwork: p.Network(), DestinationNode: 0xFF, SourceNode: p.Node(),
							DestinationSocket: SAS, SourceSocket: SAS, DDPType: DDPTypeData, Data: data,
						})
					}
				}
			}
		}
	}()
	return nil
}

func (s *SendingService) Stop() error                               { close(s.stop); return nil }
func (s *SendingService) Inbound(_ appletalk.Datagram, _ port.Port) {}
