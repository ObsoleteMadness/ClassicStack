package zip

import (
	"bytes"
	"context"
	"sync"

	"github.com/pgodw/omnitalk/protocol/ddp"
	"github.com/pgodw/omnitalk/protocol/nbp"

	"github.com/pgodw/omnitalk/netlog"
	"github.com/pgodw/omnitalk/port"
	"github.com/pgodw/omnitalk/service"
)

// NBP wire-format constants are re-exported from protocol/nbp so the
// existing zip.NBPSASSocket / zip.NBPDDPType call sites stay valid.
const (
	NBPSASSocket = nbp.SASSocket
	NBPDDPType   = nbp.DDPType

	nbpCtrlBrRq     = nbp.CtrlBrRq
	nbpCtrlLkUp     = nbp.CtrlLkUp
	nbpCtrlLkUpRply = nbp.CtrlLkUpRply
	nbpCtrlFwd      = nbp.CtrlFwd
)

type NBPRegisteredName struct {
	Object []byte
	Type   []byte
	Zone   []byte
	Socket uint8
}

type NameInformationService struct {
	ch chan struct {
		d ddp.Datagram
		p port.Port
	}
	stop   chan struct{}
	wg     sync.WaitGroup
	nameMu sync.RWMutex
	names  []NBPRegisteredName
}

// RegisterName registers a name so the router responds to NBP LkUp queries for
// it.  Call this before starting the router so Macs can discover the service.
func (s *NameInformationService) RegisterName(obj, typ, zone []byte, socket uint8) {
	s.nameMu.Lock()
	defer s.nameMu.Unlock()
	for i, n := range s.names {
		if bytes.EqualFold(n.Object, obj) && bytes.EqualFold(n.Type, typ) && bytes.EqualFold(n.Zone, zone) {
			s.names[i].Socket = socket
			return
		}
	}
	s.names = append(s.names, NBPRegisteredName{
		Object: append([]byte(nil), obj...),
		Type:   append([]byte(nil), typ...),
		Zone:   append([]byte(nil), zone...),
		Socket: socket,
	})
}

// UnregisterName removes a previously registered name.
func (s *NameInformationService) UnregisterName(obj, typ, zone []byte) {
	s.nameMu.Lock()
	defer s.nameMu.Unlock()
	for i, n := range s.names {
		if bytes.EqualFold(n.Object, obj) && bytes.EqualFold(n.Type, typ) && bytes.EqualFold(n.Zone, zone) {
			s.names = append(s.names[:i], s.names[i+1:]...)
			return
		}
	}
}

// nbpMatch / nbpZoneMatch / buildLkUpRply now live in protocol/nbp.
// We keep tiny shims so the rest of this file reads naturally.
func nbpMatch(pattern, name []byte) bool     { return nbp.NameMatch(pattern, name) }
func nbpZoneMatch(pattern, zone []byte) bool { return nbp.ZoneMatch(pattern, zone) }
func buildLkUpRply(nbpID byte, network uint16, node, socket uint8, obj, typ, zone []byte) []byte {
	return nbp.BuildLkUpRply(nbpID, network, node, socket, obj, typ, zone)
}

func NewNameInformationService() *NameInformationService {
	return &NameInformationService{
		ch: make(chan struct {
			d ddp.Datagram
			p port.Port
		}, 256),
		stop: make(chan struct{}),
	}
}

func (s *NameInformationService) Socket() uint8 { return NBPSASSocket }
func (s *NameInformationService) Stop() error {
	close(s.stop)
	s.wg.Wait()
	return nil
}
func (s *NameInformationService) Inbound(d ddp.Datagram, p port.Port) {
	select {
	case s.ch <- struct {
		d ddp.Datagram
		p port.Port
	}{d: d, p: p}:
	default:
	}
}

func (s *NameInformationService) Start(ctx context.Context, r service.Router) error {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.stop:
				return
			case item := <-s.ch:
				s.handlePacket(item.d, item.p, r)
			}
		}
	}()
	return nil
}

func (s *NameInformationService) handlePacket(d ddp.Datagram, p port.Port, r service.Router) {
	if d.DDPType != NBPDDPType {
		return
	}
	pkt, err := nbp.ParsePacket(d.Data)
	if err != nil || pkt.TupleCount != 1 {
		return
	}
	switch pkt.Function {
	case nbpCtrlBrRq, nbpCtrlFwd, nbpCtrlLkUp:
	default:
		return
	}

	replyNet := pkt.Tuple.Network
	if replyNet == 0 {
		replyNet = p.Network()
	}

	switch pkt.Function {
	case nbpCtrlBrRq:
		s.handleBrRq(d, p, r, pkt.Tuple.Object, pkt.Tuple.Type, pkt.Tuple.Zone, replyNet)
	case nbpCtrlFwd:
		s.handleFwd(d, p, r, pkt.Tuple.Object, pkt.Tuple.Type, pkt.Tuple.Zone, replyNet)
	case nbpCtrlLkUp:
		s.handleLkUp(d, p, r, pkt.Tuple.Object, pkt.Tuple.Type, pkt.Tuple.Zone, replyNet)
	}
}

func (s *NameInformationService) buildCommonPayload(d ddp.Datagram, zone []byte, replyNet uint16) ([]byte, []byte) {
	objLen := int(d.Data[7])
	typLen := int(d.Data[8+objLen])

	common := make([]byte, 0, len(d.Data)+2)
	common = append(common, d.Data[1])
	common = append(common, byte(replyNet>>8), byte(replyNet))
	common = append(common, d.Data[4:8]...)
	common = append(common, d.Data[8:8+objLen]...)
	common = append(common, d.Data[8+objLen])
	common = append(common, d.Data[9+objLen:9+objLen+typLen]...)
	common = append(common, byte(len(zone)))
	common = append(common, zone...)

	lkup := append([]byte{(nbpCtrlLkUp << 4) | 1}, common...)
	fwd := append([]byte{(nbpCtrlFwd << 4) | 1}, common...)
	return lkup, fwd
}

func (s *NameInformationService) handleBrRq(d ddp.Datagram, p port.Port, r service.Router, obj, typ, zone []byte, replyNet uint16) {
	netlog.Debug("NBP BrRq on %s: obj=%q type=%q zone=%q reply=%d.%d.%d",
		p.ShortString(), obj, typ, zone, replyNet, d.Data[4], d.Data[5])

	nbpID := d.Data[1]
	replyNode := d.Data[4]
	replySock := d.Data[5]

	s.nameMu.RLock()
	for _, n := range s.names {
		if nbpMatch(obj, n.Object) && nbpMatch(typ, n.Type) && nbpZoneMatch(zone, n.Zone) {
			rply := buildLkUpRply(nbpID, p.Network(), p.Node(), n.Socket, n.Object, n.Type, n.Zone)
			netlog.Debug("NBP BrRq: replying for registered name %q:%q@%q socket=%d", n.Object, n.Type, n.Zone, n.Socket)
			_ = r.Route(ddp.Datagram{
				DestinationNetwork: replyNet,
				DestinationNode:    replyNode,
				DestinationSocket:  replySock,
				SourceSocket:       NBPSASSocket,
				DDPType:            NBPDDPType,
				Data:               rply,
			}, true)
		}
	}
	s.nameMu.RUnlock()

	routeZone := zone
	if string(routeZone) == "*" {
		if p.ExtendedNetwork() {
			netlog.Debug("NBP BrRq: extended port with zone=* — dropping")
			return
		}
		if p.Network() != 0 {
			entry, _ := r.RoutingGetByNetwork(p.Network())
			if entry != nil {
				zones, _ := r.ZonesInNetworkRange(entry.NetworkMin, nil)
				if len(zones) == 1 {
					routeZone = zones[0]
					netlog.Debug("NBP BrRq: substituted zone=* with %q", routeZone)
				}
			}
		}
	}

	lkup, fwd := s.buildCommonPayload(d, zone, replyNet)

	if string(routeZone) == "*" {
		netlog.Debug("NBP BrRq: zone=* unresolved — broadcasting on %s", p.ShortString())
		p.Broadcast(ddp.Datagram{
			DestinationNetwork: 0, SourceNetwork: p.Network(), DestinationNode: 0xFF, SourceNode: p.Node(),
			DestinationSocket: NBPSASSocket, SourceSocket: NBPSASSocket, DDPType: NBPDDPType, Data: lkup,
		})
	} else {
		zone = routeZone
		nets := r.NetworksInZone(zone)
		netlog.Debug("NBP BrRq: routing zone=%q — %d networks", zone, len(nets))
		seen := map[port.Port]struct{}{}
		for _, n := range nets {
			entry, _ := r.RoutingGetByNetwork(n)
			if entry == nil {
				continue
			}
			if _, ok := seen[entry.Port]; ok {
				continue
			}
			seen[entry.Port] = struct{}{}
			if entry.Distance == 0 {
				netlog.Debug("NBP BrRq: sending LkUp to %s (network %d)", entry.Port.ShortString(), n)
				entry.Port.Multicast(zone, ddp.Datagram{
					DestinationNetwork: 0, SourceNetwork: entry.Port.Network(), DestinationNode: 0xFF, SourceNode: entry.Port.Node(),
					DestinationSocket: NBPSASSocket, SourceSocket: NBPSASSocket, DDPType: NBPDDPType, Data: lkup,
				})
			} else {
				netlog.Debug("NBP BrRq: routing Fwd to network %d (distance %d)", entry.NetworkMin, entry.Distance)
				_ = r.Route(ddp.Datagram{
					DestinationNetwork: entry.NetworkMin, DestinationNode: 0x00, DestinationSocket: NBPSASSocket,
					SourceSocket: NBPSASSocket, DDPType: NBPDDPType, Data: fwd,
				}, true)
			}
		}
	}
}

func (s *NameInformationService) handleFwd(d ddp.Datagram, p port.Port, r service.Router, obj, typ, zone []byte, replyNet uint16) {
	entry, _ := r.RoutingGetByNetwork(d.DestinationNetwork)
	if entry == nil || entry.Distance != 0 {
		return
	}

	lkup, _ := s.buildCommonPayload(d, zone, replyNet)

	entry.Port.Multicast(zone, ddp.Datagram{
		DestinationNetwork: 0, SourceNetwork: entry.Port.Network(), DestinationNode: 0xFF, SourceNode: entry.Port.Node(),
		DestinationSocket: NBPSASSocket, SourceSocket: NBPSASSocket, DDPType: NBPDDPType, Data: lkup,
	})
}

func (s *NameInformationService) handleLkUp(d ddp.Datagram, p port.Port, r service.Router, obj, typ, zone []byte, replyNet uint16) {
	replyNode := d.Data[4]
	replySock := d.Data[5]
	nbpID := d.Data[1]

	netlog.Debug("NBP LkUp on %s: obj=%q type=%q zone=%q reply=%d.%d.%d",
		p.ShortString(), obj, typ, zone, replyNet, replyNode, replySock)

	s.nameMu.RLock()
	var matches []NBPRegisteredName
	for _, n := range s.names {
		if nbpMatch(obj, n.Object) && nbpMatch(typ, n.Type) && nbpZoneMatch(zone, n.Zone) {
			matches = append(matches, n)
		}
	}
	s.nameMu.RUnlock()

	for _, m := range matches {
		rply := buildLkUpRply(nbpID, p.Network(), p.Node(), m.Socket, m.Object, m.Type, m.Zone)
		netlog.Debug("NBP LkUp: replying with %q:%q@%q socket=%d", m.Object, m.Type, m.Zone, m.Socket)
		_ = r.Route(ddp.Datagram{
			DestinationNetwork: replyNet,
			DestinationNode:    replyNode,
			DestinationSocket:  replySock,
			SourceSocket:       NBPSASSocket,
			DDPType:            NBPDDPType,
			Data:               rply,
		}, true)
	}
}
