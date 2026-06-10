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
type RegisteredName struct {
	Object []byte
	Type   []byte
	Zone   []byte
	Socket uint8
}

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
	return &Service{rtr: rtr, logger: logger}
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

// Names returns a copy of the registered-name table (diagnostics).
func (s *Service) Names() []RegisteredName {
	s.nameMu.RLock()
	defer s.nameMu.RUnlock()
	out := make([]RegisteredName, len(s.names))
	copy(out, s.names)
	return out
}

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

	lkup, fwd := s.buildCommonPayload(d, zone, replyNet)

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
		if nbp.NameMatch(obj, n.Object) && nbp.NameMatch(typ, n.Type) && nbp.ZoneMatch(zone, n.Zone) {
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

// compile-time assertions.
var (
	_ router.Service      = (*Service)(nil)
	_ component.Component = (*Service)(nil)
	_ component.Statful   = (*Service)(nil)
)
