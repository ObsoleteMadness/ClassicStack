// Package runtime is the compose runtime root: the single assembly the
// interactive binary, the Windows service wrapper, and the Unix daemon all share
// (§14, M9/M10). It re-expresses what the D5 skeleton main did inline — load the
// config model, build every registered component, wire them, and supervise — as a
// reusable Runtime so the three entry points stop each hand-rolling the loop.
//
// Ring: COMPOSE. It imports core/ and may import adapter/, but it does NOT pick a
// config Store or Codec itself: those are injected (Options.Store/Codec) so a
// TOML/file build, a UCI/ubus build, and an in-memory test each choose their own
// adapters at the cmd edge without this package pulling all of them in. The
// registry's build-tagged init()s decide which components exist; this root only
// assembles whatever registered.
//
// What this slice does NOT do (kept for the later M10 cutover, per .refactor/TODO):
// inject REAL device links (the ports still build inert until pcap/framing is wired
// here), flag parsing, and retiring the legacy internal/app. The cross-wiring of
// the runtime data path (service↔router, transport↔service) is the M-ng work; this
// root provides the place that wiring will live (Build → wire) and does the part
// that needs no new seam yet: load, build, dependency-ordered supervise.
package runtime

import (
	"context"
	"fmt"

	"github.com/ObsoleteMadness/ClassicStack/compose/registry"
	"github.com/ObsoleteMadness/ClassicStack/compose/supervisor"
	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/router"
)

// stubNames are registry entries that are test/skeleton scaffolding, never part of
// a real stack. Build skips them (the D5 main special-cased the same set inline).
var stubNames = map[string]bool{
	"stub-tagged":   true,
	"stub-a":        true,
	"stub-disabled": true,
}

// hardDeps declares the start-order edges between built components (a name must be
// running before the names that list it). Soft transport bindings (IPX/NetBEUI →
// NetBIOS) are component.Attachable side-effects, NOT edges (§11d), so they are not
// here. This static map re-expresses the D5 main's hardcoded edges; a declarative
// per-component dependency capability is a deliberate follow-on (noted in TODO),
// kept out of this slice so the root stays pure assembly.
//
// Only edges whose BOTH ends are actually built take effect — Build filters against
// the components it constructed, so a minimal build (no NetBEUI) simply drops the
// SMB→NetBEUI edge rather than failing.
var hardDeps = map[string][]string{
	"AFP": {"Router"},
	"SMB": {"NetBEUI"},
}

// componentSource enumerates and builds the components a Runtime assembles. The
// production source is the global compose/registry; tests inject their own so they
// neither depend on whatever build-tagged components registered nor pollute the
// global registry singleton. Build takes the shared BuildContext so a factory
// receives its collaborators (the router et al), not just the model.
type componentSource interface {
	// Instances expands the registry against the model into the components to build:
	// one ComponentID per singleton, and one per named instance of a repeated port
	// (§M11). The runtime builds each with BuildContext.Instance set from the ID.
	Instances(m *config.Model) []registry.ComponentID
	Build(name string, ctx *registry.BuildContext) (component.Component, bool, error)
}

// registrySource adapts the package-level compose/registry to componentSource.
type registrySource struct{}

func (registrySource) Instances(m *config.Model) []registry.ComponentID { return registry.Instances(m) }
func (registrySource) Build(name string, ctx *registry.BuildContext) (component.Component, bool, error) {
	return registry.Build(name, ctx)
}

// Options configures a Runtime build.
type Options struct {
	// Model is the starting config model. Required. Load() fills one from a
	// Store+Codec; a caller may also hand-build one (tests, flag-derived).
	Model *config.Model
	// Telemetry is the bus state/stats/log are published on. A nil bus disables
	// publication (the supervisor and stats subscriber both tolerate nil).
	Telemetry bus.Bus
	// Opener builds a raw device FrameLink for a port's configured interface. It is
	// threaded into every factory's BuildContext so a port can come up LIVE on a
	// NIC. nil (the default) keeps ports inert-but-routed — the cmd edge injects the
	// concrete opener (pcap under the `pcap` tag, else its stub) so this package
	// pulls in no cgo/libpcap dependency, mirroring the injected Store/Codec.
	Opener registry.LinkOpener
	// source enumerates/builds components. nil → the global compose/registry. Set
	// only by tests (kept unexported so the production API is the registry path).
	source componentSource
}

// Runtime is the assembled, not-yet-started stack: the supervisor owning every
// built component in dependency order, plus the shared model and telemetry bus the
// control plane binds to. The entry points call Start/Stop and, for the control
// plane, reach Supervisor()/Model().
type Runtime struct {
	sup       *supervisor.Supervisor
	model     *config.Model
	telemetry bus.Bus
	rtr       *router.RouterImpl // the shared router (nil if none built); cross-wire target
	built     []string           // names actually constructed (diagnostics)
}

// Load builds a config.Model from a Store + Codec. A missing store file yields the
// default model (Store.Load returns (nil,nil)); a present one is decoded through the
// codec. It is the read half of the config path the control plane's Save mirrors.
func Load(store config.Store, codec config.Codec) (*config.Model, error) {
	m := config.NewModel()
	data, err := store.Load()
	if err != nil {
		return nil, fmt.Errorf("runtime: load config: %w", err)
	}
	if len(data) == 0 {
		return m, nil // no stored config yet — defaults
	}
	if err := codec.Unmarshal(data, m); err != nil {
		return nil, fmt.Errorf("runtime: decode config: %w", err)
	}
	return m, nil
}

// Build constructs every registered (non-stub) component from the model, registers
// each with the supervisor under its filtered dependency edges, and returns the
// assembled Runtime. A factory error aborts the whole build (a misconfigured
// component must not be silently dropped). A name that is registered but returns
// (nil, true) for a disabled section is skipped.
func Build(opts Options) (*Runtime, error) {
	if opts.Model == nil {
		return nil, fmt.Errorf("runtime: Build requires a Model")
	}
	src := opts.source
	if src == nil {
		src = registrySource{}
	}
	sup := supervisor.New(opts.Model, opts.Telemetry)

	// Build the shared AppleTalk router FIRST so it can be threaded into every
	// dependent factory's BuildContext: ports bind to it as their inbound target,
	// DDP services reply through it. Building it up-front (rather than letting the
	// router factory run mid-loop) is what lets one instance reach every dependent.
	// A build with no Router component registered leaves rtr nil and the DDP stack
	// simply comes up unrouted (the graceful-degradation contract of BuildContext).
	rtr, err := buildRouter(src, opts.Model)
	if err != nil {
		return nil, err
	}

	ctx := &registry.BuildContext{
		Model:     opts.Model,
		Router:    rtr,
		Telemetry: opts.Telemetry,
		Opener:    opts.Opener,
	}

	// First pass: build the components, recording which names actually exist so the
	// dependency edges can be filtered to built-both-ends pairs. Instances expands
	// repeated ports (§M11) into one build per named instance — a singleton yields
	// one ComponentID (Name == Key); a port yields one per instance — so several
	// EtherTalk/TashTalk/IPX instances each become their own supervised component,
	// addressed by instance name.
	comps := make(map[string]component.Component)
	var order []string
	for _, id := range src.Instances(opts.Model) {
		if stubNames[id.Key] {
			continue
		}
		ictx := *ctx
		ictx.Instance = id.Instance
		c, ok, err := src.Build(id.Key, &ictx)
		if err != nil {
			return nil, fmt.Errorf("runtime: build %q: %w", id.Name, err)
		}
		if !ok || c == nil {
			continue // not in this build, or disabled section
		}
		if _, dup := comps[id.Name]; dup {
			return nil, fmt.Errorf("runtime: duplicate component name %q (instance %q of %q)", id.Name, id.Instance, id.Key)
		}
		comps[id.Name] = c
		order = append(order, id.Name)
	}

	// Cross-wire the runtime data path against the shared router: register DDP
	// services on their sockets and attach the AppleTalk ports as routed members.
	// This is the seam that makes AFP/SMB-over-DDP reachable and ports deliverable;
	// transport↔service seams (SMB over NetBIOS, IPXGW) land as that wiring matures.
	if rtr != nil {
		crossWireRouter(rtr, comps)
	}

	// Second pass: register with the supervisor under filtered edges (only edges
	// whose dependency is also built).
	for _, name := range order {
		deps := builtDeps(name, comps)
		sup.Add(comps[name], deps)
	}

	return &Runtime{
		sup:       sup,
		model:     opts.Model,
		telemetry: opts.Telemetry,
		rtr:       rtr,
		built:     order,
	}, nil
}

// router returns the shared AppleTalk router (nil if none built). Unexported — it
// is the cross-wire target, surfaced for tests; the control plane reaches routing
// through the supervisor/diagnostics, not this.
func (r *Runtime) router() *router.RouterImpl { return r.rtr }

// buildRouter constructs the Router component (if registered) up-front so it can be
// shared via the BuildContext. It is built with a context carrying no router (the
// router factory returns a fresh instance when ctx.Router is nil). Returns (nil,
// nil) when no Router is registered, or when the registered "Router" is not a
// *router.RouterImpl (e.g. a test fake) — in that case there is simply no shareable
// router instance, and the component is built normally in the main pass like any
// other; the DDP stack comes up unrouted, the graceful-degradation contract.
func buildRouter(src componentSource, m *config.Model) (*router.RouterImpl, error) {
	c, ok, err := src.Build(router.Name, &registry.BuildContext{Model: m})
	if err != nil {
		return nil, fmt.Errorf("runtime: build %q: %w", router.Name, err)
	}
	if !ok || c == nil {
		return nil, nil
	}
	rtr, _ := c.(*router.RouterImpl)
	return rtr, nil
}

// crossWireRouter binds the built components to the shared router: DDP services
// (router.Service) register on their sockets; AppleTalk ports (router.RoutedPort)
// attach as routed members. The router itself is in comps under router.Name and is
// skipped. Both bindings are best-effort by interface assertion, so a component
// that is neither (e.g. SMB, a NetBIOS-transport service) is simply left alone.
func crossWireRouter(rtr *router.RouterImpl, comps map[string]component.Component) {
	for name, c := range comps {
		if name == router.Name {
			continue
		}
		if svc, ok := c.(router.Service); ok {
			rtr.RegisterService(svc)
		}
		if p, ok := c.(router.RoutedPort); ok {
			_ = rtr.Attach(p)
		}
	}
}

// builtDeps returns name's hard dependencies, dropping any whose target was not
// built in this configuration (so a minimal build omits the edge instead of
// failing the topo sort on a missing node).
func builtDeps(name string, comps map[string]component.Component) []string {
	want := hardDeps[name]
	if len(want) == 0 {
		return nil
	}
	out := make([]string, 0, len(want))
	for _, d := range want {
		if _, ok := comps[d]; ok {
			out = append(out, d)
		}
	}
	return out
}

// Start brings the whole stack up in dependency order.
func (r *Runtime) Start(ctx context.Context) error { return r.sup.StartAll(ctx) }

// Stop brings the whole stack down in reverse dependency order.
func (r *Runtime) Stop(ctx context.Context) error { return r.sup.StopAll(ctx) }

// Supervisor returns the supervisor (the control.Supervisor surface the control
// plane drives: Status/Start/Stop/Restart/Reconfigure/Users).
func (r *Runtime) Supervisor() *supervisor.Supervisor { return r.sup }

// Model returns the shared config model (the control plane reads/edits it).
func (r *Runtime) Model() *config.Model { return r.model }

// Built returns the names of the components actually constructed, in build order
// (diagnostics / startup logging).
func (r *Runtime) Built() []string { return append([]string(nil), r.built...) }
