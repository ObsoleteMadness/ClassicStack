package supervisor

import (
	"context"
	"encoding/json"
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
	"github.com/ObsoleteMadness/ClassicStack/core/log"
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

// InstanceBuilder constructs a fresh supervised component for one repeated-section
// instance from the (already-updated) model: ownerKey is the schema/registry key
// ("NetBEUI", "EtherTalk", "IPX"), instanceName is the new instance's name (== the node
// name). It returns (nil, nil) when the key is not a buildable component in this build,
// or when the instance is disabled — the same graceful "nothing to build" shape as the
// runtime's first-pass Build. The runtime injects it (it owns the registry); the
// supervisor stays free of a compose/registry import. Used by AddInstance to stand up
// the FIRST instance of a repeated port that had no node at startup (§M11 config-builder).
type InstanceBuilder func(m *config.Model, ownerKey, instanceName string) (component.Component, []string, error)

// TransportAttacher joins a freshly-built repeated-port component to whatever
// transport mini-router carries its family (IPX → the IPX mini-router, NetBEUI → the
// NetBEUI mini-router), so a port added at runtime immediately carries NBF/NBIPX traffic
// up to SMB instead of coming up as a dark supervised link that only wires in on the next
// Save+restart. It is the runtime's compose seam (the runtime owns the mini-routers built
// during cross-wiring); the supervisor stays free of that knowledge and only invokes it
// on the node it just built. A component of neither transport family is a no-op. The
// runtime injects it via SetTransportAttacher; a nil attacher (the default, or a build
// with no NetBIOS transports) skips the step — the pre-seam behaviour.
type TransportAttacher func(c component.Component)

// node is one managed component plus its hard dependency edges and current run state.
type node struct {
	c         component.Component
	dependsOn []string  // names that must be running before this starts (and stop after it)
	rebuild   Rebuilder // optional; reconstructs c from the model during a Reconfigure restart
	running   bool
	lastErr   error // last Start failure; cleared on a successful Start
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
	buildInst  InstanceBuilder                         // injected per-instance builder (runtime owns registry); nil = none
	attachPort TransportAttacher                       // injected runtime seam joining a new port to its mini-router; nil = none
	log        log.Logger                              // optional start-failure logger; nil = silent besides Status.Error
	applyLog   func(level string)                      // retunes the process log LevelVar from [Logging].Level
	stampIdent func(c component.Component, m *config.Model)

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

// SetLogger installs the logger used when a component Start fails during StartAll
// (so a missing pcap device is recorded without aborting the rest of the stack).
func (s *Supervisor) SetLogger(l log.Logger) {
	s.mu.Lock()
	s.log = l
	s.mu.Unlock()
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
// StateChanged{stopped->running} per component as it starts. A component that fails to
// start (missing interface, bind error) is logged and recorded on Status.Error; the
// rest of the stack still comes up so the web UI can surface the failure.
func (s *Supervisor) StartAll(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	order, err := s.topoOrder()
	if err != nil {
		return err
	}
	for _, name := range order {
		if err := s.startNodeLocked(ctx, name); err != nil {
			s.logStartErrorLocked(name, err)
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
	s.logShutdown0("stopping supervised components")
	var firstErr error
	for i := len(order) - 1; i >= 0; i-- {
		if err := s.stopNodeLocked(ctx, order[i]); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if firstErr == nil {
		s.logShutdown0("supervised components stopped")
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
		n.lastErr = err
		n.running = false
		return fmt.Errorf("start %s: %w", name, err)
	}
	n.lastErr = nil
	n.running = true
	s.publish(name, stateStopped, stateRunning)
	return nil
}

func (s *Supervisor) logStartErrorLocked(name string, err error) {
	if s.log == nil {
		return
	}
	s.log.Log2(log.Error, "component start failed; continuing",
		log.Str("component", name), log.Str("err", err.Error()))
}

func (s *Supervisor) logShutdown0(msg string) {
	if s.log == nil || !s.log.Enabled(log.Info) {
		return
	}
	s.log.Log0(log.Info, "shutdown: "+msg)
}

func (s *Supervisor) logShutdown1(msg, component string) {
	if s.log == nil || !s.log.Enabled(log.Info) {
		return
	}
	s.log.Log1(log.Info, "shutdown: "+msg, log.Str("component", component))
}

func (s *Supervisor) logShutdown2(msg, component, err string) {
	if s.log == nil {
		return
	}
	s.log.Log(log.Error, "shutdown: "+msg,
		log.Str("component", component), log.Str("err", err))
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
	s.logShutdown1("waiting for component", name)
	err := n.c.Stop(ctx)
	n.running = false
	s.publish(name, stateRunning, stateStopped)
	if err != nil {
		s.logShutdown2("component stop failed", name, err.Error())
		return fmt.Errorf("stop %s: %w", name, err)
	}
	s.logShutdown1("component stopped", name)
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
		if err := s.validateMutationLocked(func(m *config.Model) { applySection(m, section) }); err != nil {
			return err
		}
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
	if err := s.validateMutationLocked(func(m *config.Model) { m.AddInstance(section) }); err != nil {
		return err
	}
	s.model.AddInstance(section)

	// Two shapes of owner (§M11):
	//
	//   - A file service (AFP/SMB): the owner is a long-lived singleton NODE that owns a
	//     LIST of volumes/shares. Adding one reconciles that existing node — the historical
	//     path below.
	//   - A repeated PORT (EtherTalk/IPX/NetBEUI): each instance is ITS OWN node, addressed
	//     by instance name. Adding the first instance of a port type that had none at startup
	//     means there is no node to reconcile — we must BUILD a new supervised node. Its node
	//     name is the section's instance name (or the owner key when unnamed, mirroring
	//     registry.Instances).
	//
	// The two shapes are told apart by whether the OWNER is the section's own schema key:
	//
	//   - Port: owner == section.Key() (both "NetBEUI"/"IPX"/"EtherTalk") — the instance is
	//     its own node. A file service's list, by contrast, has owner="AFP" but
	//     section.Key()="AFPVolumes" (owner ≠ key), so it never takes this branch.
	//
	// For a port whose new instance has no live node yet, build one via the injected builder.
	// (An owner with no matching node and no builder falls through to reconfigureLocked, which
	// returns ErrUnknownComponent — the pre-seam behaviour for an unknown component.)
	if owner == section.Key() {
		nodeName := section.InstanceName()
		if nodeName == "" {
			nodeName = owner
		}
		if _, exists := s.nodes[nodeName]; !exists && s.buildInst != nil {
			return s.addInstanceNodeLocked(ctx, owner, nodeName)
		}
		if _, exists := s.nodes[nodeName]; exists {
			// Existing port: pass the section so ApplyConfig sees iface/device changes
			// (nil would no-op on Configurable ports).
			return s.reconfigureLocked(ctx, nodeName, section)
		}
	}

	// Notify the owner with nil so a Configurable owner re-resolves the whole set from
	// the model (the volume/share reconcile path), matching the dependent-cascade
	// convention in reconfigureLocked.
	return s.reconfigureLocked(ctx, owner, nil)
}

// addInstanceNodeLocked builds a fresh supervised node for a newly-added repeated-port
// instance via the injected InstanceBuilder, registers it (with its filtered dependency
// edges), and starts it so it goes live immediately — the operator added a port and
// expects it up without a whole-stack restart. Caller holds mu. A builder that returns
// (nil, …) — the key is not buildable in this build, or the instance is disabled — leaves
// the model updated but supervises nothing (the graceful "nothing to build" contract), so
// the port comes up the next time the process (re)builds from the model if later enabled.
func (s *Supervisor) addInstanceNodeLocked(ctx context.Context, ownerKey, nodeName string) error {
	c, deps, err := s.buildInst(s.model, ownerKey, nodeName)
	if err != nil {
		return fmt.Errorf("build instance %s: %w", nodeName, err)
	}
	if c == nil {
		return nil // not buildable in this build, or disabled — nothing to supervise
	}
	// Register under the built component's own reported name (== nodeName) with its
	// dependency edges — filtered to edges whose target is an EXISTING supervised node, so a
	// dangling dependency (a peer not built in this configuration) never breaks a later topo
	// sort (§ built-both-ends, mirroring runtime.builtDeps). Then start it. Add appends to
	// s.order for topo tie-breaking.
	edges := make([]string, 0, len(deps))
	for _, d := range deps {
		if _, ok := s.nodes[d]; ok {
			edges = append(edges, d)
		}
	}
	if _, exists := s.nodes[c.Name()]; !exists {
		s.order = append(s.order, c.Name())
	}
	s.nodes[c.Name()] = &node{c: c, dependsOn: edges}
	if err := s.startNodeLocked(ctx, c.Name()); err != nil {
		return err
	}
	// Join the freshly-started port to its transport mini-router (IPX/NetBEUI) so it
	// carries NBF/NBIPX traffic to SMB immediately — the runtime-wiring half that makes a
	// port added from the config-builder UI more than an inert supervised link. A component
	// of neither transport family, or a build with no attacher wired, is a no-op. Attaching
	// after Start is safe: AddPort only installs the delivery callback + send port, which the
	// already-running read loop picks up atomically.
	if s.attachPort != nil {
		s.attachPort(c)
	}
	return nil
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

// applySection installs a section into m: repeated named instances go to Lists,
// singletons to Sections. Shared by the live mutation and the validate-on-clone path.
func applySection(m *config.Model, section config.Section) {
	if ns, ok := section.(config.NamedSection); ok {
		m.AddInstance(ns)
		return
	}
	if section != nil {
		m.Set(section)
	}
}

// validateMutationLocked clones the live model, applies mutate, and runs Model.Validate
// so an invalid section never reaches the running stack or a subsequent Save.
// Caller holds mu.
func (s *Supervisor) validateMutationLocked(mutate func(*config.Model)) error {
	if s.model == nil || mutate == nil {
		return nil
	}
	clone := s.model.Clone()
	mutate(clone)
	return clone.Validate(config.ValidateOptions{HostnameConstraints: s.hostnameConstraintsLocked()})
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
	return s.hostnameConstraintsLocked()
}

func (s *Supervisor) hostnameConstraintsLocked() []string {
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
		if n.lastErr != nil {
			u.Error = n.lastErr.Error()
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

// SetInstanceBuilder installs the per-instance component builder used by AddInstance to
// stand up the FIRST instance of a repeated port that had no supervised node at startup
// (e.g. the operator adds the first NetBEUI/IPX port from the config-builder UI). The
// runtime injects it because it owns the component registry; the supervisor stays free of
// that import. A nil builder (the default) makes AddInstance fall back to the reconcile
// path, which errors ErrUnknownComponent for an owner with no node — the pre-seam behaviour.
func (s *Supervisor) SetInstanceBuilder(fn InstanceBuilder) {
	s.mu.Lock()
	s.buildInst = fn
	s.mu.Unlock()
}

// SetTransportAttacher installs the runtime seam that joins a newly-built repeated PORT
// to its transport mini-router (IPX/NetBEUI), so a port the operator adds at runtime
// carries NBF/NBIPX traffic up to SMB without a whole-stack rebuild. It is paired with
// SetInstanceBuilder: the builder stands the port up, this attaches it to the live
// dispatch. The runtime injects it because it owns the mini-routers (built during
// cross-wiring); a nil attacher (the default) leaves a runtime-added port supervised but
// unattached — the pre-seam behaviour that only wired in on the next Save+restart.
func (s *Supervisor) SetTransportAttacher(fn TransportAttacher) {
	s.mu.Lock()
	s.attachPort = fn
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
	if err := s.validateMutationLocked(func(m *config.Model) { m.SetInterface(iface) }); err != nil {
		return err
	}
	s.model.SetInterface(iface)
	return s.reconcileInterfaceRefsLocked(ctx, iface.Name)
}

// SetLogLevelApplier installs the callback that retunes the process-wide log
// threshold when [Logging] changes. The runtime/cmd edge supplies a closure over
// the shared *log.LevelVar so verbosity takes effect without rebuilding loggers.
func (s *Supervisor) SetLogLevelApplier(fn func(level string)) {
	s.mu.Lock()
	s.applyLog = fn
	s.mu.Unlock()
}

// SetIdentityStamper installs the compose-registry callback that restamps
// Identity.Hostname/Workgroup/Description onto live services before they restart.
func (s *Supervisor) SetIdentityStamper(fn func(c component.Component, m *config.Model)) {
	s.mu.Lock()
	s.stampIdent = fn
	s.mu.Unlock()
}

// SetWellKnown updates one well-known Model field (Identity, Router, Logging, HTTP,
// Client, FUSE) that lives outside the registered Sections map. The proposed value is
// validated against a cloned model before it is committed, then dependent components
// are reconfigured or restarted so the change goes live without a full ReplaceModel.
func (s *Supervisor) SetWellKnown(ctx context.Context, key string, raw []byte) error {
	s.mu.Lock()
	clone := s.model.Clone()
	if err := applyWellKnown(clone, key, raw); err != nil {
		s.mu.Unlock()
		return err
	}
	if err := clone.Validate(config.ValidateOptions{HostnameConstraints: s.hostnameConstraintsLocked()}); err != nil {
		s.mu.Unlock()
		return err
	}
	if err := applyWellKnown(s.model, key, raw); err != nil {
		s.mu.Unlock()
		return err
	}
	if key == "Logging" && s.applyLog != nil {
		s.applyLog(s.model.Logging.Level)
	}
	if key == config.IdentityKey || key == "Router" {
		s.stampIdentityLocked()
	}
	s.mu.Unlock()

	return s.reconcileWellKnown(ctx, key)
}

func (s *Supervisor) stampIdentityLocked() {
	if s.stampIdent == nil || s.model == nil {
		return
	}
	for _, n := range s.nodes {
		if n == nil {
			continue
		}
		s.stampIdent(n.c, s.model)
	}
}

// identityConsumers are the services that advertise Identity (and, for AFP, the
// router default zone). They restart after Identity/Router well-known edits so NBP
// / NetBIOS / browse names pick up the new values.
var identityConsumers = []string{"SMB", "NetBIOS", "Browser", "Messenger", "NCP", "AFP", "EtherDFS"}

func (s *Supervisor) reconcileWellKnown(ctx context.Context, key string) error {
	switch key {
	case config.IdentityKey:
		return s.restartKnown(ctx, identityConsumers...)
	case "Router":
		if err := s.reconfigureKnown(ctx, "Router"); err != nil {
			return err
		}
		if err := s.reconfigureKnown(ctx, "RTMP", "ZIP", "MacIP", "IPXGW", "Netboot"); err != nil {
			return err
		}
		return s.restartKnown(ctx, "AFP")
	case config.ClientKey, config.FUSEKey:
		return s.reconfigureKnown(ctx, config.ClientKey)
	case "Logging", config.HTTPKey:
		return nil
	}
	return nil
}

func (s *Supervisor) reconfigureKnown(ctx context.Context, names ...string) error {
	for _, name := range names {
		if err := s.Reconfigure(ctx, name, nil); err != nil && !errors.Is(err, ErrUnknownComponent) {
			return err
		}
	}
	return nil
}

func (s *Supervisor) restartKnown(ctx context.Context, names ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, name := range names {
		n := s.nodes[name]
		if n == nil {
			continue
		}
		if err := s.restartNodeLocked(ctx, n, name); err != nil {
			return err
		}
	}
	return nil
}

func applyWellKnown(m *config.Model, key string, raw json.RawMessage) error {
	if m == nil {
		return fmt.Errorf("supervisor: nil model")
	}
	switch key {
	case config.IdentityKey:
		var v config.Identity
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
		m.Identity = v
	case "Router":
		var v config.RouterSection
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
		m.Router = v
	case "Logging":
		var v config.LoggingSection
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
		m.Logging = v
	case config.HTTPKey:
		var v config.HTTPSection
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
		m.HTTP = v
	case config.ClientKey:
		var v config.ClientSection
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
		m.Client = v
	case config.FUSEKey:
		var v config.FUSESection
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
		m.FUSE = v
	default:
		return fmt.Errorf("supervisor: unknown well-known key %q", key)
	}
	return nil
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
// resolves its effective interface to the named entry (or, for default-interface
// inheritance, matches the namespace default's name), so an interface edit propagates
// to the ports using it without a whole-stack restart. Caller holds mu. Best-effort: a component
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
		// it) or implicitly via default-interface inheritance when the changed name is
		// the namespace's default. Match either so a default-interface edit reaches its
		// inheritors.
		ref := ip.Interface().Name
		if ref != name && (ref != "" || s.model.DefaultInterface().Name != name) {
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

// ReplaceModel installs a freshly-parsed config model as the live source of truth:
// stop every component, swap the model contents in place (the Model pointer stays
// stable for anyone holding it), rebuild nodes that have a Rebuilder, stand up any
// new repeated-port instances named in the model, then start everything again.
// Used by the TOML editor Apply path so an operator can paste a full server.toml
// and have the running stack reflect it without a process restart.
func (s *Supervisor) ReplaceModel(ctx context.Context, m *config.Model) error {
	if m == nil {
		return errors.New("supervisor: nil model")
	}
	if err := s.StopAll(ctx); err != nil {
		return err
	}

	s.mu.Lock()
	cp := m.Clone()
	// Keep the same *Model pointer; swap its contents so Runtime/Plane holders stay valid.
	s.model.Identity = cp.Identity
	s.model.AdminAuth = cp.AdminAuth
	s.model.Logging = cp.Logging
	s.model.HTTP = cp.HTTP
	s.model.Client = cp.Client
	s.model.FUSE = cp.FUSE
	s.model.Router = cp.Router
	s.model.Interfaces = cp.Interfaces
	s.model.Sections = cp.Sections
	s.model.Lists = cp.Lists

	// Rebuild existing nodes from the new model.
	for _, name := range s.order {
		n := s.nodes[name]
		if n == nil || n.rebuild == nil {
			continue
		}
		c, err := n.rebuild(s.model)
		if err != nil {
			s.mu.Unlock()
			return fmt.Errorf("rebuild %s: %w", name, err)
		}
		if c != nil {
			n.c = c
		}
	}

	// Stand up repeated-port instances present in the new model but not yet supervised
	// (an [[ipx]] / [[ethertalk]] the operator added in the TOML editor).
	if s.buildInst != nil {
		for key, list := range s.model.Lists {
			for _, sec := range list {
				ns, ok := sec.(config.NamedSection)
				if !ok {
					continue
				}
				nodeName := ns.InstanceName()
				if nodeName == "" {
					nodeName = key
				}
				if _, exists := s.nodes[nodeName]; exists {
					continue
				}
				// Only port-like keys (owner == schema key) get an instance node.
				if err := s.addInstanceNodeLocked(ctx, key, nodeName); err != nil {
					s.mu.Unlock()
					return err
				}
			}
		}
	}
	s.mu.Unlock()

	return s.StartAll(ctx)
}
