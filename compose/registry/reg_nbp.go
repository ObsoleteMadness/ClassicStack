//go:build router || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/service/nbp"
)

func init() {
	// NBP is the core Name Binding Protocol name-information service (socket 2): it owns
	// the registered-name table and answers BrRq/LkUp/Fwd. Other DDP services (MacIP,
	// IPXGW) register their advertised names here so Macs discover them. It rides the
	// shared router; crossWireRouter registers its socket. Gated on the router tag.
	Register(nbp.Name, func(ctx *BuildContext) (component.Component, error) {
		logger := ctx.Logger(nbp.Name)
		return nbp.New(routerFor(ctx), logger), nil
	})
}
