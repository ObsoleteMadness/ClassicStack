package main

import (
	"context"
	"fmt"

	"github.com/ObsoleteMadness/ClassicStack/netlog"
)

// Start brings the whole stack up: the AppleTalk router (ports + DDP
// services) first, then the standalone hooks in registration order
// (transports before the layers that consume them).
func (s *Supervisor) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return fmt.Errorf("supervisor already started")
	}

	if err := s.router.Start(ctx); err != nil {
		return fmt.Errorf("router start: %w", err)
	}
	netlog.Info("[SUP] router away!")
	s.reg.SetRunning("Router", true)
	s.markServiceRunning(true)
	for _, name := range s.portNames {
		s.reg.SetRunning(name, true)
	}

	s.ctx = ctx
	for _, name := range s.order {
		if s.alreadyRunning[name] {
			// Preserved across an Apply rebuild (e.g. the Web UI); it is
			// already serving, so do not restart it.
			s.reg.SetRunning(name, true)
			continue
		}
		if err := s.startHookLocked(ctx, name); err != nil {
			netlog.Warn("[SUP][%s] start failed: %v", name, err)
		}
	}
	s.alreadyRunning = nil
	s.started = true
	return nil
}

// Stop tears the stack down in reverse order: hooks first (reverse of
// start), then the router.
func (s *Supervisor) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started {
		return nil
	}
	for i := len(s.order) - 1; i >= 0; i-- {
		name := s.order[i]
		s.stopHookLocked(name)
	}
	if err := s.router.Stop(); err != nil {
		netlog.Warn("[SUP] router stop warning: %v", err)
	}
	s.reg.SetRunning("Router", false)
	s.markServiceRunning(false)
	for _, name := range s.portNames {
		s.reg.SetRunning(name, false)
	}
	s.closeSinks()
	s.started = false
	return nil
}

// StartService starts a single named hook (and, transitively, nothing — its
// dependencies are expected to already be running). It is the UI's "start"
// action.
func (s *Supervisor) StartService(ctx context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.hooks[name]; !ok {
		return fmt.Errorf("unknown service %q", name)
	}
	return s.startHookLocked(ctx, name)
}

// StopService stops a single named hook and any hooks that depend on it
// (e.g. stopping NetBIOS first stops SMB). It is the UI's "stop" action.
func (s *Supervisor) StopService(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.hooks[name]; !ok {
		return fmt.Errorf("unknown service %q", name)
	}
	// Stop dependents first.
	for _, dep := range s.dependentsOf(name) {
		s.stopHookLocked(dep)
	}
	s.stopHookLocked(name)
	return nil
}

// RestartService stops then starts a named hook, restarting its dependents
// around it so they re-attach to the freshly started instance.
func (s *Supervisor) RestartService(ctx context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.hooks[name]; !ok {
		return fmt.Errorf("unknown service %q", name)
	}
	deps := s.dependentsOf(name)
	// Stop dependents (reverse) then the target.
	for i := len(deps) - 1; i >= 0; i-- {
		s.stopHookLocked(deps[i])
	}
	s.stopHookLocked(name)
	// Start the target then its dependents.
	if err := s.startHookLocked(ctx, name); err != nil {
		return err
	}
	for _, dep := range deps {
		if err := s.startHookLocked(ctx, dep); err != nil {
			netlog.Warn("[SUP][%s] dependent start failed: %v", dep, err)
		}
	}
	return nil
}

func (s *Supervisor) startHookLocked(ctx context.Context, name string) error {
	h := s.hooks[name]
	if h == nil {
		return nil
	}
	if err := h.Start(ctx); err != nil {
		return err
	}
	s.reg.SetRunning(name, true)
	netlog.Info("[SUP][%s] started", name)
	return nil
}

func (s *Supervisor) stopHookLocked(name string) {
	h := s.hooks[name]
	if h == nil {
		return
	}
	if err := h.Stop(); err != nil {
		netlog.Warn("[SUP][%s] stop warning: %v", name, err)
	}
	s.reg.SetRunning(name, false)
	netlog.Info("[SUP][%s] stopped", name)
}

// dependentsOf returns the hooks that declare name in their DependsOn,
// transitively, in start order.
func (s *Supervisor) dependentsOf(name string) []string {
	var out []string
	for _, candidate := range s.order {
		if candidate == name {
			continue
		}
		for _, u := range s.reg.Snapshot() {
			if u.Name != candidate {
				continue
			}
			for _, dep := range u.DependsOn {
				if dep == name {
					out = append(out, candidate)
				}
			}
		}
	}
	return out
}

// markServiceRunning flips the running flag on the DDP service units that
// live inside the router set (they share the router's lifecycle).
func (s *Supervisor) markServiceRunning(running bool) {
	for _, name := range []string{"MacIP", "IPXGW", "AFP"} {
		s.reg.SetRunning(name, running)
	}
}
