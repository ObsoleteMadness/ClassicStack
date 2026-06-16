//go:build localtalk || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
	"github.com/ObsoleteMadness/ClassicStack/core/port/localtalk"
)

func init() {
	config.Register(config.SectionSchema{
		Key: localtalk.Name,
		New: func() config.Section { return &port.Section{SKey: localtalk.Name} },
	})

	Register(localtalk.Name, func(ctx *BuildContext) (component.Component, error) {
		logger := log.New(localtalk.Name, log.NewStderrSink(log.NewLevelVar(log.Info)))
		// Router injected from context so an enabled port delivers inbound DDP to the
		// shared router. The transport link + LLAP framer are still nil: unlike
		// EtherTalk, no LLAP framer exists yet (adapter/link/framing ships only the
		// Ethernet/SNAP framer), so LocalTalk stays inert-but-routed until that lands.
		return localtalk.New(ctx.Model, nil, nil, ctx.Router, logger)
	})
}
