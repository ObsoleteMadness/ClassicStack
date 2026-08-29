package rtmp

import (
	"context"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
	"github.com/ObsoleteMadness/ClassicStack/core/router"
)

// Service is the composed RTMP component: the responder (socket 1), the periodic
// sender, and the routing-table ager, supervised as ONE unit. It is the single
// component the compose registry builds and the supervisor lists — its three
// sub-services have no independent lifecycle. It satisfies router.Service by
// delegating socket dispatch to the responder (the only sub-service that binds a
// socket), so the runtime's crossWireRouter registers RTMP's socket when it registers
// this component; the sender/ager are timer-only and need only Start/Stop.
type Service struct {
	responding *RespondingService
	sending    *SendingService
	aging      *AgingService
}

// New builds the composed RTMP service bound to its router.
func New(rtr router.ServiceRouter, logger log.Logger) *Service {
	return &Service{
		responding: NewRespondingService(rtr, logger),
		sending:    NewSendingService(rtr),
		aging:      NewAgingService(rtr),
	}
}

// Name returns the RTMP component name (the responder's well-known name).
func (s *Service) Name() string { return RespondingName }

// Kind labels RTMP a routing service for the dashboard.
func (s *Service) Kind() string { return "routing" }

// Props surfaces nothing beyond the defaults today; present so a future view can add
// per-service detail without widening the component contract.
func (s *Service) Props() map[string]string { return nil }

// Socket delegates to the responder — the only RTMP sub-service that binds a socket.
func (s *Service) Socket() uint8 { return s.responding.Socket() }

// Inbound delivers a datagram to the responder.
func (s *Service) Inbound(d ddp.Datagram, from router.RoutedPort) { s.responding.Inbound(d, from) }

// Start brings all three sub-services up. Idempotent (each sub-Start is). On a
// sub-failure it stops the ones already started so a partial Start leaves nothing
// running (§3).
func (s *Service) Start(ctx context.Context) error {
	if err := s.responding.Start(ctx); err != nil {
		return err
	}
	if err := s.sending.Start(ctx); err != nil {
		_ = s.responding.Stop(ctx)
		return err
	}
	if err := s.aging.Start(ctx); err != nil {
		_ = s.sending.Stop(ctx)
		_ = s.responding.Stop(ctx)
		return err
	}
	return nil
}

// Stop halts all three sub-services (reverse start order). Safe after a partial Start.
func (s *Service) Stop(ctx context.Context) error {
	_ = s.aging.Stop(ctx)
	_ = s.sending.Stop(ctx)
	return s.responding.Stop(ctx)
}

var (
	_ router.Service        = (*Service)(nil)
	_ component.Component   = (*Service)(nil)
	_ component.Describable = (*Service)(nil)
)
