package supervisor

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/core/auth"
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

// Rebuilder reconstructs a component from the (already-updated) shared model. The supervisor
// calls it during a restart-driven Reconfigure (§11a step 4: "rebuild from section"). A nil
// rebuilder means the live instance is reused across restart (fine for components whose state
// is not config-derived, e.g. Phase 1 placeholders).
type Rebuilder func(m *config.Model) (component.Component, error)

// node is one managed component plus its hard dependency edges and current run state.
type node struct {
	c         component.Component
	dependsOn []string  // names that must be running before this starts (and stop after it)
	rebuild   Rebuilder // optional; reconstructs c from the model during a Reconfigure restart
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
	order     []string       // insertion order, the tie-breaker in topo sort
	users     auth.UserStore // wired user store; nil = no user administration available
}

// compile-time assertions: Supervisor satisfies the control plane's lifecycle
// surface and (when a store is wired) its user-administration surface.
var (
	_ control.Supervisor = (*Supervisor)(nil)
	_ control.UserAdmin  = (*Supervisor)(nil)
)

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

// AddBuildable is Add plus a Rebuilder so a restart-driven Reconfigure can reconstruct the
// component from the updated model (§11a). Re-adding a name replaces the prior node.
func (s *Supervisor) AddBuildable(c component.Component, dependsOn []string, rebuild Rebuilder) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name := c.Name()
	if _, exists := s.nodes[name]; !exists {
		s.order = append(s.order, name)
	}
	edges := append([]string(nil), dependsOn...)
	s.nodes[name] = &node{c: c, dependsOn: edges, rebuild: rebuild}
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

// SetUserStore wires the authentication store the user-administration surface
// (control.UserAdmin, driven by the web UI) operates on. The compose root builds
// the store once (registry.BuildUserStore) and hands it here as well as to each
// file service (SetAuthenticator). A nil store means no user administration is
// available — the Users/SetUser/… methods then report control.ErrUnavailable.
func (s *Supervisor) SetUserStore(store auth.UserStore) {
	s.mu.Lock()
	s.users = store
	s.mu.Unlock()
}

// Users lists the stored identities (control.UserAdmin). No user store wired →
// ErrUnavailable.
func (s *Supervisor) Users() ([]control.UserInfo, error) {
	s.mu.Lock()
	store := s.users
	s.mu.Unlock()
	if store == nil {
		return nil, control.ErrUnavailable
	}
	us, err := store.Users()
	if err != nil {
		return nil, err
	}
	out := make([]control.UserInfo, len(us))
	for i, u := range us {
		out[i] = control.UserInfo{Name: u.Name, Disabled: u.Disabled}
	}
	return out, nil
}

// SetUser adds a user or resets a password (control.UserAdmin).
func (s *Supervisor) SetUser(name, password string) error {
	s.mu.Lock()
	store := s.users
	s.mu.Unlock()
	if store == nil {
		return control.ErrUnavailable
	}
	return store.SetUser(name, password)
}

// SetUserDisabled parks/unparks an account (control.UserAdmin).
func (s *Supervisor) SetUserDisabled(name string, disabled bool) error {
	s.mu.Lock()
	store := s.users
	s.mu.Unlock()
	if store == nil {
		return control.ErrUnavailable
	}
	return store.SetDisabled(name, disabled)
}

// RemoveUser deletes a user (control.UserAdmin).
func (s *Supervisor) RemoveUser(name string) error {
	s.mu.Lock()
	store := s.users
	s.mu.Unlock()
	if store == nil {
		return control.ErrUnavailable
	}
	return store.RemoveUser(name)
}

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

// Reconfigure applies a new section to ONE named component and cascades a restart to dependents
// only as far as each cannot absorb it live. ADDRESSED, not diffed (§11a): there is NO model
// comparison pass — the caller names the component, we update its section and ask it (and, in
// turn, each dependent) whether it can hot-apply.
//
// Algorithm (§11a):
//  1. model.Set(section)                       update the shared model section
//  2. if Configurable: ApplyConfig(section)
//     nil            -> live; publish StateChanged(running->reconfigured); stop here
//     ErrNeedsRestart-> fall through to restart
//     other error    -> real failure, return it
//  3. restart: Stop; rebuild from model; Start (each publishes its own StateChanged)
//  4. for each hard dependent: Reconfigure-notify (the dependent answers the same question;
//     the cascade stops wherever a dependent hot-applies). Attachable bindings (§11d) are
//     re-run by Stop/Start as side effects and are NOT dependents, so never enter the cascade.
func (s *Supervisor) Reconfigure(ctx context.Context, name string, section config.Section) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if section != nil {
		s.model.Set(section)
	}
	return s.reconfigureLocked(ctx, name, section)
}

// reconfigureLocked is the addressed reconfigure for one component plus the dependent cascade.
// Caller holds mu. `section` is the component's own section for the head call; dependents are
// notified with nil (they re-resolve from the already-updated model).
func (s *Supervisor) reconfigureLocked(ctx context.Context, name string, section config.Section) error {
	n := s.nodes[name]
	if n == nil {
		return fmt.Errorf("%w: %s", ErrUnknownComponent, name)
	}

	if cfg, ok := n.c.(component.Configurable); ok {
		err := cfg.ApplyConfig(section)
		switch {
		case err == nil:
			// Hot-applied live: no restart, and the cascade STOPS here for this node's subtree.
			if n.running {
				s.publish(name, stateRunning, stateReconfigured)
			}
			return nil
		case errors.Is(err, component.ErrNeedsRestart):
			// fall through to restart + notify dependents
		default:
			return fmt.Errorf("reconfigure %s: %w", name, err)
		}
	}

	// Restart this node (§11a step 3), then notify dependents (step 4).
	if err := s.restartNodeLocked(ctx, n, name); err != nil {
		return err
	}
	for _, dep := range s.dependents(name) {
		// Dependents re-resolve their own section from the model; pass nil so a Configurable
		// dependent's ApplyConfig sees "re-evaluate from model", and the cascade can stop there.
		var depSection config.Section
		if ds, ok := s.model.Get(dep); ok {
			depSection = ds
		}
		if err := s.reconfigureLocked(ctx, dep, depSection); err != nil {
			return err
		}
	}
	return nil
}

// restartNodeLocked stops the node, rebuilds it from the model if a Rebuilder is set, then
// starts it. Caller holds mu. Stop/Start publish their own StateChanged transitions.
func (s *Supervisor) restartNodeLocked(ctx context.Context, n *node, name string) error {
	wasRunning := n.running
	if err := s.stopNodeLocked(ctx, name); err != nil {
		return err
	}
	if n.rebuild != nil {
		c, err := n.rebuild(s.model)
		if err != nil {
			return fmt.Errorf("rebuild %s: %w", name, err)
		}
		if c != nil {
			n.c = c
		}
	}
	if wasRunning {
		if err := s.startNodeLocked(ctx, name); err != nil {
			return err
		}
	}
	return nil
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
