// Package netbeui is the NetBEUI name-dispatch mini-router. Like the IPX mini-router it is a
// peer of the AppleTalk router, not a member of it (§3): NetBEUI (NBF) is a NetBIOS transport
// with its own address space (16-byte NetBIOS names) and its own inbound dispatch, so it does
// not ride the DDP router. It is fed by the M3 NetBEUI frame port via a delivery callback and
// sends through it.
//
// Scope is name dispatch: route a decoded non-session NBF frame to the handler registered for
// its destination NetBIOS name, with a broadcast handler for the name-claim / datagram-group
// frames addressed to no single registered name. Session-command frames (Command 0x14–0x1F,
// which the port delivers out of LLC Type-2 I-frames) are dispatched to a session handler if
// one is registered, else dropped. The LLC Type-2 connection machine itself (SABME/UA/RR/
// I-frame/DISC) lives in the port (core/port/netbeui); by the time a session command reaches
// this router the connection state has already been handled.
//
// Ring: CORE (stdlib only). Modelled on the IPX mini-router; uses core/log (no netlog).
package netbeui

import (
	"errors"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/core/log"
	portnetbeui "github.com/ObsoleteMadness/ClassicStack/core/port/netbeui"
	nbf "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbeui"
)

// Port is the NetBEUI frame port the mini-router drives: it installs an inbound delivery
// callback and sends UI frames (unicast to a MAC, or broadcast). The core NetBEUI port
// (core/port/netbeui) satisfies it; the callback type is the port package's so satisfaction
// is exact.
type Port interface {
	SetDeliveryCallback(cb portnetbeui.DeliveryCallback)
	Send(dstMAC [6]byte, frame *nbf.Frame) error
	SendBroadcast(frame *nbf.Frame) error
}

// NameHandler receives a decoded non-session NBF frame addressed to a registered NetBIOS name,
// with the Ethernet source/destination MACs (the source MAC is the reply address).
type NameHandler interface {
	HandleFrame(srcMAC, dstMAC [6]byte, frame *nbf.Frame)
}

// SessionHandler receives session-command NBF frames (Command 0x14–0x1F) that the port has
// already extracted from LLC Type-2 I-frames. The NBF session lifecycle (SESSION_INITIALIZE →
// SESSION_CONFIRM, DATA_*, SESSION_END) lives in the NetBIOS service; until a handler is
// registered, session frames are dropped.
type SessionHandler interface {
	HandleSessionFrame(srcMAC, dstMAC [6]byte, frame *nbf.Frame)
}

// Router dispatches inbound NBF frames to per-name handlers, a broadcast handler, and a
// session handler. Safe for concurrent use.
type Router struct {
	logger    log.Logger
	mu        sync.RWMutex
	names     map[[16]byte]NameHandler
	broadcast NameHandler
	session   SessionHandler
	ports     []Port
}

// NewRouter returns an empty NetBEUI mini-router.
func NewRouter(logger log.Logger) *Router {
	return &Router{logger: logger, names: make(map[[16]byte]NameHandler)}
}

// RegisterName attaches handler to inbound non-session frames whose destination NetBIOS name
// matches. Returns an error when the name is already registered.
func (r *Router) RegisterName(name [16]byte, handler NameHandler) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.names[name]; exists {
		return errors.New("netbeui: name already registered")
	}
	r.names[name] = handler
	return nil
}

// UnregisterName removes a RegisterName binding. Idempotent.
func (r *Router) UnregisterName(name [16]byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.names, name)
}

// RegisterBroadcast attaches handler to non-session frames addressed to no registered name
// (name-claim queries, group datagrams). Returns an error when one is already registered.
func (r *Router) RegisterBroadcast(handler NameHandler) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.broadcast != nil {
		return errors.New("netbeui: broadcast handler already registered")
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

// RegisterSession installs the session-command handler (the M7 LLC Type-2 machine). Returns an
// error when one is already registered.
func (r *Router) RegisterSession(handler SessionHandler) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.session != nil {
		return errors.New("netbeui: session handler already registered")
	}
	r.session = handler
	return nil
}

// UnregisterSession removes the session handler. Idempotent.
func (r *Router) UnregisterSession() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.session = nil
}

// AddPort attaches a port and installs the inbound delivery callback that drives Inbound.
func (r *Router) AddPort(p Port) {
	r.mu.Lock()
	r.ports = append(r.ports, p)
	r.mu.Unlock()
	p.SetDeliveryCallback(r.Inbound)
}

// Send writes a UI frame to dstMAC through the first attached port.
func (r *Router) Send(dstMAC [6]byte, frame *nbf.Frame) error {
	r.mu.RLock()
	if len(r.ports) == 0 {
		r.mu.RUnlock()
		return errors.New("netbeui: no ports attached")
	}
	port := r.ports[0]
	r.mu.RUnlock()
	return port.Send(dstMAC, frame)
}

// SendBroadcast writes a UI frame to the NetBIOS multicast address through the first port.
func (r *Router) SendBroadcast(frame *nbf.Frame) error {
	r.mu.RLock()
	if len(r.ports) == 0 {
		r.mu.RUnlock()
		return errors.New("netbeui: no ports attached")
	}
	port := r.ports[0]
	r.mu.RUnlock()
	return port.SendBroadcast(frame)
}

// Inbound is the port-side delivery callback. Session-command frames go to the session handler
// (M7); a non-session frame goes to the handler for its destination name, else to the
// broadcast handler.
func (r *Router) Inbound(srcMAC, dstMAC [6]byte, frame *nbf.Frame) {
	if nbf.IsSessionCommand(frame.Command) {
		r.mu.RLock()
		session := r.session
		r.mu.RUnlock()
		if session != nil {
			session.HandleSessionFrame(srcMAC, dstMAC, frame)
		}
		return
	}
	r.mu.RLock()
	handler, ok := r.names[frame.DestinationName]
	broadcast := r.broadcast
	r.mu.RUnlock()
	if ok {
		handler.HandleFrame(srcMAC, dstMAC, frame)
		return
	}
	if broadcast != nil {
		broadcast.HandleFrame(srcMAC, dstMAC, frame)
	}
}

// compile-time assertion: the concrete core NetBEUI port satisfies the mini-router's Port.
var _ Port = (*portnetbeui.Port)(nil)
