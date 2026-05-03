package port

import "github.com/ObsoleteMadness/ClassicStack/protocol/ddp"

type RouterHooks interface {
	Inbound(datagram ddp.Datagram, rx Port)
}

type Port interface {
	ShortString() string
	Start(router RouterHooks) error
	Stop() error
	Unicast(network uint16, node uint8, datagram ddp.Datagram)
	Broadcast(datagram ddp.Datagram)
	Multicast(zoneName []byte, datagram ddp.Datagram)
	SetNetworkRange(networkMin, networkMax uint16) error

	Network() uint16
	Node() uint8
	NetworkMin() uint16
	NetworkMax() uint16
	ExtendedNetwork() bool
}

// BridgeConfigurable is implemented by ports that participate in an
// Ethernet-style bridge and need operator control over bridge mode and
// host-MAC synthesis. It is optional — callers type-assert on a Port to
// discover whether these knobs apply. EtherTalk pcap/tap ports
// implement it; LocalTalk and LToUDP ports do not.
//
// Keeping these methods out of the core Port interface means adding a
// new transport that does not need bridge configuration (e.g. a pure
// raw-socket port or a virtual test port) does not force a stub
// implementation.
type BridgeConfigurable interface {
	// SetBridgeModeString sets the bridge mode from its textual form
	// (e.g. "auto", "ethernet", "wifi"). Ports define their own accepted
	// values; invalid input returns a non-nil error.
	SetBridgeModeString(mode string) error
	// SetBridgeHostMAC sets the MAC address the port presents to the
	// bridged Ethernet segment. hostMAC must be a 6-byte EUI-48.
	SetBridgeHostMAC(hostMAC []byte) error
}
