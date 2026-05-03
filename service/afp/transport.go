//go:build afp || all

package afp

import (
	"context"

	"github.com/ObsoleteMadness/ClassicStack/protocol/ddp"

	"github.com/ObsoleteMadness/ClassicStack/port"
	"github.com/ObsoleteMadness/ClassicStack/service"
)

// CommandHandler handles decoded AFP commands from transport protocols.
type CommandHandler interface {
	HandleCommand(data []byte) ([]byte, int32)
	GetStatus() []byte
}

// Transport represents a network transport that serves the AFP protocol (e.g., ASP over DDP, or DSI over TCP/IP).
type Transport interface {
	// Start starts the transport using the provided router (for AppleTalk NBP/routing).
	Start(ctx context.Context, router service.Router) error

	// Stop shuts down the transport and cleans up any resources.
	Stop() error

	// Inbound processes an incoming AppleTalk datagram, if the transport uses DDP.
	// For IP-only transports, this can be a no-op.
	Inbound(d ddp.Datagram, p port.Port)

	// MaxReadSize returns the largest single-reply payload the transport can
	// deliver, used by AFP to cap FPRead ReqCount and any range-limited
	// filesystem fetches. Transports without a fixed limit return 0.
	// Called by AFP after the transport has resolved its quantum (e.g. ASP
	// after SPGetParms); MaxReadSize before that point may return 0.
	MaxReadSize() int
}
