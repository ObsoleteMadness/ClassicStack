/*
Package aep implements the AppleTalk Echo Protocol (AEP) as a omnitalk service.

AEP uses DDP type 4 on socket 4. An echo request (command byte 1) is reflected
back to the sender as an echo reply (command byte 2).

Inside Macintosh: Networking, Chapter 3.
*/
package aep

import (
	"github.com/pgodw/omnitalk/go/appletalk"
	"github.com/pgodw/omnitalk/go/port"
	"github.com/pgodw/omnitalk/go/service"
)

const (
	// Socket is the well-known AEP socket number.
	Socket     = 4
	ddpTypeAEP = 4
	cmdRequest = 1
	cmdReply   = 2
)

// Service implements the AppleTalk Echo Protocol.
type Service struct {
	ch   chan item
	stop chan struct{}
}

type item struct {
	d appletalk.Datagram
	p port.Port
}

// New creates an AEP service.
func New() *Service {
	return &Service{
		ch:   make(chan item, 64),
		stop: make(chan struct{}),
	}
}

// Skt returns the socket number this service listens on.
func (s *Service) Socket() uint8 { return Socket }

// Start launches the AEP processing goroutine.
func (s *Service) Start(router service.Router) error {
	go func() {
		for {
			select {
			case <-s.stop:
				return
			case it := <-s.ch:
				d := it.d
				if d.DDPType != ddpTypeAEP || len(d.Data) == 0 || d.Data[0] != cmdRequest {
					continue
				}
				reply := append([]byte{cmdReply}, d.Data[1:]...)
				router.Reply(d, it.p, ddpTypeAEP, reply)
			}
		}
	}()
	return nil
}

// Stop shuts down the AEP service.
func (s *Service) Stop() error {
	close(s.stop)
	return nil
}

// Inbound queues an incoming datagram for processing.
func (s *Service) Inbound(d appletalk.Datagram, p port.Port) {
	select {
	case s.ch <- item{d, p}:
	default:
	}
}
