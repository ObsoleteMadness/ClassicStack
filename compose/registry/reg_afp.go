//go:build afp || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/service/afp"
)

func init() {
	// Register the AFP volume repeated-section schema so codecs round-trip each
	// configured volume as a named section. Kept here (not in an afp-package init)
	// so the section exists exactly when the AFP service is built.
	afp.RegisterVolumes()

	Register(afp.Name, func(m *config.Model) (component.Component, error) {
		logger := log.New(afp.Name, log.NewStderrSink(log.NewLevelVar(log.Info)))
		// volSpecsFromModel maps the configured volume sections to VolumeSpecs with
		// id 1..N in registration order. Shared by the initial build and the
		// hot-apply resolver so both see one definition of "the desired set".
		volSpecsFromModel := func(m *config.Model) []afp.VolumeSpec {
			specs := afp.SpecsFromModel(m)
			out := make([]afp.VolumeSpec, 0, len(specs))
			for i, spec := range specs {
				out = append(out, afp.VolumeSpec{ID: uint16(i + 1), Name: spec.Name, Share: spec})
			}
			return out
		}
		svc := afp.New(logger)
		// §10d: build each volume over the shared FS-mutation bus for its host path,
		// so a same-host-path SMB share sees this volume's mutations (and vice-versa).
		// Set BEFORE the volumes are built so the initial set gets the shared bus too.
		svc.SetBusResolver(fsBus.busFor)
		// Wire the hot-apply resolver: a Reconfigure of an AFP volume section then
		// reconciles the live volume set against the model via share.Manager
		// (Add/Update/Remove) without restarting the service (§11b).
		svc.SetVolumeResolver(func() ([]afp.VolumeSpec, error) {
			return volSpecsFromModel(m), nil
		})
		// Populate the initial volume set through the reconcile path so it is built
		// over the shared bus. A bad spec (invalid fs_type×fork×codec triple or
		// missing required param) fails the build loudly here. An empty model yields
		// a service with no volumes (the historical zero-config default).
		if err := svc.ReconcileVolumes(volSpecsFromModel(m)); err != nil {
			return nil, err
		}
		return svc, nil
	})
}
