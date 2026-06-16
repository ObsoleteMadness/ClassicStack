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
	// Repeated schema: several named IPX instances, each its own interface/segment;
	// they join the IPX mini-router (not the AppleTalk router) — §M11.
	config.Register(config.SectionSchema{
		Key:      ipx.Name,
		New:      func() config.Section { return &port.Section{SKey: ipx.Name} },
		Repeated: true,
	})

	RegisterPort(ipx.Name, func(ctx *BuildContext) (component.Component, error) {
		sec := port.InstanceFromModel(ctx.Model, ipx.Name, ctx.Instance)
		logger := log.New(sec.InstanceName(), log.NewStderrSink(log.NewLevelVar(log.Info)))
		// The IPX port feeds its own mini-router, not the AppleTalk router, so it
		// takes no ctx.Router. The configured station MAC is threaded from the
		// section; the device FrameLink itself is the sibling follow-on (the frame
		// port takes a pre-resolved MAC, not an opener), so the port is inert-but-
		// configured until that lands.
		return ipx.NewInstance(sec, nil, sectionMACFor(sec), logger)
	})
}
