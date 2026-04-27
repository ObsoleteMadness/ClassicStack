//go:build afp

package afp

import (
	"github.com/pgodw/omnitalk/protocol/ddp"

	"github.com/pgodw/omnitalk/port"
	"github.com/pgodw/omnitalk/service"
)

// CommandHandler handles decoded AFP commands from transport protocols.
type CommandHandler interface {
	HandleCommand(data []byte) ([]byte, int32)
	GetStatus() []byte
}

// Transport represents a network transport that serves the AFP protocol (e.g., ASP over DDP, or DSI over TCP/IP).
type Transport interface {
	// Start starts the transport using the provided router (for AppleTalk NBP/routing).
	Start(router service.Router) error

	// Stop shuts down the transport and cleans up any resources.
	Stop() error

	// Inbound processes an incoming AppleTalk datagram, if the transport uses DDP.
	// For IP-only transports, this can be a no-op.
	Inbound(d ddp.Datagram, p port.Port)
}
