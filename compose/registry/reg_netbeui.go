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
	config.Register(config.SectionSchema{
		Key: netbeui.Name,
		New: func() config.Section { return &port.Section{SKey: netbeui.Name} },
	})

	Register(netbeui.Name, func(ctx *BuildContext) (component.Component, error) {
		logger := log.New(netbeui.Name, log.NewStderrSink(log.NewLevelVar(log.Info)))
		// Feeds its own NetBEUI mini-router, not the AppleTalk router, so no
		// ctx.Router. The configured station MAC is threaded from the section; the
		// device FrameLink is the sibling follow-on. Inert-but-configured until then.
		return netbeui.New(ctx.Model, nil, sectionMAC(ctx.Model, netbeui.Name), logger)
	})
}
