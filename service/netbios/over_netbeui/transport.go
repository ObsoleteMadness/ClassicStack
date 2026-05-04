// Package over_netbeui adapts a NetBEUI port to the netbios.Transport
// contract. The transport is a stub: send paths return nil but emit
// nothing, and inbound NBF frames are decoded and forwarded to the
// CommandHandler if one is set.
package over_netbeui

import (
	"context"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/port/netbeui"
	protocol "github.com/ObsoleteMadness/ClassicStack/protocol/netbios"
	netbeuiproto "github.com/ObsoleteMadness/ClassicStack/protocol/netbeui"
	"github.com/ObsoleteMadness/ClassicStack/service/netbios"
)

type transport struct {
	port netbeui.Port

	mu      sync.RWMutex
	handler netbios.CommandHandler
}

// NewTransport returns a netbios.Transport backed by an existing
// NetBEUI port. The port must already be configured (source MAC,
// rawlink open) by the caller.
func NewTransport(p netbeui.Port) netbios.Transport {
	return &transport{port: p}
}

func (t *transport) Start(_ context.Context) error {
	t.port.SetDeliveryCallback(t.onFrame)
	return nil
}

func (t *transport) Stop() error {
	t.port.SetDeliveryCallback(nil)
	return nil
}

// SendName/SendDatagram/SendSession are stubs: NBF carries all three
// inside a single frame format, but the mapping from session-layer
// PDUs to NBF commands is non-trivial and lands with the real
// transport implementation.
func (t *transport) SendName(_ protocol.Name) error              { return netbios.ErrNotImplemented }
func (t *transport) SendDatagram(_ *protocol.Datagram) error     { return netbios.ErrNotImplemented }
func (t *transport) SendSession(_ *protocol.SessionPacket) error { return netbios.ErrNotImplemented }

func (t *transport) SetCommandHandler(h netbios.CommandHandler) {
	t.mu.Lock()
	t.handler = h
	t.mu.Unlock()
}

// onFrame is the netbeui.Port delivery callback. Real NBF dispatch
// will decode the NBF command and dispatch to the handler's session
// or datagram path; the stub drops everything.
func (t *transport) onFrame(_ *netbeuiproto.Frame) {
	t.mu.RLock()
	_ = t.handler
	t.mu.RUnlock()
}
