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
	// Register the AFP server-level singleton section (advertised name/zone + the
	// classic/modern transport bindings) so the codec round-trips it and the service
	// can read the operator's identity + binding choices.
	afp.RegisterServer()

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
		// Server-level identity + bindings (§4): the AFP server section carries the
		// advertised Chooser name and zone and which transport stacks to bind. An empty
		// ServerName falls back to the shared Identity.Hostname (then the service's own
		// default); an empty Transports list binds all built transports (back-compat).
		srv := afp.ServerSectionFromModel(m)
		svc.SetServerName(srv.EffectiveServerName(m.Identity.Hostname))
		// Advertised zone: the AFP section's own zone, falling back to the router's
		// configured default_zone. Resolving it from CONFIG (not the live ZIT) makes the
		// NBP registration independent of startup ordering — AFP.Start runs before the
		// router's member ports attach and seed their zones, so a live-ZIT lookup would be
		// empty at that moment and AFP would register into no zone (invisible in Chooser).
		zone := srv.Zone
		if zone == "" {
			zone = m.Router.DefaultZone
		}
		svc.SetZone(zone)
		svc.SetTransports(srv.Transports)
		// Bind the shared AppleTalk router so the AFP/ASP service replies and the
		// runtime root can RegisterService it on its DDP socket. nil (a standalone
		// build with no router) leaves it unrouted, the historical default. The classic
		// DDP stack is active only when the AFP port instance is a router member AND the
		// ddp transport binding is on; the router membership is the operator's join.
		if ctx.Router != nil && srv.Binds(afp.TransportDDP) {
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
