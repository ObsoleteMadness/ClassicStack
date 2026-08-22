//go:build afp || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/adapter/dsi"
	"github.com/ObsoleteMadness/ClassicStack/core/component"
)

func init() {
	// The DSI (AFP-over-TCP) transport is an adapter listener with its own lifecycle,
	// so it is a supervised component — distinct from the AFP command service. It is
	// built INERT (no handler, no address): the compose transport cross-wire installs
	// the AFP command handler and the listen address from the AFP server section once
	// AFP is resolved (mirrors reg_smbtcp.go). With no AFP service, or with tcp_addr
	// unset, it stays inert — Start is a no-op.
	Register(dsi.Name, func(ctx *BuildContext) (component.Component, error) {
		logger := ctx.Logger(dsi.Name)
		return dsi.New("", nil, logger), nil
	})
}
