// Package aep implements the AppleTalk Echo Protocol as a core router service.
//
// AEP uses DDP type 4 on socket 4 (Inside Macintosh: Networking, Chapter 3). An echo request
// (command byte 1) is reflected back to the sender as an echo reply (command byte 2).
//
// Ring: CORE (stdlib only). The router is injected at construction; the service rides it as a
// router.Service (lifecycle + socket dispatch).
package aep

import (
	"context"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
	"github.com/ObsoleteMadness/ClassicStack/core/router"
)

const (
	// Socket is the statically-assigned AEP socket number.
	Socket = 4
	// DDPType is the DDP packet type for AEP packets.
	DDPType = 4
	// CmdRequest is the AEP command byte for an echo request.
	CmdRequest = 1
	// CmdReply is the AEP command byte for an echo reply.
	CmdReply = 2
)

// Name is the component/section key for the AEP service.
const Name = "AEP"

type item struct {
	d    ddp.Datagram
	from router.RoutedPort
}

// Service is the AEP responder. It queues inbound datagrams and reflects echo requests on a
// worker goroutine so the router's read path never blocks.
type Service struct {
	rtr    router.ServiceRouter
	logger log.Logger

	mu      sync.Mutex
	running bool
	ch      chan item
	stop    chan struct{}
	wg      sync.WaitGroup
}

// New builds an AEP service bound to the router it replies through.
func New(rtr router.ServiceRouter, logger log.Logger) *Service {
	return &Service{rtr: rtr, logger: logger}
}

// Name returns the component name.
func (s *Service) Name() string { return Name }

// Socket returns the AEP socket so the router dispatches AEP datagrams here.
func (s *Service) Socket() uint8 { return Socket }

// Start launches the responder goroutine. Idempotent (§3).
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return nil
	}
	s.running = true
	s.ch = make(chan item, 64)
	s.stop = make(chan struct{})
	s.wg.Add(1)
	go s.run(ctx, s.ch, s.stop)
	return nil
}

// Stop shuts the responder down. Safe after a partial Start (§3) and idempotent.
func (s *Service) Stop(ctx context.Context) error {
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

// Inbound queues a datagram for the responder; a full queue drops (echo is best-effort).
func (s *Service) Inbound(d ddp.Datagram, from router.RoutedPort) {
	s.mu.Lock()
	ch := s.ch
	running := s.running
	s.mu.Unlock()
	if !running {
		return
	}
	select {
	case ch <- item{d: d, from: from}:
	default:
	}
}

func (s *Service) run(ctx context.Context, ch chan item, stop chan struct{}) {
	defer s.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case it := <-ch:
			d := it.d
			if d.DDPType != DDPType || len(d.Data) == 0 || d.Data[0] != CmdRequest {
				continue
			}
			reply := append([]byte{CmdReply}, d.Data[1:]...)
			s.rtr.Reply(d, it.from, DDPType, reply)
		}
	}
}

// Dependencies declares AEP's start-order edge: the AppleTalk router must be running
// first (AEP is a DDP echo service). Drops in a no-router build.
func (s *Service) Dependencies() []string { return []string{router.Name} }

// compile-time assertions.
var (
	_ router.Service      = (*Service)(nil)
	_ component.Component = (*Service)(nil)
	_ component.DependsOn = (*Service)(nil)
)
