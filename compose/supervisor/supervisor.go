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
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
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
	mu         sync.Mutex
	model      *config.Model
	telemetry  bus.Bus
	nodes      map[string]*node
	order      []string                                // insertion order, the tie-breaker in topo sort
	users      auth.UserStore                          // wired user store; nil = no user administration available
	enumIfaces func() ([]control.InterfaceInfo, error) // injected host-NIC enumerator (cmd edge); nil = none

	statsMu   sync.Mutex
	statsStop chan struct{} // closed to stop the periodic stats flush; nil when not running
	statsWG   sync.WaitGroup
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

// SetAdminAuth stamps the web-management-interface admin credential (§4-ter) into the
// shared model under the supervisor lock. The control plane calls it from SetAdmin,
// then persists the model via the Save path. The credential is hash-only (the HTTP
// adapter derived it); no plaintext reaches here.
func (s *Supervisor) SetAdminAuth(a config.AdminAuth) {
	s.mu.Lock()
	s.model.AdminAuth = a
	s.mu.Unlock()
}

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
		s.setSectionLocked(section)
	}
	return s.reconfigureLocked(ctx, name, section)
}

// setSectionLocked installs a section into the model on the right map: a repeated
// (NamedSection) instance goes to Model.Lists via AddInstance (replacing the same
// InstanceName), a singleton to Model.Sections via Set. Caller holds mu. Without the
// NamedSection branch a reconfigure of one volume/share would mis-write it as a
// singleton and never reach the owning service's instance set.
func (s *Supervisor) setSectionLocked(section config.Section) {
	if ns, ok := section.(config.NamedSection); ok {
		s.model.AddInstance(ns)
		return
	}
	s.model.Set(section)
}

// AddInstance stages a new (or replacement) repeated-section instance — an AFP volume,
// an SMB share — into the model under its schema key, then reconfigures the owning
// service component so it reconciles its live instance set from the model (no restart
// when the owner is Configurable; §11b). owner is the component that consumes the list
// (e.g. "AFP" for "AFPVolumes"). The UI supplies it; the supervisor stays free of
// section-key→owner knowledge.
func (s *Supervisor) AddInstance(ctx context.Context, owner string, section config.NamedSection) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.model.AddInstance(section)
	// Notify the owner with nil so a Configurable owner re-resolves the whole set from
	// the model (the volume/share reconcile path), matching the dependent-cascade
	// convention in reconfigureLocked.
	return s.reconfigureLocked(ctx, owner, nil)
}

// RemoveInstance drops the named repeated-section instance under key from the model,
// then reconfigures the owning component so it removes the live volume/share. A no-op
// (nil) if the instance was not present.
func (s *Supervisor) RemoveInstance(ctx context.Context, owner, key, instanceName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.model.RemoveInstance(key, instanceName) {
		return nil
	}
	return s.reconfigureLocked(ctx, owner, nil)
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
// HostnameConstraints aggregates the active server-hostname constraints across the live
// component set: each component implementing component.HostnameConstrainer that reports
// active contributes its constraint key (e.g. "netbios" when NetBIOS is enabled). The
// control plane passes the result to Model.Validate so the consumer-gated hostname rules
// apply WITHOUT the plane naming any specific service (§4-bis; the leak fix for C2). The
// keys are de-duplicated; order is unspecified.
func (s *Supervisor) HostnameConstraints() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := map[string]struct{}{}
	var out []string
	for _, n := range s.nodes {
		if n == nil {
			continue
		}
		if hc, ok := n.c.(component.HostnameConstrainer); ok {
			if key, active := hc.HostnameConstraint(); active && key != "" {
				if _, dup := seen[key]; !dup {
					seen[key] = struct{}{}
					out = append(out, key)
				}
			}
		}
	}
	return out
}

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
		if d, ok := n.c.(component.Describable); ok {
			u.Kind = d.Kind()
			u.Props = d.Props()
		}
		out = append(out, u)
	}
	return out
}

// SetInterfaceEnumerator installs the host-NIC enumeration source. The cmd edge injects
// it (adapter/link/pcap.ListDevices), so the supervisor — and core/control through it —
// stays free of the pcap/cgo dependency, mirroring how the LinkOpener is injected. A nil
// enumerator (the default, or a build with no pcap backend) leaves ListInterfaces empty.
func (s *Supervisor) SetInterfaceEnumerator(fn func() ([]control.InterfaceInfo, error)) {
	s.mu.Lock()
	s.enumIfaces = fn
	s.mu.Unlock()
}

// ListInterfaces returns the host network interfaces from the injected enumerator, or an
// empty list when none is wired (a headless / no-pcap build).
func (s *Supervisor) ListInterfaces() ([]control.InterfaceInfo, error) {
	s.mu.Lock()
	fn := s.enumIfaces
	s.mu.Unlock()
	if fn == nil {
		return nil, nil
	}
	return fn()
}

// SetInterface adds or replaces a named entry in the interface namespace
// (Model.Interfaces) under the lock, then reconciles every port that references the
// changed interface so the change goes live (a port re-resolves EffectiveInterface on
// rebuild). An entry with no Name is rejected as a no-op (the namespace is keyed by
// name). Ports that do not reference the interface are left untouched.
func (s *Supervisor) SetInterface(ctx context.Context, iface config.InterfaceSection) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if iface.Name == "" {
		return nil
	}
	s.model.SetInterface(iface)
	return s.reconcileInterfaceRefsLocked(ctx, iface.Name)
}

// RemoveInterface drops the named interface-namespace entry under the lock and
// reconciles referencing ports (which then resolve the name to a bare nic, the
// back-compat fallback in EffectiveInterface). A no-op when the entry was absent.
func (s *Supervisor) RemoveInterface(ctx context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.model.Interfaces == nil {
		return nil
	}
	if _, ok := s.model.Interfaces[name]; !ok {
		return nil
	}
	delete(s.model.Interfaces, name)
	return s.reconcileInterfaceRefsLocked(ctx, name)
}

// reconcileInterfaceRefsLocked reconfigures every built component whose section
// resolves its effective interface to the named entry (or, for the default Bridge
// inheritance, matches the bridge name), so an interface edit propagates to the ports
// using it without a whole-stack restart. Caller holds mu. Best-effort: a component
// with no model section, or one that is not interface-bound, is skipped. The first
// reconfigure error is returned (later ports are not attempted), matching the addressed
// reconfigure semantics.
func (s *Supervisor) reconcileInterfaceRefsLocked(ctx context.Context, name string) error {
	for _, compName := range s.order {
		sec, ok := s.model.Get(compName)
		if !ok {
			continue
		}
		ip, ok := sec.(config.InterfaceProvider)
		if !ok {
			continue
		}
		// A port references the changed interface either explicitly (its override names
		// it) or implicitly via the default Bridge inheritance when the changed name is
		// the bridge's. Match either so a bridge edit reaches its inheritors.
		ref := ip.Interface().Name
		if ref != name && !(ref == "" && s.model.Bridge.Name == name) {
			continue
		}
		if err := s.reconfigureLocked(ctx, compName, sec); err != nil {
			return err
		}
	}
	return nil
}

// ListFSTypes returns the registered FileSystem backend types (afp/smb shares pick
// one). It reads the fs factory registry, so a UI can populate an fs-type dropdown
// and then fetch each type's param schema via the plane's ParamsFor.
func (s *Supervisor) ListFSTypes() []string { return fs.Types() }
