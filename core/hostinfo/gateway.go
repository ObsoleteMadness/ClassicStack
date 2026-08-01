package hostinfo

import (
	"errors"
	"net"
)

// gateway.go resolves the host's default-route gateway IP — the upstream router the OS
// would send off-subnet traffic to. It complements primary.go (which finds the local
// source IP / interface for the default route) by returning the NEXT HOP itself, which
// the UDP-dial trick cannot reveal. The MacIP gateway advertises this to MacTCP clients
// in bridge mode so they receive a real, on-subnet gateway rather than 0.0.0.0.
//
// Resolution consults the OS routing table via a per-OS implementation (gateway_*.go):
// Linux reads /proc/net/route, Windows calls iphlpapi GetBestRoute, and other platforms
// fall back to unsupported. All are pcap-free and need no privileges.

// ErrNoDefaultGateway is returned when the default-route gateway cannot be resolved (no
// default route, or the platform lookup is unsupported).
var ErrNoDefaultGateway = errors.New("hostinfo: no default gateway (no default route)")

// DefaultGateway returns the IPv4 address of the host's default-route gateway. It
// returns ErrNoDefaultGateway when there is no default route or the platform cannot
// resolve one. The result is a next-hop router address (e.g. 192.168.0.1), never the
// host's own address.
func DefaultGateway() (net.IP, error) {
	return defaultGateway()
}
