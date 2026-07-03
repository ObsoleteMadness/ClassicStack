//go:build browser || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/service/browser"
)

func init() {
	Register(browser.Name, func(ctx *BuildContext) (component.Component, error) {
		m := ctx.Model
		logger := ctx.Logger(browser.Name)
		// The browser advertises the shared server identity (§4-bis): hostname as the
		// server name and workgroup, the description as its browse-list comment. It is
		// built with NO mailslot sink — the sink (the mailslot router) needs the
		// NetBIOS service, which is not in the BuildContext, so the runtime cross-wire
		// installs it later via SetSink and registers this service on the mailslot
		// router for \MAILSLOT\BROWSE (crossWireTransports). Until then it is built but
		// unwired; with no NetBIOS transport in the build it simply never receives or
		// sends a datagram.
		svc := browser.New(logger, nil, m.Identity.Hostname, m.Identity.Workgroup)
		svc.SetDescription(m.Identity.Description)
		return svc, nil
	})
}
