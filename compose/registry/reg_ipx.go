//go:build ipx || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
	"github.com/ObsoleteMadness/ClassicStack/core/port/ipx"
)

func init() {
	config.Register(config.SectionSchema{
		Key: ipx.Name,
		New: func() config.Section { return &port.Section{SKey: ipx.Name} },
	})

	Register(ipx.Name, func(ctx *BuildContext) (component.Component, error) {
		logger := log.New(ipx.Name, log.NewStderrSink(log.NewLevelVar(log.Info)))
		// The IPX port feeds its own mini-router, not the AppleTalk router, so it
		// takes no ctx.Router. The configured station MAC is threaded from the
		// section; the device FrameLink itself is the sibling follow-on (the frame
		// port takes a pre-resolved MAC, not an opener), so the port is inert-but-
		// configured until that lands.
		return ipx.New(ctx.Model, nil, sectionMAC(ctx.Model, ipx.Name), logger)
	})
}
