package router

import (
	"context"
	"errors"

	"github.com/ObsoleteMadness/ClassicStack/protocol/ddp"

	"github.com/ObsoleteMadness/ClassicStack/netlog"
	"github.com/ObsoleteMadness/ClassicStack/pkg/telemetry"
	"github.com/ObsoleteMadness/ClassicStack/port"
	"github.com/ObsoleteMadness/ClassicStack/port/localtalk"
	"github.com/ObsoleteMadness/ClassicStack/service"
	"github.com/ObsoleteMadness/ClassicStack/service/aep"
	"github.com/ObsoleteMadness/ClassicStack/service/llap"
	"github.com/ObsoleteMadness/ClassicStack/service/rtmp"
	"github.com/ObsoleteMadness/ClassicStack/service/zip"
)

var framesInTotal = telemetry.NewCounter("classicstack_router_frames_in_total")

type Router struct {
	shortStr             string
	Ports                []port.Port
	Services             []service.Service
	servicesBySAS        map[uint8]service.Service
	RoutingTable         *RoutingTable
	ZoneInformationTable *ZoneInformationTable
	observer             func(ddp.Datagram, port.Port)
}

// SetObserver installs a callback that is invoked for every datagram delivered
// locally (after DDP decoding, before service dispatch). Pass nil to remove.
func (r *Router) SetObserver(fn func(ddp.Datagram, port.Port)) {
	r.observer = fn
}

func New(shortStr string, ports []port.Port, services []service.Service) *Router {
	r := &Router{
		shortStr:             shortStr,
		Ports:                ports,
		servicesBySAS:        map[uint8]service.Service{},
		ZoneInformationTable: NewZoneInformationTable(),
	}
	r.RoutingTable = NewRoutingTable(r)
	if services == nil {
		services = defaultServices()
	}
	r.Services = services
	r.bindLLAPManager()
	for _, s := range services {
		switch v := s.(type) {
		case interface{ Socket() uint8 }:
			r.servicesBySAS[v.Socket()] = s
		case *rtmp.RespondingService:
			r.servicesBySAS[rtmp.SAS] = s
		case *zip.RespondingService:
			r.servicesBySAS[zip.SAS] = s
		case *aep.Service:
			r.servicesBySAS[aep.Socket] = s
		case *zip.NameInformationService:
			r.servicesBySAS[zip.NBPSASSocket] = s
		case *rtmp.RoutingTableAgingService:
			// RoutingTableAgingService doesn't work on socket basis
		}
	}
	return r
}

func (r *Router) ShortString() string { return r.shortStr }

func defaultServices() []service.Service {
	return []service.Service{
		llap.New(),
		aep.New(),
		zip.NewNameInformationService(),
		rtmp.NewRoutingTableAgingService(),
		rtmp.NewRespondingService(),
		rtmp.NewSendingService(),
		zip.NewRespondingService(),
		zip.NewSendingService(),
	}
}

func (r *Router) bindLLAPManager() {
	var llapSvc *llap.Service
	for _, svc := range r.Services {
		if candidate, ok := svc.(*llap.Service); ok {
			llapSvc = candidate
			break
		}
	}
	if llapSvc == nil {
		return
	}
	for _, p := range r.Ports {
		if managed, ok := p.(interface{ SetLLAPLinkManager(localtalk.LinkManager) }); ok {
			managed.SetLLAPLinkManager(llapSvc)
		}
	}
}

func (r *Router) deliver(datagram ddp.Datagram, rxPort port.Port) {
	if svc, ok := r.servicesBySAS[datagram.DestinationSocket]; ok {
		svc.Inbound(datagram, rxPort)
	}
}

func (r *Router) Start(ctx context.Context) error {
	for _, s := range r.Services {
		if _, ok := s.(*llap.Service); !ok {
			continue
		}
		netlog.Info("starting %T...", s)
		if err := s.Start(ctx, r); err != nil {
			return err
		}
	}
	for _, p := range r.Ports {
		netlog.Info("starting %T...", p)
		if err := p.Start(r); err != nil {
			return err
		}
	}
	netlog.Info("all ports started!")
	for _, s := range r.Services {
		if _, ok := s.(*llap.Service); ok {
			continue
		}
		netlog.Info("starting %T...", s)
		if err := s.Start(ctx, r); err != nil {
			return err
		}
	}
	netlog.Info("all services started!")
	return nil
}

func (r *Router) Stop() error {
	var errs []error
	for _, s := range r.Services {
		netlog.Info("stopping %T...", s)
		if err := s.Stop(); err != nil {
			errs = append(errs, err)
		}
	}
	netlog.Info("all services stopped!")
	for _, p := range r.Ports {
		netlog.Info("stopping %T...", p)
		if err := p.Stop(); err != nil {
			errs = append(errs, err)
		}
	}
	netlog.Info("all ports stopped!")
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func (r *Router) Inbound(datagram ddp.Datagram, rxPort port.Port) {
	framesInTotal.Inc()
	if rxPort.Network() != 0 {
		if datagram.DestinationNetwork == 0 && datagram.SourceNetwork == 0 {
			datagram.DestinationNetwork = rxPort.Network()
			datagram.SourceNetwork = rxPort.Network()
		} else if datagram.DestinationNetwork == 0 {
			datagram.DestinationNetwork = rxPort.Network()
		} else if datagram.SourceNetwork == 0 {
			datagram.SourceNetwork = rxPort.Network()
		}
	}
	if r.observer != nil {
		r.observer(datagram, rxPort)
	}
	if datagram.DestinationNetwork == 0 || datagram.DestinationNetwork == rxPort.Network() {
		if datagram.DestinationNode == 0 || datagram.DestinationNode == rxPort.Node() || datagram.DestinationNode == 0xFF {
			r.deliver(datagram, rxPort)
		}
		return
	}
	entry, _ := r.RoutingTable.GetByNetwork(datagram.DestinationNetwork)
	if entry != nil && entry.Distance == 0 {
		if datagram.DestinationNetwork == entry.Port.Network() && datagram.DestinationNode == entry.Port.Node() {
			r.deliver(datagram, rxPort)
			return
		} else if datagram.DestinationNode == 0 {
			r.deliver(datagram, rxPort)
			return
		} else if datagram.DestinationNode == 0xFF {
			r.deliver(datagram, rxPort)
		}
	}
	_ = r.Route(datagram, false)
}

func (r *Router) Route(datagram ddp.Datagram, originating bool) error {
	if originating {
		if datagram.HopCount != 0 {
			return errors.New("originated datagrams must have hop count of 0")
		}
		if datagram.DestinationNetwork == 0 {
			return errors.New("originated datagrams must have nonzero destination network")
		}
	}
	if datagram.DestinationNetwork == 0 || datagram.HopCount >= 15 {
		return nil
	}
	entry, _ := r.RoutingTable.GetByNetwork(datagram.DestinationNetwork)
	if entry == nil {
		return nil
	}
	if originating {
		if entry.Port.Network() == 0 || entry.Port.Node() == 0 {
			netlog.Debug("router: dropping originated datagram to %d.%d — port %s not yet ready (network=%d node=%d)",
				datagram.DestinationNetwork, datagram.DestinationNode,
				entry.Port.ShortString(), entry.Port.Network(), entry.Port.Node())
			return nil
		}
		// Only fill in source address from the outgoing port if the caller has not
		// pre-set it.  Callers that are replying to a request want the source to
		// reflect the address the client originally sent to (so ATP TResp source
		// matches the TReq destination), not the outgoing port's local address.
		if datagram.SourceNetwork == 0 {
			datagram.SourceNetwork = entry.Port.Network()
		}
		if datagram.SourceNode == 0 {
			datagram.SourceNode = entry.Port.Node()
		}
	} else {
		if datagram.SourceNode == 0 || datagram.SourceNode == 0xFF {
			return nil
		}
		datagram = datagram.Hop()
	}
	if entry.Distance != 0 {
		entry.Port.Unicast(entry.NextNetwork, entry.NextNode, datagram)
	} else if datagram.DestinationNode == 0 {
	} else if datagram.DestinationNetwork == entry.Port.Network() && datagram.DestinationNode == entry.Port.Node() {
	} else if datagram.DestinationNode == 0xFF {
		entry.Port.Broadcast(datagram)
	} else {
		entry.Port.Unicast(datagram.DestinationNetwork, datagram.DestinationNode, datagram)
	}
	return nil
}

func (r *Router) Reply(datagram ddp.Datagram, rxPort port.Port, ddpType uint8, data []byte) {
	if datagram.SourceNode == 0 || datagram.SourceNode == 0xFF {
		return
	}
	if rxPort.Node() != 0 && (datagram.SourceNetwork == 0 || (datagram.SourceNetwork >= 0xFF00 && datagram.SourceNetwork <= 0xFFFE) ||
		datagram.SourceNetwork < rxPort.NetworkMin() || datagram.SourceNetwork > rxPort.NetworkMax()) {
		rxPort.Broadcast(ddp.Datagram{
			HopCount:           0,
			DestinationNetwork: 0,
			SourceNetwork:      rxPort.Network(),
			DestinationNode:    0xFF,
			SourceNode:         rxPort.Node(),
			DestinationSocket:  datagram.SourceSocket,
			SourceSocket:       datagram.DestinationSocket,
			DDPType:            ddpType,
			Data:               append([]byte(nil), data...),
		})
		return
	}
	_ = r.Route(ddp.Datagram{
		HopCount:           0,
		DestinationNetwork: datagram.SourceNetwork,
		SourceNetwork:      datagram.DestinationNetwork, // reply FROM the address the client sent TO
		DestinationNode:    datagram.SourceNode,
		SourceNode:         datagram.DestinationNode, // reply FROM the address the client sent TO
		DestinationSocket:  datagram.SourceSocket,
		SourceSocket:       datagram.DestinationSocket,
		DDPType:            ddpType,
		Data:               append([]byte(nil), data...),
	}, true)
}

func (r *Router) RoutingTableAge() {
	r.RoutingTable.Age()
}

func (r *Router) PortsList() []port.Port { return r.Ports }

func asServiceEntry(e *RoutingTableEntry) *service.RouteEntry {
	if e == nil {
		return nil
	}
	return &service.RouteEntry{
		ExtendedNetwork: e.ExtendedNetwork,
		NetworkMin:      e.NetworkMin,
		NetworkMax:      e.NetworkMax,
		Distance:        e.Distance,
		Port:            e.Port,
		NextNetwork:     e.NextNetwork,
		NextNode:        e.NextNode,
	}
}

func (r *Router) RoutingGetByNetwork(network uint16) (*service.RouteEntry, *bool) {
	e, bad := r.RoutingTable.GetByNetwork(network)
	return asServiceEntry(e), bad
}

func (r *Router) RoutingEntries() []struct {
	Entry *service.RouteEntry
	Bad   bool
} {
	x := r.RoutingTable.Entries()
	out := make([]struct {
		Entry *service.RouteEntry
		Bad   bool
	}, 0, len(x))
	for _, item := range x {
		out = append(out, struct {
			Entry *service.RouteEntry
			Bad   bool
		}{Entry: asServiceEntry(item.Entry), Bad: item.Bad})
	}
	return out
}

func (r *Router) RoutingConsider(entry *service.RouteEntry) bool {
	return r.RoutingTable.Consider(&RoutingTableEntry{
		ExtendedNetwork: entry.ExtendedNetwork,
		NetworkMin:      entry.NetworkMin,
		NetworkMax:      entry.NetworkMax,
		Distance:        entry.Distance,
		Port:            entry.Port,
		NextNetwork:     entry.NextNetwork,
		NextNode:        entry.NextNode,
	})
}

func (r *Router) RoutingMarkBad(networkMin, networkMax uint16) bool {
	return r.RoutingTable.MarkBad(networkMin, networkMax)
}

func (r *Router) ZonesInNetworkRange(networkMin uint16, networkMax *uint16) ([][]byte, error) {
	return r.ZoneInformationTable.ZonesInNetworkRange(networkMin, networkMax)
}

func (r *Router) NetworksInZone(zoneName []byte) []uint16 {
	return r.ZoneInformationTable.NetworksInZone(zoneName)
}

func (r *Router) Zones() [][]byte {
	return r.ZoneInformationTable.Zones()
}

func (r *Router) AddNetworksToZone(zoneName []byte, networkMin uint16, networkMax *uint16) error {
	return r.ZoneInformationTable.AddNetworksToZone(zoneName, networkMin, networkMax)
}

func (r *Router) RoutingSetPortRange(pt port.Port, networkMin, networkMax uint16) {
	r.RoutingTable.SetPortRange(pt, networkMin, networkMax)
}
