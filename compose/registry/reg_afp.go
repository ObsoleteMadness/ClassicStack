//go:build afp || all

package registry

import (
	"os"

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

	Register(afp.Name, func(ctx *BuildContext) (component.Component, error) {
		m := ctx.Model
		logger := log.New(afp.Name, log.NewStderrSink(log.NewLevelVar(log.Info)))
		// extMapCache memoises parsed extension maps by file path so several volumes
		// sharing one extmap file (the common case) read+parse it once per resolve. A
		// bad/missing file logs and yields no map (defaulting simply does not apply) —
		// capture-style best-effort, never failing the volume build.
		extMapFor := func(path string) *afp.ExtensionMap {
			if path == "" {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				logger.Log(log.Warn, "AFP extension map unreadable; type/creator defaulting disabled for volumes using it",
					log.Str("path", path), log.Str("error", err.Error()))
				return nil
			}
			em, err := afp.ParseExtensionMap(data)
			if err != nil {
				logger.Log(log.Warn, "AFP extension map invalid; type/creator defaulting disabled",
					log.Str("path", path), log.Str("error", err.Error()))
				return nil
			}
			return em
		}
		// volSpecsFromModel maps the configured volume sections to VolumeSpecs with
		// id 1..N in registration order, attaching each volume's parsed extension map
		// (read from its ExtMapPath at this compose edge — core does no config file
		// I/O). Shared by the initial build and the hot-apply resolver so both see one
		// definition of "the desired set".
		volSpecsFromModel := func(m *config.Model) []afp.VolumeSpec {
			specs := afp.SpecsFromModel(m)
			secs := afp.VolumesFromModel(m)
			cache := map[string]*afp.ExtensionMap{}
			out := make([]afp.VolumeSpec, 0, len(specs))
			for i, spec := range specs {
				vs := afp.VolumeSpec{ID: uint16(i + 1), Name: spec.Name, Share: spec}
				if i < len(secs) {
					p := secs[i].ExtMapPath
					em, ok := cache[p]
					if !ok {
						em = extMapFor(p)
						cache[p] = em
					}
					vs.ExtMap = em
				}
				out = append(out, vs)
			}
			return out
		}
		svc := afp.New(logger)
		// Bind the shared AppleTalk router so the AFP/ASP service replies and the
		// runtime root can RegisterService it on its DDP socket. nil (a standalone
		// build with no router) leaves it unrouted, the historical default.
		if ctx.Router != nil {
			svc.SetRouter(ctx.Router)
		}
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
