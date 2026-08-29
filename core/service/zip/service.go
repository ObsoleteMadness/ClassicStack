package zip

import (
	"context"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
	"github.com/ObsoleteMadness/ClassicStack/core/router"
)

// Service is the composed ZIP component: the responder (socket 6) answering ZIP
// queries / GetNetInfo / the ATP-carried zone queries, and the sender that asks for
// zones of newly-learned networks. Supervised as ONE unit (the sub-services have no
// independent lifecycle). It satisfies router.Service by delegating socket dispatch to
// the responder, so the runtime's crossWireRouter binds ZIP's socket when it registers
// this component; the sender is timer-only.
type Service struct {
	responding *RespondingService
	sending    *SendingService
}

// New builds the composed ZIP service bound to its router.
func New(rtr router.ServiceRouter, logger log.Logger) *Service {
	return &Service{
		responding: NewRespondingService(rtr, logger),
		sending:    NewSendingService(rtr),
	}
}

// Name returns the ZIP component name (the responder's well-known name).
func (s *Service) Name() string { return RespondingName }

// Kind labels ZIP a routing service for the dashboard.
func (s *Service) Kind() string { return "routing" }

// Props surfaces nothing beyond the defaults today.
func (s *Service) Props() map[string]string { return nil }

// Socket delegates to the responder (the sender binds none).
func (s *Service) Socket() uint8 { return s.responding.Socket() }

// Inbound delivers a datagram to the responder.
func (s *Service) Inbound(d ddp.Datagram, from router.RoutedPort) { s.responding.Inbound(d, from) }

// Start brings both sub-services up; a sub-failure stops the one already started.
func (s *Service) Start(ctx context.Context) error {
	if err := s.responding.Start(ctx); err != nil {
		return err
	}
	if err := s.sending.Start(ctx); err != nil {
		_ = s.responding.Stop(ctx)
		return err
	}
	return nil
}

// Stop halts both sub-services (reverse start order). Safe after a partial Start.
func (s *Service) Stop(ctx context.Context) error {
	_ = s.sending.Stop(ctx)
	return s.responding.Stop(ctx)
}

var (
	_ router.Service        = (*Service)(nil)
	_ component.Component   = (*Service)(nil)
	_ component.Describable = (*Service)(nil)
)
