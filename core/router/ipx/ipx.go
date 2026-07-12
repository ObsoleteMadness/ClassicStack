// Package ipx is the IPX socket-dispatch mini-router. It is a peer of the AppleTalk router,
// not a member of it (§3): IPX has its own address space (4-byte network + 6-byte node +
// 2-byte socket) and its own inbound dispatch, so it does not ride the DDP router. It is fed
// by the M3 IPX frame port via a delivery callback and sends through it.
//
// The router holds a single IPX identity for the process: one network number (per-segment,
// operator-configured) and one node ID (typically the interface MAC). The single-identity
// model is by design — bridging two IPX segments would need per-port identity, out of scope.
//
// Ring: CORE (stdlib only). Ported from the legacy router/ipx, re-expressed against the core
// IPX port (a small Port interface here, to avoid importing the concrete port package) and
// core/log (no netlog). On Ethernet the IPX node ID is the MAC, so unicast resolves the
// destination MAC directly from the datagram's DstNode.
package ipx

import (
	"errors"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/core/log"
	portipx "github.com/ObsoleteMadness/ClassicStack/core/port/ipx"
	protocol "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx"
)

// BroadcastNode is the IPX node-ID broadcast address (all-ones), used for SAP, RIP, and
// NetBIOS-over-IPX name claims.
var BroadcastNode = [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}

// InternalNode is the node ID of the server on its internal network. NetWare's internal
// network always hosts the server at node 00-00-00-00-00-01 (mars_nwe nwserv.c: node
// defaults to 1; a real NetWare 4 server advertises the same). SAP advertises the NCP
// file service at internal-net:InternalNode:0x0451.
var InternalNode = [6]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x01}

// DeriveInternalNetwork returns the default NetWare internal network number for a
// station: the low four bytes of its node ID. The internal network must be nonzero
// and unique on the internetwork; deriving it from the (unique) hardware address is
// the same spirit as mars_nwe's AUTO mode, which derives it from the host's IP
// address. A node whose low bytes are all zero falls back to a fixed nonzero number.
func DeriveInternalNetwork(node [6]byte) [4]byte {
	net := [4]byte{node[2], node[3], node[4], node[5]}
	if net == ([4]byte{}) {
		return [4]byte{0x00, 0x00, 0x00, 0x01}
	}
	return net
}

// DefaultNetwork is the fall-back IPX network number when the operator has not configured one.
// All-zeros ("local segment, unknown") matches what Win98/NWLink uses before a NetWare server
// assigns a real number, so ClassicStack and its clients appear on the same segment.
var DefaultNetwork = [4]byte{0x00, 0x00, 0x00, 0x00}

// Port is the IPX frame port the mini-router drives: it installs an inbound delivery callback
// and sends datagrams to a resolved destination MAC. The core IPX port (core/port/ipx)
// satisfies it; the callback type is the port package's so satisfaction is exact.
type Port interface {
	SetDeliveryCallback(cb portipx.DeliveryCallback)
	Send(dstMAC [6]byte, d *protocol.Datagram) error
	SrcMAC() [6]byte
}

// SocketHandler receives IPX datagrams whose destination socket matches a RegisterSocket call.
type SocketHandler interface {
	HandleDatagram(d *protocol.Datagram)
}

// NodeHandler receives every inbound IPX datagram addressed to a specific (non-router-owned)
// node ID. The MacIPX gateway uses this to claim the pool of node IDs it hands to Mac clients.
// NodeHandler takes precedence over SocketHandler dispatch.
type NodeHandler interface {
	HandleNodeDatagram(d *protocol.Datagram)
}

// Router dispatches inbound IPX datagrams to socket/node/broadcast handlers and fills source
// addresses on outbound datagrams. Implementations are safe for concurrent use.
type Router struct {
	logger      log.Logger
	mu          sync.RWMutex
	network     [4]byte
	node        [6]byte
	internalNet [4]byte // NetWare internal network (zero = none); see SetInternalIdentity
	sockets     map[[2]byte]SocketHandler
	nodes       map[[6]byte]NodeHandler
	broadcast   NodeHandler
	ports       []Port
}

// NewRouter returns a router with the default network number and a zero node ID. Callers
// should set both via SetIdentity before any traffic flows.
func NewRouter(logger log.Logger) *Router {
	return &Router{
		logger:  logger,
		network: DefaultNetwork,
		sockets: make(map[[2]byte]SocketHandler),
		nodes:   make(map[[6]byte]NodeHandler),
	}
}

// SetIdentity configures the network and node ID this router presents on the wire.
func (r *Router) SetIdentity(network [4]byte, node [6]byte) {
	r.mu.Lock()
	r.network = network
	r.node = node
	r.mu.Unlock()
}

// Network returns the configured IPX network number.
func (r *Router) Network() [4]byte {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.network
}

// Node returns the configured IPX node ID.
func (r *Router) Node() [6]byte {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.node
}

// SetInternalNetwork configures the NetWare internal network number. The server is
// addressable on it as internal-net:InternalNode (the NCP file service's advertised
// address, mars_nwe's my_server_adr): inbound datagrams so addressed pass the
// destination filter and dispatch by socket as usual. Zero disables the internal
// network (the default).
func (r *Router) SetInternalNetwork(network [4]byte) {
	r.mu.Lock()
	r.internalNet = network
	r.mu.Unlock()
}

// InternalNetwork returns the configured NetWare internal network number (zero = none).
func (r *Router) InternalNetwork() [4]byte {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.internalNet
}

// RegisterSocket attaches handler to inbound datagrams whose destination socket matches.
func (r *Router) RegisterSocket(socket [2]byte, handler SocketHandler) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.sockets[socket]; exists {
		return errors.New("ipx: socket already registered")
	}
	r.sockets[socket] = handler
	return nil
}

// UnregisterSocket removes a RegisterSocket binding. Idempotent.
func (r *Router) UnregisterSocket(socket [2]byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sockets, socket)
}

// RegisterNode attaches handler to every inbound datagram whose destination node matches.
func (r *Router) RegisterNode(node [6]byte, handler NodeHandler) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.nodes[node]; exists {
		return errors.New("ipx: node already registered")
	}
	r.nodes[node] = handler
	return nil
}

// UnregisterNode removes a RegisterNode binding. Idempotent.
func (r *Router) UnregisterNode(node [6]byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.nodes, node)
}

// RegisterBroadcast attaches handler to every inbound datagram whose destination node is the
// broadcast address. Broadcast handlers run in addition to any matching socket handler.
func (r *Router) RegisterBroadcast(handler NodeHandler) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.broadcast != nil {
		return errors.New("ipx: broadcast handler already registered")
	}
	r.broadcast = handler
	return nil
}

// UnregisterBroadcast removes the broadcast handler. Idempotent.
func (r *Router) UnregisterBroadcast() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.broadcast = nil
}

// AddPort attaches a port and installs the inbound delivery callback that drives Inbound.
func (r *Router) AddPort(p Port) {
	r.mu.Lock()
	r.ports = append(r.ports, p)
	r.mu.Unlock()
	p.SetDeliveryCallback(r.Inbound)
}

// Send fills SrcNet/SrcNode on d (when zero) and writes it through the first attached port. On
// Ethernet the IPX node is the MAC, so the destination MAC is d.DstNode (broadcast node →
// broadcast MAC). Source fields already set are respected (forwarding).
func (r *Router) Send(d *protocol.Datagram) error {
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
	return port.Send(d.DstNode, d)
}

// Inbound is the port-side delivery callback. It enforces the addressed-to-us filter (the
// kernel filter only narrows by framing, not destination) before dispatching. Node-scoped
// handlers take precedence; broadcasts fan out to a socket handler AND the broadcast handler.
func (r *Router) Inbound(d *protocol.Datagram) {
	if !r.acceptsDest(d.DstNet, d.DstNode) {
		return
	}
	r.mu.RLock()
	nodeHandler, hasNode := r.nodes[d.DstNode]
	socketHandler, hasSocket := r.sockets[d.DstSock]
	broadcast := r.broadcast
	r.mu.RUnlock()

	if hasNode {
		nodeHandler.HandleNodeDatagram(d)
		return
	}
	isBroadcast := d.DstNode == BroadcastNode
	if hasSocket {
		socketHandler.HandleDatagram(d)
	}
	if isBroadcast && broadcast != nil {
		broadcast.HandleNodeDatagram(d)
	}
}

// acceptsDest reports whether (network, node) matches the router's identity or is a broadcast.
// Broadcast-node datagrams are accepted regardless of destination network: we serve every
// segment the port hears, and a client that has learned a real wire network number (e.g. from
// a coexisting NetWare server's RIP/SAP) addresses its broadcasts to that net — a SAP
// GetNearestServer so addressed must still reach the advertiser. For unicast, network 0
// ("local segment, unknown") is accepted alongside our own network, and the NetWare internal
// address (internal-net:InternalNode) is accepted when an internal network is configured.
func (r *Router) acceptsDest(network [4]byte, node [6]byte) bool {
	r.mu.RLock()
	ours := r.network
	myNode := r.node
	internal := r.internalNet
	_, claimed := r.nodes[node]
	r.mu.RUnlock()

	if node == BroadcastNode {
		return true
	}
	if !isZero4(internal) && network == internal && node == InternalNode {
		return true
	}
	if !isZero4(network) && network != ours {
		return false
	}
	return node == myNode || claimed
}

func isZero4(b [4]byte) bool { return b == [4]byte{} }
func isZero6(b [6]byte) bool { return b == [6]byte{} }

// compile-time assertion: the concrete core IPX port satisfies the mini-router's Port.
var _ Port = (*portipx.Port)(nil)
