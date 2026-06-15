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
		// Build one Volume per configured AFP volume section. A model with no
		// volumes yields a service with none (the historical zero-config default);
		// a bad spec (invalid fs_type×fork×codec triple or missing required param)
		// fails the build loudly here rather than mangling names at runtime.
		specs := afp.SpecsFromModel(m)
		if len(specs) == 0 {
			return afp.New(logger), nil
		}
		volSpecs := make([]afp.VolumeSpec, 0, len(specs))
		for i, spec := range specs {
			volSpecs = append(volSpecs, afp.VolumeSpec{ID: uint16(i + 1), Name: spec.Name, Share: spec})
		}
		return afp.NewWithVolumes(logger, volSpecs...)
	})
}
