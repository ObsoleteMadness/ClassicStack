//go:build router || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/service/zip"
)

func init() {
	// ZIP is a core router service: it answers ZIP queries / GetNetInfo / the
	// ATP-carried GetMyZone/GetZoneList on socket 6 and queries for zones of newly
	// learned networks. It rides the shared router; crossWireRouter registers its
	// socket. Gated on the router tag (no router → nothing to serve).
	Register(zip.RespondingName, func(ctx *BuildContext) (component.Component, error) {
		logger := log.New(zip.RespondingName, log.NewStderrSink(log.NewLevelVar(log.Info)))
		return zip.New(routerFor(ctx), logger), nil
	})
}
