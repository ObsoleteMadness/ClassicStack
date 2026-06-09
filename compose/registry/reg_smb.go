//go:build smb || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/service/smb"
)

func init() {
	Register(smb.Name, func(m *config.Model) (component.Component, error) {
		logger := log.New(smb.Name, log.NewStderrSink(log.NewLevelVar(log.Info)))
		return smb.New(logger), nil
	})
}
