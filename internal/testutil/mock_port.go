// Package testutil provides shared test helpers used across OmniTalk's
// service and port packages. Live under internal/ so external consumers
// cannot depend on these mocks; only project tests may import.
package testutil

import (
	"github.com/pgodw/omnitalk/port"
	"github.com/pgodw/omnitalk/protocol/ddp"
)

// MockPort is a fake port.Port whose behaviour is driven by func fields.
// Leave any field nil and its method is unsafe to call; wire up only the
// behaviours the test needs.
type MockPort struct {
	ShortStringFunc     func() string
	StartFunc           func(router port.RouterHooks) error
	StopFunc            func() error
	UnicastFunc         func(network uint16, node uint8, datagram ddp.Datagram)
	BroadcastFunc       func(datagram ddp.Datagram)
	MulticastFunc       func(zoneName []byte, datagram ddp.Datagram)
	SetNetworkRangeFunc func(networkMin, networkMax uint16) error
	NetworkFunc         func() uint16
	NodeFunc            func() uint8
	NetworkMinFunc      func() uint16
	NetworkMaxFunc      func() uint16
	ExtendedNetworkFunc func() bool
}

func (m *MockPort) ShortString() string                 { return m.ShortStringFunc() }
func (m *MockPort) Start(router port.RouterHooks) error { return m.StartFunc(router) }
func (m *MockPort) Stop() error                         { return m.StopFunc() }
func (m *MockPort) Unicast(network uint16, node uint8, datagram ddp.Datagram) {
	m.UnicastFunc(network, node, datagram)
}
func (m *MockPort) Broadcast(datagram ddp.Datagram) { m.BroadcastFunc(datagram) }
func (m *MockPort) Multicast(zoneName []byte, datagram ddp.Datagram) {
	m.MulticastFunc(zoneName, datagram)
}
func (m *MockPort) SetNetworkRange(networkMin, networkMax uint16) error {
	return m.SetNetworkRangeFunc(networkMin, networkMax)
}
func (m *MockPort) Network() uint16       { return m.NetworkFunc() }
func (m *MockPort) Node() uint8           { return m.NodeFunc() }
func (m *MockPort) NetworkMin() uint16    { return m.NetworkMinFunc() }
func (m *MockPort) NetworkMax() uint16    { return m.NetworkMaxFunc() }
func (m *MockPort) ExtendedNetwork() bool { return m.ExtendedNetworkFunc() }

// NewMockPort returns a MockPort pre-wired with common constant accessors
// (network, node, short string, extended flag). Call-time behaviours
// (Unicast/Broadcast/etc.) remain unset and must be supplied by the test.
func NewMockPort(network uint16, node uint8, shortString string, isExtended bool) *MockPort {
	return &MockPort{
		ShortStringFunc:     func() string { return shortString },
		NetworkFunc:         func() uint16 { return network },
		NodeFunc:            func() uint8 { return node },
		NetworkMinFunc:      func() uint16 { return network },
		NetworkMaxFunc:      func() uint16 { return network },
		ExtendedNetworkFunc: func() bool { return isExtended },
	}
}
