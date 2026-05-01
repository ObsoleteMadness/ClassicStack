package testutil

import (
	"github.com/pgodw/omnitalk/port"
	"github.com/pgodw/omnitalk/protocol/ddp"
	"github.com/pgodw/omnitalk/service"
)

// MockRouter is a fake service.Router whose behaviour is driven by func
// fields. Leave any field nil and its method is unsafe to call.
type MockRouter struct {
	RouteFunc               func(datagram ddp.Datagram, originating bool) error
	ReplyFunc               func(datagram ddp.Datagram, rxPort port.Port, ddpType uint8, data []byte)
	PortsListFunc           func() []port.Port
	RoutingGetByNetworkFunc func(network uint16) (*service.RouteEntry, *bool)
	RoutingEntriesFunc      func() []struct {
		Entry *service.RouteEntry
		Bad   bool
	}
	RoutingConsiderFunc     func(entry *service.RouteEntry) bool
	RoutingMarkBadFunc      func(networkMin, networkMax uint16) bool
	ZonesInNetworkRangeFunc func(networkMin uint16, networkMax *uint16) ([][]byte, error)
	NetworksInZoneFunc      func(zoneName []byte) []uint16
	ZonesFunc               func() [][]byte
	AddNetworksToZoneFunc   func(zoneName []byte, networkMin uint16, networkMax *uint16) error
	RoutingTableAgeFunc     func()
}

func (m *MockRouter) Route(datagram ddp.Datagram, originating bool) error {
	return m.RouteFunc(datagram, originating)
}
func (m *MockRouter) Reply(datagram ddp.Datagram, rxPort port.Port, ddpType uint8, data []byte) {
	m.ReplyFunc(datagram, rxPort, ddpType, data)
}
func (m *MockRouter) PortsList() []port.Port { return m.PortsListFunc() }
func (m *MockRouter) RoutingGetByNetwork(network uint16) (*service.RouteEntry, *bool) {
	return m.RoutingGetByNetworkFunc(network)
}
func (m *MockRouter) RoutingEntries() []struct {
	Entry *service.RouteEntry
	Bad   bool
} {
	return m.RoutingEntriesFunc()
}
func (m *MockRouter) RoutingConsider(entry *service.RouteEntry) bool {
	return m.RoutingConsiderFunc(entry)
}
func (m *MockRouter) RoutingMarkBad(networkMin, networkMax uint16) bool {
	return m.RoutingMarkBadFunc(networkMin, networkMax)
}
func (m *MockRouter) ZonesInNetworkRange(networkMin uint16, networkMax *uint16) ([][]byte, error) {
	return m.ZonesInNetworkRangeFunc(networkMin, networkMax)
}
func (m *MockRouter) NetworksInZone(zoneName []byte) []uint16 { return m.NetworksInZoneFunc(zoneName) }
func (m *MockRouter) Zones() [][]byte                         { return m.ZonesFunc() }
func (m *MockRouter) AddNetworksToZone(zoneName []byte, networkMin uint16, networkMax *uint16) error {
	return m.AddNetworksToZoneFunc(zoneName, networkMin, networkMax)
}
func (m *MockRouter) RoutingTableAge() { m.RoutingTableAgeFunc() }

// NewMockRouter returns a MockRouter with no behaviours wired up. Tests
// set the fields they need before use.
func NewMockRouter() *MockRouter { return &MockRouter{} }
