//go:build ipx || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/port/ipx"
)

func init() {
	Register(ipx.Name, func(m *config.Model) (component.Component, error) {
		logger := log.New(ipx.Name, log.NewStderrSink(log.NewLevelVar(log.Info)))
		// Registry path: no device link/srcMAC injected (adapter/compose concern,
		// M8/M10). Inert until then; M3 exercises the real path via tests.
		return ipx.New(m, nil, [6]byte{}, logger)
	})
}
