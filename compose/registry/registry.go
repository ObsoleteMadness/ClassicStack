package registry

import (
	"io"
	"sort"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
	"github.com/ObsoleteMadness/ClassicStack/core/router"
)

// LinkOpener opens a raw L2 FrameLink for a NIC by name. It is the seam by which a
// port factory obtains a real device link WITHOUT core/ or this registry importing
// the pcap/cgo adapter: the compose runtime root selects the concrete opener (libpcap
// under the `pcap` tag, a stub otherwise) at the cmd edge and injects it here —
// exactly as the config Store/Codec are injected rather than imported. It is called
// per Start (a fresh handle each time) so a reopened port gets a new link. nil means
// no NIC backend in this build → NIC ports come up inert-but-routed (the
// graceful-degradation contract of BuildContext).
type LinkOpener func(iface string) (link.FrameLink, error)

// SerialOpener opens a serial device (by path + baud) and returns the raw byte
// stream — NOT a FrameLink. The transport framer (tashtalk today) wraps the stream
// into a FrameLink. It is the §3b/D7 "shared serial opener" injected at the cmd edge
// (adapter/serial) so this registry imports no serial library. nil → serial-kind
// ports come up inert. baud 0 means "the opener's default".
type SerialOpener func(device string, baud uint) (io.ReadWriteCloser, error)

// SerialFramer wraps an open serial byte stream into a core/link.FrameLink for one
// serial transport (tashtalk.NewStream). A factory whose interface kind is serial
// pairs the injected SerialOpener with its own SerialFramer: open the device once,
// frame the stream. Separating the two keeps the device-open (cgo-ish, cmd-edge
// injected) from the pure-Go framing (the adapter), per the kind→opener split.
type SerialFramer func(io.ReadWriteCloser) (link.FrameLink, error)

// BuildContext carries everything a factory needs to build a FULLY-WIRED component:
// the config model plus the shared collaborators a component binds to (§14). It
// replaces the bare *config.Model the factory used to receive — that signature
// could only build inert/unrouted components, which is why ports came up with a nil
// router and the macip factory returned a placeholder. The compose runtime root
// populates the collaborators (building the shared Router first) and hands one
// BuildContext to every factory, so a port/service is born already bound to the
// router rather than wired up afterwards through setters.
//
// A field is nil when its collaborator is not available in this build/config (e.g.
// Router is nil if the router component is not registered, or for a unit test that
// builds one component in isolation). A factory must tolerate a nil collaborator by
// building the inert/standalone form — the same graceful degradation the model-only
// path had.
type BuildContext struct {
	// Model is the shared, editable config model. Always set.
	Model *config.Model
	// Router is the shared AppleTalk router instance every DDP port and service
	// binds to (ports via router.Router, services via router.ServiceRouter +
	// RegisterService). nil when the router is not in this build. The runtime root
	// builds it before any dependent factory so this is populated for them.
	Router *router.RouterImpl
	// Telemetry is the bus stats/state/log are published on. May be nil.
	Telemetry bus.Bus
	// Opener builds a raw NIC FrameLink for a port's configured interface (kind nic
	// or bridge). nil when no NIC backend is in this build (e.g. a tag-free / TinyGo
	// build, or a unit test): a NIC port factory then builds the inert form. The
	// runtime root injects the concrete opener (pcap or its stub) at the cmd edge.
	Opener LinkOpener
	// Serial opens a serial byte stream for a port whose interface kind is serial
	// (device path + baud). nil when no serial backend is in this build: a serial
	// port factory then builds the inert form. Injected at the cmd edge
	// (adapter/serial) alongside Opener, so the kind→opener dispatch (M11.c/D6) can
	// pick NIC vs serial from the resolved interface rather than the port type.
	Serial SerialOpener
	// Instance is the per-instance name a REPEATED port factory should build (§M11):
	// a transport is a repeated section, so the runtime calls the factory once per
	// instance with Instance set to that instance's name, and the factory resolves
	// its section via port.InstanceFromModel(Model, key, Instance). Empty means the
	// singleton/default instance (a non-port factory, or a port config that still
	// uses a single section), so existing factories keep working unchanged.
	Instance string
}

// Factory builds a fully-wired component from its BuildContext. Returns the
// component or an error; a disabled section yields (nil, nil).
type Factory func(*BuildContext) (component.Component, error)

var (
	mu        sync.RWMutex
	factories = map[string]Factory{}
	// portKeys marks the registry keys that are REPEATED port schemas (§M11): the
	// runtime expands each into one component per named instance in Model.Lists[key],
	// rather than building a single component under the key. A key absent here is a
	// singleton (one component, BuildContext.Instance empty).
	portKeys = map[string]bool{}
)

// Register records a name->factory mapping. Call from a build-tagged init(): a component whose
// build tag is absent never registers, so the supervisor simply cannot Build it (the §8
// replacement for *_disabled.go). A later Register for the same name replaces the earlier one
// (last wins), allowing a build to override a default.
func Register(name string, f Factory) {
	mu.Lock()
	defer mu.Unlock()
	factories[name] = f
}

// RegisterPort records a REPEATED port factory under its schema key: the runtime
// expands it into one component per named instance (Instances), each built with
// BuildContext.Instance set. Otherwise identical to Register. Call from a port
// package's build-tagged init().
func RegisterPort(key string, f Factory) {
	mu.Lock()
	defer mu.Unlock()
	factories[key] = f
	portKeys[key] = true
}

// IsPort reports whether key was registered as a repeated port schema.
func IsPort(key string) bool {
	mu.RLock()
	defer mu.RUnlock()
	return portKeys[key]
}

// Build constructs the named component from the context. ok=false means the name was never
// registered (a clean not-found, NOT an error — the caller logs "requested but not built").
// A registered factory that returns (nil, nil) for a disabled section yields (nil, true, nil).
//
// For a repeated port key the name is the SCHEMA key and ctx.Instance selects which
// instance to build; the runtime drives this once per instance (see Instances). A
// singleton leaves ctx.Instance empty.
func Build(name string, ctx *BuildContext) (component.Component, bool, error) {
	mu.RLock()
	f, ok := factories[name]
	mu.RUnlock()
	if !ok {
		return nil, false, nil
	}
	c, err := f(ctx)
	return c, true, err
}

// ComponentID identifies one component to build: its registry Key plus, for a
// repeated port, the Instance name (empty for a singleton). Name is the identity the
// built component reports and the supervisor addresses it by.
type ComponentID struct {
	Key      string // registry/schema key ("EtherTalk", "AFP", "Router")
	Instance string // repeated-port instance name ("et-lab"); "" for a singleton
	Name     string // component identity: Instance for a port, else Key
}

// Instances expands the registered components against the model into the full set
// of components to build: a singleton yields one ComponentID (Name == Key); a
// repeated port key yields one per named instance in Model.Lists[key]. A repeated
// port with NO instances in the model yields none (nothing enabled to build) —
// callers that want a default singleton must add an instance. Order is deterministic
// (Names() is sorted; instances keep model/document order).
func Instances(m *config.Model) []ComponentID {
	var out []ComponentID
	for _, key := range Names() {
		if !IsPort(key) {
			out = append(out, ComponentID{Key: key, Name: key})
			continue
		}
		for _, s := range m.List(key) {
			inst := ""
			if ns, ok := s.(config.NamedSection); ok {
				inst = ns.InstanceName()
			}
			if inst == "" {
				inst = key
			}
			out = append(out, ComponentID{Key: key, Instance: inst, Name: inst})
		}
	}
	return out
}

// sectionMACFor resolves a port instance's configured station MAC as a fixed
// [6]byte (the form the frame-port constructors take, for repeated instances). An
// absent or malformed MAC yields the zero address — the constructor then falls back
// to the interface's own hardware address at open time. Shared by the IPX/NetBEUI
// factories.
func sectionMACFor(sec *port.Section) [6]byte {
	if sec.MAC == "" {
		return [6]byte{}
	}
	mac, err := port.ParseMAC(sec.MAC)
	if err != nil {
		return [6]byte{}
	}
	return mac
}

// Names returns the registered component names, sorted for deterministic iteration.
func Names() []string {
	mu.RLock()
	out := make([]string, 0, len(factories))
	for name := range factories {
		out = append(out, name)
	}
	mu.RUnlock()
	sort.Strings(out)
	return out
}
