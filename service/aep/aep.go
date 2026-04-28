/*
Package aep implements the AppleTalk Echo Protocol (AEP) as a omnitalk service.

AEP uses DDP type 4 on socket 4. An echo request (command byte 1) is reflected
back to the sender as an echo reply (command byte 2).

Inside Macintosh: Networking, Chapter 3.
*/
package aep

import (
	"context"

	"github.com/pgodw/omnitalk/protocol/aep"
	"github.com/pgodw/omnitalk/protocol/ddp"

	"github.com/pgodw/omnitalk/port"
	"github.com/pgodw/omnitalk/service"
)

// Socket is the well-known AEP socket number, re-exported from protocol/aep
// for callers wiring a router.
const Socket = aep.Socket

const (
	ddpTypeAEP = aep.DDPType
	cmdRequest = aep.CmdRequest
	cmdReply   = aep.CmdReply
)

// Service implements the AppleTalk Echo Protocol.
type Service struct {
	ch   chan item
	stop chan struct{}
}

type item struct {
	d ddp.Datagram
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
func (s *Service) Start(ctx context.Context, router service.Router) error {
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
func (s *Service) Inbound(d ddp.Datagram, p port.Port) {
	select {
	case s.ch <- item{d, p}:
	default:
	}
}
