package zip

import (
	"context"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/protocol/ddp"

	"github.com/ObsoleteMadness/ClassicStack/port"
	"github.com/ObsoleteMadness/ClassicStack/service"
)

type SendingService struct {
	timeout time.Duration
	stop    chan struct{}
	wg      sync.WaitGroup
}

func NewSendingService() *SendingService {
	return &SendingService{timeout: 10 * time.Second, stop: make(chan struct{})}
}

func (s *SendingService) Start(ctx context.Context, r service.Router) error {
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
						e.Port.Broadcast(ddp.Datagram{
							DestinationNetwork: 0, SourceNetwork: e.Port.Network(), DestinationNode: 0xFF, SourceNode: e.Port.Node(),
							DestinationSocket: SAS, SourceSocket: SAS, DDPType: DDPType, Data: data,
						})
					} else {
						e.Port.Unicast(e.NextNetwork, e.NextNode, ddp.Datagram{
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

func (s *SendingService) Stop() error {
	close(s.stop)
	s.wg.Wait()
	return nil
}

func (s *SendingService) Inbound(_ ddp.Datagram, _ port.Port) {}
