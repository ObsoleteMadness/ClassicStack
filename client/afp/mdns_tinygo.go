//go:build tinygo

// TinyGo's baremetal targets have no multicast UDP socket (golang.org/x/net/ipv4)
// and no net.Interface.Addrs()/net.InterfaceByName, so mDNS/Bonjour discovery is
// unavailable there; the real implementation lives in mdns.go.
package afp

import (
	"errors"
	"time"
)

// ErrMDNSUnsupported is returned by DiscoverTCP on builds that cannot browse mDNS.
var ErrMDNSUnsupported = errors.New("afp: mDNS discovery is not supported on this build")

// DiscoverTCP is a stub on TinyGo builds: see ErrMDNSUnsupported.
func DiscoverTCP(_ string, _ time.Duration) ([]TCPServer, error) {
	return nil, ErrMDNSUnsupported
}
