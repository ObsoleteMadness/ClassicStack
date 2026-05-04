// Package over_ipx adapts the IPX router to the netbios.Transport
// contract. NetBIOS over IPX (NWLink) uses three sockets:
//
//	0x0455 — NetBIOS-over-IPX
//	0x0553 — NetBIOS datagram
//	0x0554 — NetBIOS name service
package over_ipx

import (
	"context"
	"sync"

	ipxproto "github.com/ObsoleteMadness/ClassicStack/protocol/ipx"
	protocol "github.com/ObsoleteMadness/ClassicStack/protocol/netbios"
	"github.com/ObsoleteMadness/ClassicStack/router/ipx"
	"github.com/ObsoleteMadness/ClassicStack/service/netbios"
)

// Sockets is the ordered list of IPX socket numbers NetBIOS-over-IPX
// claims. Exposed for documentation and tests.
var Sockets = [3][2]byte{
	{0x04, 0x55},
	{0x05, 0x53},
	{0x05, 0x54},
}

type transport struct {
	router ipx.Router

	mu      sync.RWMutex
	handler netbios.CommandHandler
}

// NewTransport returns a netbios.Transport that registers on r's
// well-known NetBIOS sockets when started.
func NewTransport(r ipx.Router) netbios.Transport {
	return &transport{router: r}
}

func (t *transport) Start(_ context.Context) error {
	for _, sock := range Sockets {
		// The transport itself implements ipx.SocketHandler so the IPX
		// router can deliver inbound datagrams here.
		if err := t.router.RegisterSocket(sock, t); err != nil {
			return err
		}
	}
	return nil
}

func (t *transport) Stop() error { return nil }

func (t *transport) SendName(_ protocol.Name) error              { return netbios.ErrNotImplemented }
func (t *transport) SendDatagram(_ *protocol.Datagram) error     { return netbios.ErrNotImplemented }
func (t *transport) SendSession(_ *protocol.SessionPacket) error { return netbios.ErrNotImplemented }

func (t *transport) SetCommandHandler(h netbios.CommandHandler) {
	t.mu.Lock()
	t.handler = h
	t.mu.Unlock()
}

// HandleDatagram implements router/ipx.SocketHandler. The stub
// drops inbound IPX datagrams without parsing the NetBIOS payload.
func (t *transport) HandleDatagram(_ *ipxproto.Datagram) {
	t.mu.RLock()
	_ = t.handler
	t.mu.RUnlock()
}
