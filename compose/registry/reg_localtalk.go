//go:build localtalk || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/port/localtalk"
)

func init() {
	Register(localtalk.Name, func(m *config.Model) (component.Component, error) {
		logger := log.New(localtalk.Name, log.NewStderrSink(log.NewLevelVar(log.Info)))
		// Registry path: inert until compose injects a transport link + LLAP
		// framer + router (M8/M10). M3 exercises the real read loop via tests.
		return localtalk.New(m, nil, nil, nil, logger)
	})
}
