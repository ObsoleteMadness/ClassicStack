//go:build router || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/router"
)

func init() {
	Register(router.Name, func(ctx *BuildContext) (component.Component, error) {
		// The router is the shared collaborator every DDP port and service binds
		// to. The runtime root builds it FIRST and threads it into the context, so
		// the factory returns that one instance — every dependent then receives the
		// same router. (A standalone Build with no pre-built router still works: we
		// construct one on demand.)
		if ctx.Router != nil {
			return ctx.Router, nil
		}
		logger := ctx.Logger(router.Name)
		return router.New(logger), nil
	})
}
