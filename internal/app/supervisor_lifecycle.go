package app

import (
	"context"
	"fmt"
	"time"

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

	// Periodically refresh live dashboard counts (MacIP leases/sessions),
	// which change at runtime and so cannot be captured once at wire time.
	if s.macIP != nil {
		stop := make(chan struct{})
		s.statusTickerStop = stop
		go s.runStatusRefresh(stop)
	}
	return nil
}

// runStatusRefresh re-publishes time-varying dashboard status (MacIP live
// counts) on a fixed cadence until stop is closed. It does not hold s.mu — it
// reads stable post-Start fields and the independently-locked status registry.
func (s *Supervisor) runStatusRefresh(stop chan struct{}) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			s.refreshMacIPStatus()
		}
	}
}

// Stop tears the stack down in reverse order: hooks first (reverse of
// start), then the router.
func (s *Supervisor) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started {
		return nil
	}
	if s.statusTickerStop != nil {
		close(s.statusTickerStop)
		s.statusTickerStop = nil
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
	if err := s.startHookLocked(ctx, name); err != nil {
		return err
	}
	// If this hook is a transport provider (IPX/NetBEUI), re-attach its
	// bindings into the higher layers (NetBIOS transports, SMB direct-IPX)
	// now that its protocol is freshly started.
	s.attachTransportBindings(name)
	return nil
}

// StopService stops a single named hook and any hooks that depend on it
// (e.g. stopping NetBIOS first stops SMB). It is the UI's "stop" action.
func (s *Supervisor) StopService(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.hooks[name]; !ok {
		return fmt.Errorf("unknown service %q", name)
	}
	// Detach this hook's transport bindings before stopping it, so the
	// bound transport releases its port/sockets while they are still open —
	// and the higher layer (NetBIOS/SMB) keeps running on its remaining
	// bindings instead of being torn down.
	s.detachTransportBindings(name)
	// Stop dependents first.
	for _, dep := range s.dependentsOf(name) {
		s.stopHookLocked(dep)
	}
	s.stopHookLocked(name)
	return nil
}

// attachTransportBindings re-establishes the bindings the named transport hook
// contributes to higher layers and refreshes the affected units' status.
func (s *Supervisor) attachTransportBindings(name string) {
	bindings := s.transportBindings[name]
	owners := map[string]bool{}
	for _, b := range bindings {
		if b.attach != nil {
			if err := b.attach(); err != nil {
				netlog.Warn("[SUP][%s] attach binding to %s: %v", name, b.owner, err)
			}
		}
		owners[b.owner] = true
	}
	s.refreshBindingOwners(owners)
}

// detachTransportBindings tears down the bindings the named transport hook
// contributes and refreshes the affected units' status.
func (s *Supervisor) detachTransportBindings(name string) {
	bindings := s.transportBindings[name]
	owners := map[string]bool{}
	for _, b := range bindings {
		if b.detach != nil {
			b.detach()
		}
		owners[b.owner] = true
	}
	s.refreshBindingOwners(owners)
}

// refreshBindingOwners re-publishes status for the layers whose bindings just
// changed, so the dashboard reflects the current transport set.
func (s *Supervisor) refreshBindingOwners(owners map[string]bool) {
	if owners["NetBIOS"] {
		s.refreshNetBIOSStatus(s.cfg.NetBIOSEnabled)
	}
	if owners["NetBIOS"] || owners["SMB"] {
		// SMB's transport summary derives from NetBIOS's transports too.
		s.refreshSMBStatus()
	}
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
	// Stop dependents (reverse) then the target, detaching the target's
	// transport bindings first so its bound transports release cleanly.
	for i := len(deps) - 1; i >= 0; i-- {
		s.stopHookLocked(deps[i])
	}
	s.detachTransportBindings(name)
	s.stopHookLocked(name)
	// Start the target, re-attach its bindings, then its dependents.
	if err := s.startHookLocked(ctx, name); err != nil {
		return err
	}
	s.attachTransportBindings(name)
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
	s.onHookStateChanged(name)
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
	s.onHookStateChanged(name)
	netlog.Info("[SUP][%s] stopped", name)
}

// onHookStateChanged refreshes the transport-summary status of layers whose
// displayed bindings depend on the hook that just started/stopped. NetBIOS
// (de)activating changes both its own transport list and SMB's served set;
// IPX/NetBEUI are handled via their transport bindings, but their running flag
// also affects the summaries, so refresh on those too.
func (s *Supervisor) onHookStateChanged(name string) {
	switch name {
	case "NetBIOS":
		s.refreshNetBIOSStatus(s.cfg.NetBIOSEnabled)
		s.refreshSMBStatus()
	case "IPX", "NetBEUI":
		s.refreshNetBIOSStatus(s.cfg.NetBIOSEnabled)
		s.refreshSMBStatus()
	}
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
