//go:build afp || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/service/afp"
)

func init() {
	Register(afp.Name, func(m *config.Model) (component.Component, error) {
		logger := log.New(afp.Name, log.NewStderrSink(log.NewLevelVar(log.Info)))
		return afp.New(logger), nil
	})
}
