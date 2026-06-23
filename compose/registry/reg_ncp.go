//go:build ncp || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/service/ncp"
)

func init() {
	// Register the NCP volume repeated-section schema so codecs round-trip each
	// configured volume as a named section. Kept here (not in an ncp-package init)
	// so the section exists exactly when the NCP service is built.
	ncp.RegisterVolumes()

	Register(ncp.Name, func(ctx *BuildContext) (component.Component, error) {
		m := ctx.Model
		logger := log.New(ncp.Name, log.NewStderrSink(log.NewLevelVar(log.Info)))
		svc := ncp.New(logger)
		// Server identity is the shared §4-bis Identity hostname/description (the NCP
		// SAP advertisement and Get Server Info report it), upper-cased to a NetWare
		// name by the service.
		svc.SetServerName(m.Identity.Hostname)
		svc.SetDescription(m.Identity.Description)
		// §10d: build each volume over the shared FS-mutation bus for its host path, so
		// a same-host-path AFP volume / SMB share sees this volume's mutations.
		svc.SetBusResolver(fsBus.busFor)
		// The bindery login Authenticator is wired centrally by the runtime
		// (wireAuthenticator) from the shared user store, exactly like AFP/SMB — not
		// here — so NCP and the other file services cannot diverge on the store.
		// Hot-apply: a Reconfigure of an NCP volume section reconciles the live set.
		svc.SetShareResolver(func() ([]ncp.VolumeSpec, error) {
			return ncp.SpecsFromModel(m), nil
		})
		if err := svc.ReconcileVolumes(ncp.SpecsFromModel(m)); err != nil {
			return nil, err
		}
		return svc, nil
	})
}
