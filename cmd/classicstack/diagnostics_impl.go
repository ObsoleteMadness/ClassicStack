package main

import (
	"context"

	"github.com/ObsoleteMadness/ClassicStack/pkg/control"
	"github.com/ObsoleteMadness/ClassicStack/router"
)

// routerDiagnostics implements control.Diagnostics against the live
// router's routing and zone tables. The read-only probes (ListZones,
// DDPEnumerate) are served directly from those tables; the active probes
// (AEPEcho, ZIPEnumerate) and SMBBrowse are reported as unavailable until
// their protocol-level implementations are wired in.
type routerDiagnostics struct {
	sup *Supervisor
}

// wireDiagnostics installs the diagnostics implementation onto the plane.
func wireDiagnostics(plane *control.Plane, sup *Supervisor) {
	plane.SetDiagnostics(&routerDiagnostics{sup: sup})
}

func (d *routerDiagnostics) router() *router.Router { return d.sup.Router() }

// ListZones returns the AppleTalk zones known to the router.
func (d *routerDiagnostics) ListZones(context.Context) ([]control.ZoneInfo, error) {
	r := d.router()
	if r == nil {
		return nil, control.ErrDiagUnavailable
	}
	zones := r.Zones()
	out := make([]control.ZoneInfo, 0, len(zones))
	for _, z := range zones {
		out = append(out, control.ZoneInfo{Name: string(z)})
	}
	return out, nil
}

// DDPEnumerate lists the networks the router can reach, from its routing
// table.
func (d *routerDiagnostics) DDPEnumerate(context.Context) ([]control.NetworkInfo, error) {
	r := d.router()
	if r == nil {
		return nil, control.ErrDiagUnavailable
	}
	entries := r.RoutingEntries()
	out := make([]control.NetworkInfo, 0, len(entries))
	for _, e := range entries {
		if e.Entry == nil {
			continue
		}
		portName := ""
		if e.Entry.Port != nil {
			portName = e.Entry.Port.ShortString()
		}
		out = append(out, control.NetworkInfo{
			NetworkMin: e.Entry.NetworkMin,
			NetworkMax: e.Entry.NetworkMax,
			Distance:   e.Entry.Distance,
			Port:       portName,
		})
	}
	return out, nil
}

// ZIPEnumerate currently mirrors ListZones; a dedicated ZIP GetZoneList
// walk can replace this when wired.
func (d *routerDiagnostics) ZIPEnumerate(ctx context.Context) ([]control.ZoneInfo, error) {
	return d.ListZones(ctx)
}

// AEPEcho is not yet wired to an AEP requester.
func (d *routerDiagnostics) AEPEcho(context.Context, uint16, uint8) (control.EchoResult, error) {
	return control.EchoResult{}, control.ErrDiagUnavailable
}

// SMBBrowse depends on the SMB subsystem exposing a browser walk; until
// that is wired through, the probe reports unavailable rather than guessing.
func (d *routerDiagnostics) SMBBrowse(context.Context) ([]control.ServerInfo, error) {
	return nil, control.ErrDiagUnavailable
}
