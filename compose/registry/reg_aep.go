//go:build router || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/service/aep"
)

func init() {
	// AEP is the core AppleTalk Echo Protocol responder (socket 4): it reflects echo
	// requests back to the sender, the substrate for the AEP-echo diagnostic. It rides
	// the shared router; crossWireRouter registers its socket. Gated on the router tag.
	Register(aep.Name, func(ctx *BuildContext) (component.Component, error) {
		logger := log.New(aep.Name, log.NewStderrSink(log.NewLevelVar(log.Info)))
		return aep.New(routerFor(ctx), logger), nil
	})
}
