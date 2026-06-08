package supervisor

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/control"
)

// State labels published on the telemetry bus via StateChanged (§3/§11). They are the To/From
// values of a transition; "running"/"stopped" are the stable states.
const (
	stateStopped      = "stopped"
	stateRunning      = "running"
	stateReconfigured = "reconfigured"
)

// ErrUnknownComponent is returned by per-name operations addressing a component the supervisor
// does not own.
var ErrUnknownComponent = errors.New("supervisor: unknown component")

// node is one managed component plus its hard dependency edges and current run state.
type node struct {
	c         component.Component
	dependsOn []string // names that must be running before this starts (and stop after it)
	running   bool
}

// Supervisor owns the component set + dependency DAG. It starts components in dependency order
// and stops them in reverse, publishing StateChanged on every transition (§3/§11). It
// implements control.Supervisor (B10).
//
// Whole-stack lifecycle is StartAll/StopAll; the per-name Start/Stop/Restart/Reconfigure are the
// control-plane surface (control.Supervisor), driven by the UI.
type Supervisor struct {
	mu        sync.Mutex
	model     *config.Model
	telemetry bus.Bus
	nodes     map[string]*node
	order     []string // insertion order, the tie-breaker in topo sort
}

// compile-time assertion: Supervisor satisfies the control plane's lifecycle surface.
var _ control.Supervisor = (*Supervisor)(nil)

// New builds an empty supervisor bound to the shared model and telemetry bus.
func New(m *config.Model, telemetry bus.Bus) *Supervisor {
	return &Supervisor{
		model:     m,
		telemetry: telemetry,
		nodes:     make(map[string]*node),
	}
}

// Add registers a component with its hard dependencies (DAG edges). dependsOn are component
// names that must be running before this one starts (and stop after it). Soft bindings use
// component.Attachable instead (§11d), NOT dependsOn. Re-adding a name replaces the prior node.
func (s *Supervisor) Add(c component.Component, dependsOn []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name := c.Name()
	if _, exists := s.nodes[name]; !exists {
		s.order = append(s.order, name)
	}
	edges := append([]string(nil), dependsOn...)
	s.nodes[name] = &node{c: c, dependsOn: edges}
}

// StartAll brings every component up in dependency order (topological), publishing a
// StateChanged{stopped->running} per component as it starts.
func (s *Supervisor) StartAll(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	order, err := s.topoOrder()
	if err != nil {
		return err
	}
	for _, name := range order {
		if err := s.startNodeLocked(ctx, name); err != nil {
			return err
		}
	}
	return nil
}

// StopAll brings every running component down in reverse dependency order, publishing a
// StateChanged{running->stopped} per component. It attempts every Stop and returns the first
// error encountered, so a single failing Stop never strands the rest.
func (s *Supervisor) StopAll(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	order, err := s.topoOrder()
	if err != nil {
		return err
	}
	var firstErr error
	for i := len(order) - 1; i >= 0; i-- {
		if err := s.stopNodeLocked(ctx, order[i]); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// startNodeLocked starts one node (idempotent) and publishes its transition. Caller holds mu.
func (s *Supervisor) startNodeLocked(ctx context.Context, name string) error {
	n := s.nodes[name]
	if n == nil {
		return fmt.Errorf("%w: %s", ErrUnknownComponent, name)
	}
	if n.running {
		return nil
	}
	if err := n.c.Start(ctx); err != nil {
		return fmt.Errorf("start %s: %w", name, err)
	}
	n.running = true
	s.publish(name, stateStopped, stateRunning)
	return nil
}

// stopNodeLocked stops one node (safe if already stopped) and publishes its transition.
func (s *Supervisor) stopNodeLocked(ctx context.Context, name string) error {
	n := s.nodes[name]
	if n == nil {
		return fmt.Errorf("%w: %s", ErrUnknownComponent, name)
	}
	if !n.running {
		return nil
	}
	err := n.c.Stop(ctx)
	n.running = false
	s.publish(name, stateRunning, stateStopped)
	if err != nil {
		return fmt.Errorf("stop %s: %w", name, err)
	}
	return nil
}

// publish emits a StateChanged on the telemetry bus, if one is configured.
func (s *Supervisor) publish(name, from, to string) {
	if s.telemetry == nil {
		return
	}
	s.telemetry.Publish(bus.StateChanged{Component: name, From: from, To: to})
}

// topoOrder returns component names in dependency order (a dependency precedes its dependents).
// Ties break on insertion order for determinism. Returns an error on a missing edge target or
// a dependency cycle.
func (s *Supervisor) topoOrder() ([]string, error) {
	const (
		white = 0 // unvisited
		grey  = 1 // on the current DFS stack (cycle detection)
		black = 2 // finished
	)
	color := make(map[string]int, len(s.nodes))
	var out []string
	var visit func(name string) error
	visit = func(name string) error {
		switch color[name] {
		case black:
			return nil
		case grey:
			return fmt.Errorf("supervisor: dependency cycle at %s", name)
		}
		n := s.nodes[name]
		if n == nil {
			return fmt.Errorf("%w: %s", ErrUnknownComponent, name)
		}
		color[name] = grey
		deps := append([]string(nil), n.dependsOn...)
		sort.Strings(deps)
		for _, dep := range deps {
			if _, ok := s.nodes[dep]; !ok {
				return fmt.Errorf("%w: %s (required by %s)", ErrUnknownComponent, dep, name)
			}
			if err := visit(dep); err != nil {
				return err
			}
		}
		color[name] = black
		out = append(out, name)
		return nil
	}
	for _, name := range s.order {
		if err := visit(name); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// transitiveDeps returns the set of names the given component depends on (transitively).
// Caller holds mu.
func (s *Supervisor) transitiveDeps(name string) map[string]bool {
	out := map[string]bool{}
	var walk func(string)
	walk = func(n string) {
		node := s.nodes[n]
		if node == nil {
			return
		}
		for _, d := range node.dependsOn {
			if !out[d] {
				out[d] = true
				walk(d)
			}
		}
	}
	walk(name)
	return out
}

// dependents returns the names that hard-depend on the given component (its DAG out-edges).
// Caller holds mu.
func (s *Supervisor) dependents(name string) []string {
	var out []string
	for n, node := range s.nodes {
		for _, d := range node.dependsOn {
			if d == name {
				out = append(out, n)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

// --- control.Supervisor surface (per-name, UI-driven) --------------------------------------

// Model returns the shared in-memory model.
func (s *Supervisor) Model() *config.Model { return s.model }

// Start starts one component, bringing up its hard dependencies first (idempotent).
func (s *Supervisor) Start(ctx context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startTreeLocked(ctx, name)
}

// startTreeLocked starts name's transitive dependencies (in order) then name. Caller holds mu.
func (s *Supervisor) startTreeLocked(ctx context.Context, name string) error {
	if _, ok := s.nodes[name]; !ok {
		return fmt.Errorf("%w: %s", ErrUnknownComponent, name)
	}
	order, err := s.topoOrder()
	if err != nil {
		return err
	}
	deps := s.transitiveDeps(name)
	for _, n := range order {
		if n == name || deps[n] {
			if err := s.startNodeLocked(ctx, n); err != nil {
				return err
			}
		}
	}
	return nil
}

// Stop stops one component and everything that hard-depends on it (so we never leave a
// dependent running on a stopped dependency).
func (s *Supervisor) Stop(ctx context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopTreeLocked(ctx, name)
}

// stopTreeLocked stops name's dependents (recursively) then name. Caller holds mu.
func (s *Supervisor) stopTreeLocked(ctx context.Context, name string) error {
	if _, ok := s.nodes[name]; !ok {
		return fmt.Errorf("%w: %s", ErrUnknownComponent, name)
	}
	var firstErr error
	for _, dep := range s.dependents(name) {
		if err := s.stopTreeLocked(ctx, dep); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if err := s.stopNodeLocked(ctx, name); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// Restart stops then starts the named component (and the dependents it had to take down).
func (s *Supervisor) Restart(ctx context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.stopTreeLocked(ctx, name); err != nil {
		return err
	}
	return s.startTreeLocked(ctx, name)
}

// Reconfigure is fully implemented in C3 (addressed reconfigure + notify). Defined here so the
// control.Supervisor interface is satisfied from C2 onward.
func (s *Supervisor) Reconfigure(ctx context.Context, name string, section config.Section) error {
	return errors.New("supervisor: Reconfigure not yet implemented (C3)")
}

// Status reports a snapshot Unit per managed component for the dashboard.
func (s *Supervisor) Status() []control.Unit {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]control.Unit, 0, len(s.nodes))
	for _, name := range s.order {
		n := s.nodes[name]
		if n == nil {
			continue
		}
		u := control.Unit{
			Name:      name,
			Running:   n.running,
			Enabled:   true,
			DependsOn: append([]string(nil), n.dependsOn...),
		}
		if en, ok := n.c.(component.Enableable); ok {
			u.Enabled = en.Enabled()
		}
		if b, ok := n.c.(component.Bindable); ok {
			u.Binding = b.Binding()
		}
		out = append(out, u)
	}
	return out
}

// ListInterfaces is a placeholder until interface enumeration adapters land (Phase 2).
func (s *Supervisor) ListInterfaces() ([]control.InterfaceInfo, error) { return nil, nil }

// ListFSTypes is a placeholder until FS factories register (Phase 2).
func (s *Supervisor) ListFSTypes() []string { return nil }
