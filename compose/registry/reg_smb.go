//go:build smb || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/service/smb"
)

func init() {
	// Register the SMB share repeated-section schema so codecs round-trip each
	// configured share as a named section. Kept here (not in an smb-package init)
	// so the section exists exactly when the SMB service is built.
	smb.RegisterShares()
	// Register the SMB server-level singleton section (transport bindings) so the
	// codec round-trips it and the transport cross-wire can read which transports the
	// operator wants bound.
	smb.RegisterServer()

	Register(smb.Name, func(ctx *BuildContext) (component.Component, error) {
		m := ctx.Model
		logger := log.New(smb.Name, log.NewStderrSink(log.NewLevelVar(log.Info)))
		// Build one Share per configured share section. A model with no shares
		// yields a service with none (the historical zero-config default); a bad
		// spec (invalid fs_type×fork×codec triple or missing required param) fails
		// the build loudly here rather than mangling names at runtime.
		svc := smb.New(logger)
		// Server identity is one top-level value (§4-bis): SMB advertises the shared
		// Identity.Hostname/Workgroup/Description — no per-service name field, so SMB
		// and NetBIOS cannot diverge. These hold even with NetBIOS absent (direct-TCP
		// :445): SMB still reports a name and comment in NetServerEnum2.
		svc.SetServerName(m.Identity.Hostname)
		svc.SetWorkgroup(m.Identity.Workgroup)
		svc.SetDescription(m.Identity.Description)
		// Record the operator's transport bindings + listen addresses on the service so
		// it DECLARES its own transport intent (BoundTransports), dependency edges
		// (Dependencies), and TCP/NBT addresses; the compose transport cross-wire then
		// asks the service instead of re-reading the section (§B). Empty list = bind
		// every built transport (back-compat); empty addr = do not bind that address.
		smbSec := smb.ServerSectionFromModel(m)
		svc.SetBoundTransports(smbSec.Transports)
		svc.SetTCPListenAddrs(smbSec.DirectTCPAddr(), smbSec.NBTListenAddr())
		// §10d: build each share over the shared FS-mutation bus for its host path, so
		// a same-host-path AFP volume sees this share's mutations (and vice-versa). Set
		// BEFORE the shares are built so the initial set gets the shared bus too.
		svc.SetBusResolver(fsBus.busFor)
		// Wire the hot-apply resolver: a Reconfigure of an SMB share section then
		// reconciles the live share set against the model via share.Manager
		// (Add/Update/Remove) without restarting the service (§11b).
		svc.SetShareResolver(func() ([]smb.ShareSpec, error) {
			return smb.SpecsFromModel(m), nil
		})
		// Populate the initial share set through the reconcile path so it is built
		// over the shared bus. A bad spec fails the build loudly here; an empty model
		// yields a service with no shares (the historical zero-config default).
		if err := svc.ReconcileShares(smb.SpecsFromModel(m)); err != nil {
			return nil, err
		}
		return svc, nil
	})
}
