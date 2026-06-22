// Package diag is the real control-plane diagnostics probe surface, reading the live
// AppleTalk router's zone and routing tables, the NBP name table, and the MacIP
// gateway's leases. It replaces the core's default "unavailable" diagnostics once the
// runtime is built (the router only carries real data after the RTMP/ZIP services
// populate its tables; the name/lease probes only after NBP/MacIP are wired). The
// cmd/compose edge wires it via control.Plane.SetDiagnostics, so core/control stays
// free of router/service knowledge.
//
// Ring: COMPOSE (it knows both core/control and the core router/services).
package diag

import (
	"context"
	"net"
	"sort"

	"github.com/ObsoleteMadness/ClassicStack/core/control"
	"github.com/ObsoleteMadness/ClassicStack/core/router"
)

// nbpSource is the read-only NBP name-table seam (structurally satisfied by
// *nbp.Service). A local interface keeps diag from hard-importing the service for what
// is a single read, and lets a nil source degrade to ErrUnavailable.
type nbpSource interface {
	Names() []NBPName
}

// macipSource is the read-only MacIP lease seam (structurally satisfied by
// *macip.Service). Same rationale as nbpSource.
type macipSource interface {
	Leases() []MacIPLease
}

// NBPName mirrors nbp.RegisterName's table row in the shape diag consumes (the byte
// NVE tuple). It is the local view so the seam stays package-free; the diag impl
// decodes the bytes to display strings for control.NBPName.
type NBPName struct {
	Object []byte
	Type   []byte
	Zone   []byte
	Socket uint8
}

// MacIPLease mirrors macip.LeaseInfo in the shape diag consumes.
type MacIPLease struct {
	IP        [4]byte
	ATNetwork uint16
	ATNode    uint8
	Source    string
}

// Diagnostics reads the router and the optional NBP/MacIP services for read-only
// probes. A nil router makes the zone probe report control.ErrUnavailable; a nil
// nbp/macip source makes the corresponding drill-down probe report it. So a build
// missing any source degrades gracefully, the same shape as the core default.
type Diagnostics struct {
	rtr   *router.RouterImpl
	nbp   nbpSource
	macip macipSource
}

// New builds a Diagnostics over the shared router. Pass nil for a no-router build. The
// NBP and MacIP sources are attached separately (SetNBP/SetMacIP) by the compose edge
// once those services are resolved, so the constructor stays a single positional arg
// (router) for the existing callers.
func New(rtr *router.RouterImpl) *Diagnostics { return &Diagnostics{rtr: rtr} }

// SetNBP attaches the NBP name-table source (the registered-names drill-down). A nil
// source leaves the probe reporting ErrUnavailable.
func (d *Diagnostics) SetNBP(src nbpSource) {
	if src != nil {
		d.nbp = src
	}
}

// SetMacIP attaches the MacIP lease source (the active-leases drill-down). A nil source
// leaves the probe reporting ErrUnavailable.
func (d *Diagnostics) SetMacIP(src macipSource) {
	if src != nil {
		d.macip = src
	}
}

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

// RegisteredNames returns the NBP name table (object:type@zone + socket), decoding the
// MacRoman-ish NVE byte fields to display strings. ErrUnavailable when no NBP service
// is wired. Sorted by object then type for stable output.
func (d *Diagnostics) RegisteredNames(_ context.Context) ([]control.NBPName, error) {
	if d.nbp == nil {
		return nil, control.ErrUnavailable
	}
	raw := d.nbp.Names()
	out := make([]control.NBPName, 0, len(raw))
	for _, n := range raw {
		out = append(out, control.NBPName{
			Object: string(n.Object),
			Type:   string(n.Type),
			Zone:   string(n.Zone),
			Socket: n.Socket,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Object != out[j].Object {
			return out[i].Object < out[j].Object
		}
		return out[i].Type < out[j].Type
	})
	return out, nil
}

// MacIPLeases returns the MacIP gateway's active leases (IP↔AppleTalk + source).
// ErrUnavailable when no MacIP gateway is wired. Sorted by IP for stable output.
func (d *Diagnostics) MacIPLeases(_ context.Context) ([]control.MacIPLease, error) {
	if d.macip == nil {
		return nil, control.ErrUnavailable
	}
	raw := d.macip.Leases()
	out := make([]control.MacIPLease, 0, len(raw))
	for _, l := range raw {
		out = append(out, control.MacIPLease{
			IP:        net.IP(l.IP[:]).String(),
			ATNetwork: l.ATNetwork,
			ATNode:    l.ATNode,
			Source:    l.Source,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].IP < out[j].IP })
	return out, nil
}

// compile-time assertion: *Diagnostics satisfies the control probe surface.
var _ control.Diagnostics = (*Diagnostics)(nil)
