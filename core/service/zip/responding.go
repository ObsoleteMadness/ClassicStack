package zip

import (
	"bytes"
	"context"
	"sync"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"

	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
	"github.com/ObsoleteMadness/ClassicStack/core/router"
)

// RespondingName is the component/section key for the ZIP responding service.
const RespondingName = "ZIP"

type respItem struct {
	d    ddp.Datagram
	from router.RoutedPort
}

// RespondingService answers ZIP queries and the ATP-carried zone requests, and accumulates
// ZIP Reply / ExtReply tuples into the zone information table.
type RespondingService struct {
	rtr    router.ServiceRouter
	logger log.Logger

	mu              sync.Mutex
	running         bool
	ch              chan respItem
	stop            chan struct{}
	wg              sync.WaitGroup
	pendingExtReply map[uint16]map[string]struct{} // network_min -> set of zone names
}

// NewRespondingService builds the ZIP responder bound to its router.
func NewRespondingService(rtr router.ServiceRouter, logger log.Logger) *RespondingService {
	return &RespondingService{rtr: rtr, logger: logger, pendingExtReply: map[uint16]map[string]struct{}{}}
}

// Name returns the component name.
func (s *RespondingService) Name() string { return RespondingName }

// Dependencies declares ZIP's start-order edge: the AppleTalk router must be running
// first (ZIP rides the shared router's socket table). Drops in a no-router build.
func (s *RespondingService) Dependencies() []string { return []string{router.Name} }

// Socket returns the ZIP socket so the router dispatches ZIP datagrams here.
func (s *RespondingService) Socket() uint8 { return SAS }

// Start launches the responder goroutine. Idempotent (§3).
func (s *RespondingService) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return nil
	}
	s.running = true
	s.ch = make(chan respItem, 256)
	s.stop = make(chan struct{})
	s.wg.Add(1)
	go s.run(ctx, s.ch, s.stop)
	return nil
}

// Stop shuts the responder down. Safe after a partial Start (§3) and idempotent.
func (s *RespondingService) Stop(ctx context.Context) error {
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

// Inbound queues a datagram for the responder; a full queue drops.
func (s *RespondingService) Inbound(d ddp.Datagram, from router.RoutedPort) {
	s.mu.Lock()
	ch, running := s.ch, s.running
	s.mu.Unlock()
	if !running {
		return
	}
	select {
	case ch <- respItem{d: d, from: from}:
	default:
	}
}

func (s *RespondingService) run(ctx context.Context, ch chan respItem, stop chan struct{}) {
	defer s.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case it := <-ch:
			s.handle(it.d, it.from)
		}
	}
}

// handle dispatches one ZIP datagram by DDP type and function code.
func (s *RespondingService) handle(d ddp.Datagram, rx router.RoutedPort) {
	switch d.DDPType {
	case DDPType:
		if len(d.Data) < 2 {
			return
		}
		switch d.Data[0] {
		case FuncReply:
			s.handleReply(d)
		case FuncExtReply:
			s.handleExtReply(d)
		case FuncQuery:
			s.handleQuery(d, rx)
		case FuncGetNetInfoReq:
			s.handleGetNetInfo(d, rx)
		}
	case ATPDDPType:
		if len(d.Data) != 8 {
			return
		}
		ctrl, bitmap, fn, zero := d.Data[0], d.Data[1], d.Data[4], d.Data[5]
		if ctrl != ATPFuncTReq || bitmap != 1 || zero != 0 {
			return
		}
		switch fn {
		case ATPGetMyZone:
			s.handleGetMyZone(d, rx)
		case ATPGetZoneList:
			s.handleGetZoneList(d, rx, false)
		case ATPGetLocalZoneList:
			s.handleGetZoneList(d, rx, true)
		}
	}
}

// handleReply commits each (network, zone) tuple of a ZIP Reply immediately.
func (s *RespondingService) handleReply(d ddp.Datagram) {
	data := d.Data[2:]
	for len(data) >= 3 {
		nmin := bp.BE16(data[0:2])
		l := int(data[2])
		if len(data) < 3+l {
			break
		}
		zone := data[3 : 3+l]
		data = data[3+l:]
		if l == 0 {
			continue
		}
		entry, _ := s.rtr.RoutingTable().GetByNetwork(nmin)
		if entry == nil {
			s.warn("ZIP reply refers to an unknown network range", log.Int("network_min", int64(nmin)))
			continue
		}
		nmax := entry.NetworkMax
		if err := s.rtr.Zones().AddNetworksToZone(append([]byte(nil), zone...), nmin, &nmax); err != nil {
			s.warn("ZIP reply couldn't be added to zone information table", log.Str("err", err.Error()))
		}
	}
}

// handleExtReply accumulates ExtReply tuples until the announced count is reached, then
// commits them (zone-list replies span multiple datagrams).
func (s *RespondingService) handleExtReply(d ddp.Datagram) {
	if len(d.Data) < 2 {
		return
	}
	count := int(d.Data[1])
	data := d.Data[2:]

	var lastNmin uint16
	for len(data) >= 3 {
		nmin := bp.BE16(data[0:2])
		l := int(data[2])
		if len(data) < 3+l {
			break
		}
		zone := data[3 : 3+l]
		data = data[3+l:]
		if l == 0 {
			continue
		}
		lastNmin = nmin
		if s.pendingExtReply[nmin] == nil {
			s.pendingExtReply[nmin] = map[string]struct{}{}
		}
		s.pendingExtReply[nmin][string(zone)] = struct{}{}
	}

	if count >= 1 && len(s.pendingExtReply[lastNmin]) >= count {
		entry, _ := s.rtr.RoutingTable().GetByNetwork(lastNmin)
		if entry != nil {
			nmax := entry.NetworkMax
			for zoneStr := range s.pendingExtReply[lastNmin] {
				z := []byte(zoneStr)
				if err := s.rtr.Zones().AddNetworksToZone(z, lastNmin, &nmax); err != nil {
					s.warn("ZIP ext reply couldn't be added to zone information table", log.Str("err", err.Error()))
				}
			}
		}
		delete(s.pendingExtReply, lastNmin)
	}
}

// handleQuery answers a ZIP Query with one or more ExtReply datagrams listing the zones of the
// requested networks.
func (s *RespondingService) handleQuery(d ddp.Datagram, rx router.RoutedPort) {
	if len(d.Data) < 2 {
		return
	}
	nc := int(d.Data[1])
	if len(d.Data) != 2+nc*2 {
		return
	}
	for i := range nc {
		req := bp.BE16(d.Data[2+i*2 : 4+i*2])
		entry, _ := s.rtr.RoutingTable().GetByNetwork(req)
		if entry == nil {
			continue
		}
		zones, err := s.rtr.Zones().ZonesInNetworkRange(entry.NetworkMin, nil)
		if err != nil || len(zones) == 0 {
			continue
		}
		buf := []byte{FuncExtReply, byte(len(zones))}
		for _, z := range zones {
			item := make([]byte, 3+len(z))
			item[0] = byte(entry.NetworkMin >> 8)
			item[1] = byte(entry.NetworkMin)
			item[2] = byte(len(z))
			copy(item[3:], z)
			if len(buf)+len(item) > ddp.MaxDataLength {
				s.rtr.Reply(d, rx, DDPType, buf)
				buf = []byte{FuncExtReply, byte(len(zones))}
			}
			buf = append(buf, item...)
		}
		if len(buf) > 2 {
			s.rtr.Reply(d, rx, DDPType, buf)
		}
	}
}

// handleGetNetInfo answers a GetNetInfo request with the port's network range, the validity of
// the client's proposed zone, and the multicast address for the (default or matched) zone.
func (s *RespondingService) handleGetNetInfo(d ddp.Datagram, rx router.RoutedPort) {
	if rx.Network() == 0 || rx.NetworkMin() == 0 || rx.NetworkMax() == 0 {
		return
	}
	if len(d.Data) < 7 {
		return
	}
	if !bytes.Equal(d.Data[1:6], []byte{0, 0, 0, 0, 0}) {
		return
	}
	zoneLen := int(d.Data[6])
	if len(d.Data) < 7+zoneLen {
		return
	}
	givenZone := d.Data[7 : 7+zoneLen]

	nmax := rx.NetworkMax()
	zones, err := s.rtr.Zones().ZonesInNetworkRange(rx.NetworkMin(), &nmax)
	if err != nil {
		s.warn("couldn't get zone names for GetNetInfo", log.Str("err", err.Error()))
		return
	}
	if len(zones) == 0 {
		return
	}

	flags := byte(GetNetInfoZoneInvalid | GetNetInfoOnlyOneZone)
	defaultZone := zones[0]
	var mcastAddr []byte
	if ma, ok := rx.(multicastAddresser); ok {
		mcastAddr = ma.MulticastAddress(defaultZone)
	}

	givenUC := string(toUCase(givenZone))
	for i, zone := range zones {
		if i == 1 {
			flags &^= GetNetInfoOnlyOneZone
		}
		if string(toUCase(zone)) == givenUC {
			flags &^= GetNetInfoZoneInvalid
			if ma, ok := rx.(multicastAddresser); ok {
				mcastAddr = ma.MulticastAddress(zone)
			}
		}
		if i > 0 && flags&GetNetInfoZoneInvalid == 0 {
			break
		}
	}

	if len(mcastAddr) == 0 {
		flags |= GetNetInfoUseBroadcast
	}

	reply := []byte{FuncGetNetInfoRep, flags,
		byte(rx.NetworkMin() >> 8), byte(rx.NetworkMin()),
		byte(rx.NetworkMax() >> 8), byte(rx.NetworkMax()),
		byte(len(givenZone))}
	reply = append(reply, givenZone...)
	reply = append(reply, byte(len(mcastAddr)))
	reply = append(reply, mcastAddr...)
	if flags&GetNetInfoZoneInvalid != 0 {
		reply = append(reply, byte(len(defaultZone)))
		reply = append(reply, defaultZone...)
	}
	s.rtr.Reply(d, rx, DDPType, reply)
}

// handleGetMyZone answers the ATP GetMyZone transaction with the default zone of the source
// network.
func (s *RespondingService) handleGetMyZone(d ddp.Datagram, rx router.RoutedPort) {
	tid := bp.BE16(d.Data[2:4])
	entry, _ := s.rtr.RoutingTable().GetByNetwork(d.SrcNetwork)
	if entry == nil {
		return
	}
	zones, err := s.rtr.Zones().ZonesInNetworkRange(entry.NetworkMin, nil)
	if err != nil || len(zones) == 0 {
		return
	}
	zone := zones[0]
	resp := []byte{ATPFuncTResp | ATPEOM, 0,
		byte(tid >> 8), byte(tid),
		0, 0,
		0, 1,
		byte(len(zone))}
	resp = append(resp, zone...)
	s.rtr.Reply(d, rx, ATPDDPType, resp)
}

// handleGetZoneList answers the ATP GetZoneList / GetLocalZones transaction with a page of
// zone names from the requested start index.
func (s *RespondingService) handleGetZoneList(d ddp.Datagram, rx router.RoutedPort, local bool) {
	tid := bp.BE16(d.Data[2:4])
	startIndex := int(bp.BE16(d.Data[6:8])) // 1-relative

	var zones [][]byte
	if local {
		nmax := rx.NetworkMax()
		var err error
		zones, err = s.rtr.Zones().ZonesInNetworkRange(rx.NetworkMin(), &nmax)
		if err != nil {
			s.warn("couldn't get zone names for GetLocalZones", log.Str("err", err.Error()))
			return
		}
	} else {
		zones = s.rtr.Zones().Zones()
	}

	if startIndex > 1 {
		skip := startIndex - 1
		if skip >= len(zones) {
			zones = nil
		} else {
			zones = zones[skip:]
		}
	}

	lastFlag := byte(0)
	var zoneList []byte
	numZones := 0
	const atpHdrLen = 8
	for i, zone := range zones {
		if atpHdrLen+len(zoneList)+1+len(zone) > ddp.MaxDataLength {
			break
		}
		zoneList = append(zoneList, byte(len(zone)))
		zoneList = append(zoneList, zone...)
		numZones++
		if i == len(zones)-1 {
			lastFlag = 1 // exhausted the list
		}
	}

	resp := []byte{ATPFuncTResp | ATPEOM, 0,
		byte(tid >> 8), byte(tid),
		lastFlag, 0,
		byte(numZones >> 8), byte(numZones)}
	resp = append(resp, zoneList...)
	s.rtr.Reply(d, rx, ATPDDPType, resp)
}

// warn logs a warning if the logger is configured and the level is enabled.
func (s *RespondingService) warn(msg string, f log.Field) {
	if s.logger == nil || !s.logger.Enabled(log.Warn) {
		return
	}
	s.logger.Log1(log.Warn, msg, f)
}
