// Package diag is the real control-plane diagnostics probe surface, reading the live
// AppleTalk router's zone and routing tables. It replaces the core's default
// "unavailable" diagnostics once the router is built (the router only carries real data
// after the RTMP/ZIP services populate its tables). The cmd/compose edge wires it via
// control.Plane.SetDiagnostics, so core/control stays free of router knowledge.
//
// Ring: COMPOSE (it knows both core/control and core/router).
package diag

import (
	"context"
	"sort"

	"github.com/ObsoleteMadness/ClassicStack/core/control"
	"github.com/ObsoleteMadness/ClassicStack/core/router"
)

// Diagnostics reads the router for read-only probes. A nil router makes every probe
// report control.ErrUnavailable (the same shape as the core default), so a build with
// no router degrades gracefully.
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

// compile-time assertion: *Diagnostics satisfies the control probe surface.
var _ control.Diagnostics = (*Diagnostics)(nil)
