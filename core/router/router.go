package router

import (
	"context"
	"errors"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
)

// RoutedPort is the data half a routed port exposes to the router (the lifecycle half is
// component.Component). A port is RoutedPort + Component. The router never knows whether the
// port's datagrams came from a kernel socket or a Framing(FrameLink) (§2).
type RoutedPort interface {
	component.Component
	Unicast(network uint16, node uint8, d ddp.Datagram)
	Broadcast(d ddp.Datagram)
	Multicast(zoneName []byte, d ddp.Datagram)
	Network() uint16
	Node() uint8
	NetworkMin() uint16
	NetworkMax() uint16
}

// extendedReporter is an optional RoutedPort capability: a port reports whether it serves an
// extended (multi-network) range. Ports that don't implement it are treated as extended iff
// their advertised range spans more than one network.
type extendedReporter interface{ ExtendedNetwork() bool }

// rangeSetter is an optional RoutedPort capability used by RTMP: a port that has not yet
// claimed a network range can adopt the range learned from a neighbour's RTMP data.
type rangeSetter interface {
	SetNetworkRange(networkMin, networkMax uint16) error
}

// Router is a Component. Attach/Detach are membership events: Detach withdraws the port's
// directly-connected routes IMMEDIATELY (no aging delay, §3). Inbound is the port→router hook.
type Router interface {
	component.Component
	Attach(p RoutedPort) error
	Detach(p RoutedPort) error
	Inbound(d ddp.Datagram, from RoutedPort)
}

// Service is a DDP service riding the router (RTMP, ZIP, AEP, …). It is a Component for
// lifecycle, plus an Inbound hook for the datagrams the router dispatches to its socket(s),
// and a Socket() declaration of the static socket it listens on (0 = none, e.g. a timer-only
// aging service). The router is supplied at Start so the service can reply and consult tables.
type Service interface {
	component.Component
	Socket() uint8
	Inbound(d ddp.Datagram, from RoutedPort)
}

// ServiceRouter is the router surface the DDP services (RTMP/ZIP/AEP) consume: reply/forward,
// the routing and zone tables, the attached-port list, and the aging tick. Defined as an
// interface so a service can be unit-tested against a fake router. *RouterImpl satisfies it.
type ServiceRouter interface {
	// Reply sends a service response back to the originator of d.
	Reply(d ddp.Datagram, from RoutedPort, ddpType uint8, data []byte)
	// Route forwards a datagram toward its destination network.
	Route(d ddp.Datagram, originating bool) error
	// RoutingTable is the router's routing table (route lookup/consider/mark-bad/age/snapshot).
	RoutingTable() *RoutingTable
	// Zones is the router's zone information table.
	Zones() *ZoneInformationTable
	// Ports returns the currently attached ports (for the periodic sending loops).
	Ports() []RoutedPort
}

// Name is the component name for the AppleTalk router.
const Name = "Router"

// RouterImpl is the real AppleTalk router: it owns the routing and zone tables, dispatches
// inbound datagrams to services by socket or forwards them to other ports, and drives
// event-driven port membership (Attach/Detach).
type RouterImpl struct {
	mu      sync.RWMutex
	running bool
	ports   map[string]RoutedPort
	socket  map[uint8]Service
	logger  log.Logger

	rt  *RoutingTable
	zit *ZoneInformationTable

	observer func(ddp.Datagram, RoutedPort)
}

// New builds the real AppleTalk router with empty tables.
func New(logger log.Logger) *RouterImpl {
	zit := NewZoneInformationTable()
	return &RouterImpl{
		ports:  make(map[string]RoutedPort),
		socket: make(map[uint8]Service),
		logger: logger,
		zit:    zit,
		rt:     NewRoutingTable(zit, logger),
	}
}

// Name returns the component name.
func (r *RouterImpl) Name() string { return Name }

// RoutingTable returns the router's routing table (for the RTMP/ZIP services and diagnostics).
func (r *RouterImpl) RoutingTable() *RoutingTable { return r.rt }

// Zones returns the router's zone information table (for the ZIP service and diagnostics).
func (r *RouterImpl) Zones() *ZoneInformationTable { return r.zit }

// SetObserver installs a callback invoked for every datagram delivered locally (after DDP
// decode, before service dispatch). Pass nil to remove. Used by diagnostics/capture.
func (r *RouterImpl) SetObserver(fn func(ddp.Datagram, RoutedPort)) {
	r.mu.Lock()
	r.observer = fn
	r.mu.Unlock()
}

// RegisterService records the socket a service listens on so Inbound can dispatch to it. A
// service with Socket()==0 (e.g. the RTMP aging timer) registers no socket. Called by the
// composition layer as it adds services to the router.
func (r *RouterImpl) RegisterService(s Service) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if sock := s.Socket(); sock != 0 {
		r.socket[sock] = s
	}
}

// UnregisterService drops a service's socket dispatch entry.
func (r *RouterImpl) UnregisterService(s Service) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for sock, svc := range r.socket {
		if svc == s {
			delete(r.socket, sock)
		}
	}
}

// Ports returns a snapshot of the currently attached ports (for the RTMP/ZIP sending loops).
func (r *RouterImpl) Ports() []RoutedPort {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]RoutedPort, 0, len(r.ports))
	for _, p := range r.ports {
		out = append(out, p)
	}
	return out
}

// Start brings the router up. Idempotent (§3).
func (r *RouterImpl) Start(ctx context.Context) error {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		return nil
	}
	r.running = true
	r.logf("router started")
	return nil
}

// Stop brings the router down. Safe after a failed/partial Start (§3).
func (r *RouterImpl) Stop(ctx context.Context) error {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.running {
		return nil
	}
	r.running = false
	r.logf("router stopped")
	return nil
}

// Attach adds a routed port and installs its directly-connected route (if it has already
// claimed a network range). RTMP advertisement and ZIP queries pick the port up from there.
func (r *RouterImpl) Attach(p RoutedPort) error {
	r.mu.Lock()
	if !r.running {
		r.mu.Unlock()
		return errors.New("router: cannot attach port to stopped router")
	}
	name := p.Name()
	if _, ok := r.ports[name]; ok {
		r.mu.Unlock()
		return errors.New("router: port already attached")
	}
	r.ports[name] = p
	r.mu.Unlock()

	if nmin, nmax := p.NetworkMin(), p.NetworkMax(); nmin != 0 && nmax != 0 {
		r.rt.SetPortRange(p, nmin, nmax)
	}
	r.logf1("port attached to router", log.Str("port", name))
	return nil
}

// Detach removes a routed port and withdraws every route and zone reachable through it
// IMMEDIATELY (§3 event-driven membership — no aging delay).
func (r *RouterImpl) Detach(p RoutedPort) error {
	r.mu.Lock()
	name := p.Name()
	if _, ok := r.ports[name]; !ok {
		r.mu.Unlock()
		return errors.New("router: port not attached")
	}
	delete(r.ports, name)
	r.mu.Unlock()

	r.rt.RemoveEntriesForPort(p)
	r.logf1("port detached from router", log.Str("port", name))
	return nil
}

// Inbound is the port→router hook: it fills in the source/destination network from the rx
// port where the datagram left them zero, delivers locally-addressed datagrams to the
// destination-socket service, and forwards everything else via Route.
func (r *RouterImpl) Inbound(d ddp.Datagram, from RoutedPort) {
	if from.Network() != 0 {
		switch {
		case d.DestNetwork == 0 && d.SrcNetwork == 0:
			d.DestNetwork = from.Network()
			d.SrcNetwork = from.Network()
		case d.DestNetwork == 0:
			d.DestNetwork = from.Network()
		case d.SrcNetwork == 0:
			d.SrcNetwork = from.Network()
		}
	}

	r.mu.RLock()
	obs := r.observer
	r.mu.RUnlock()
	if obs != nil {
		obs(d, from)
	}

	if d.DestNetwork == 0 || d.DestNetwork == from.Network() {
		if d.DestNode == 0 || d.DestNode == from.Node() || d.DestNode == 0xFF {
			r.deliver(d, from)
		}
		return
	}

	entry, _ := r.rt.GetByNetwork(d.DestNetwork)
	if entry != nil && entry.Distance == 0 {
		switch {
		case d.DestNetwork == entry.Port.Network() && d.DestNode == entry.Port.Node():
			r.deliver(d, from)
			return
		case d.DestNode == 0:
			r.deliver(d, from)
			return
		case d.DestNode == 0xFF:
			r.deliver(d, from)
		}
	}
	_ = r.Route(d, false)
}

// deliver dispatches a locally-addressed datagram to the service bound to its destination
// socket, if any.
func (r *RouterImpl) deliver(d ddp.Datagram, from RoutedPort) {
	r.mu.RLock()
	svc, ok := r.socket[d.DestSocket]
	r.mu.RUnlock()
	if ok {
		svc.Inbound(d, from)
	}
}

// Route forwards a datagram toward its destination network. originating marks a datagram the
// router itself sourced (a service reply); learned-route forwarding hops the datagram and
// honours the 15-hop limit.
func (r *RouterImpl) Route(d ddp.Datagram, originating bool) error {
	if originating {
		if d.Hops != 0 {
			return errors.New("router: originated datagrams must have hop count of 0")
		}
		if d.DestNetwork == 0 {
			return errors.New("router: originated datagrams must have nonzero destination network")
		}
	}
	if d.DestNetwork == 0 || d.Hops >= 15 {
		return nil
	}
	entry, _ := r.rt.GetByNetwork(d.DestNetwork)
	if entry == nil {
		return nil
	}
	if originating {
		if entry.Port.Network() == 0 || entry.Port.Node() == 0 {
			return nil // outgoing port not yet ready (address unclaimed)
		}
		// Only fill in the source from the outgoing port if the caller left it zero. A reply
		// keeps the address the client originally sent TO as its source.
		if d.SrcNetwork == 0 {
			d.SrcNetwork = entry.Port.Network()
		}
		if d.SrcNode == 0 {
			d.SrcNode = entry.Port.Node()
		}
	} else {
		if d.SrcNode == 0 || d.SrcNode == 0xFF {
			return nil
		}
		d.Hops++
	}
	switch {
	case entry.Distance != 0:
		entry.Port.Unicast(entry.NextNetwork, entry.NextNode, d)
	case d.DestNode == 0:
		// directly connected, addressed to network only — nothing to do
	case d.DestNetwork == entry.Port.Network() && d.DestNode == entry.Port.Node():
		// addressed to the outgoing port itself — nothing to forward
	case d.DestNode == 0xFF:
		entry.Port.Broadcast(d)
	default:
		entry.Port.Unicast(d.DestNetwork, d.DestNode, d)
	}
	return nil
}

// Reply sends a service response back to the originator of d. It mirrors the source/dest of
// the request, broadcasting when the source address is non-local (a startup-range or
// unnumbered client) and otherwise routing the reply normally.
func (r *RouterImpl) Reply(d ddp.Datagram, from RoutedPort, ddpType uint8, data []byte) {
	if d.SrcNode == 0 || d.SrcNode == 0xFF {
		return
	}
	if from.Node() != 0 && (d.SrcNetwork == 0 || (d.SrcNetwork >= 0xFF00 && d.SrcNetwork <= 0xFFFE) ||
		d.SrcNetwork < from.NetworkMin() || d.SrcNetwork > from.NetworkMax()) {
		from.Broadcast(ddp.Datagram{
			Hops:        0,
			DestNetwork: 0,
			SrcNetwork:  from.Network(),
			DestNode:    0xFF,
			SrcNode:     from.Node(),
			DestSocket:  d.SrcSocket,
			SrcSocket:   d.DestSocket,
			DDPType:     ddpType,
			Data:        append([]byte(nil), data...),
		})
		return
	}
	_ = r.Route(ddp.Datagram{
		Hops:        0,
		DestNetwork: d.SrcNetwork,
		SrcNetwork:  d.DestNetwork, // reply FROM the address the client sent TO
		DestNode:    d.SrcNode,
		SrcNode:     d.DestNode,
		DestSocket:  d.SrcSocket,
		SrcSocket:   d.DestSocket,
		DDPType:     ddpType,
		Data:        append([]byte(nil), data...),
	}, true)
}

// PortIsExtended reports whether p serves an extended (multi-network) range. It honours an
// optional ExtendedNetwork() capability, else infers it from the advertised range.
func PortIsExtended(p RoutedPort) bool {
	if er, ok := p.(extendedReporter); ok {
		return er.ExtendedNetwork()
	}
	return p.NetworkMin() != p.NetworkMax()
}

// AdoptRange asks p to adopt a network range learned from an RTMP neighbour, if p supports it
// and has not already claimed one. Returns false when the port cannot adopt a range.
func AdoptRange(p RoutedPort, networkMin, networkMax uint16) bool {
	if rs, ok := p.(rangeSetter); ok {
		return rs.SetNetworkRange(networkMin, networkMax) == nil
	}
	return false
}

// logf emits one info line through the logger if configured.
func (r *RouterImpl) logf(msg string) {
	if r.logger == nil || !r.logger.Enabled(log.Info) {
		return
	}
	r.logger.Log1(log.Info, msg, log.Str("scope", Name))
}

// logf1 emits one info line with an extra field.
func (r *RouterImpl) logf1(msg string, f log.Field) {
	if r.logger == nil || !r.logger.Enabled(log.Info) {
		return
	}
	r.logger.Log2(log.Info, msg, log.Str("scope", Name), f)
}

// compile-time assertions.
var (
	_ Router              = (*RouterImpl)(nil)
	_ component.Component = (*RouterImpl)(nil)
)
