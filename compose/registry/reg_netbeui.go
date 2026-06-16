//go:build netbeui || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/port/netbeui"
)

func init() {
	Register(netbeui.Name, func(ctx *BuildContext) (component.Component, error) {
		logger := log.New(netbeui.Name, log.NewStderrSink(log.NewLevelVar(log.Info)))
		// Frame port: no device link/srcMAC injected yet (the real link from config
		// is the next slice). Feeds its own NetBEUI mini-router, not the AppleTalk
		// router, so no ctx.Router. Inert until the link lands.
		return netbeui.New(ctx.Model, nil, [6]byte{}, logger)
	})
}
