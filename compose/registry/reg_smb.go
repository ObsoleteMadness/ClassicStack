//go:build smb || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/service/smb"
)

func init() {
	// Register the SMB share repeated-section schema so codecs round-trip each
	// configured share as a named section. Kept here (not in an smb-package init)
	// so the section exists exactly when the SMB service is built.
	smb.RegisterShares()

	Register(smb.Name, func(m *config.Model) (component.Component, error) {
		logger := log.New(smb.Name, log.NewStderrSink(log.NewLevelVar(log.Info)))
		// Build one Share per configured share section. A model with no shares
		// yields a service with none (the historical zero-config default); a bad
		// spec (invalid fs_type×fork×codec triple or missing required param) fails
		// the build loudly here rather than mangling names at runtime.
		specs := smb.SpecsFromModel(m)
		if len(specs) == 0 {
			return smb.New(logger), nil
		}
		return smb.NewWithShares(logger, specs...)
	})
}
