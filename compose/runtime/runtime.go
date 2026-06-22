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
	"github.com/ObsoleteMadness/ClassicStack/core/control"
	"github.com/ObsoleteMadness/ClassicStack/core/router"
	"github.com/ObsoleteMadness/ClassicStack/core/service/macip"
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
	// The SMB-over-TCP listener drives the SMB service's session consumer, so SMB must
	// be running before it accepts connections (and stop after it). Name matches
	// smbtcp.Name; the edge drops if SMB-TCP was not built.
	"SMB-TCP": {"SMB"},
	// The core DDP router services ride the shared router: they must start after it (so
	// their socket registration + table access has a live router) and stop before it.
	// Names match the registry/component names (rtmp.RespondingName etc.); only edges
	// whose both ends are built take effect, so a no-router build drops these silently.
	"RTMP": {"Router"},
	"ZIP":  {"Router"},
	"NBP":  {"Router"},
	"AEP":  {"Router"},
	// MacIP is a DDP service (socket 72) and registers its IPGATEWAY name via NBP, so
	// it starts after both the router and NBP and stops before them.
	"MacIP": {"Router", "NBP"},
	// IPXGW (MacIPX, socket 78) registers its "IPX Gateway" names via NBP — same edges.
	"IPXGW": {"Router", "NBP"},
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

// MacIPEgress is the IP-side egress an opener returns: the macip.IPEgress seam plus
// its own lifecycle. The supervisor does not own it (it is not a Component); the
// runtime starts it during cross-wiring and the MacIP service drives it.
type MacIPEgress interface {
	macip.IPEgress
	// Start brings the IP link up (capture + ARP). Called once after wiring.
	Start()
	// Close tears the egress down (frees the libpcap handle + forwarding state).
	Close() error
}

// MacIPEgressOpener builds the IP-side egress for the MacIP gateway. params is the
// section-derived IP-side config; ownsIP is the service's lease predicate (used for
// proxy ARP / inbound filtering). It returns nil egress (and a nil error) when no
// interface is configured, or an error when an interface is named but the link cannot
// open — the caller logs it and leaves MacIP AppleTalk-only.
type MacIPEgressOpener func(params macip.EgressParams, ownsIP func(macip.IPv4) bool) (MacIPEgress, error)

// Options configures a Runtime build.
type Options struct {
	// Model is the starting config model. Required. Load() fills one from a
	// Store+Codec; a caller may also hand-build one (tests, flag-derived).
	Model *config.Model
	// Telemetry is the bus state/stats/log are published on. A nil bus disables
	// publication (the supervisor and stats subscriber both tolerate nil).
	Telemetry bus.Bus
	// Opener builds a raw NIC FrameLink for a port's configured interface (kind nic
	// or bridge). It is threaded into every factory's BuildContext so a NIC port can
	// come up LIVE. nil (the default) keeps NIC ports inert-but-routed — the cmd edge
	// injects the concrete opener (pcap under the `pcap` tag, else its stub) so this
	// package pulls in no cgo/libpcap dependency, mirroring the injected Store/Codec.
	Opener registry.LinkOpener
	// Serial opens a serial byte stream for a port whose interface kind is serial
	// (TashTalk). Like Opener it is injected at the cmd edge (adapter/serial) and
	// threaded into every BuildContext, so the kind→opener dispatch (M11.c) can pick
	// NIC vs serial from the resolved interface. nil → serial ports come up inert.
	Serial registry.SerialOpener
	// InterfaceEnumerator lists the host's NICs for the control plane's ListInterfaces
	// (the UI's NIC picker). Injected at the cmd edge (adapter/link/pcap.ListDevices) so
	// the runtime/supervisor pull in no pcap/cgo dependency. nil → ListInterfaces empty.
	InterfaceEnumerator func() ([]control.InterfaceInfo, error)
	// MacIPEgress builds the IP-side egress adapter for the MacIP gateway from its
	// section params + the service's lease predicate. Injected at the cmd edge
	// (adapter/macipgw, which needs pcap/cgo) and called during cross-wiring when the
	// MacIP section names an interface. nil (or a build error) leaves MacIP
	// AppleTalk-only. Kept out of compose/runtime so this package stays cgo-free.
	MacIPEgress MacIPEgressOpener
	// source enumerates/builds components. nil → the global compose/registry. Set
	// only by tests (kept unexported so the production API is the registry path).
	source componentSource
}

// Runtime is the assembled, not-yet-started stack: the supervisor owning every
// built component in dependency order, plus the shared model and telemetry bus the
// control plane binds to. The entry points call Start/Stop and, for the control
// plane, reach Supervisor()/Model().
type Runtime struct {
	sup         *supervisor.Supervisor
	model       *config.Model
	telemetry   bus.Bus
	rtr         *router.RouterImpl             // the shared router (nil if none built); cross-wire target
	members     []router.RoutedPort            // ports declared in [Router].members, attached after the router starts (§3d)
	built       []string                       // names actually constructed (diagnostics)
	macipEgress MacIPEgress                    // IP-side MacIP egress (nil if none); started/closed with the runtime
	comps       map[string]component.Component // built components by name, for compose-edge lookups (diagnostics wiring)
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
	sup.SetInterfaceEnumerator(opts.InterfaceEnumerator)

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
		Serial:    opts.Serial,
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
		// The Router was already built up-front (buildRouter) so it could be threaded
		// into every dependent's BuildContext; reuse THAT instance as the supervised
		// component rather than building a second one — otherwise the cross-wire target
		// and the supervised (started) router diverge, and members attach to a router
		// that never runs.
		if id.Key == router.Name && rtr != nil {
			comps[id.Name] = rtr
			order = append(order, id.Name)
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
	// services on their sockets now, and SELECT the [Router].members ports (§3d) to
	// be attached once the router is running (deferred to Start). This is the seam
	// that makes AFP/SMB-over-DDP reachable and ports deliverable; transport↔service
	// seams (SMB over NetBIOS, IPXGW) land as that wiring matures.
	var members []router.RoutedPort
	if rtr != nil {
		members = crossWireRouter(rtr, comps, opts.Model.Router)
	}

	// Cross-wire the NetBIOS transports (§M-ng2): stand up the IPX/NetBEUI mini-
	// routers, attach their ports, register the NBF/NBIPX session engines, and
	// install SMB as the upper-layer session consumer. Unlike the AppleTalk router
	// these mini-routers have no lifecycle of their own (the ports own start/stop),
	// so they are built here rather than supervised. A build without the NetBIOS
	// service is a no-op.
	macipEgress := crossWireTransports(comps, opts.Model, opts.MacIPEgress)

	// Wire the user store (§4): build the configured store once and hand it to the
	// supervisor (the web UI's user CRUD surface) AND to every built file service as
	// its login Authenticator. BuildUserStore returns (nil,nil) in a build with no
	// file service, in which case user administration is unavailable and the services
	// stay guest-only — the historical default. A build error (e.g. an unwritable
	// store path) is surfaced so a misconfigured deployment fails loudly rather than
	// silently dropping authentication.
	if store, err := registry.BuildUserStore(opts.Model); err != nil {
		return nil, fmt.Errorf("runtime: build user store: %w", err)
	} else if store != nil {
		sup.SetUserStore(store)
		wireAuthenticator(comps, store)
	}

	// Second pass: register with the supervisor under filtered edges (only edges
	// whose dependency is also built).
	for _, name := range order {
		deps := builtDeps(name, comps)
		sup.Add(comps[name], deps)
	}

	return &Runtime{
		sup:         sup,
		model:       opts.Model,
		telemetry:   opts.Telemetry,
		rtr:         rtr,
		members:     members,
		built:       order,
		macipEgress: macipEgress,
		comps:       comps,
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

// crossWireRouter registers the built DDP services on the shared router and selects
// which AppleTalk ports are router MEMBERS. Service registration happens here and is
// unconditional — a service binds to its socket regardless of which ports route, and
// RegisterService does not require a running router. Port ATTACH is deferred: the
// router rejects attaching while stopped (§3 event-driven membership), so the member
// ports are returned for Start to attach once the supervisor has brought the router
// up, and Stop detaches them in turn.
//
// Membership is §3d/D8: only the port instances NAMED in [Router].members become
// members. An enabled port NOT listed comes up standalone — built, supervised, and
// live on its own segment, but never attached, so it takes no part in RTMP/ZIP or
// inter-port forwarding. An empty members list selects NONE (D9, opt-in). The router
// itself is in comps under router.Name and is skipped. Bindings are best-effort by
// interface assertion, so a component that is neither service nor port (e.g. SMB) is
// simply left alone.
func crossWireRouter(rtr *router.RouterImpl, comps map[string]component.Component, rsec config.RouterSection) []router.RoutedPort {
	var members []router.RoutedPort
	for name, c := range comps {
		if name == router.Name {
			continue
		}
		if svc, ok := c.(router.Service); ok {
			rtr.RegisterService(svc)
		}
		if p, ok := c.(router.RoutedPort); ok && rsec.IsMember(name) {
			members = append(members, p)
		}
	}
	return members
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

// Start brings the whole stack up in dependency order, then attaches the declared
// router members (§3d). Attach is deferred to here because the router rejects
// membership changes while stopped (§3); by now the supervisor's dependency order
// has brought the Router up ahead of its members. A failed attach aborts Start so a
// misrouted member is not silently dropped.
func (r *Runtime) Start(ctx context.Context) error {
	if err := r.sup.StartAll(ctx); err != nil {
		return err
	}
	// Bring the MacIP IP-side egress up once the stack is running (the MacIP service's
	// Start has wired the egress inbound callback). Not a supervised component — the
	// runtime owns its lifecycle. A nil egress (AppleTalk-only) is a no-op.
	if r.macipEgress != nil {
		r.macipEgress.Start()
	}
	if r.rtr != nil {
		for _, p := range r.members {
			if err := r.rtr.Attach(p); err != nil {
				return fmt.Errorf("runtime: attach router member %q: %w", p.Name(), err)
			}
		}
	}
	// Begin the telemetry stats flush once the stack is up: it polls every Statful
	// component and wires push sinks, feeding the compose/stats rate collector and the
	// control plane's SSE stream (§5). A nil bus makes this a no-op.
	r.sup.StartStatsFlush(supervisor.DefaultStatsInterval)
	return nil
}

// Stop detaches the router members (reversing Start's attach) and then brings the
// whole stack down in reverse dependency order. Detach is best-effort — a member
// already withdrawn (e.g. by an individual Stop) must not block shutdown.
func (r *Runtime) Stop(ctx context.Context) error {
	r.sup.StopStatsFlush()
	if r.macipEgress != nil {
		_ = r.macipEgress.Close()
	}
	if r.rtr != nil {
		for _, p := range r.members {
			_ = r.rtr.Detach(p)
		}
	}
	return r.sup.StopAll(ctx)
}

// Supervisor returns the supervisor (the control.Supervisor surface the control
// plane drives: Status/Start/Stop/Restart/Reconfigure/Users).
func (r *Runtime) Supervisor() *supervisor.Supervisor { return r.sup }

// Model returns the shared config model (the control plane reads/edits it).
func (r *Runtime) Model() *config.Model { return r.model }

// Router returns the shared AppleTalk router (nil when none was built). The cmd edge
// uses it to wire the real diagnostics probe surface (zone/routing-table reads).
func (r *Runtime) Router() *router.RouterImpl { return r.rtr }

// Built returns the names of the components actually constructed, in build order
// (diagnostics / startup logging).
func (r *Runtime) Built() []string { return append([]string(nil), r.built...) }

// Component returns the built component under name, or nil when it was not built. The
// compose edge uses it to attach optional collaborators to the diagnostics surface
// (the NBP name table, the MacIP lease table) without re-running the build.
func (r *Runtime) Component(name string) component.Component {
	if r.comps == nil {
		return nil
	}
	return r.comps[name]
}
