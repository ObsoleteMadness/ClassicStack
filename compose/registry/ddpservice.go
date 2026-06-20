//go:build router || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/router"
)

// routerFor returns the shared AppleTalk router a core DDP service (RTMP/ZIP/NBP/AEP)
// binds to, falling back to an on-demand standalone router when the BuildContext has
// none. A factory must always return a valid component (the graceful-degradation
// contract the conformance harness checks), and these services hold a non-nil
// router.ServiceRouter to avoid a nil deref in their timer/dispatch loops; an
// unattached standalone router simply has no ports, so the service runs inert until a
// real router + ports arrive. Mirrors reg_router.go's standalone path.
func routerFor(ctx *BuildContext) router.ServiceRouter {
	if ctx.Router != nil {
		return ctx.Router
	}
	return router.New(log.New(router.Name, log.NewStderrSink(log.NewLevelVar(log.Info))))
}
