//go:build router || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/service/rtmp"
)

func init() {
	// RTMP is a core router service: it answers RTMP requests on socket 1, advertises
	// the routing table periodically, and ages it. It binds to the shared router from
	// the BuildContext; the runtime's crossWireRouter then registers its socket. A nil
	// ctx.Router (a standalone Build, e.g. the conformance harness) gets an on-demand
	// router so the component is always valid-but-inert — the graceful-degradation
	// contract every factory honours (mirrors reg_router.go's standalone path).
	Register(rtmp.RespondingName, func(ctx *BuildContext) (component.Component, error) {
		logger := ctx.Logger(rtmp.RespondingName)
		return rtmp.New(routerFor(ctx), logger), nil
	})
}
