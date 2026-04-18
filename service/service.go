package service

import (
	"github.com/pgodw/omnitalk/go/appletalk"
	"github.com/pgodw/omnitalk/go/port"
)

type Service interface {
	Start(router Router) error
	Stop() error
	Inbound(datagram appletalk.Datagram, rxPort port.Port)
}

// PacketDumper is a sink for service-level packet logging.
type PacketDumper interface {
	LogPacket(message string)
}

// PacketDumpAware is implemented by services that can emit parsed packet logs.
type PacketDumpAware interface {
	SetPacketDumper(dumper PacketDumper)
}

type Router interface {
	Route(datagram appletalk.Datagram, originating bool) error
	Reply(datagram appletalk.Datagram, rxPort port.Port, ddpType uint8, data []byte)
	PortsList() []port.Port
	RoutingGetByNetwork(network uint16) (*RouteEntry, *bool)
	RoutingEntries() []struct {
		Entry *RouteEntry
		Bad   bool
	}
	RoutingConsider(entry *RouteEntry) bool
	RoutingMarkBad(networkMin, networkMax uint16) bool
	ZonesInNetworkRange(networkMin uint16, networkMax *uint16) ([][]byte, error)
	NetworksInZone(zoneName []byte) []uint16
	Zones() [][]byte
	AddNetworksToZone(zoneName []byte, networkMin uint16, networkMax *uint16) error
	RoutingTableAge()
}

type RouteEntry struct {
	ExtendedNetwork bool
	NetworkMin      uint16
	NetworkMax      uint16
	Distance        uint8
	Port            port.Port
	NextNetwork     uint16
	NextNode        uint8
}
