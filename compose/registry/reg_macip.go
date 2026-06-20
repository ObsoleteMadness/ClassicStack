//go:build macip || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/service/macip"
)

func init() {
	// Register the MacIP singleton section (IP-side identity + gateway mode) so the
	// codec round-trips it and the factory can read the operator's config.
	macip.RegisterSection()

	// Build the REAL MacIP gateway (no longer a placeholder): it rides the shared
	// AppleTalk router (ctx.Router) for its ATP/DDP socket-72 protocol, and is built
	// with a nil NBP + nil IP egress here — the compose transport cross-wire injects
	// the NBP service (for the IPGATEWAY registration) once it is resolved (wireMacIP),
	// and an IP-egress adapter when one is configured. With egress nil the gateway runs
	// in AppleTalk-only mode: address assignment + config replies work and the gateway
	// is NBP-discoverable, but IP DATA has nowhere to go until an egress lands. A
	// disabled or absent section builds nothing.
	Register(macip.Name, func(ctx *BuildContext) (component.Component, error) {
		sec := macip.SectionFromModel(ctx.Model)
		cfg := macip.Config{}
		enabled := false
		if sec != nil {
			cfg = sec.ToConfig()
			enabled = sec.Enabled
		}
		logger := log.New(macip.Name, log.NewStderrSink(log.NewLevelVar(log.Info)))
		// Always build a valid component (the conformance contract); routerFor supplies
		// an on-demand router when ctx.Router is nil (a standalone Build / the harness).
		// The Enabled flag rides on the service (component.Enableable) so a disabled
		// section shows "Disabled" on the dashboard rather than being absent, and the
		// supervisor's enable-aware start can skip it.
		svc := macip.New(routerFor(ctx), nil, nil, cfg, logger)
		svc.SetEnabled(enabled)
		return svc, nil
	})
}
