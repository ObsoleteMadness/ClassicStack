//go:build (macip && router) || all

// MacIP is a DDP/ATP socket-72 service: it rides the shared AppleTalk router and
// builds via routerFor (ddpservice.go, gated `router || all`). So its registration
// requires `router` as well as `macip` — a `macip`-only build has no routerFor and
// would not link. The umbrella `all` tag satisfies both.

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
			// Resolve the section's interface NAME through the [[interface]] namespace to
			// the real pcap device (Npcap's "\Device\NPF_{GUID}" on Windows), the same
			// way every other pcap-bound port does (reg_ipx/reg_netbeui/reg_ethertalk).
			// EgressParams carries the raw name; without this the egress opener was handed
			// "br-lan" and libpcap could not open it — the gateway silently fell back to
			// AppleTalk-only and MacTCP got no usable address.
			if ep.Interface != "" {
				if dev := ctx.Model.EffectiveInterfaceFor(sec).PcapDevice(); dev != "" {
					ep.Interface = dev
				}
			}
			svc.SetEgressParams(&ep)
		}
		return svc, nil
	})
}
