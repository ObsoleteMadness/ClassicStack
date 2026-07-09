// Package sap is the shared Service Advertising Protocol advertiser: one IPX
// socket-0x0452 handler that advertises EVERY registered service on the segment, so
// NETx / VLM (NCP file server) and a SAP-browsing NetBIOS-over-IPX station both
// discover the services ClassicStack offers without a preferred-server binding.
//
// It is a shared registrar (the analogue of the legacy service/ipx SAPRegistrar):
// each service that wants to be discoverable calls Register with its SAPEntry and gets
// a cancel to withdraw it. The advertiser:
//
//   - answers SAP nearest-service (type 3) and general-service (type 1) queries for
//     any registered entry whose type the query wants (exact or wildcard), and
//   - broadcasts an unsolicited general-service response carrying all registered
//     entries every sapInterval so a client that missed the query handshake still
//     learns them.
//
// The router allows one handler per socket, so there is exactly ONE advertiser on
// 0x0452 for the whole runtime; NCP and NB-IPX register through it rather than each
// owning the socket. It rides the same core/router/ipx mini-router as its clients:
// compose hands it the router as egress (IPXSender) and the server's IPX
// network+node as the identity, then registers it on the SAP socket.
//
// Ring: CORE (stdlib only). Reference: Novell SAP (IPX socket 0x0452); the legacy
// service/ipx SAP service.
package sap

import (
	"sync"
	"time"

	ipxproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx"
	ncpproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ncp"
)

// Name is the component name for the SAP advertiser.
const Name = "SAP"

// sapInterval is how often the advertiser broadcasts an unsolicited SAP response.
// NetWare servers advertise every 60 seconds.
const sapInterval = 60 * time.Second

// ipxBroadcastNode is the all-ones IPX node the periodic advertisement fans to.
var ipxBroadcastNode = [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}

// IPXSender is the IPX datagram egress the advertiser drives: the core/router/ipx
// mini-router's Send satisfies it, so the advertiser never imports the router.
type IPXSender interface {
	Send(d *ipxproto.Datagram) error
}

// Advertiser is the shared SAP registrar/broadcaster. Register adds an entry (the
// caller withdraws it via the returned cancel); the advertiser answers 0x0452 queries
// and periodically broadcasts every registered entry.
type Advertiser struct {
	sender IPXSender

	mu      sync.Mutex
	network [4]byte
	node    [6]byte
	entries map[int]ncpproto.SAPEntry
	nextID  int
	stopCh  chan struct{}
	started bool
}

// New builds a SAP advertiser bound to the IPX egress. Compose sets the IPX identity
// (SetIdentity), registers each discoverable service's entry, starts it, and registers
// it on the SAP socket.
func New(sender IPXSender) *Advertiser {
	return &Advertiser{sender: sender, entries: make(map[int]ncpproto.SAPEntry)}
}

// SetIdentity sets the server's IPX network + node stamped into any registered entry
// that left them zero (a service knows its own socket but not the shared IPX address).
// Compose reads these from the mini-router.
func (a *Advertiser) SetIdentity(network [4]byte, node [6]byte) {
	a.mu.Lock()
	a.network = network
	a.node = node
	a.mu.Unlock()
}

// Register adds an advertised service and returns a cancel that withdraws it. The
// entry's Network/Node are filled from the advertiser's identity when left zero, so a
// service supplies only its own type/name/socket. Safe to call before or after Start.
func (a *Advertiser) Register(e ncpproto.SAPEntry) (cancel func()) {
	a.mu.Lock()
	id := a.nextID
	a.nextID++
	a.entries[id] = e
	a.mu.Unlock()
	return func() {
		a.mu.Lock()
		delete(a.entries, id)
		a.mu.Unlock()
	}
}

// Start begins the periodic broadcast loop. Idempotent.
func (a *Advertiser) Start() {
	a.mu.Lock()
	if a.started {
		a.mu.Unlock()
		return
	}
	a.started = true
	a.stopCh = make(chan struct{})
	stop := a.stopCh
	a.mu.Unlock()
	go a.loop(stop)
}

// Stop halts the broadcast loop. Idempotent.
func (a *Advertiser) Stop() {
	a.mu.Lock()
	if !a.started {
		a.mu.Unlock()
		return
	}
	a.started = false
	close(a.stopCh)
	a.mu.Unlock()
}

// loop broadcasts every registered entry every sapInterval until stopped.
func (a *Advertiser) loop(stop chan struct{}) {
	t := time.NewTicker(sapInterval)
	defer t.Stop()
	a.broadcast() // advertise immediately on start
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			a.broadcast()
		}
	}
}

// snapshot returns the current entries with the shared IPX identity filled in for any
// that left Network/Node zero.
func (a *Advertiser) snapshot() []ncpproto.SAPEntry {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]ncpproto.SAPEntry, 0, len(a.entries))
	for _, e := range a.entries {
		if e.Network == ([4]byte{}) {
			e.Network = a.network
		}
		if e.Node == ([6]byte{}) {
			e.Node = a.node
		}
		out = append(out, e)
	}
	return out
}

// matching returns the registered entries whose type the query wants (exact or
// wildcard), with the shared IPX identity filled in.
func (a *Advertiser) matching(serviceType uint16) []ncpproto.SAPEntry {
	all := a.snapshot()
	out := all[:0]
	for _, e := range all {
		if serviceType == ncpproto.SAPServerTypeWildcard || e.Type == serviceType {
			out = append(out, e)
		}
	}
	return out
}

// broadcast sends an unsolicited SAP general-service response carrying every
// registered entry to the IPX broadcast address on the SAP socket. Nothing is sent
// when no entries are registered.
func (a *Advertiser) broadcast() {
	entries := a.snapshot()
	if len(entries) == 0 {
		return
	}
	payload := ncpproto.MarshalResponse(ncpproto.SAPGeneralResponse, entries, nil)
	a.mu.Lock()
	net := a.network
	a.mu.Unlock()
	_ = a.sender.Send(&ipxproto.Datagram{
		Type:    0x04, // PEP
		DstNet:  net,
		DstNode: ipxBroadcastNode,
		DstSock: ncpproto.SAPSocket,
		SrcSock: ncpproto.SAPSocket,
		Payload: payload,
	})
}

// HandleDatagram is the core/router/ipx SocketHandler entry point for the SAP socket:
// a client query. It answers a nearest/general query with the registered entries whose
// type the query wants, addressed back to the querier (a nearest query gets a nearest
// response, a general query a general response).
func (a *Advertiser) HandleDatagram(d *ipxproto.Datagram) {
	if d == nil {
		return
	}
	q, err := ncpproto.UnmarshalSAPQuery(d.Payload)
	if err != nil {
		return
	}
	var op uint16
	switch q.Operation {
	case ncpproto.SAPNearestQuery:
		op = ncpproto.SAPNearestResponse
	case ncpproto.SAPGeneralQuery:
		op = ncpproto.SAPGeneralResponse
	default:
		return // not a query we answer
	}
	entries := a.matching(q.ServiceType)
	if len(entries) == 0 {
		return
	}
	payload := ncpproto.MarshalResponse(op, entries, nil)
	_ = a.sender.Send(&ipxproto.Datagram{
		Type:    0x04,
		DstNet:  d.SrcNet,
		DstNode: d.SrcNode,
		DstSock: d.SrcSock,
		SrcSock: ncpproto.SAPSocket,
		Payload: payload,
	})
}
