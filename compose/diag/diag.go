// Package diag is the neutral control-plane diagnostics impl: it answers the one
// protocol-neutral probe the management plane still carries — ListZones, from the
// AppleTalk router's ZIP-populated zone table. It replaces the core's default
// "unavailable" diagnostics once the runtime is built; the cmd/compose edge wires it via
// control.Plane.SetDiagnostics, so core/control stays free of router knowledge.
//
// The PROTOCOL-specific drill-downs (NBP names, MacIP leases) are NOT here — they would
// leak a protocol DTO into the neutral plane. They are served by the diagnostics ADAPTER
// (adapter/control/diag), which imports the service packages and bridges them to the
// web/ubus front-ends directly. This package only adds the router's zone list.
//
// Ring: COMPOSE (it knows core/control and the core router).
package diag

import (
	"context"
	"sort"

	"github.com/ObsoleteMadness/ClassicStack/core/control"
	"github.com/ObsoleteMadness/ClassicStack/core/router"
)

// Diagnostics answers ListZones from the router. A nil router reports
// control.ErrUnavailable.
type Diagnostics struct {
	rtr *router.RouterImpl
}

// New builds a Diagnostics over the shared router. Pass nil for a no-router build.
func New(rtr *router.RouterImpl) *Diagnostics { return &Diagnostics{rtr: rtr} }

// ListZones returns the AppleTalk zones the router knows (from the ZIP-populated zone
// information table), sorted for stable output. ErrUnavailable when no router is wired.
func (d *Diagnostics) ListZones(_ context.Context) ([]string, error) {
	if d.rtr == nil {
		return nil, control.ErrUnavailable
	}
	raw := d.rtr.Zones().Zones()
	out := make([]string, 0, len(raw))
	for _, z := range raw {
		out = append(out, string(z))
	}
	sort.Strings(out)
	return out, nil
}

// compile-time assertion: *Diagnostics satisfies the (now ListZones-only) control probe
// surface.
var _ control.Diagnostics = (*Diagnostics)(nil)
