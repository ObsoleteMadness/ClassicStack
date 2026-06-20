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

// Stats is the typed (no-reflection) snapshot Statful returns and StatSample carries (§5).
type Stats struct {
	Counters map[string]uint64  // monotonic: frames_rx, bytes_tx, decode_errors, …
	Gauges   map[string]float64 // point-in-time: routes, active_leases, open_sessions, …
}

// ErrNeedsRestart is the sentinel ApplyConfig returns for structural changes (errors.Is).
var ErrNeedsRestart = errors.New("component: change needs restart")
