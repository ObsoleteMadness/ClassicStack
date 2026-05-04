package ipx

import (
	"context"

	ipxproto "github.com/ObsoleteMadness/ClassicStack/protocol/ipx"
	routeripx "github.com/ObsoleteMadness/ClassicStack/router/ipx"
)

// SAPSocket is the well-known socket number for IPX SAP.
var SAPSocket = [2]byte{0x04, 0x52}

// SAPService is a stub for IPX Service Advertising Protocol. It
// registers the SAP socket on the IPX router but produces no traffic.
type SAPService struct {
	router routeripx.Router
}

// NewSAPService returns a stubbed SAP service bound to r.
func NewSAPService(r routeripx.Router) *SAPService {
	return &SAPService{router: r}
}

// Start registers the SAP socket on the IPX router.
func (s *SAPService) Start(_ context.Context) error {
	return s.router.RegisterSocket(SAPSocket, s)
}

// Stop is a no-op for the stub.
func (s *SAPService) Stop() error { return nil }

// HandleDatagram implements router/ipx.SocketHandler.
func (s *SAPService) HandleDatagram(_ *ipxproto.Datagram) {}
