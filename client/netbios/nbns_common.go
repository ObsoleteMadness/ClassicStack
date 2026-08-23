package netbios

import "net"

// NBNSAnswer is one unique-name mapping returned by a name-service query: the
// NetBIOS name (trimmed) and the IPv4 that registered it.
type NBNSAnswer struct {
	Name string
	IP   net.IP
}
