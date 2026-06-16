//go:build localtalk || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/port/localtalk"
)

func init() {
	Register(localtalk.Name, func(ctx *BuildContext) (component.Component, error) {
		logger := log.New(localtalk.Name, log.NewStderrSink(log.NewLevelVar(log.Info)))
		// Router injected from context so an enabled port delivers inbound DDP to
		// the shared router. Transport link + LLAP framer are still nil (the real
		// link from config is the next slice); inert-but-routed until then.
		return localtalk.New(ctx.Model, nil, nil, ctx.Router, logger)
	})
}
