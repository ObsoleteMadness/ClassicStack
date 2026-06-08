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

type Enableable interface{ Enabled() bool }           // configured-enabled (≠ running)
type Bindable interface{ Binding() string }           // "eth0", ":548", "ipx:0550"
type Statful interface{ Stats() Stats }               // point-in-time snapshot (§5)
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
