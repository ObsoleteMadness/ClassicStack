//go:build netbios || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/service/netbios"
)

func init() {
	// Register the NetBIOS singleton section (transport bindings + scope) so the codec
	// round-trips it and the transport cross-wire can read which transports to bind.
	netbios.RegisterSection()

	Register(netbios.Name, func(ctx *BuildContext) (component.Component, error) {
		m := ctx.Model
		logger := ctx.Logger(netbios.Name)
		// Server identity is one top-level value (§4-bis): NetBIOS claims the shared
		// Identity.Hostname as its workstation/file-server name (upper-cased, as
		// NetBIOS names are). No per-service name field — the hostname lives only on
		// Identity, so NetBIOS and SMB cannot disagree. An empty hostname yields the
		// nameless service (transports attach later; a name may be set then).
		name := m.Identity.NetBIOSName()
		var svc *netbios.Service
		if name == "" {
			svc = netbios.New(logger)
		} else {
			svc = netbios.NewService(logger, name)
		}
		// Record the operator's transport bindings on the service so it DECLARES its own
		// transport intent (BoundTransports); the compose transport cross-wire then asks
		// the service instead of re-reading the section (§B). Empty = bind every built
		// transport (back-compat).
		nbSec := netbios.SectionFromModel(m)
		svc.SetBoundTransports(nbSec.Transports)
		// NBT (:139) is a NetBIOS transport, so its listen address lives on the NetBIOS
		// section. Record it on the service (§B); the compose cross-wire (wireSMBTCP) reads
		// it from here when the nbt binding is on (the :139 listener is physically shared
		// with SMB's direct-TCP transport, which shares framing).
		svc.SetNBTListenAddr(nbSec.NBTListenAddr())
		return svc, nil
	})
}
