//go:build ipxgw || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/service/ipxgw"
)

func init() {
	// Register the IPXGW singleton section (enable / IPX network / NBP zone bindings)
	// so the codec round-trips it.
	ipxgw.RegisterSection()

	// Build the IPX gateway (MacIPX): it rides the shared AppleTalk router on socket 78,
	// answering MacIPX clients. Built with nil NBP + no IPX mini-router here — the
	// compose transport cross-wire injects the NBP service (for the "IPX Gateway" NBP
	// registrations) and, when an IPX port + mini-router exist, the mini-router (so
	// encapsulated IPX is forwarded to native IPX peers). With no mini-router it runs in
	// log-only mode for IPX data; assignment + NBP discovery still work. Always builds a
	// valid component (the conformance contract); routerFor supplies an on-demand router
	// when ctx.Router is nil. The Enabled flag rides on the service so a disabled section
	// shows Disabled rather than being absent.
	Register(ipxgw.Name, func(ctx *BuildContext) (component.Component, error) {
		sec := ipxgw.SectionFromModel(ctx.Model)
		var (
			cfg      ipxgw.Config
			bindings []ipxgw.ZoneBinding
			enabled  bool
		)
		if sec != nil {
			cfg = sec.Config()
			bindings = sec.ZoneBindings()
			enabled = sec.Enabled
		}
		logger := log.New(ipxgw.Name, log.NewStderrSink(log.NewLevelVar(log.Info)))
		svc := ipxgw.NewWithConfig(routerFor(ctx), nil, bindings, cfg, logger)
		svc.SetEnabled(enabled)
		return svc, nil
	})
}
