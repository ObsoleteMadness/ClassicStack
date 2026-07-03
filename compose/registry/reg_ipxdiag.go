//go:build ipxdiag || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/service/ipxdiag"
)

func init() {
	Register(ipxdiag.Name, func(ctx *BuildContext) (component.Component, error) {
		logger := ctx.Logger(ipxdiag.Name)
		// Built with NO sender and a zero node: the IPX mini-router that carries the
		// reply egress is stood up during the transport cross-wire (crossWireTransports),
		// which then injects the sender via SetSender, sets the station node via SetNode,
		// and registers this responder on the mini-router as the SocketHandler for the
		// diagnostic socket 0x0456. With no IPX port in the build it stays built but
		// unwired — it simply never receives a request. This mirrors how the browser is
		// built sink-less and wired later.
		return ipxdiag.New(logger, nil, [6]byte{}), nil
	})
}
