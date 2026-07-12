// Package rip is the IPX RIP responder: the socket-0x0453 handler that answers
// route queries for the networks this server owns — above all the NetWare internal
// network the NCP file service is advertised on. It is the missing half of SAP
// discovery: a NetWare client that received a GetNearestServer response broadcasts
// a RIP Request for the advertised network (the "GetLocalTarget" step) and will not
// open an NCP connection until someone answers; the answer's source node is the
// MAC the client then frames NCP packets to.
//
// Behaviour follows mars_nwe nwroute.c:
//
//   - handle_rip: a Request is answered with the matching owned networks (or all of
//     them for the 0xFFFFFFFF wildcard), unicast back to the querier.
//   - build_rip_buff/ins_rip_buff: a directly served network is hops 1 / ticks 2.
//   - send_rip_broadcast: owned routes are also broadcast periodically (60 s), and a
//     shutdown broadcast advertises them at hops 16 (unreachable) so clients drop them.
//
// It rides the same core/router/ipx mini-router as its clients through the IPXSender
// seam (never importing the router), exactly like the shared SAP advertiser.
//
// Ring: CORE (stdlib only). Reference: Novell RIP (IPX socket 0x0453); mars_nwe
// nwroute.c (CLAUDE.md #7).
package rip

import (
	"sync"
	"time"

	ipxproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx"
	ripproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/rip"
)

// Name is the component name for the RIP responder.
const Name = "RIP"

// broadcastInterval is how often owned routes are re-broadcast. NetWare routers
// broadcast RIP every 60 seconds (mars_nwe does the same).
const broadcastInterval = 60 * time.Second

// directHops and directTicks are the metric for a network this server serves
// directly: one router hop away, two ticks (mars_nwe ins_rip_buff(internal_net, 1, 2);
// a real NetWare 4 server answers GetLocalTarget identically).
const (
	directHops  uint16 = 1
	directTicks uint16 = 2
)

// ipxBroadcastNode is the all-ones IPX node the periodic broadcast fans to.
var ipxBroadcastNode = [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}

// IPXSender is the IPX datagram egress the responder drives: the core/router/ipx
// mini-router's Send satisfies it, so the responder never imports the router.
type IPXSender interface {
	Send(d *ipxproto.Datagram) error
}

// Responder answers RIP requests for the owned networks and broadcasts them
// periodically. Register it on IPX socket 0x0453.
type Responder struct {
	sender IPXSender

	mu      sync.Mutex
	nets    [][4]byte
	stopCh  chan struct{}
	started bool
}

// New builds a RIP responder bound to the IPX egress. Compose sets the owned
// networks (SetNetworks), starts it, and registers it on the RIP socket.
func New(sender IPXSender) *Responder {
	return &Responder{sender: sender}
}

// SetNetworks sets the networks this server answers route queries for (zero
// entries are dropped). Today that is the NetWare internal network; a wire
// network learned or configured later joins the same list.
func (r *Responder) SetNetworks(nets ...[4]byte) {
	owned := make([][4]byte, 0, len(nets))
	for _, n := range nets {
		if n != ([4]byte{}) {
			owned = append(owned, n)
		}
	}
	r.mu.Lock()
	r.nets = owned
	r.mu.Unlock()
}

// Start begins the periodic route broadcast (an immediate broadcast, then every
// broadcastInterval). Idempotent.
func (r *Responder) Start() {
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return
	}
	r.started = true
	r.stopCh = make(chan struct{})
	stop := r.stopCh
	r.mu.Unlock()
	go r.loop(stop)
}

// Stop halts the broadcast loop and broadcasts the owned routes at hops 16
// (unreachable) so clients drop them (mars_nwe's shutdown response). Idempotent.
func (r *Responder) Stop() {
	r.mu.Lock()
	if !r.started {
		r.mu.Unlock()
		return
	}
	r.started = false
	close(r.stopCh)
	r.mu.Unlock()
	r.broadcast(ripproto.HopsUnreachable)
}

// loop broadcasts the owned routes every broadcastInterval until stopped.
func (r *Responder) loop(stop chan struct{}) {
	t := time.NewTicker(broadcastInterval)
	defer t.Stop()
	r.broadcast(directHops) // advertise immediately on start
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			r.broadcast(directHops)
		}
	}
}

// owned snapshots the owned networks.
func (r *Responder) owned() [][4]byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.nets
}

// broadcast sends an unsolicited RIP response carrying every owned network at the
// given hop metric to the IPX broadcast address. Nothing is sent with no networks.
func (r *Responder) broadcast(hops uint16) {
	nets := r.owned()
	if len(nets) == 0 {
		return
	}
	entries := make([]ripproto.Entry, 0, len(nets))
	for _, n := range nets {
		entries = append(entries, ripproto.Entry{Network: n, Hops: hops, Ticks: directTicks})
	}
	r.send(entries, [4]byte{}, ipxBroadcastNode, ripproto.Socket)
}

// HandleDatagram is the core/router/ipx SocketHandler entry point for the RIP
// socket. A Request is answered with the owned networks it asks about — all of
// them for the wildcard — unicast back to the querier; hops 1 / ticks 2, the
// directly-served metric. Responses (other routers' broadcasts) are ignored: this
// is a responder, not a route learner.
func (r *Responder) HandleDatagram(d *ipxproto.Datagram) {
	if d == nil {
		return
	}
	q, err := ripproto.Unmarshal(d.Payload)
	if err != nil || q.Operation != ripproto.OpRequest {
		return
	}
	nets := r.owned()
	var entries []ripproto.Entry
	for _, want := range q.Entries {
		for _, n := range nets {
			if want.Network == n || want.Network == ripproto.NetworkWildcard {
				entries = append(entries, ripproto.Entry{Network: n, Hops: directHops, Ticks: directTicks})
			}
		}
		if want.Network == ripproto.NetworkWildcard {
			break // the wildcard already matched everything
		}
	}
	if len(entries) == 0 {
		return
	}
	r.send(entries, d.SrcNet, d.SrcNode, d.SrcSock)
}

// send marshals a RIP response and writes it to the given destination. The source
// network/node are left zero for the mini-router to fill with the wire identity
// (the responder's node is what a GetLocalTarget client frames NCP packets to).
func (r *Responder) send(entries []ripproto.Entry, dstNet [4]byte, dstNode [6]byte, dstSock [2]byte) {
	p := ripproto.Packet{Operation: ripproto.OpResponse, Entries: entries}
	_ = r.sender.Send(&ipxproto.Datagram{
		Type:    ripproto.IPXType,
		DstNet:  dstNet,
		DstNode: dstNode,
		DstSock: dstSock,
		SrcSock: ripproto.Socket,
		Payload: p.Marshal(nil),
	})
}
