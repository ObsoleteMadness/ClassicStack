// Package over_tcp implements NBT (NetBIOS over TCP/IP) — RFC 1001/
// 1002 — as a netbios.Transport. The transport is a stub: Start/Stop
// are no-ops and the listener machinery lands when the real NBT
// handshake does.
package over_tcp

import (
	"context"
	"sync"

	protocol "github.com/ObsoleteMadness/ClassicStack/protocol/netbios"
	"github.com/ObsoleteMadness/ClassicStack/service/netbios"
)

// Default well-known NBT ports.
const (
	NameServiceUDPPort     = 137
	DatagramServiceUDPPort = 138
	SessionServiceTCPPort  = 139
)

type transport struct {
	mu      sync.RWMutex
	handler netbios.CommandHandler
}

// NewTransport returns a netbios.Transport for NBT.
func NewTransport() netbios.Transport {
	return &transport{}
}

func (t *transport) Start(_ context.Context) error { return nil }
func (t *transport) Stop() error                   { return nil }

func (t *transport) SendName(_ protocol.Name) error              { return netbios.ErrNotImplemented }
func (t *transport) SendDatagram(_ *protocol.Datagram) error     { return netbios.ErrNotImplemented }
func (t *transport) SendSession(_ *protocol.SessionPacket) error { return netbios.ErrNotImplemented }

func (t *transport) SetCommandHandler(h netbios.CommandHandler) {
	t.mu.Lock()
	t.handler = h
	t.mu.Unlock()
}
