package rtmp

import (
	"context"
	"sync"
	"time"

	"github.com/pgodw/omnitalk/protocol/ddp"

	"github.com/pgodw/omnitalk/port"
	"github.com/pgodw/omnitalk/service"
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
				for _, p := range r.PortsList() {
					if p.Node() == 0 || p.Network() == 0 {
						continue
					}
					for _, data := range makeRoutingTableDatagramData(r, p, true) {
						p.Broadcast(ddp.Datagram{
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

func (s *SendingService) Stop() error {
	close(s.stop)
	s.wg.Wait()
	return nil
}

func (s *SendingService) Inbound(_ ddp.Datagram, _ port.Port) {}
