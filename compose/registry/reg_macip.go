//go:build macip || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/service/macip"
)

func init() {
	// Register the MacIP singleton section (IP-side identity + gateway mode) so the
	// codec round-trips it and the factory can read the operator's config.
	macip.RegisterSection()

	// Build the REAL MacIP gateway (no longer a placeholder): it rides the shared
	// AppleTalk router (ctx.Router) for its ATP/DDP socket-72 protocol, and is built
	// with a nil NBP + nil IP egress here — the compose transport cross-wire (wireMacIP)
	// injects the NBP service (for the IPGATEWAY registration) once resolved, and the
	// IP-side egress adapter (adapter/macipgw: proxy-ARP / NAT / DHCP-relay over a pcap
	// link) when the section names an interface and the cmd edge supplied an opener. With
	// egress nil the gateway runs AppleTalk-only: address assignment + config replies work
	// and the gateway is NBP-discoverable, but IP DATA has nowhere to go. A disabled or
	// absent section builds nothing.
	Register(macip.Name, func(ctx *BuildContext) (component.Component, error) {
		sec := macip.SectionFromModel(ctx.Model)
		cfg := macip.Config{}
		enabled := false
		if sec != nil {
			cfg = sec.ToConfig()
			enabled = sec.Enabled
		}
		logger := ctx.Logger(macip.Name)
		// Always build a valid component (the conformance contract); routerFor supplies
		// an on-demand router when ctx.Router is nil (a standalone Build / the harness).
		// The Enabled flag rides on the service (component.Enableable) so a disabled
		// section shows "Disabled" on the dashboard rather than being absent, and the
		// supervisor's enable-aware start can skip it.
		svc := macip.New(routerFor(ctx), nil, nil, cfg, logger)
		svc.SetEnabled(enabled)
		// Record the IP-side egress intent on the service so it DECLARES whether it
		// wants egress; the compose transport cross-wire reads EgressParams() and builds
		// the pcap/cgo egress adapter, instead of re-reading the section (§B). Only when
		// the section is enabled — a disabled gateway wants no egress.
		if sec != nil && enabled {
			ep := sec.EgressParams()
			svc.SetEgressParams(&ep)
		}
		return svc, nil
	})
}
