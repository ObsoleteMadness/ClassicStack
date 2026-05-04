package ipx

import (
	"context"

	ipxproto "github.com/ObsoleteMadness/ClassicStack/protocol/ipx"
	routeripx "github.com/ObsoleteMadness/ClassicStack/router/ipx"
)

// RIPSocket is the well-known socket number for IPX RIP.
var RIPSocket = [2]byte{0x04, 0x53}

// RIPService is a stub for IPX Routing Information Protocol. It
// registers the RIP socket on the IPX router but produces no traffic.
type RIPService struct {
	router routeripx.Router
}

// NewRIPService returns a stubbed RIP service bound to r.
func NewRIPService(r routeripx.Router) *RIPService {
	return &RIPService{router: r}
}

// Start registers the RIP socket on the IPX router.
func (s *RIPService) Start(_ context.Context) error {
	return s.router.RegisterSocket(RIPSocket, s)
}

// Stop is a no-op for the stub; the router does not yet support
// socket deregistration.
func (s *RIPService) Stop() error { return nil }

// HandleDatagram implements router/ipx.SocketHandler. The stub
// silently drops everything until the real RIP exchange lands.
func (s *RIPService) HandleDatagram(_ *ipxproto.Datagram) {}
