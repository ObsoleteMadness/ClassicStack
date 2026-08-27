package runtime

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/compose/registry"
	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
	"github.com/ObsoleteMadness/ClassicStack/core/router"
)

// fakeSource is an in-test componentSource: a name→factory map, so a Runtime test
// neither depends on whatever build-tagged components registered globally nor
// pollutes the global registry singleton.
type fakeSource map[string]registry_factory

type registry_factory func(*registry.BuildContext) (component.Component, error)

// Instances treats every fake entry as a singleton (Name == Key, no instance
// expansion) — the runtime's repeated-port expansion is exercised in the registry
// tests; here we only need the singleton path.
func (s fakeSource) Instances(*config.Model) []registry.ComponentID {
	names := make([]string, 0, len(s))
	for n := range s {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]registry.ComponentID, 0, len(names))
	for _, n := range names {
		out = append(out, registry.ComponentID{Key: n, Name: n})
	}
	return out
}

func (s fakeSource) Build(name string, ctx *registry.BuildContext) (component.Component, bool, error) {
	f, ok := s[name]
	if !ok {
		return nil, false, nil
	}
	c, err := f(ctx)
	return c, true, err
}

// --- test component that records Start/Stop order on a shared log ---

type recComponent struct {
	name string
	log  *startLog
	deps []string // declared start-order edges (component.DependsOn)
}

func (c *recComponent) Name() string { return c.name }
func (c *recComponent) Start(context.Context) error {
	c.log.add("start:" + c.name)
	return nil
}
func (c *recComponent) Stop(context.Context) error {
	c.log.add("stop:" + c.name)
	return nil
}
func (c *recComponent) Dependencies() []string { return c.deps }

type startLog struct {
	mu  sync.Mutex
	seq []string
}

func (l *startLog) add(s string) { l.mu.Lock(); l.seq = append(l.seq, s); l.mu.Unlock() }
func (l *startLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.seq...)
}

// indexOf returns the position of s in seq, or -1.
func indexOf(seq []string, s string) int {
	for i, v := range seq {
		if v == s {
			return i
		}
	}
	return -1
}

// --- fake config Store + Codec for Load tests ---

type fakeStore struct {
	data    []byte
	loadErr error
}

func (s *fakeStore) Load() ([]byte, error)       { return s.data, s.loadErr }
func (s *fakeStore) Save([]byte) (string, error) { return "", nil }

type fakeCodec struct {
	unmarshalErr error
}

func (c *fakeCodec) Marshal(*config.Model) ([]byte, error) { return nil, nil }
func (c *fakeCodec) Unmarshal(_ []byte, m *config.Model) error {
	if c.unmarshalErr != nil {
		return c.unmarshalErr
	}
	m.Identity.Hostname = "DECODED"
	return nil
}

func TestLoad_MissingFileYieldsDefaults(t *testing.T) {
	// Store.Load returning (nil,nil) is "no config yet" — Load must return a model
	// without invoking the codec (nothing to decode).
	m, err := Load(&fakeStore{data: nil}, &fakeCodec{unmarshalErr: errors.New("must not be called")})
	if err != nil {
		t.Fatalf("Load with empty store = %v, want nil", err)
	}
	if m == nil {
		t.Fatal("Load returned a nil model")
	}
	if m.Identity.Hostname != "" {
		t.Fatalf("expected default (empty) hostname, got %q", m.Identity.Hostname)
	}
}

func TestLoad_DecodesPresentConfig(t *testing.T) {
	m, err := Load(&fakeStore{data: []byte("anything")}, &fakeCodec{})
	if err != nil {
		t.Fatalf("Load = %v, want nil", err)
	}
	if m.Identity.Hostname != "DECODED" {
		t.Fatalf("codec.Unmarshal did not run: hostname = %q, want DECODED", m.Identity.Hostname)
	}
}

func TestLoad_StoreErrorPropagates(t *testing.T) {
	_, err := Load(&fakeStore{loadErr: errors.New("disk gone")}, &fakeCodec{})
	if err == nil {
		t.Fatal("expected an error from a failing store, got nil")
	}
}

// TestBuild_SkipsStubsAndSupervises registers two real test components plus the
// reserved stub names, then proves Build constructs only the real ones and the
// supervisor starts/stops them.
func TestBuild_SkipsStubsAndSupervises(t *testing.T) {
	log := &startLog{}
	src := fakeSource{
		"rt-solo": func(*registry.BuildContext) (component.Component, error) {
			return &recComponent{name: "rt-solo", log: log}, nil
		},
		// A reserved stub name must be skipped even though it is registered.
		"stub-a": func(*registry.BuildContext) (component.Component, error) {
			return &recComponent{name: "stub-a", log: log}, nil
		},
	}

	rt, err := Build(Options{Model: config.NewModel(), Telemetry: bus.New(8), source: src})
	if err != nil {
		t.Fatalf("Build = %v", err)
	}
	if indexOf(rt.Built(), "rt-solo") < 0 {
		t.Fatalf("Built() = %v, want to contain rt-solo", rt.Built())
	}
	if indexOf(rt.Built(), "stub-a") >= 0 {
		t.Fatalf("Built() = %v, must NOT contain the reserved stub name", rt.Built())
	}

	ctx := context.Background()
	if err := rt.Start(ctx); err != nil {
		t.Fatalf("Start = %v", err)
	}
	if err := rt.Stop(ctx); err != nil {
		t.Fatalf("Stop = %v", err)
	}
	seq := log.snapshot()
	if indexOf(seq, "start:rt-solo") < 0 || indexOf(seq, "stop:rt-solo") < 0 {
		t.Fatalf("rt-solo was not started and stopped: %v", seq)
	}
	if indexOf(seq, "start:stub-a") >= 0 {
		t.Fatal("a reserved stub component was started")
	}
}

// TestBuild_HardDepOrdering proves a built dependency starts before its dependent
// and stops after it. The dependent declares its edge via component.DependsOn (the
// per-component dependency capability that replaced the static hardDeps map).
func TestBuild_HardDepOrdering(t *testing.T) {
	log := &startLog{}
	mk := func(name string, deps ...string) registry_factory {
		return func(*registry.BuildContext) (component.Component, error) {
			return &recComponent{name: name, log: log, deps: deps}, nil
		}
	}
	src := fakeSource{"Router": mk("Router"), "AFP": mk("AFP", "Router")}

	rt, err := Build(Options{Model: config.NewModel(), Telemetry: nil, source: src})
	if err != nil {
		t.Fatalf("Build = %v", err)
	}
	ctx := context.Background()
	if err := rt.Start(ctx); err != nil {
		t.Fatalf("Start = %v", err)
	}
	if err := rt.Stop(ctx); err != nil {
		t.Fatalf("Stop = %v", err)
	}
	seq := log.snapshot()

	// AFP depends on Router: Router starts first, stops last.
	if indexOf(seq, "start:Router") > indexOf(seq, "start:AFP") {
		t.Fatalf("Router must start before AFP: %v", seq)
	}
	if indexOf(seq, "stop:AFP") > indexOf(seq, "stop:Router") {
		t.Fatalf("AFP must stop before Router: %v", seq)
	}
}

// TestBuild_DropsEdgeWhenDependencyAbsent proves a declared edge whose target was
// not built is dropped rather than failing the topo sort. SMB→NetBEUI: register SMB
// (declaring the NetBEUI edge) but NOT NetBEUI; Build must still succeed and start SMB.
func TestBuild_DropsEdgeWhenDependencyAbsent(t *testing.T) {
	log := &startLog{}
	src := fakeSource{
		"SMB": func(*registry.BuildContext) (component.Component, error) {
			return &recComponent{name: "SMB", log: log, deps: []string{"NetBEUI"}}, nil
		},
		// Deliberately omit NetBEUI from this test build.
	}

	rt, err := Build(Options{Model: config.NewModel(), source: src})
	if err != nil {
		t.Fatalf("Build with a missing dependency target = %v, want nil (edge dropped)", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start = %v", err)
	}
	if indexOf(log.snapshot(), "start:SMB") < 0 {
		t.Fatalf("SMB was not started: %v", log.snapshot())
	}
}

// TestBuild_FactoryErrorAborts proves a factory error aborts the whole build (a
// misconfigured component is not silently dropped).
func TestBuild_FactoryErrorAborts(t *testing.T) {
	src := fakeSource{
		"rt-bad": func(*registry.BuildContext) (component.Component, error) {
			return nil, errors.New("bad spec")
		},
	}
	_, err := Build(Options{Model: config.NewModel(), source: src})
	if err == nil {
		t.Fatal("expected Build to abort on a factory error, got nil")
	}
}

func TestBuild_RequiresModel(t *testing.T) {
	if _, err := Build(Options{Model: nil}); err == nil {
		t.Fatal("expected Build to reject a nil model")
	}
}

// --- cross-wire test: a DDP service in the build set is registered on the shared
// router so the router dispatches an inbound datagram to it. ---

// fakeDDPService is a minimal router.Service: it listens on a socket and records
// the datagrams the router delivers to it.
type fakeDDPService struct {
	sock     uint8
	received int
}

func (s *fakeDDPService) Name() string                { return "FakeDDP" }
func (s *fakeDDPService) Start(context.Context) error { return nil }
func (s *fakeDDPService) Stop(context.Context) error  { return nil }
func (s *fakeDDPService) Socket() uint8               { return s.sock }
func (s *fakeDDPService) Inbound(ddp.Datagram, router.RoutedPort) {
	s.received++
}

// fakeFrom is a minimal rx port for driving router.Inbound (only Network()/Node()
// are consulted on the local-delivery path).
type fakeFrom struct{ router.RoutedPort }

func (fakeFrom) Name() string    { return "FakeFrom" }
func (fakeFrom) Network() uint16 { return 0 }
func (fakeFrom) Node() uint8     { return 0 }

// TestBuild_CrossWiresServiceOntoRouter proves the runtime root binds a built DDP
// service to the shared router: a datagram addressed to the service's socket,
// pushed through the router, reaches the service — which only happens if Build
// called RegisterService during cross-wiring.
func TestBuild_CrossWiresServiceOntoRouter(t *testing.T) {
	const sock = 200
	svc := &fakeDDPService{sock: sock}
	src := fakeSource{
		router.Name: func(*registry.BuildContext) (component.Component, error) {
			return router.New(log.New(router.Name)), nil
		},
		"FakeDDP": func(*registry.BuildContext) (component.Component, error) {
			return svc, nil
		},
	}

	rt, err := Build(Options{Model: config.NewModel(), source: src})
	if err != nil {
		t.Fatalf("Build = %v", err)
	}
	// Start so the router is running (Inbound dispatch needs nothing more for a
	// socket-local delivery, but Start mirrors real use).
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start = %v", err)
	}
	defer rt.Stop(context.Background())

	// Resolve the shared router from the build and drive a datagram to the socket.
	rtr := rt.router()
	if rtr == nil {
		t.Fatal("runtime built no router")
	}
	rtr.Inbound(ddp.Datagram{DestSocket: sock}, fakeFrom{})

	if svc.received != 1 {
		t.Fatalf("service received %d datagrams, want 1 (not cross-wired onto the router)", svc.received)
	}
}

// fakeRoutedPort is a minimal component.Component + router.RoutedPort: it does no
// real I/O, it exists so the runtime's membership gate (§3d) can be observed via
// the router's Ports() set.
type fakeRoutedPort struct{ name string }

func (p *fakeRoutedPort) Name() string                        { return p.name }
func (p *fakeRoutedPort) Start(context.Context) error         { return nil }
func (p *fakeRoutedPort) Stop(context.Context) error          { return nil }
func (p *fakeRoutedPort) Unicast(uint16, uint8, ddp.Datagram) {}
func (p *fakeRoutedPort) Broadcast(ddp.Datagram)              {}
func (p *fakeRoutedPort) Multicast([]byte, ddp.Datagram)      {}
func (p *fakeRoutedPort) Network() uint16                     { return 0 }
func (p *fakeRoutedPort) Node() uint8                         { return 0 }
func (p *fakeRoutedPort) NetworkMin() uint16                  { return 0 }
func (p *fakeRoutedPort) NetworkMax() uint16                  { return 0 }

// attachedPorts returns the names of the ports currently attached to the router.
func attachedPorts(rtr *router.RouterImpl) map[string]bool {
	out := map[string]bool{}
	for _, p := range rtr.Ports() {
		out[p.Name()] = true
	}
	return out
}

// buildWithMembers assembles a runtime whose model has the given router members and
// two named RoutedPorts, returning the shared router so a test can inspect which
// ports were attached.
func buildWithMembers(t *testing.T, members []string) *router.RouterImpl {
	t.Helper()
	m := config.NewModel()
	m.Router = config.RouterSection{Members: members}
	src := fakeSource{
		router.Name: func(*registry.BuildContext) (component.Component, error) {
			return router.New(log.New(router.Name)), nil
		},
		"et-lab": func(*registry.BuildContext) (component.Component, error) {
			return &fakeRoutedPort{name: "et-lab"}, nil
		},
		"et-dmz": func(*registry.BuildContext) (component.Component, error) {
			return &fakeRoutedPort{name: "et-dmz"}, nil
		},
	}
	rt, err := Build(Options{Model: m, source: src})
	if err != nil {
		t.Fatalf("Build = %v", err)
	}
	rtr := rt.router()
	if rtr == nil {
		t.Fatal("runtime built no router")
	}
	// Membership attach is deferred to Start (the router rejects attach while
	// stopped, §3) — so start the runtime before inspecting the attached set.
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start = %v", err)
	}
	t.Cleanup(func() { rt.Stop(context.Background()) })
	return rtr
}

// TestBuild_RouterMembersAttachesOnlyListed proves §3d/D8: only the port instances
// NAMED in [Router].members are Attached to the router; an enabled-but-unlisted port
// runs standalone (built and supervised, but not a router member).
func TestBuild_RouterMembersAttachesOnlyListed(t *testing.T) {
	rtr := buildWithMembers(t, []string{"et-lab"})
	got := attachedPorts(rtr)
	if !got["et-lab"] {
		t.Errorf("et-lab is in members but was not attached: %v", got)
	}
	if got["et-dmz"] {
		t.Errorf("et-dmz is NOT in members but was attached (should run standalone): %v", got)
	}
}

// TestBuild_RouterEmptyMembersAttachesNone proves D9 (opt-in): an empty/unspecified
// members list attaches NO ports — the deliberate divergence from the legacy
// "empty = bind every enabled transport" default.
func TestBuild_RouterEmptyMembersAttachesNone(t *testing.T) {
	rtr := buildWithMembers(t, nil)
	if got := attachedPorts(rtr); len(got) != 0 {
		t.Errorf("empty members attached %v, want none (membership is opt-in)", got)
	}
}

// TestBuild_RouterMembersAttachesAllListed proves both named instances join when
// both appear in members (the multi-drop AppleTalk router case).
func TestBuild_RouterMembersAttachesAllListed(t *testing.T) {
	rtr := buildWithMembers(t, []string{"et-lab", "et-dmz"})
	got := attachedPorts(rtr)
	if !got["et-lab"] || !got["et-dmz"] {
		t.Errorf("both listed ports should be attached, got %v", got)
	}
}

// fakeSeedPort is a member port that asserts a seed network range + zone, like a real
// seed EtherTalk/LToUDP port after its range is pre-loaded from config.
type fakeSeedPort struct {
	fakeRoutedPort
	nmin, nmax uint16
	zone       string
}

func (p *fakeSeedPort) NetworkMin() uint16 { return p.nmin }
func (p *fakeSeedPort) NetworkMax() uint16 { return p.nmax }
func (p *fakeSeedPort) SeedZone() string   { return p.zone }

// lateClaimPort simulates a real AARP-based EtherTalk port: NetworkMin/Max are 0 when
// Start returns (matching runport/aarp's async claimLoop, which probes over the wire in
// a background goroutine and calls SetAddress only once a node address is accepted —
// Start itself never blocks on it). The range becomes available `delay` after Start.
type lateClaimPort struct {
	fakeRoutedPort
	mu         sync.Mutex
	nmin, nmax uint16
	zone       string
	delay      time.Duration
}

func (p *lateClaimPort) Start(ctx context.Context) error {
	go func() {
		time.Sleep(p.delay)
		p.mu.Lock()
		p.nmin, p.nmax = 3, 5
		p.mu.Unlock()
	}()
	return p.fakeRoutedPort.Start(ctx)
}
func (p *lateClaimPort) NetworkMin() uint16 { p.mu.Lock(); defer p.mu.Unlock(); return p.nmin }
func (p *lateClaimPort) NetworkMax() uint16 { p.mu.Lock(); defer p.mu.Unlock(); return p.nmax }
func (p *lateClaimPort) SeedZone() string   { return p.zone }

// TestStart_LateClaimingMemberNeverJoinsRoutingTable is the regression guard for the
// dead-ZIP/ASP-reply bug: Runtime.Start attaches + seeds each router member SYNCHRONOUSLY
// right after StartAll returns (runtime.go's Start loop), but a real EtherTalk port's AARP
// claim finishes in a background goroutine — Start does not wait for it. When the claim
// lands after Attach already ran with NetworkMin()==0, router.Attach's own
// `if nmin != 0 && nmax != 0 { SetPortRange }` guard skips installing the directly-connected
// route, and nothing ever retries it: the port later announces its range correctly over RTMP
// and answers same-network unicast fine, but any reply that must round-trip through
// router.Reply→Route (ZIP's ATP zone queries, AFP's ASP session reads) does
// `RoutingTable.GetByNetwork(net)` and gets a permanent nil — the reply is dropped with no
// error, forever, even though the port is otherwise live. This proves the race exists today.
func TestStart_LateClaimingMemberNeverJoinsRoutingTable(t *testing.T) {
	m := config.NewModel()
	m.Router = config.RouterSection{Members: []string{"et0"}}
	port := &lateClaimPort{
		fakeRoutedPort: fakeRoutedPort{name: "et0"},
		zone:           "EtherTalk Network",
		delay:          20 * time.Millisecond,
	}
	src := fakeSource{
		router.Name: func(*registry.BuildContext) (component.Component, error) {
			return router.New(log.New(router.Name)), nil
		},
		"et0": func(*registry.BuildContext) (component.Component, error) { return port, nil },
	}
	rt, err := Build(Options{Model: m, source: src})
	if err != nil {
		t.Fatalf("Build = %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start = %v", err)
	}
	t.Cleanup(func() { rt.Stop(context.Background()) })

	// Give the AARP-simulating goroutine time to land its claim (well past `delay`) and
	// the runtime's late-claim watcher (which polls every claimWatchInterval) a full
	// cycle to notice and install the route.
	time.Sleep(250 * time.Millisecond)

	if got := port.NetworkMin(); got != 3 {
		t.Fatalf("port never claimed its range in this test setup: NetworkMin=%d", got)
	}

	rtr := rt.router()
	// GetByNetwork's second return is a "marked bad" flag, not a found/ok flag (every
	// caller in router.go checks entry == nil instead) — a fresh entry is state-good,
	// so that bool is false here even on success.
	if entry, _ := rtr.RoutingTable().GetByNetwork(3); entry == nil {
		t.Errorf("REGRESSION: network 3 has no routing-table entry even after the port claimed" +
			" range 3-5 — router.Reply()'s Route() call will silently drop any service reply" +
			" addressed to this network forever, because Attach ran while NetworkMin() was still 0")
	}
	found := false
	for _, z := range rtr.Zones().Zones() {
		if string(z) == "EtherTalk Network" {
			found = true
		}
	}
	if !found {
		t.Errorf("REGRESSION: seed zone never installed into the ZIT — seedZone() also ran while" +
			" NetworkMin() was still 0 and its own `if nmin == 0 { return }` guard skipped it")
	}
}

// TestStart_SeedsMemberZoneIntoZIT is the regression guard for the empty-Chooser bug:
// when a seed member port attaches, the runtime must install its network range into the
// router's Zone Information Table under the port's seed zone, so a self-contained seed
// router has a zone to advertise over ZIP (previously nothing seeded the ZIT and Chooser
// showed no zones/server).
func TestStart_SeedsMemberZoneIntoZIT(t *testing.T) {
	m := config.NewModel()
	m.Router = config.RouterSection{Members: []string{"lt0"}}
	src := fakeSource{
		router.Name: func(*registry.BuildContext) (component.Component, error) {
			return router.New(log.New(router.Name)), nil
		},
		"lt0": func(*registry.BuildContext) (component.Component, error) {
			return &fakeSeedPort{
				fakeRoutedPort: fakeRoutedPort{name: "lt0"},
				nmin:           1, nmax: 2, zone: "LToUDP Network",
			}, nil
		},
	}
	rt, err := Build(Options{Model: m, source: src})
	if err != nil {
		t.Fatalf("Build = %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start = %v", err)
	}
	t.Cleanup(func() { rt.Stop(context.Background()) })

	zones := rt.router().Zones().Zones()
	found := false
	for _, z := range zones {
		if string(z) == "LToUDP Network" {
			found = true
		}
	}
	if !found {
		t.Fatalf("seed zone not installed into ZIT after attach; zones = %v", zones)
	}
}

// nicPortSection is a minimal config.Section that names an interface — the shape
// every real NIC-bound port's section takes (NetBEUI/IPX/EtherTalk/... all resolve
// their device through an `iface` reference into the namespace at build time).
type nicPortSection struct {
	iface config.InterfaceSection
}

func (s nicPortSection) Key() string                        { return "port" }
func (s nicPortSection) Clone() config.Section              { return s }
func (s nicPortSection) Validate() error                    { return nil }
func (s nicPortSection) Interface() config.InterfaceSection { return s.iface }

var _ config.InterfaceProvider = nicPortSection{}

// nicPortComp is a component.Configurable standing in for a real NIC port: it
// resolves its device once, at build time, from the BuildContext.Model, and
// ApplyConfig always signals ErrNeedsRestart (a NIC port has no live hot-apply —
// any config change, an interface swap most of all, needs a fresh open). Start
// records the device it was built with into a shared slot so a test can tell
// whether a later restart reused the SAME built object (stale device) or a freshly
// rebuilt one (current device).
type nicPortComp struct {
	device      string
	lastStarted *string
}

func (c *nicPortComp) Name() string                { return "port" }
func (c *nicPortComp) Start(context.Context) error { *c.lastStarted = c.device; return nil }
func (c *nicPortComp) Stop(context.Context) error  { return nil }
func (c *nicPortComp) ApplyConfig(any) error       { return component.ErrNeedsRestart }

var _ component.Configurable = (*nicPortComp)(nil)

// TestSetInterface_RebuildsNICPort is the regression guard for a live bridge-device
// edit (the web UI's Change Interface) never reaching an already-built NIC port —
// exactly what NetBEUI/IPX/EtherTalk hit in production. Build wired every component
// with a plain Add (no Rebuilder), so the restart-driven reconfigure ApplyConfig's
// ErrNeedsRestart triggers just stopped and restarted the SAME component object,
// which had resolved its device once, at initial Build, and never again — the port
// kept reopening the OLD device no matter how many times the interface was edited.
func TestSetInterface_RebuildsNICPort(t *testing.T) {
	m := config.NewModel()
	m.Set(nicPortSection{iface: config.InterfaceSection{Name: "br-lan"}})
	m.SetInterface(config.InterfaceSection{
		Name: "br-lan", Kind: config.IfaceKindNIC, Backend: config.IfaceBackendPcap, Device: "en5",
	})

	var lastStarted string
	src := fakeSource{
		"port": func(ctx *registry.BuildContext) (component.Component, error) {
			sec, _ := ctx.Model.Get("port")
			return &nicPortComp{
				device:      ctx.Model.EffectiveInterfaceFor(sec).Device,
				lastStarted: &lastStarted,
			}, nil
		},
	}

	rt, err := Build(Options{Model: m, source: src})
	if err != nil {
		t.Fatalf("Build = %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start = %v", err)
	}
	t.Cleanup(func() { rt.Stop(context.Background()) })

	if lastStarted != "en5" {
		t.Fatalf("initial device = %q, want en5", lastStarted)
	}

	if err := rt.Supervisor().SetInterface(context.Background(), config.InterfaceSection{
		Name: "br-lan", Kind: config.IfaceKindNIC, Backend: config.IfaceBackendPcap, Device: "en1",
	}); err != nil {
		t.Fatalf("SetInterface = %v", err)
	}

	if lastStarted != "en1" {
		t.Fatalf("device after SetInterface = %q, want en1 (component must be rebuilt, not just restarted)", lastStarted)
	}
}
