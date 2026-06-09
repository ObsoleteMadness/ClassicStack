//go:build macip || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/service/macip"
)

func init() {
	Register(macip.Name, func(m *config.Model) (component.Component, error) {
		logger := log.New(macip.Name, log.NewStderrSink(log.NewLevelVar(log.Info)))
		return macip.New(logger), nil
	})
}
