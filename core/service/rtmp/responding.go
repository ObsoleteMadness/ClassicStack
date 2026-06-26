package rtmp

import (
	"context"
	"sync"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"

	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
	"github.com/ObsoleteMadness/ClassicStack/core/router"
)

// RespondingName is the component/section key for the RTMP responding service.
const RespondingName = "RTMP"

type respItem struct {
	d    ddp.Datagram
	from router.RoutedPort
}

// RespondingService answers RTMP requests (range request, routing-table data request) and
// folds RTMP Data packets from neighbours into the routing table.
type RespondingService struct {
	rtr    router.ServiceRouter
	logger log.Logger

	mu      sync.Mutex
	running bool
	ch      chan respItem
	stop    chan struct{}
	wg      sync.WaitGroup
}

// NewRespondingService builds the RTMP responder bound to its router.
func NewRespondingService(rtr router.ServiceRouter, logger log.Logger) *RespondingService {
	return &RespondingService{rtr: rtr, logger: logger}
}

// Name returns the component name.
func (s *RespondingService) Name() string { return RespondingName }

// Dependencies declares RTMP's start-order edge: the AppleTalk router must be running
// first (RTMP rides the shared router's socket table). Drops in a no-router build.
func (s *RespondingService) Dependencies() []string { return []string{router.Name} }

// Socket returns the RTMP socket so the router dispatches RTMP datagrams here.
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

// handle dispatches one RTMP datagram by DDP type.
func (s *RespondingService) handle(d ddp.Datagram, rx router.RoutedPort) {
	switch d.DDPType {
	case DDPTypeData:
		s.handleData(d, rx)
	case DDPTypeRequest:
		s.handleRequest(d, rx)
	}
}

// handleData folds a neighbour's RTMP Data packet (sender header + routing tuples) into the
// routing table: it adopts the sender's range if this port has none, then Considers each
// reachable tuple and MarkBads each unreachable one.
func (s *RespondingService) handleData(d ddp.Datagram, rx router.RoutedPort) {
	if len(d.Data) < 4 {
		return
	}
	senderNetwork := bp.BE16(d.Data[0:2])
	if d.Data[2] != 8 {
		return
	}
	senderNode := d.Data[3]
	data := d.Data[4:]

	var senderNetworkMin, senderNetworkMax uint16
	var rtmpVersion byte
	if router.PortIsExtended(rx) {
		if len(data) < 6 {
			return
		}
		senderNetworkMin = bp.BE16(data[0:2])
		if data[2] != 0x80 {
			return
		}
		senderNetworkMax = bp.BE16(data[3:5])
		rtmpVersion = data[5]
		data = data[6:] // skip the sender's own extended tuple before neighbour tuples
	} else {
		if len(data) < 3 {
			return
		}
		senderNetworkMin = senderNetwork
		senderNetworkMax = senderNetwork
		if bp.BE16(data[0:2]) != 0 {
			return
		}
		rtmpVersion = data[2]
		data = data[3:]
	}
	if rtmpVersion != Version {
		return
	}
	if rx.NetworkMin() == 0 && rx.NetworkMax() == 0 {
		router.AdoptRange(rx, senderNetworkMin, senderNetworkMax)
	}

	rt := s.rtr.RoutingTable()
	i := 0
	for i+3 <= len(data) {
		nmin := bp.BE16(data[i : i+2])
		rd := data[i+2]
		i += 3
		extended := rd&0x80 != 0
		nmax := nmin
		dist := rd & 0x1F
		if extended {
			if i+3 > len(data) {
				break
			}
			nmax = bp.BE16(data[i : i+2])
			i += 3
		}
		if dist >= 15 {
			rt.MarkBad(nmin, nmax)
		} else {
			rt.Consider(&router.RoutingTableEntry{
				ExtendedNetwork: extended,
				NetworkMin:      nmin,
				NetworkMax:      nmax,
				Distance:        dist + 1,
				Port:            rx,
				NextNetwork:     senderNetwork,
				NextNode:        senderNode,
			})
		}
	}
}

// handleRequest answers an RTMP Request: a range request with this port's network range, or a
// routing-data request with the full table (split-horizon honoured per function code).
func (s *RespondingService) handleRequest(d ddp.Datagram, rx router.RoutedPort) {
	if len(d.Data) == 0 {
		return
	}
	switch d.Data[0] {
	case FuncRequest:
		if rx.NetworkMin() == 0 || rx.NetworkMax() == 0 || d.Hops != 0 {
			return
		}
		resp := []byte{byte(rx.Network() >> 8), byte(rx.Network()), 8, rx.Node()}
		if router.PortIsExtended(rx) {
			resp = append(resp, byte(rx.NetworkMin()>>8), byte(rx.NetworkMin()), 0x80,
				byte(rx.NetworkMax()>>8), byte(rx.NetworkMax()), Version)
		}
		s.rtr.Reply(d, rx, DDPTypeData, resp)
	case FuncRDRSplitHorizon, FuncRDRNoSplitHorizon:
		split := d.Data[0] == FuncRDRSplitHorizon
		for _, dd := range makeRoutingTableDatagramData(s.rtr, rx, split) {
			s.rtr.Reply(d, rx, DDPTypeData, dd)
		}
	}
}
