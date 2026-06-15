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
		// Server identity is one top-level value (§4-bis): NetBIOS claims the shared
		// Identity.Hostname as its workstation/file-server name (upper-cased, as
		// NetBIOS names are). No per-service name field — the hostname lives only on
		// Identity, so NetBIOS and SMB cannot disagree. An empty hostname yields the
		// nameless service (transports attach later; a name may be set then).
		name := m.Identity.NetBIOSName()
		if name == "" {
			return netbios.New(logger), nil
		}
		return netbios.NewService(logger, name), nil
	})
}
