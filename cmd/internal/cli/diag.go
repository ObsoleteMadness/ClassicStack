package cli

import (
	"github.com/ObsoleteMadness/ClassicStack/compose/diag"
	"github.com/ObsoleteMadness/ClassicStack/compose/runtime"
	"github.com/ObsoleteMadness/ClassicStack/core/service/macip"
	"github.com/ObsoleteMadness/ClassicStack/core/service/nbp"
)

// buildDiagnostics constructs the compose diagnostics probe surface over the runtime's
// router and attaches the optional NBP name-table and MacIP lease sources when those
// services were built. The source shims convert the services' own row types to diag's
// package-free local types (diag must not depend on the service packages' wire shapes).
// A build missing any of these leaves the corresponding probe reporting ErrUnavailable.
func buildDiagnostics(rt *runtime.Runtime) *diag.Diagnostics {
	d := diag.New(rt.Router())
	if c := rt.Component(nbp.Name); c != nil {
		if svc, ok := c.(*nbp.Service); ok {
			d.SetNBP(nbpNamesShim{svc})
		}
	}
	if c := rt.Component(macip.Name); c != nil {
		if svc, ok := c.(*macip.Service); ok {
			d.SetMacIP(macipLeasesShim{svc})
		}
	}
	return d
}

// nbpNamesShim adapts *nbp.Service to diag.nbpSource (Names() []diag.NBPName).
type nbpNamesShim struct{ svc *nbp.Service }

func (s nbpNamesShim) Names() []diag.NBPName {
	raw := s.svc.Names()
	out := make([]diag.NBPName, 0, len(raw))
	for _, n := range raw {
		out = append(out, diag.NBPName{Object: n.Object, Type: n.Type, Zone: n.Zone, Socket: n.Socket})
	}
	return out
}

// macipLeasesShim adapts *macip.Service to diag.macipSource (Leases() []diag.MacIPLease).
type macipLeasesShim struct{ svc *macip.Service }

func (s macipLeasesShim) Leases() []diag.MacIPLease {
	raw := s.svc.Leases()
	out := make([]diag.MacIPLease, 0, len(raw))
	for _, l := range raw {
		out = append(out, diag.MacIPLease{IP: l.IP, ATNetwork: l.ATNetwork, ATNode: l.ATNode, Source: l.Source})
	}
	return out
}
