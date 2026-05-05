package ipx

import (
	"context"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/netlog"
	ipxproto "github.com/ObsoleteMadness/ClassicStack/protocol/ipx"
	routeripx "github.com/ObsoleteMadness/ClassicStack/router/ipx"
)

// RIPSocket is the well-known socket number for IPX RIP.
var RIPSocket = [2]byte{0x04, 0x53}

// DefaultRIPPeriod is the broadcast cadence used by NetWare-era IPX
// routers and matches what most clients expect to see.
const DefaultRIPPeriod = 60 * time.Second

// RIPService is an IPX Routing Information Protocol responder for a
// single-segment deployment. We are not a router: we don't forward
// traffic, and we only ever advertise our own network number with
// hops=1, ticks=1. We do, however, respond to RIP requests so that
// clients learn about us and we send periodic broadcasts so clients
// don't time us out of their tables.
type RIPService struct {
	router routeripx.Router

	// Period is the broadcast cadence. Zero or negative means use
	// DefaultRIPPeriod.
	Period time.Duration

	// now/sleep let tests substitute a fake clock without changing
	// the production code path.
	now   func() time.Time
	sleep func(d time.Duration) <-chan time.Time

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

// NewRIPService returns a RIP service bound to r.
func NewRIPService(r routeripx.Router) *RIPService {
	return &RIPService{
		router: r,
		now:    time.Now,
		sleep: func(d time.Duration) <-chan time.Time {
			return time.After(d)
		},
	}
}

// Start registers the RIP socket and spawns the periodic broadcaster.
func (s *RIPService) Start(ctx context.Context) error {
	if err := s.router.RegisterSocket(RIPSocket, s); err != nil {
		return err
	}
	loopCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.cancel = cancel
	s.done = make(chan struct{})
	s.mu.Unlock()
	go s.broadcastLoop(loopCtx)
	return nil
}

// Stop cancels the broadcaster and waits for it to exit.
func (s *RIPService) Stop() error {
	s.mu.Lock()
	cancel := s.cancel
	done := s.done
	s.cancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
	return nil
}

// HandleDatagram implements router/ipx.SocketHandler. The address
// filter on the router has already accepted this datagram as
// addressed to us (or broadcast); RIP itself decides whether to
// respond.
func (s *RIPService) HandleDatagram(d *ipxproto.Datagram) {
	pkt, err := DecodeRIP(d.Payload)
	if err != nil {
		return
	}
	if pkt.Operation != RIPRequest {
		// We are not a router: we ignore RIP responses from other
		// nodes (we don't maintain a routing table beyond our own
		// single network).
		return
	}
	resp := s.respondToRequest(pkt)
	if resp == nil {
		return
	}
	if err := s.sendResponse(d, resp); err != nil {
		netlog.Warn("[IPX][RIP] send response: %v", err)
	}
}

// respondToRequest builds a response packet for a RIP request, or
// returns nil when the request asked about networks we don't know.
//
// We know exactly one network — our own. If the request entries
// include either ours or the wildcard (RIPNetworkAny), we respond
// with our own entry; otherwise we don't reply.
func (s *RIPService) respondToRequest(req *RIPPacket) *RIPPacket {
	ours := s.router.Network()

	// A request with no entries is treated as a wildcard for
	// compatibility with old clients.
	wildcard := len(req.Entries) == 0
	matchesOurs := false
	for _, e := range req.Entries {
		if e.Network == RIPNetworkAny {
			wildcard = true
		}
		if e.Network == ours {
			matchesOurs = true
		}
	}
	if !wildcard && !matchesOurs {
		return nil
	}
	return &RIPPacket{
		Operation: RIPResponse,
		Entries: []RIPEntry{
			{Network: ours, Hops: 1, Ticks: 1},
		},
	}
}

// sendResponse posts a unicast reply to the requester. The router
// fills the source net/node automatically.
func (s *RIPService) sendResponse(req *ipxproto.Datagram, resp *RIPPacket) error {
	body, err := EncodeRIP(resp)
	if err != nil {
		return err
	}
	out := &ipxproto.Datagram{
		Type:    1, // RIP packet type
		DstNet:  req.SrcNet,
		DstNode: req.SrcNode,
		DstSock: RIPSocket,
		SrcSock: RIPSocket,
		Payload: body,
	}
	return s.router.Send(out)
}

// broadcastLoop emits a periodic RIP response naming our own
// network. Stops when the context is cancelled.
func (s *RIPService) broadcastLoop(ctx context.Context) {
	defer func() {
		s.mu.Lock()
		done := s.done
		s.done = nil
		s.mu.Unlock()
		if done != nil {
			close(done)
		}
	}()

	period := s.Period
	if period <= 0 {
		period = DefaultRIPPeriod
	}

	// First broadcast goes out immediately so the segment learns
	// about us without waiting a full period.
	s.broadcast()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.sleep(period):
			s.broadcast()
		}
	}
}

// broadcast emits an unsolicited RIP response advertising our
// network, addressed to the broadcast node on socket 0x0453.
func (s *RIPService) broadcast() {
	ours := s.router.Network()
	resp := &RIPPacket{
		Operation: RIPResponse,
		Entries: []RIPEntry{
			{Network: ours, Hops: 1, Ticks: 1},
		},
	}
	body, err := EncodeRIP(resp)
	if err != nil {
		return
	}
	out := &ipxproto.Datagram{
		Type:    1,
		DstNet:  ours,
		DstNode: routeripx.BroadcastNode,
		DstSock: RIPSocket,
		SrcSock: RIPSocket,
		Payload: body,
	}
	if err := s.router.Send(out); err != nil {
		netlog.Debug("[IPX][RIP] broadcast: %v", err)
	}
}
