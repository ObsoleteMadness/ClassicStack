//go:build netbios || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/service/netbios"
)

func init() {
	Register(netbios.Name, func(m *config.Model) (component.Component, error) {
		logger := log.New(netbios.Name, log.NewStderrSink(log.NewLevelVar(log.Info)))
		return netbios.New(logger), nil
	})
}
