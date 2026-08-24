// Package nbp implements the AppleTalk Name Binding Protocol name-information
// service as a core router service: it owns the registered-name table and answers
// NBP BrRq / LkUp / Fwd queries on the NIS socket (socket 2, DDP type 2).
//
// Wire codec lives in core/protocol/nbp; this package is the stateful service that
// rides the router. Other DDP services (MacIP, IPXGW) register their advertised
// names here so Macs discover them via NBP lookups.
//
// Ring: CORE (stdlib only, reflection-free). The router is injected at construction;
// the service rides it as a router.Service (lifecycle + socket dispatch).
//
// Reference: spec/04-nbp.md and Inside AppleTalk, 2nd ed., chapter 7.
package nbp

import (
	"bytes"
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/nbp"
	"github.com/ObsoleteMadness/ClassicStack/core/router"
)

// Socket and DDPType re-export the NBP well-known values from the codec so call
// sites can address the service without importing the codec directly.
const (
	Socket  = nbp.SASSocket // 2
	DDPType = nbp.DDPType   // 2
)

// Name is the component/section key for the NBP name-information service.
const Name = "NBP"

// zoneWildcard is the NBP zone wildcard "*".
var zoneWildcard = []byte{nbp.ZoneWildcard}

// RegisteredName is one name this router will answer NBP lookups for: the
// object:type@zone entity plus the DDP socket the named service lives on.
// AnyObject marks a wildcard-object registration (see RegisterNameAnyObject):
// the entry matches a query for ANY object of its type, and the reply tuple
// echoes the requested object.
type RegisteredName struct {
	Object    []byte
	Type      []byte
	Zone      []byte
	Socket    uint8
	AnyObject bool
}

// NBPEntity is one resolved NBP tuple returned by Lookup: the object/type/zone
// strings and the AppleTalk address (network.node:socket) the responder gave.
type NBPEntity struct {
	Object  []byte
	Type    []byte
	Zone    []byte
	Network uint16
	Node    uint8
	Socket  uint8
}

// defaultLookupWindow bounds how long Lookup waits for LkUp-Rply replies before
// returning what it collected. NBP has no "no more replies" signal, so a requester
// always waits a fixed window (Inside AppleTalk, NBP; matches the client requester).
const defaultLookupWindow = 2 * time.Second

type item struct {
	d    ddp.Datagram
	from router.RoutedPort
}

// Service answers NBP queries against a table of registered names. It queues
// inbound datagrams and dispatches them on a worker goroutine so the router's
// read path never blocks.
type Service struct {
	rtr    router.ServiceRouter
	logger log.Logger

	nameMu sync.RWMutex
	names  []RegisteredName

	// pending holds in-flight self-originated Lookup requests keyed by NBP id, so
	// inbound LkUp-Rply datagrams (which the router dispatches here on socket 2) are
	// delivered to the waiting Lookup goroutine instead of being dropped.
	pendMu  sync.Mutex
	pending map[byte]chan NBPEntity

	mu      sync.Mutex
	running bool
	ch      chan item
	stop    chan struct{}
	wg      sync.WaitGroup

	// counters published as StatSample (§5).
	statMu  sync.Mutex
	brrq    uint64
	lkup    uint64
	fwd     uint64
	replies uint64
}

// New builds an NBP name-information service bound to the router it replies through.
func New(rtr router.ServiceRouter, logger log.Logger) *Service {
	return &Service{rtr: rtr, logger: logger, pending: make(map[byte]chan NBPEntity)}
}

// Name returns the component name.
func (s *Service) Name() string { return Name }

// Socket returns the NIS socket so the router dispatches NBP datagrams here.
func (s *Service) Socket() uint8 { return Socket }

// RegisterName registers a name so the service answers NBP queries for it. An
// existing entry with the same object/type/zone (case-insensitive) is updated
// in place. Names may be registered before or after Start.
func (s *Service) RegisterName(obj, typ, zone []byte, socket uint8) {
	s.nameMu.Lock()
	defer s.nameMu.Unlock()
	for i, n := range s.names {
		if bytes.EqualFold(n.Object, obj) && bytes.EqualFold(n.Type, typ) && bytes.EqualFold(n.Zone, zone) {
			s.names[i].Socket = socket
			return
		}
	}
	s.names = append(s.names, RegisteredName{
		Object: append([]byte(nil), obj...),
		Type:   append([]byte(nil), typ...),
		Zone:   append([]byte(nil), zone...),
		Socket: socket,
	})
}

// RegisterNameAnyObject registers a name that answers a lookup for ANY object
// of the given type: the LkUp-Rply tuple echoes the object the querier asked
// for, falling back to obj for wildcard ("=") queries. Needed by services whose
// advertised object name is client-chosen — the netboot BootServer object is
// the client's PRAM serverNum in hex, so a fixed registration cannot know it.
func (s *Service) RegisterNameAnyObject(obj, typ, zone []byte, socket uint8) {
	s.nameMu.Lock()
	defer s.nameMu.Unlock()
	for i, n := range s.names {
		if bytes.EqualFold(n.Object, obj) && bytes.EqualFold(n.Type, typ) && bytes.EqualFold(n.Zone, zone) {
			s.names[i].Socket = socket
			s.names[i].AnyObject = true
			return
		}
	}
	s.names = append(s.names, RegisteredName{
		Object:    append([]byte(nil), obj...),
		Type:      append([]byte(nil), typ...),
		Zone:      append([]byte(nil), zone...),
		Socket:    socket,
		AnyObject: true,
	})
}

// UnregisterName removes a previously registered name (case-insensitive match).
func (s *Service) UnregisterName(obj, typ, zone []byte) {
	s.nameMu.Lock()
	defer s.nameMu.Unlock()
	for i, n := range s.names {
		if bytes.EqualFold(n.Object, obj) && bytes.EqualFold(n.Type, typ) && bytes.EqualFold(n.Zone, zone) {
			s.names = append(s.names[:i], s.names[i+1:]...)
			return
		}
	}
}

// Names returns a copy of the registered-name table (diagnostics). The diagnostics
// adapter (adapter/control/diag) reads this and decodes the NVE tuple for display, so
// the management plane carries no NBP type.
func (s *Service) Names() []RegisteredName {
	s.nameMu.RLock()
	defer s.nameMu.RUnlock()
	out := make([]RegisteredName, len(s.names))
	copy(out, s.names)
	return out
}

// Lookup broadcasts a BrRq for object:type in zone and returns every LkUp-Rply tuple
// received within the default collection window. An empty (or "=") object/type is the
// name wildcard; an empty (or "*") zone is the this-zone wildcard. This is the requester
// side of NBP — used, e.g., by the MacIP gateway's startup reregistration search for
// "=:IPADDRESS@*" (spec/14-macip-gateway.md §3). Safe to call only while running.
func (s *Service) Lookup(object, typ, zone []byte) []NBPEntity {
	return s.LookupTimeout(object, typ, zone, defaultLookupWindow)
}

// LookupTimeout is Lookup with a caller-chosen collection window (a window ≤ 0 uses the
// default). It registers a pending waiter keyed by a fresh NBP id, broadcasts the BrRq on
// every attached port, then returns the de-duplicated replies collected before the window
// elapses or the service stops.
func (s *Service) LookupTimeout(object, typ, zone []byte, window time.Duration) []NBPEntity {
	if window <= 0 {
		window = defaultLookupWindow
	}
	obj := wildcardOrName(object, nbp.NameWildcard)
	tp := wildcardOrName(typ, nbp.NameWildcard)
	zn := wildcardOrName(zone, nbp.ZoneWildcard)

	s.mu.Lock()
	running := s.running
	stop := s.stop
	s.mu.Unlock()
	if !running {
		return nil
	}

	id := nbpID()
	rply := make(chan NBPEntity, 64)
	s.pendMu.Lock()
	// A prior waiter under the same id (id collisions are possible) is superseded; its
	// goroutine will simply time out. Overwrite so late replies reach the newest waiter.
	s.pending[id] = rply
	s.pendMu.Unlock()
	defer func() {
		s.pendMu.Lock()
		if s.pending[id] == rply {
			delete(s.pending, id)
		}
		s.pendMu.Unlock()
	}()

	// Broadcast the BrRq on every attached port; the local router turns it into LkUps
	// across the zones, and matching responders reply on socket 2 back to us.
	for _, p := range s.rtr.Ports() {
		pkt := nbp.BuildLkUp(nbp.CtrlBrRq, id, p.Network(), p.Node(), Socket, obj, tp, zn)
		p.Broadcast(ddp.Datagram{
			DestNetwork: 0, SrcNetwork: p.Network(), DestNode: 0xFF, SrcNode: p.Node(),
			DestSocket: Socket, SrcSocket: Socket, DDPType: DDPType, Data: pkt,
		})
	}
	s.bump(&s.brrq)

	var out []NBPEntity
	seen := map[string]bool{}
	deadline := time.After(window)
	for {
		select {
		case ent := <-rply:
			key := string(ent.Object) + ":" + string(ent.Type) + ":" + string(ent.Zone)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, ent)
		case <-deadline:
			return out
		case <-stop:
			return out
		}
	}
}

// wildcardOrName returns the single wildcard byte for an empty/wildcard input, else the
// input bytes copied.
func wildcardOrName(b []byte, wildcard byte) []byte {
	if len(b) == 0 || (len(b) == 1 && (b[0] == nbp.NameWildcard || b[0] == nbp.ZoneWildcard)) {
		return []byte{wildcard}
	}
	return append([]byte(nil), b...)
}

// nbpIDCounter backs nbpID: a process-wide monotonic counter so CONCURRENT lookups get
// distinct NBP ids. The gateway runs several lookups at once (the pre-assign duplicate
// probe, the periodic Confirm loop, the startup reregistration search), and the pending-
// waiter map is keyed by this id — a time-derived id (the low byte of UnixNano) collided
// between simultaneous lookups, so one lookup's reply was delivered to another's channel
// (or a superseded, deleted one) and the first timed out. That intermittently made the
// pre-assign probe miss a live duplicate and hand out an in-use address. A rolling counter
// gives 256 distinct ids before wrap, far more than the handful ever in flight.
var nbpIDCounter atomic.Uint32

// nbpID returns the next per-lookup NBP id byte (monotonic, wraps at 256).
func nbpID() byte { return byte(nbpIDCounter.Add(1)) }

// Start launches the responder goroutine. Idempotent (§3).
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return nil
	}
	s.running = true
	s.ch = make(chan item, 256)
	s.stop = make(chan struct{})
	s.wg.Add(1)
	go s.run(ctx, s.ch, s.stop)
	return nil
}

// Stop shuts the responder down. Safe after a partial Start (§3) and idempotent.
func (s *Service) Stop(ctx context.Context) error {
	_ = ctx
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	close(s.stop)
	s.mu.Unlock()
	s.wg.Wait()
	return nil
}

// Inbound queues a datagram for the responder; a full queue drops (best-effort).
func (s *Service) Inbound(d ddp.Datagram, from router.RoutedPort) {
	s.mu.Lock()
	ch := s.ch
	running := s.running
	s.mu.Unlock()
	if !running {
		return
	}
	select {
	case ch <- item{d: d, from: from}:
	default:
	}
}

// Stats publishes NBP query/reply counters (§5).
func (s *Service) Stats() component.Stats {
	s.statMu.Lock()
	defer s.statMu.Unlock()
	s.nameMu.RLock()
	registered := uint64(len(s.names))
	s.nameMu.RUnlock()
	return component.Stats{
		Counters: map[string]uint64{
			"brrq":    s.brrq,
			"lkup":    s.lkup,
			"fwd":     s.fwd,
			"replies": s.replies,
		},
		Gauges: map[string]float64{
			"registered_names": float64(registered),
		},
	}
}

func (s *Service) run(ctx context.Context, ch chan item, stop chan struct{}) {
	defer s.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case it := <-ch:
			s.handlePacket(it.d, it.from)
		}
	}
}

func (s *Service) handlePacket(d ddp.Datagram, from router.RoutedPort) {
	if d.DDPType != DDPType {
		return
	}
	pkt, err := nbp.ParsePacket(d.Data)
	if err != nil || pkt.TupleCount != 1 {
		return
	}

	replyNet := pkt.Tuple.Network
	if replyNet == 0 {
		replyNet = from.Network()
	}

	switch pkt.Function {
	case nbp.CtrlBrRq:
		s.bump(&s.brrq)
		s.handleBrRq(d, from, pkt.Tuple.Object, pkt.Tuple.Type, pkt.Tuple.Zone, replyNet)
	case nbp.CtrlFwd:
		s.bump(&s.fwd)
		s.handleFwd(d, from, pkt.Tuple.Zone, replyNet)
	case nbp.CtrlLkUp:
		s.bump(&s.lkup)
		s.handleLkUp(d, from, pkt.Tuple.Object, pkt.Tuple.Type, pkt.Tuple.Zone, replyNet)
	case nbp.CtrlLkUpRply:
		// Reply to one of our own self-originated Lookups (§Lookup). Deliver it to the
		// waiting goroutine keyed by NBP id; drop it if no lookup is pending.
		s.deliverReply(pkt)
	}
}

// deliverReply hands an inbound LkUp-Rply tuple to the pending Lookup waiter registered
// under its NBP id, if any. Non-blocking: a full waiter channel drops the extra reply
// (the collection window is best-effort).
func (s *Service) deliverReply(pkt nbp.Packet) {
	s.pendMu.Lock()
	ch := s.pending[pkt.NBPID]
	s.pendMu.Unlock()
	if ch == nil {
		return
	}
	ent := NBPEntity{
		Object:  append([]byte(nil), pkt.Tuple.Object...),
		Type:    append([]byte(nil), pkt.Tuple.Type...),
		Zone:    append([]byte(nil), pkt.Tuple.Zone...),
		Network: pkt.Tuple.Network,
		Node:    pkt.Tuple.Node,
		Socket:  pkt.Tuple.Socket,
	}
	select {
	case ch <- ent:
	default:
	}
}

// buildCommonPayload reconstructs the NBP tuple body (with the resolved reply
// network and zone) for re-broadcast as a LkUp or Fwd. Returns (lkup, fwd).
func (s *Service) buildCommonPayload(d ddp.Datagram, zone []byte, replyNet uint16) ([]byte, []byte) {
	objLen := int(d.Data[7])
	typLen := int(d.Data[8+objLen])

	common := make([]byte, 0, len(d.Data)+2)
	common = append(common, d.Data[1]) // NBPID
	common = append(common, byte(replyNet>>8), byte(replyNet))
	common = append(common, d.Data[4:8]...) // node, socket, enumerator, objLen
	common = append(common, d.Data[8:8+objLen]...)
	common = append(common, d.Data[8+objLen]) // typLen
	common = append(common, d.Data[9+objLen:9+objLen+typLen]...)
	common = append(common, byte(len(zone)))
	common = append(common, zone...)

	lkup := append([]byte{(nbp.CtrlLkUp << 4) | 1}, common...)
	fwd := append([]byte{(nbp.CtrlFwd << 4) | 1}, common...)
	return lkup, fwd
}

func (s *Service) handleBrRq(d ddp.Datagram, from router.RoutedPort, obj, typ, zone []byte, replyNet uint16) {
	nbpID := d.Data[1]
	replyNode := d.Data[4]
	replySock := d.Data[5]

	// Answer for any locally-registered name that matches.
	s.replyMatches(obj, typ, zone, nbpID, from, replyNet, replyNode, replySock)

	// Resolve a zone=* request to the rx port's single zone where possible, so the
	// lookup can be routed to a specific zone rather than blindly broadcast.
	routeZone := zone
	if bytes.Equal(routeZone, zoneWildcard) {
		if router.PortIsExtended(from) {
			return // extended port with zone=* — drop (legacy behaviour)
		}
		if from.Network() != 0 {
			if entry, _ := s.rtr.RoutingTable().GetByNetwork(from.Network()); entry != nil {
				zones, _ := s.rtr.Zones().ZonesInNetworkRange(entry.NetworkMin, nil)
				if len(zones) == 1 {
					routeZone = zones[0]
				}
			}
		}
	}

	lkup, fwd := s.buildCommonPayload(d, routeZone, replyNet)

	if bytes.Equal(routeZone, zoneWildcard) {
		// Unresolved zone=* — broadcast the lookup on the receiving port only.
		from.Broadcast(ddp.Datagram{
			DestNetwork: 0, SrcNetwork: from.Network(), DestNode: 0xFF, SrcNode: from.Node(),
			DestSocket: Socket, SrcSocket: Socket, DDPType: DDPType, Data: lkup,
		})
		return
	}

	s.routeToZone(routeZone, lkup, fwd)
}

// routeToZone delivers a LkUp to every directly-connected port serving the zone
// (multicast) and a Fwd toward each remote network in the zone.
func (s *Service) routeToZone(zone, lkup, fwd []byte) {
	nets := s.rtr.Zones().NetworksInZone(zone)
	seen := map[string]struct{}{}
	for _, n := range nets {
		entry, _ := s.rtr.RoutingTable().GetByNetwork(n)
		if entry == nil || entry.Port == nil {
			continue
		}
		if _, ok := seen[entry.Port.Name()]; ok {
			continue
		}
		seen[entry.Port.Name()] = struct{}{}
		if entry.Distance == 0 {
			entry.Port.Multicast(zone, ddp.Datagram{
				DestNetwork: 0, SrcNetwork: entry.Port.Network(), DestNode: 0xFF, SrcNode: entry.Port.Node(),
				DestSocket: Socket, SrcSocket: Socket, DDPType: DDPType, Data: lkup,
			})
		} else {
			_ = s.rtr.Route(ddp.Datagram{
				DestNetwork: entry.NetworkMin, DestNode: 0x00, DestSocket: Socket,
				SrcSocket: Socket, DDPType: DDPType, Data: fwd,
			}, true)
		}
	}
}

func (s *Service) handleFwd(d ddp.Datagram, from router.RoutedPort, zone []byte, replyNet uint16) {
	_ = from
	entry, _ := s.rtr.RoutingTable().GetByNetwork(d.DestNetwork)
	if entry == nil || entry.Distance != 0 || entry.Port == nil {
		return
	}
	lkup, _ := s.buildCommonPayload(d, zone, replyNet)
	entry.Port.Multicast(zone, ddp.Datagram{
		DestNetwork: 0, SrcNetwork: entry.Port.Network(), DestNode: 0xFF, SrcNode: entry.Port.Node(),
		DestSocket: Socket, SrcSocket: Socket, DDPType: DDPType, Data: lkup,
	})
}

func (s *Service) handleLkUp(d ddp.Datagram, from router.RoutedPort, obj, typ, zone []byte, replyNet uint16) {
	nbpID := d.Data[1]
	replyNode := d.Data[4]
	replySock := d.Data[5]
	s.replyMatches(obj, typ, zone, nbpID, from, replyNet, replyNode, replySock)
}

// replyMatches sends a LkUp-Rply for each registered name matching the query,
// addressed back to the querier (replyNet.replyNode:replySock).
func (s *Service) replyMatches(obj, typ, zone []byte, nbpID byte, from router.RoutedPort, replyNet uint16, replyNode, replySock uint8) {
	s.nameMu.RLock()
	var matches []RegisteredName
	for _, n := range s.names {
		if !nbp.NameMatch(typ, n.Type) || !nbp.ZoneMatch(zone, n.Zone) {
			continue
		}
		switch {
		case n.AnyObject:
			// Wildcard-object registration: match any object and echo the
			// requested one back (a literal query object names the entity the
			// client expects in the reply tuple); wildcard queries fall back to
			// the registered object.
			m := n
			if len(obj) > 0 && (len(obj) != 1 || obj[0] != nbp.NameWildcard) {
				m.Object = append([]byte(nil), obj...)
			}
			matches = append(matches, m)
		case nbp.NameMatch(obj, n.Object):
			matches = append(matches, n)
		}
	}
	s.nameMu.RUnlock()

	for _, m := range matches {
		rply := nbp.BuildLkUpRply(nbpID, from.Network(), from.Node(), m.Socket, m.Object, m.Type, m.Zone)
		s.bump(&s.replies)
		_ = s.rtr.Route(ddp.Datagram{
			DestNetwork: replyNet,
			DestNode:    replyNode,
			DestSocket:  replySock,
			SrcSocket:   Socket,
			DDPType:     DDPType,
			Data:        rply,
		}, true)
	}
}

func (s *Service) bump(c *uint64) {
	s.statMu.Lock()
	*c++
	s.statMu.Unlock()
}

// Dependencies declares NBP's start-order edge: the AppleTalk router must be running
// first (NBP is a DDP service on the names socket). Drops in a no-router build.
func (s *Service) Dependencies() []string { return []string{router.Name} }

// compile-time assertions.
var (
	_ router.Service      = (*Service)(nil)
	_ component.Component = (*Service)(nil)
	_ component.DependsOn = (*Service)(nil)
	_ component.Statful   = (*Service)(nil)
)
