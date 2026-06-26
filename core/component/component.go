package component

import (
	"context"
	"errors"
)

// Component is the lifecycle every port, service, router, and transport satisfies.
// Start MUST be idempotent (calling it on a started component returns nil). Stop MUST be
// safe after a failed/partial Start. Neither blocks indefinitely; honour ctx.
type Component interface {
	Name() string
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// --- Optional capabilities. A component implements only those that apply; callers
// --- discover them via type assertion. NEVER widen Component to include these.

type Enableable interface{ Enabled() bool } // configured-enabled (≠ running)
type Bindable interface{ Binding() string } // "eth0", ":548", "ipx:0550"
type Statful interface{ Stats() Stats }     // point-in-time snapshot (§5)

// StatsEmitter is the PUSH half of the stats contract (§5): a component that can report
// a meaningful change immediately (a session opened, a lease assigned) accepts a sink
// and calls it with a fresh snapshot when it wants, rather than waiting for the next
// poll. The supervisor supplies the sink (a closure that wraps the snapshot in a
// bus.StatSample and publishes it). Optional and complementary to Statful: a component
// that only implements Statful is covered by the supervisor's periodic flush, which is
// what keeps gauges (leases, sessions) fresh while idle; one that also implements
// StatsEmitter additionally pushes on change for low-latency updates. A nil sink clears.
type StatsEmitter interface{ SetStatsSink(func(Stats)) }

// Describable lets a component surface dashboard metadata beyond its lifecycle: a
// Kind label (e.g. "service", "port", "router") and free-form Props a UI renders as
// key/value detail (bound transports, zones, share/volume counts). Optional, like the
// other capabilities — the supervisor type-asserts it in Status() and leaves Kind ""
// / Props nil for components that do not implement it.
type Describable interface {
	Kind() string
	Props() map[string]string
}
type Bridged interface{ SetBridgeMode(string) error } // §2
type Metered interface {
	SetTrafficObserver(func(rxBytes, txBytes int))
} // §5

// Configurable hot-applies a new config section without restart when it can. It MUST
// return ErrNeedsRestart (not some other error) when the change can't be applied live,
// so the supervisor falls back to restart-and-notify (§11). `section` is the component's
// typed config.Section (§4), passed as any to avoid a core import cycle.
type Configurable interface{ ApplyConfig(section any) error }

// Attachable models a SOFT binding (e.g. a transport into NetBIOS, §11d): attach/detach
// are re-runnable side effects of the OWNER's start/stop, not a hard DAG dependency.
type Attachable interface {
	Attach(ctx context.Context) error
	Detach(ctx context.Context) error
}

// DependsOn lets a component DECLARE its own hard start-order edges — the component
// names that must be RUNNING before it starts (and stop after it). Optional: a
// component with no edges omits it. The result is read from the CONSTRUCTED component,
// so it may vary by how the component was configured (e.g. SMB depends on "NetBEUI"
// only when its NetBEUI transport binding is on). This inverts the old composition-root
// static map: each component owns its dependencies. The runtime filters the returned
// names to those whose target was also built in this configuration, so a minimal build
// simply drops an edge to an absent component rather than failing the topo sort.
type DependsOn interface{ Dependencies() []string }

// TransportBinder lets a service DECLARE which named transport families it wants bound,
// so the compose root wires only those WITHOUT re-reading the service's config section
// itself. Returns the lower-cased family names the service understands (e.g. "ipx",
// "netbeui", "nbt", "tcp"). Optional: a service that takes no transport bindings omits
// it. Like DependsOn, the value comes from the constructed component, so it reflects the
// service's own configuration — the root asks the component instead of interrogating the
// model on its behalf (§transport-families).
type TransportBinder interface{ BoundTransports() []string }

// HostnameConstrainer lets a component DECLARE that it imposes a constraint on the
// server hostname when it is enabled (e.g. NetBIOS requires ≤15 bytes). The supervisor
// aggregates this across the live component set so config validation can apply the rule
// WITHOUT the management plane naming any specific service. Constraint is a stable key
// the config validator understands (e.g. "netbios"). Optional: a component with no
// hostname constraint omits it.
type HostnameConstrainer interface {
	HostnameConstraint() (constraint string, active bool)
}

// Stats is the typed (no-reflection) snapshot Statful returns and StatSample carries (§5).
type Stats struct {
	Counters map[string]uint64  // monotonic: frames_rx, bytes_tx, decode_errors, …
	Gauges   map[string]float64 // point-in-time: routes, active_leases, open_sessions, …
}

// ErrNeedsRestart is the sentinel ApplyConfig returns for structural changes (errors.Is).
var ErrNeedsRestart = errors.New("component: change needs restart")
