package ipx

import (
	"errors"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/port/ipx"
	protocol "github.com/ObsoleteMadness/ClassicStack/protocol/ipx"
)

var ErrNotImplemented = errors.New("not implemented")

// SocketHandler receives IPX datagrams for a specific socket.
type SocketHandler interface {
	HandleDatagram(d *protocol.Datagram)
}

// Router dispatches incoming IPX datagrams to registered sockets and routes
// outgoing datagrams to the correct port.
type Router interface {
	RegisterSocket(socket [2]byte, handler SocketHandler) error
	Send(d *protocol.Datagram) error
	AddPort(p ipx.Port)
	Inbound(d *protocol.Datagram)
}

type routerImpl struct {
	mu      sync.RWMutex
	sockets map[[2]byte]SocketHandler
	ports   []ipx.Port
}

// NewRouter creates a new IPX router.
func NewRouter() Router {
	return &routerImpl{
		sockets: make(map[[2]byte]SocketHandler),
	}
}

func (r *routerImpl) RegisterSocket(socket [2]byte, handler SocketHandler) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.sockets[socket]; exists {
		return errors.New("socket already registered")
	}
	r.sockets[socket] = handler
	return nil
}

func (r *routerImpl) AddPort(p ipx.Port) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ports = append(r.ports, p)
	p.SetDeliveryCallback(r.Inbound)
}

func (r *routerImpl) Send(d *protocol.Datagram) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.ports) == 0 {
		return errors.New("no ports available")
	}
	// For now, just send on the first port
	return r.ports[0].Send(d)
}

func (r *routerImpl) Inbound(d *protocol.Datagram) {
	r.mu.RLock()
	handler, ok := r.sockets[d.DstSock]
	r.mu.RUnlock()
	
	if ok {
		handler.HandleDatagram(d)
	}
}
