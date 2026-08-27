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
package runtime

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/compose/registry"
	"github.com/ObsoleteMadness/ClassicStack/compose/supervisor"
	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/control"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
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

// hardDeps is the FALLBACK start-order edge map for components that have not (yet)
// adopted the component.DependsOn capability — declaredDeps consults the component
// first and only falls back here. Every component with edges now declares its own
// dependencies (afp/smb/smbtcp/macip/ipxgw + the rtmp/zip/nbp/aep DDP services), so
// this map is empty: each component owns its edges, and SMB's NetBEUI edge varies by
// its transport-binding config (which a static map could not express). It is retained
// (empty) as the seam for any future component that prefers static declaration. Soft
// transport bindings (IPX/NetBEUI → NetBIOS) are component.Attachable side-effects, NOT
// edges (§11d), so they were never here.
var hardDeps = map[string][]string{}

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
	// DefaultDevice resolves the host's primary (default-route) NIC to the pcap device a
	// NIC port opens when its interface names none (server "Easy mode" auto-NIC). Injected
	// at the cmd edge (pcap.ListDevices + core/hostinfo.PrimaryDevice) and threaded into
	// every BuildContext so an unnamed NIC port comes up LIVE on the primary NIC instead of
	// inert. nil (a tag-free build, or a test) disables auto-detection. A configured iface
	// always wins — this is a fallback only.
	DefaultDevice func() (string, error)
	// HostMAC resolves the real hardware address of a pcap device so a NIC port that
	// names no mac / hw_address can stamp the host NIC's own MAC (required on WiFi).
	// Injected at the cmd edge (pcap.ListDevices + hostinfo.HardwareAddrForDevice) and
	// threaded into every BuildContext. nil skips auto-detect (zero-MAC fallback).
	HostMAC func(device string) ([6]byte, error)
	// MacIPEgress builds the IP-side egress adapter for the MacIP gateway from its
	// section params + the service's lease predicate. Injected at the cmd edge
	// (adapter/macipgw, which needs pcap/cgo) and called during cross-wiring when the
	// MacIP section names an interface. nil (or a build error) leaves MacIP
	// AppleTalk-only. Kept out of compose/runtime so this package stays cgo-free.
	MacIPEgress MacIPEgressOpener
	// LogSinks are extra log sinks installed on every component logger, threaded into
	// each factory's BuildContext (in addition to the stderr sink built at the
	// configured [Logging] Level, and the [Logging] Path file sink when set).
	// Injected at the cmd edge — e.g. the web-UI ring buffer feeding the log viewer.
	// nil keeps components stderr-only.
	LogSinks []log.Sink
	// LogLevel is the shared process log threshold. Threaded into every
	// BuildContext so [Logging] Level can retune live. Nil creates one from
	// Model.Logging.Level.
	LogLevel *log.LevelVar
	// source enumerates/builds components. nil → the global compose/registry. Set
	// only by tests (kept unexported so the production API is the registry path).
	source componentSource
}

// Runtime is the assembled, not-yet-started stack: the supervisor owning every
// built component in dependency order, plus the shared model and telemetry bus the
// control plane binds to. The entry points call Start/Stop and, for the control
// plane, reach Supervisor()/Model().
type Runtime struct {
	sup        *supervisor.Supervisor
	model      *config.Model
	telemetry  bus.Bus
	rtr        *router.RouterImpl             // the shared router (nil if none built); cross-wire target
	members    []router.RoutedPort            // ports declared in [Router].members, attached after the router starts (§3d)
	built      []string                       // names actually constructed (diagnostics)
	transports *transportWiring               // retained IPX/NetBEUI mini-routers + MacIP egress; drives runtime port attach + egress lifecycle
	comps      map[string]component.Component // built components by name, for compose-edge lookups (diagnostics wiring)
	log        log.Logger

	claimWatchStop chan struct{}  // closed by Stop to cancel any still-polling late-claim watchers (§ late-claim fix)
	claimWatchWG   sync.WaitGroup // Stop waits on this so no watcher touches the router after Stop returns
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
	logLevel := opts.LogLevel
	if logLevel == nil {
		lvl := log.Info
		if opts.Model.Logging.Level != "" {
			lvl = registry.ParseLevel(opts.Model.Logging.Level)
		}
		logLevel = log.NewLevelVar(lvl)
	}
	sinks := []log.Sink{log.NewStderrSink(logLevel)}
	if path := strings.TrimSpace(opts.Model.Logging.Path); path != "" {
		if fsink, ferr := log.NewFileSink(path, logLevel); ferr != nil {
			log.New("runtime", log.NewStderrSink(logLevel)).Log2(log.Warn, "log file unwritable",
				log.Str("path", path), log.Str("err", ferr.Error()))
		} else {
			sinks = append(sinks, fsink)
		}
	}
	sinks = append(sinks, opts.LogSinks...)
	rtLog := log.New("runtime", sinks...)
	sup.SetLogger(log.New("supervisor", sinks...))
	sup.SetLogLevelApplier(func(level string) {
		logLevel.Set(registry.ParseLevel(level))
	})
	sup.SetIdentityStamper(registry.StampIdentity)

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
		Model:         opts.Model,
		Router:        rtr,
		Telemetry:     opts.Telemetry,
		Opener:        opts.Opener,
		Serial:        opts.Serial,
		DefaultDevice: opts.DefaultDevice,
		HostMAC:       opts.HostMAC,
		LogSinks:      opts.LogSinks,
		LogLevel:      logLevel,
	}

	// First pass: build the components, recording which names actually exist so the
	// dependency edges can be filtered to built-both-ends pairs. Instances expands
	// repeated ports (§M11) into one build per named instance — a singleton yields
	// one ComponentID (Name == Key); a port yields one per instance — so several
	// EtherTalk/TashTalk/IPX instances each become their own supervised component,
	// addressed by instance name.
	comps := make(map[string]component.Component)
	var order []string
	// rebuilders holds a Rebuilder per component built below, so a restart-driven
	// reconfigure (ApplyConfig returning component.ErrNeedsRestart — an interface
	// swap, most notably) reconstructs the component from the CURRENT model instead
	// of restarting the original object with whatever it resolved at process
	// startup. Without this a NIC-bound port (NetBEUI/IPX/EtherTalk/...) that
	// captured its pcap device once, at build time, would keep reopening that same
	// stale device forever — a live interface edit would update the model and the
	// namespace entry, but never the already-built port.
	rebuilders := make(map[string]supervisor.Rebuilder)
	for _, id := range src.Instances(opts.Model) {
		if stubNames[id.Key] {
			continue
		}
		// Client is built after the file services (second pass) so LocalVolumes can
		// resolve live AFP/SMB/NCP/EtherDFS components from the built map.
		if id.Key == config.ClientKey {
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
		key, instance := id.Key, id.Instance
		rebuilders[id.Name] = func(m *config.Model) (component.Component, error) {
			rctx := *ctx
			rctx.Model = m
			rctx.Instance = instance
			c, ok, err := src.Build(key, &rctx)
			if err != nil || !ok {
				return nil, err
			}
			return c, nil
		}
	}

	// Second pass (Client): the in-process file client lists live local shares from
	// the built file services, so it is registered after them.
	if client, ok, err := registry.BuildClient(ctx, comps); err != nil {
		return nil, fmt.Errorf("runtime: build %q: %w", config.ClientKey, err)
	} else if ok && client != nil {
		name := client.Name()
		if _, dup := comps[name]; dup {
			return nil, fmt.Errorf("runtime: duplicate component name %q", name)
		}
		comps[name] = client
		order = append(order, name)
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
	// service is a no-op. The returned wiring is retained so a port added at RUNTIME
	// can be attached to its mini-router (SetTransportAttacher, below) and so the
	// MacIP egress lifecycle can be driven from Start/Stop.
	transports := crossWireTransports(comps, opts.MacIPEgress, ctx.Logger)

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
	// whose dependency is also built). AddBuildable with a nil Rebuilder (Router,
	// reused up-front; Client, built after this loop) behaves exactly like Add.
	for _, name := range order {
		deps := builtDeps(name, comps)
		if name == config.ClientKey {
			deps = registry.ClientDeps(comps)
		}
		sup.AddBuildable(comps[name], deps, rebuilders[name])
	}

	// Inject the per-instance builder so the supervisor can stand up the FIRST instance of
	// a repeated port the operator adds at runtime (e.g. the first NetBEUI/IPX port from the
	// config-builder UI) — a port key that had no instance at startup has no supervised node,
	// so AddInstance must BUILD one. It reuses the same build context (router, openers, log
	// sinks) as the startup pass, with Instance set to the new instance's name; the supervisor
	// filters the returned deps against its live nodes. A nil/disabled build yields (nil,nil).
	baseCtx := *ctx
	sup.SetInstanceBuilder(func(m *config.Model, ownerKey, instanceName string) (component.Component, []string, error) {
		ictx := baseCtx
		ictx.Model = m
		ictx.Instance = instanceName
		c, ok, err := src.Build(ownerKey, &ictx)
		if err != nil || !ok || c == nil {
			return nil, nil, err
		}
		return c, declaredDeps(ownerKey, map[string]component.Component{ownerKey: c}), nil
	})

	rt := &Runtime{
		sup:        sup,
		model:      opts.Model,
		telemetry:  opts.Telemetry,
		rtr:        rtr,
		members:    members,
		built:      order,
		transports: transports,
		comps:      comps,
		log:        rtLog,
	}

	// Inject the port attacher so a PORT the supervisor stands up at runtime is joined to
	// the cross-wiring the compose root built once, here. Two moments need it:
	//
	//   - A repeated port ADDED at runtime (the InstanceBuilder above). Without this it
	//     came up as a live supervised link but stayed dark to the NetBIOS engines until a
	//     Save+restart rebuilt the whole stack.
	//   - A port REBUILT by a restart-driven reconfigure (an interface edit, most of all).
	//     The rebuild produces a NEW object; the mini-routers and the shared AppleTalk
	//     router still hold the one built at startup, so without the handover the operator
	//     fixes the NIC, the port dutifully reopens the right device — and still moves no
	//     traffic, because every wire still points at the object it replaced.
	//
	// rewirePort covers both families: the mini-routers (retained by crossWireTransports)
	// and the AppleTalk router membership. It is a no-op for a component of neither family
	// and for a build that wired no transports.
	sup.SetTransportAttacher(rt.rewirePort)

	return rt, nil
}

// rewirePort hands the compose-root cross-wiring from a port object to its replacement.
// prev is the object built earlier (nil when the port is brand new), next the one now
// supervised under that name. It is the supervisor's TransportAttacher (see
// supervisor.TransportAttacher) and runs while the supervisor holds its lock, straight
// after the rebuild and BEFORE the new object is started.
//
// Two wirings are re-pointed. The NBF/NBIPX mini-routers take the swap directly
// (transportWiring.AttachPort). The AppleTalk router is membership-based (§3d): only a
// port named in [Router].members is attached, so a replacement inherits its predecessor's
// membership — the stale entry is detached (the router keys ports by name, and prev is
// already stopped at this point) and the slot in r.members is re-pointed so a later
// Runtime.Stop detaches the object that is actually attached. The Attach itself is left
// to the caller's Start path: the router refuses to attach while stopped, and an
// AARP/LLAP port has not claimed its address yet — Runtime.Start's attach loop, which
// already handles both, runs against the updated members slice.
func (r *Runtime) rewirePort(prev, next component.Component) {
	r.transports.AttachPort(prev, next)
	if r.rtr == nil || prev == nil || next == nil {
		return
	}
	prevPort, ok := prev.(router.RoutedPort)
	if !ok {
		return
	}
	nextPort, ok := next.(router.RoutedPort)
	if !ok {
		return
	}
	for i, m := range r.members {
		if m != prevPort {
			continue
		}
		_ = r.rtr.Detach(prevPort)
		r.members[i] = nextPort
		r.attachMember(nextPort)
		return
	}
}

// router returns the shared AppleTalk router (nil if none built). Unexported — it
// is the cross-wire target, surfaced for tests; the control plane reaches routing
// through the supervisor/diagnostics, not this.
func (r *Runtime) router() *router.RouterImpl { return r.rtr }

// egress returns the MacIP IP-side egress the transport wiring built, or nil when the
// stack is AppleTalk-only (or wired no transports). Nil-safe so Start/Stop can call it
// without guarding the wiring pointer.
func (r *Runtime) egress() MacIPEgress {
	if r.transports == nil {
		return nil
	}
	return r.transports.egress
}

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
		// A service owning more than one DDP socket (netboot: ABP boot socket +
		// the ChainBoot EBP socket) exposes the extra bindings as thin shim
		// services; register them on their sockets alongside the component.
		if extra, ok := c.(interface{ ExtraRouterServices() []router.Service }); ok {
			for _, es := range extra.ExtraRouterServices() {
				rtr.RegisterService(es)
			}
		}
		if p, ok := c.(router.RoutedPort); ok && rsec.IsMember(name) {
			members = append(members, p)
		}
	}
	return members
}

// zoneSeeder is the optional port capability the runtime reads at attach: a seed port
// reports the zone name it asserts on its segment (runport.SeedZone). A port that does
// not implement it (or reports "") seeds no zone — it is a non-seed segment that learns
// its zone via ZIP from a neighbouring router instead.
type zoneSeeder interface{ SeedZone() string }

// seedZone installs a freshly-attached member port's directly-connected network range
// into the router's Zone Information Table under the port's configured seed zone. This is
// the missing half of seed-router bring-up: Attach installs the ROUTE (from the port's
// seed-preloaded NetworkMin/Max), and this installs the ZONE for that same range — so a
// self-contained seed router (no upstream router to learn zones from) has a zone to serve
// over ZIP, which is what makes the server + its zone appear in the Chooser. A port with
// no seed zone, or no seed range yet, is left to learn its zone via ZIP as before.
func seedZone(rtr *router.RouterImpl, p router.RoutedPort) {
	zs, ok := p.(zoneSeeder)
	if !ok {
		return
	}
	zone := zs.SeedZone()
	if zone == "" {
		return
	}
	nmin, nmax := p.NetworkMin(), p.NetworkMax()
	if nmin == 0 {
		return // non-seed / range not yet asserted; ZIP will learn the zone
	}
	if nmax == 0 {
		nmax = nmin
	}
	if err := rtr.Zones().AddNetworksToZone([]byte(zone), nmin, &nmax); err != nil {
		// A duplicate/overlap (e.g. a re-Attach after Stop→Start) is benign — the zone is
		// already known; only surface genuinely unexpected failures at debug level via the
		// router's own logging is not reachable here, so we simply ignore the idempotent case.
		_ = err
	}
}

// builtDeps returns name's hard dependencies, dropping any whose target was not
// built in this configuration (so a minimal build omits the edge instead of
// failing the topo sort on a missing node).
//
// The edges come from the COMPONENT itself when it implements component.DependsOn
// (each component owns and may config-vary its dependencies); hardDeps is only a
// fallback for components that have not yet adopted the capability. Once every edged
// component declares its own dependencies, hardDeps is empty and can be removed.
func builtDeps(name string, comps map[string]component.Component) []string {
	want := declaredDeps(name, comps)
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

// declaredDeps returns the unfiltered dependency names for a built component: the
// component's own DependsOn declaration when present, else the static hardDeps fallback.
func declaredDeps(name string, comps map[string]component.Component) []string {
	if c, ok := comps[name]; ok {
		if d, ok := c.(component.DependsOn); ok {
			return d.Dependencies()
		}
	}
	return hardDeps[name]
}

// Start brings the whole stack up in dependency order, then attaches the declared
// router members (§3d). Attach is deferred to here because the router rejects
// membership changes while stopped (§3); by now the supervisor's dependency order
// has brought the Router up ahead of its members. A failed attach is logged so a
// misrouted member does not keep the web UI from starting.
func (r *Runtime) Start(ctx context.Context) error {
	if err := r.sup.StartAll(ctx); err != nil {
		return err
	}
	// Bring the MacIP IP-side egress up once the stack is running (the MacIP service's
	// Start has wired the egress inbound callback). Not a supervised component — the
	// runtime owns its lifecycle. A nil egress (AppleTalk-only) is a no-op.
	if eg := r.egress(); eg != nil {
		eg.Start()
	}
	if r.rtr != nil {
		r.claimWatchStop = make(chan struct{})
		for _, p := range r.members {
			r.attachMember(p)
		}
	}
	// Begin the telemetry stats flush once the stack is up: it polls every Statful
	// component and wires push sinks, feeding the compose/stats rate collector and the
	// control plane's SSE stream (§5). A nil bus makes this a no-op.
	r.sup.StartStatsFlush(supervisor.DefaultStatsInterval)
	return nil
}

// attachMember attaches one [Router].members port to the shared router, installs its
// seed zone, and — when the port has not claimed an AppleTalk address yet — starts the
// watcher that finishes the install once the claim lands. Shared by Runtime.Start's
// bring-up loop and rewirePort's handover to a rebuilt member, so a port replaced at
// runtime rejoins routing on exactly the terms it joined on at boot. An attach failure is
// logged and skipped, never fatal: the rest of the members still route.
//
// The late-claim watcher is only started while the stack is running (claimWatchStop
// non-nil); outside that window Runtime.Start's own loop will run it.
func (r *Runtime) attachMember(p router.RoutedPort) {
	if err := r.rtr.Attach(p); err != nil {
		if r.log != nil {
			r.log.Log2(log.Error, "router member attach failed; continuing",
				log.Str("member", p.Name()), log.Str("err", err.Error()))
		}
		return
	}
	seedZone(r.rtr, p)
	if p.NetworkMin() != 0 || r.claimWatchStop == nil {
		return
	}
	// A real AARP/LLAP claim (EtherTalk/LToUDP/TashTalk) finishes in a background
	// goroutine well after Start returns (runport/aarp never blocks Start on the probe
	// burst) — Attach ran above with NetworkMin()==0, so its own directly-connected
	// route was skipped (router.go's `if nmin != 0 && nmax != 0` guard) and seedZone's
	// own zero-range guard skipped the ZIT too. Nothing else ever retries either
	// install: the port later announces its claimed range fine over RTMP and answers
	// same-network traffic fine (Inbound's same-network fast path needs no routing-table
	// entry), but every service reply that must round-trip through router.Reply→Route
	// (ZIP's ATP zone queries, AFP's ASP session) does RoutingTable.GetByNetwork and
	// gets a silent, permanent nil. Poll briefly for the claim to land and (re)run the
	// same install once it does — SetPortRange/AddNetworksToZone are both idempotent
	// against an already-correct entry, so this is a no-op on the fast path where the
	// claim beat Attach.
	r.claimWatchWG.Add(1)
	go r.awaitLateClaim(p, r.claimWatchStop)
}

// claimWatchInterval is the poll period awaitLateClaim uses while waiting for a
// member port's AARP/LLAP claim to land.
const claimWatchInterval = 100 * time.Millisecond

// claimWatchAttempts bounds how long awaitLateClaim polls before giving up (30 ×
// 100ms = 3s — generous over AARP's normal probe-burst duration; a port that has not
// claimed by then logs a warning and is left for its own retry/conflict logic).
const claimWatchAttempts = 30

// awaitLateClaim polls p for its AARP/LLAP claim to land, then installs its
// directly-connected route + seed zone (see the Start comment for why this install
// can be skipped at Attach time). Runs until the claim lands, claimWatchAttempts is
// exhausted, or stop is closed by Runtime.Stop. r.claimWatchWG.Done is deferred so
// Stop can wait out any watcher still polling before it detaches ports.
func (r *Runtime) awaitLateClaim(p router.RoutedPort, stop chan struct{}) {
	defer r.claimWatchWG.Done()
	for range claimWatchAttempts {
		select {
		case <-stop:
			return
		case <-time.After(claimWatchInterval):
		}
		if p.NetworkMin() == 0 {
			continue
		}
		r.rtr.RoutingTable().SetPortRange(p, p.NetworkMin(), p.NetworkMax())
		seedZone(r.rtr, p)
		return
	}
	if r.log != nil {
		r.log.Log1(log.Warn, "router member never claimed an address; routing table has no directly-connected entry for it",
			log.Str("member", p.Name()))
	}
}

// Stop detaches the router members (reversing Start's attach) and then brings the
// whole stack down in reverse dependency order. Detach is best-effort — a member
// already withdrawn (e.g. by an individual Stop) must not block shutdown.
func (r *Runtime) Stop(ctx context.Context) error {
	if r.claimWatchStop != nil {
		close(r.claimWatchStop)
		r.claimWatchWG.Wait()
		r.claimWatchStop = nil
	}
	if r.log != nil && r.log.Enabled(log.Info) {
		r.log.Log0(log.Info, "shutdown: stopping telemetry stats flush")
	}
	r.sup.StopStatsFlush()
	if eg := r.egress(); eg != nil {
		if r.log != nil && r.log.Enabled(log.Info) {
			r.log.Log0(log.Info, "shutdown: closing MacIP egress")
		}
		_ = eg.Close()
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
