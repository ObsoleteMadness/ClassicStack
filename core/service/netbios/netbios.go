package netbios

import (
	"context"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
)

// Name is the component name for the NetBIOS service.
const Name = "NetBIOS"

// Service is the placeholder NetBIOS component.
type Service struct {
	mu      sync.Mutex
	running bool
	logger  log.Logger
}

// New builds the Phase 1 NetBIOS placeholder service.
func New(logger log.Logger) *Service {
	return &Service{logger: logger}
}

// Name returns the component name.
func (s *Service) Name() string { return Name }

// Start brings the placeholder service up. Idempotent (§3).
func (s *Service) Start(ctx context.Context) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return nil
	}
	s.running = true
	s.logf("NetBIOS service started (protocol not implemented)")
	return nil
}

// Stop brings the placeholder service down. Safe after failed/partial Start (§3).
func (s *Service) Stop(ctx context.Context) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return nil
	}
	s.running = false
	s.logf("NetBIOS service stopped")
	return nil
}

// logf emits one info line through the logger if configured.
func (s *Service) logf(msg string) {
	if s.logger == nil || !s.logger.Enabled(log.Info) {
		return
	}
	s.logger.Log1(log.Info, msg, log.Str("scope", Name))
}

// compile-time assertions.
var (
	_ component.Component = (*Service)(nil)
)
