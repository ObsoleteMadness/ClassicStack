package zip

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
				for _, item := range r.RoutingEntries() {
					e := item.Entry
					z, err := r.ZonesInNetworkRange(e.NetworkMin, &e.NetworkMax)
					if err == nil && len(z) > 0 {
						continue
					}
					if e.Port.Node() == 0 || e.Port.Network() == 0 {
						continue
					}
					data := []byte{FuncQuery, 1, byte(e.NetworkMin >> 8), byte(e.NetworkMin)}
					if e.Distance == 0 {
						e.Port.Broadcast(appletalk.Datagram{
							DestinationNetwork: 0, SourceNetwork: e.Port.Network(), DestinationNode: 0xFF, SourceNode: e.Port.Node(),
							DestinationSocket: SAS, SourceSocket: SAS, DDPType: DDPType, Data: data,
						})
					} else {
						e.Port.Unicast(e.NextNetwork, e.NextNode, appletalk.Datagram{
							DestinationNetwork: e.NextNetwork, SourceNetwork: e.Port.Network(), DestinationNode: e.NextNode, SourceNode: e.Port.Node(),
							DestinationSocket: SAS, SourceSocket: SAS, DDPType: DDPType, Data: data,
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
