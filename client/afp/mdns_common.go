package afp

// DSIPort is the well-known AFP-over-TCP (DSI) port. Servers advertise it via
// Bonjour as _afpovertcp._tcp.local. (RFC 6762 / Apple AFP over TCP).
const DSIPort = 548

// TCPServer is one AFP-over-TCP server learned from mDNS (Bonjour), not from NBP.
// Host is an IPv4 when the response carried an A record, otherwise the SRV target
// hostname. Port is 548 when the advertisement omitted SRV.
type TCPServer struct {
	Name string // instance label (the Chooser-style server name)
	Host string // IPv4 or hostname to dial
	Port uint16
}
