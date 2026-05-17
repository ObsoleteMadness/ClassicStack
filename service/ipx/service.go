// Package ipx hosts IPX-stack services (RIP, SAP, ...). They are
// lifecycle siblings of AppleTalk services, not members of the
// AppleTalk service.Service set: IPX has its own router and does
// not consume DDP datagrams.
package ipx

import "context"

// Service is the lifecycle contract for an IPX-stack service.
// Implementations register their own sockets with the IPX router
// during Start and tear them down during Stop.
type Service interface {
	Start(ctx context.Context) error
	Stop() error
}
