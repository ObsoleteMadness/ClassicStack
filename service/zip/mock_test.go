package zip

import (
	"github.com/pgodw/omnitalk/appletalk"
	"github.com/pgodw/omnitalk/port"
	"github.com/pgodw/omnitalk/service"
)

type mockPort struct {
	shortStringFunc     func() string
	startFunc           func(router port.RouterHooks) error
	stopFunc            func() error
	unicastFunc         func(network uint16, node uint8, datagram appletalk.Datagram)
	broadcastFunc       func(datagram appletalk.Datagram)
	multicastFunc       func(zoneName []byte, datagram appletalk.Datagram)
	setNetworkRangeFunc func(networkMin, networkMax uint16) error
	networkFunc         func() uint16
	nodeFunc            func() uint8
	networkMinFunc      func() uint16
	networkMaxFunc      func() uint16
	extendedNetworkFunc func() bool
}

func (m *mockPort) ShortString() string                 { return m.shortStringFunc() }
func (m *mockPort) Start(router port.RouterHooks) error { return m.startFunc(router) }
func (m *mockPort) Stop() error                         { return m.stopFunc() }
func (m *mockPort) Unicast(network uint16, node uint8, datagram appletalk.Datagram) {
	m.unicastFunc(network, node, datagram)
}
func (m *mockPort) Broadcast(datagram appletalk.Datagram) { m.broadcastFunc(datagram) }
func (m *mockPort) Multicast(zoneName []byte, datagram appletalk.Datagram) {
	m.multicastFunc(zoneName, datagram)
}
func (m *mockPort) SetNetworkRange(networkMin, networkMax uint16) error {
	return m.setNetworkRangeFunc(networkMin, networkMax)
}
func (m *mockPort) Network() uint16       { return m.networkFunc() }
func (m *mockPort) Node() uint8           { return m.nodeFunc() }
func (m *mockPort) NetworkMin() uint16    { return m.networkMinFunc() }
func (m *mockPort) NetworkMax() uint16    { return m.networkMaxFunc() }
func (m *mockPort) ExtendedNetwork() bool { return m.extendedNetworkFunc() }

type mockRouter struct {
	routeFunc               func(datagram appletalk.Datagram, originating bool) error
	replyFunc               func(datagram appletalk.Datagram, rxPort port.Port, ddpType uint8, data []byte)
	portsListFunc           func() []port.Port
	routingGetByNetworkFunc func(network uint16) (*service.RouteEntry, *bool)
	routingEntriesFunc      func() []struct {
		Entry *service.RouteEntry
		Bad   bool
	}
	routingConsiderFunc     func(entry *service.RouteEntry) bool
	routingMarkBadFunc      func(networkMin, networkMax uint16) bool
	zonesInNetworkRangeFunc func(networkMin uint16, networkMax *uint16) ([][]byte, error)
	networksInZoneFunc      func(zoneName []byte) []uint16
	zonesFunc               func() [][]byte
	addNetworksToZoneFunc   func(zoneName []byte, networkMin uint16, networkMax *uint16) error
	routingTableAgeFunc     func()
}

func (m *mockRouter) Route(datagram appletalk.Datagram, originating bool) error {
	return m.routeFunc(datagram, originating)
}
func (m *mockRouter) Reply(datagram appletalk.Datagram, rxPort port.Port, ddpType uint8, data []byte) {
	m.replyFunc(datagram, rxPort, ddpType, data)
}
func (m *mockRouter) PortsList() []port.Port { return m.portsListFunc() }
func (m *mockRouter) RoutingGetByNetwork(network uint16) (*service.RouteEntry, *bool) {
	return m.routingGetByNetworkFunc(network)
}
func (m *mockRouter) RoutingEntries() []struct {
	Entry *service.RouteEntry
	Bad   bool
} {
	return m.routingEntriesFunc()
}
func (m *mockRouter) RoutingConsider(entry *service.RouteEntry) bool {
	return m.routingConsiderFunc(entry)
}
func (m *mockRouter) RoutingMarkBad(networkMin, networkMax uint16) bool {
	return m.routingMarkBadFunc(networkMin, networkMax)
}
func (m *mockRouter) ZonesInNetworkRange(networkMin uint16, networkMax *uint16) ([][]byte, error) {
	return m.zonesInNetworkRangeFunc(networkMin, networkMax)
}
func (m *mockRouter) NetworksInZone(zoneName []byte) []uint16 { return m.networksInZoneFunc(zoneName) }
func (m *mockRouter) Zones() [][]byte                         { return m.zonesFunc() }
func (m *mockRouter) AddNetworksToZone(zoneName []byte, networkMin uint16, networkMax *uint16) error {
	return m.addNetworksToZoneFunc(zoneName, networkMin, networkMax)
}
func (m *mockRouter) RoutingTableAge() { m.routingTableAgeFunc() }
