//go:build messenger || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/service/messenger"
)

func init() {
	Register(messenger.Name, func(ctx *BuildContext) (component.Component, error) {
		m := ctx.Model
		logger := log.New(messenger.Name, log.NewStderrSink(log.NewLevelVar(log.Info)))
		// The messenger receives "net send" pop-ups for the shared server identity
		// (§4-bis hostname/workgroup) and publishes them on the telemetry bus for the
		// UI. Like the browser it is built with NO mailslot sink — the runtime
		// cross-wire installs it via SetSink and registers this service on the mailslot
		// router for \MAILSLOT\MESSNGR (crossWireTransports). The sink matters only to
		// the send path; the receive path needs none, so an unwired messenger still
		// logs/publishes inbound pop-ups once a NetBIOS transport delivers them.
		// Pass the telemetry bus only when present: messenger.New treats a nil
		// Publisher as "do not publish", but a nil bus.Bus wrapped in the Publisher
		// interface is non-nil and would panic on Publish. Guard with an explicit nil.
		var pub messenger.Publisher
		if ctx.Telemetry != nil {
			pub = ctx.Telemetry
		}
		svc := messenger.New(logger, pub, nil, m.Identity.Hostname, m.Identity.Workgroup)
		return svc, nil
	})
}
