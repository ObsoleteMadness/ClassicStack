// Package ipx is the IPX socket-dispatch router. It is a peer of the
// AppleTalk router, not a member of it: IPX has its own address space
// (network number + 6-byte node ID + 2-byte socket) and its own
// inbound dispatch.
//
// The router holds a single IPX identity for the process: one network
// number (per-segment, configured by the operator) and one node ID
// (typically the interface MAC). The single-identity model is by
// design — bridging two IPX segments would need per-port identity,
// which is out of scope.
package ipx

import (
	"errors"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/netlog"
	"github.com/ObsoleteMadness/ClassicStack/port/ipx"
	protocol "github.com/ObsoleteMadness/ClassicStack/protocol/ipx"
)

// BroadcastNode is the IPX node-ID broadcast address (all-ones) used
// for SAP, RIP, and NetBIOS-over-IPX name claims.
var BroadcastNode = [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}

// DefaultNetwork is the fall-back IPX network number when the
// operator has not configured one. The all-zeros value ("local
// segment, unknown") matches the network number that Win98/NWLink
// uses before a NetWare server assigns a real network number, so
// ClassicStack and its clients appear on the same segment and can
// reach each other without routing. Operators running alongside a
// real NetWare server should configure an explicit network number.
var DefaultNetwork = [4]byte{0x00, 0x00, 0x00, 0x00}

// ErrNotImplemented is returned by stub call sites that have not yet
// been filled in.
var ErrNotImplemented = errors.New("ipx: not implemented")

// SocketHandler receives IPX datagrams whose destination socket
// matches a Register call.
type SocketHandler interface {
	HandleDatagram(d *protocol.Datagram)
}

// Router dispatches inbound IPX datagrams to socket handlers and
// fills source addresses on outbound datagrams. Implementations must
// be safe for concurrent use.
type Router interface {
	// SetIdentity configures the network and node ID this router
	// presents on the wire. Calling it after Start is allowed but
	// callers should not change identity while traffic is in flight.
	SetIdentity(network [4]byte, node [6]byte)
	// Network returns the configured IPX network number.
	Network() [4]byte
	// Node returns the configured IPX node ID.
	Node() [6]byte
	// RegisterSocket attaches handler to inbound datagrams whose
	// destination socket matches. Returns an error when socket is
	// already registered.
	RegisterSocket(socket [2]byte, handler SocketHandler) error
	// Send fills SrcNet/SrcNode on d (when zero) and forwards to the
	// first attached port. Returns an error when no port is attached.
	Send(d *protocol.Datagram) error
	// AddPort attaches a port to the router and installs the inbound
	// delivery callback that drives Inbound.
	AddPort(p ipx.Port)
	// Inbound is called by attached ports for each decoded inbound
	// datagram. The router enforces the address filter (DstNet/DstNode
	// match ours or broadcast) before dispatching to a SocketHandler.
	Inbound(d *protocol.Datagram)
}

type routerImpl struct {
	mu      sync.RWMutex
	network [4]byte
	node    [6]byte
	sockets map[[2]byte]SocketHandler
	ports   []ipx.Port
}

// NewRouter returns a router with the default network number and a
// zero node ID. Callers should set both via SetIdentity before any
// traffic flows.
func NewRouter() Router {
	return &routerImpl{
		network: DefaultNetwork,
		sockets: make(map[[2]byte]SocketHandler),
	}
}

func (r *routerImpl) SetIdentity(network [4]byte, node [6]byte) {
	r.mu.Lock()
	r.network = network
	r.node = node
	r.mu.Unlock()
}

func (r *routerImpl) Network() [4]byte {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.network
}

func (r *routerImpl) Node() [6]byte {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.node
}

func (r *routerImpl) RegisterSocket(socket [2]byte, handler SocketHandler) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.sockets[socket]; exists {
		return errors.New("ipx: socket already registered")
	}
	r.sockets[socket] = handler
	netlog.Debug("[IPX][Router] registered socket=%02x%02x", socket[0], socket[1])
	return nil
}

func (r *routerImpl) AddPort(p ipx.Port) {
	r.mu.Lock()
	r.ports = append(r.ports, p)
	r.mu.Unlock()
	p.SetDeliveryCallback(r.Inbound)
}

// Send fills SrcNet and SrcNode on the outgoing datagram (when zero)
// and writes it through the first attached port. Source fields that
// are already set are respected so callers that need to override
// (e.g. for forwarding traffic) still can.
func (r *routerImpl) Send(d *protocol.Datagram) error {
	r.mu.RLock()
	if len(r.ports) == 0 {
		r.mu.RUnlock()
		return errors.New("ipx: no ports attached")
	}
	port := r.ports[0]
	if isZero4(d.SrcNet) {
		d.SrcNet = r.network
	}
	if isZero6(d.SrcNode) {
		d.SrcNode = r.node
	}
	r.mu.RUnlock()
	netlog.Debug("[IPX][Router] tx type=0x%02x src=%x.%x:%02x%02x dst=%x.%x:%02x%02x payload=%d",
		d.Type,
		d.SrcNet, d.SrcNode, d.SrcSock[0], d.SrcSock[1],
		d.DstNet, d.DstNode, d.DstSock[0], d.DstSock[1],
		len(d.Payload),
	)
	return port.Send(d)
}

// Inbound is the port-side delivery callback. It enforces the
// addressed-to-us filter (kernel pcap delivers every IPX frame on the
// wire; the kernel filter only narrows by framing, not by destination)
// before dispatching to the registered socket handler.
func (r *routerImpl) Inbound(d *protocol.Datagram) {
	accepted, reason := r.acceptsDest(d.DstNet, d.DstNode)
	if !accepted {
		r.mu.RLock()
		ours := r.network
		myNode := r.node
		r.mu.RUnlock()
		netlog.Debug("[IPX][Router] drop inbound (dest mismatch: %s) type=0x%02x src=%x.%x:%02x%02x dst=%x.%x:%02x%02x local=%x.%x payload=%d",
			reason,
			d.Type,
			d.SrcNet, d.SrcNode, d.SrcSock[0], d.SrcSock[1],
			d.DstNet, d.DstNode, d.DstSock[0], d.DstSock[1],
			ours, myNode,
			len(d.Payload),
		)
		return
	}
	netlog.Debug("[IPX][Router] rx type=0x%02x src=%x.%x:%02x%02x dst=%x.%x:%02x%02x payload=%d",
		d.Type,
		d.SrcNet, d.SrcNode, d.SrcSock[0], d.SrcSock[1],
		d.DstNet, d.DstNode, d.DstSock[0], d.DstSock[1],
		len(d.Payload),
	)
	r.mu.RLock()
	handler, ok := r.sockets[d.DstSock]
	r.mu.RUnlock()
	if !ok {
		netlog.Debug("[IPX][Router] no handler for socket=%02x%02x", d.DstSock[0], d.DstSock[1])
		return
	}
	handler.HandleDatagram(d)
}

// acceptsDest returns true when (network, node) matches the router's
// identity or is a broadcast address. Network 0 ("local segment,
// unknown") is also accepted because some clients send name-claim
// broadcasts that way before learning the network number.
func (r *routerImpl) acceptsDest(network [4]byte, node [6]byte) (bool, string) {
	r.mu.RLock()
	ours := r.network
	myNode := r.node
	r.mu.RUnlock()

	if !isZero4(network) && network != ours {
		return false, "network"
	}
	if node == BroadcastNode {
		return true, ""
	}
	if node != myNode {
		return false, "node"
	}
	return true, ""
}

func isZero4(b [4]byte) bool { return b == [4]byte{} }
func isZero6(b [6]byte) bool { return b == [6]byte{} }
