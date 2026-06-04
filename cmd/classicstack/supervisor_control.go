package main

import (
	"context"
	"fmt"

	"github.com/ObsoleteMadness/ClassicStack/config"
	"github.com/ObsoleteMadness/ClassicStack/netlog"
	"github.com/ObsoleteMadness/ClassicStack/pkg/control"
	"github.com/ObsoleteMadness/ClassicStack/port/rawlink"
)

// The methods here adapt the Supervisor to the control.Supervisor
// interface the management plane drives. RestartService is already
// implemented in supervisor_lifecycle.go.

// Apply re-wires the running stack to match the supplied config model. For
// now this is an atomic whole-stack rebuild: the entire stack is stopped,
// reconstructed from the new model, and started again. Finer-grained
// per-service application can layer on later using the dynamic-router
// primitives; the control-plane contract (and the UI) is unchanged by that
// evolution.
func (s *Supervisor) Apply(ctx context.Context, cfg control.ConfigModel) error {
	model, ok := cfg.(*config.Model)
	if !ok {
		return fmt.Errorf("supervisor: unexpected config type %T", cfg)
	}

	newCfg, err := appConfigFromModel(model)
	if err != nil {
		return fmt.Errorf("supervisor: invalid config: %w", err)
	}

	netlog.Info("[SUP] applying new configuration (atomic rebuild)")
	if err := s.Stop(); err != nil {
		netlog.Warn("[SUP] stop during apply: %v", err)
	}

	// Rebuild a fresh supervisor state from the new model, then graft its
	// freshly constructed components onto this instance so the control
	// plane keeps pointing at the same Supervisor.
	rebuilt, err := NewSupervisor(newCfg, s.source, model)
	if err != nil {
		return fmt.Errorf("supervisor: rebuild failed: %w", err)
	}
	s.adoptFrom(rebuilt)

	if err := s.Start(ctx); err != nil {
		return fmt.Errorf("supervisor: restart failed: %w", err)
	}
	netlog.Info("[SUP] configuration applied")
	return nil
}

// adoptFrom replaces this supervisor's built components with those from a
// freshly constructed one (used by Apply after Stop). The caller must hold
// no locks; Apply runs Stop/Start which lock internally.
func (s *Supervisor) adoptFrom(other *Supervisor) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = other.cfg
	s.model = other.model
	s.router = other.router
	s.ports = other.ports
	s.portNames = other.portNames
	s.hooks = other.hooks
	s.order = other.order
	s.captureSinks = other.captureSinks
	s.nbp = other.nbp
	s.shortHook = other.shortHook
	s.macIP = other.macIP
	s.ipxGW = other.ipxGW
	s.started = false
}

// ListInterfaces returns the host's network interface names for the UI
// dropdowns.
func (s *Supervisor) ListInterfaces() ([]string, error) {
	return rawlink.InterfaceNames()
}
