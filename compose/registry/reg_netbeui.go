//go:build netbeui || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
	"github.com/ObsoleteMadness/ClassicStack/core/port/netbeui"
)

func init() {
	// Repeated schema: several named NetBEUI instances, each its own interface; they
	// feed the NetBEUI mini-router (not the AppleTalk router) — §M11.
	config.Register(config.SectionSchema{
		Key:      netbeui.Name,
		New:      func() config.Section { return &port.Section{SKey: netbeui.Name} },
		Repeated: true,
	})

	RegisterPort(netbeui.Name, func(ctx *BuildContext) (component.Component, error) {
		sec := port.InstanceFromModel(ctx.Model, netbeui.Name, ctx.Instance)
		logger := log.New(sec.InstanceName(), log.NewStderrSink(log.NewLevelVar(log.Info)))
		// Feeds its own NetBEUI mini-router, not the AppleTalk router, so no
		// ctx.Router. The configured station MAC is threaded from the section; the
		// device FrameLink is the sibling follow-on. Inert-but-configured until then.
		return netbeui.NewInstance(sec, nil, sectionMACFor(sec), logger)
	})
}
