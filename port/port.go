package port

import "github.com/pgodw/omnitalk/appletalk"

type RouterHooks interface {
	Inbound(datagram appletalk.Datagram, rx Port)
}

type Port interface {
	ShortString() string
	Start(router RouterHooks) error
	Stop() error
	Unicast(network uint16, node uint8, datagram appletalk.Datagram)
	Broadcast(datagram appletalk.Datagram)
	Multicast(zoneName []byte, datagram appletalk.Datagram)
	SetNetworkRange(networkMin, networkMax uint16) error

	Network() uint16
	Node() uint8
	NetworkMin() uint16
	NetworkMax() uint16
	ExtendedNetwork() bool
}
