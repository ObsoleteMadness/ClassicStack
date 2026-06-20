//go:build smb || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/adapter/smbtcp"
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
)

func init() {
	// The SMB-over-TCP transport (direct-TCP :445 / NBT :139) is an adapter listener
	// with its own lifecycle, so it is a supervised component — distinct from the SMB
	// command service. It is built INERT (no consumer, no address): the compose
	// transport cross-wire installs the SMB session consumer and the listen address
	// from the SMB server section once SMB is resolved (mirrors how the browser/
	// messenger are built sink-less and wired later). With no SMB service, or with the
	// tcp/nbt bindings off, it stays inert — Start is a no-op.
	Register(smbtcp.Name, func(ctx *BuildContext) (component.Component, error) {
		logger := log.New(smbtcp.Name, log.NewStderrSink(log.NewLevelVar(log.Info)))
		return smbtcp.New("", nil, logger), nil
	})
}
