package service

import (
	"context"

	"github.com/pgodw/omnitalk/protocol/ddp"

	"github.com/pgodw/omnitalk/port"
)

// Service is the contract every service registered with the router
// satisfies. Start receives a parent context that is cancelled when the
// router shuts down; implementations should derive their own per-goroutine
// contexts from it so background work can be aborted without waiting for
// hardcoded timeouts. Stop is still required for synchronous teardown of
// resources that the context cannot itself release (open files, OS NAT,
// pcap handles).
type Service interface {
	Start(ctx context.Context, router Router) error
	Stop() error
	Inbound(datagram ddp.Datagram, rxPort port.Port)
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
	Route(datagram ddp.Datagram, originating bool) error
	Reply(datagram ddp.Datagram, rxPort port.Port, ddpType uint8, data []byte)
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
