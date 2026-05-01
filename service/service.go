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

// DatagramRouter is what every service can assume of the router: send a
// datagram and reply to one. The router-shaped capabilities below
// (RouteIndex, ZoneIndex) are layered on for the small number of
// services that maintain those tables.
type DatagramRouter interface {
	Route(datagram ddp.Datagram, originating bool) error
	Reply(datagram ddp.Datagram, rxPort port.Port, ddpType uint8, data []byte)
	PortsList() []port.Port
	Zones() [][]byte
}

// RouteIndex exposes the routing table to RTMP (which owns it) and to
// ZIP's sending path (which iterates known networks). Services that do
// not maintain or scan the routing table must not depend on this.
type RouteIndex interface {
	RoutingGetByNetwork(network uint16) (*RouteEntry, *bool)
	RoutingEntries() []struct {
		Entry *RouteEntry
		Bad   bool
	}
	RoutingConsider(entry *RouteEntry) bool
	RoutingMarkBad(networkMin, networkMax uint16) bool
	RoutingTableAge()
}

// ZoneIndex exposes the zone-information table to ZIP and to seed-zone
// registration during port startup. AddNetworksToZone is called by
// ports via anonymous-interface assertion at port-Start time, not
// through the service.Router contract.
type ZoneIndex interface {
	ZonesInNetworkRange(networkMin uint16, networkMax *uint16) ([][]byte, error)
	NetworksInZone(zoneName []byte) []uint16
	AddNetworksToZone(zoneName []byte, networkMin uint16, networkMax *uint16) error
}

// Router is the union every concrete router (router.Router) satisfies and
// that Service.Start receives. Services should narrow this to the
// capability subset they actually use as soon as it crosses into their
// own code — see zip and rtmp for the pattern.
type Router interface {
	DatagramRouter
	RouteIndex
	ZoneIndex
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
