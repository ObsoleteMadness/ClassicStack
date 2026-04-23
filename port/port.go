package port

import "github.com/pgodw/omnitalk/protocol/ddp"

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
