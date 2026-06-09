//go:build ethertalk || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/port/ethertalk"
)

func init() {
	Register(ethertalk.Name, func(m *config.Model) (component.Component, error) {
		logger := log.New(ethertalk.Name, log.NewStderrSink(log.NewLevelVar(log.Info)))
		// Registry path: no device link or router is injected here (those are
		// adapter/compose concerns wired at cmd cutover, M8/M10). The port comes
		// up inert — lifecycle/stats/config only — until then. M3 exercises the
		// real read loop via tests that inject an inmem link + framer + router.
		return ethertalk.New(m, nil, nil, nil, logger)
	})
}
