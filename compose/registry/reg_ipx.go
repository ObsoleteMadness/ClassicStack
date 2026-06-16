//go:build ipx || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/port/ipx"
)

func init() {
	Register(ipx.Name, func(ctx *BuildContext) (component.Component, error) {
		logger := log.New(ipx.Name, log.NewStderrSink(log.NewLevelVar(log.Info)))
		// Frame port: no device link/srcMAC injected yet (the real link from config
		// is the next slice). The IPX port feeds its own mini-router, not the
		// AppleTalk router, so it takes no ctx.Router. Inert until the link lands.
		return ipx.New(ctx.Model, nil, [6]byte{}, logger)
	})
}
