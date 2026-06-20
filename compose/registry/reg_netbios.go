//go:build netbios || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/service/netbios"
)

func init() {
	// Register the NetBIOS singleton section (transport bindings + scope) so the codec
	// round-trips it and the transport cross-wire can read which transports to bind.
	netbios.RegisterSection()

	Register(netbios.Name, func(ctx *BuildContext) (component.Component, error) {
		m := ctx.Model
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
