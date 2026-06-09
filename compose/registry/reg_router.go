//go:build router || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/router"
)

func init() {
	Register(router.Name, func(m *config.Model) (component.Component, error) {
		// A placeholder router is always enabled in the registry if built, but
		// can be configured via RouterSection. In Phase 1 we return it directly.
		logger := log.New(router.Name, log.NewStderrSink(log.NewLevelVar(log.Info)))
		return router.New(logger), nil
	})
}
