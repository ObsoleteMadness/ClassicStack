package ncp

// sap.go is the Service Advertising Protocol responder/broadcaster that makes the
// NCP file server discoverable: NETx / VLM issue a SAP "Get Nearest Server" query
// (or listen for periodic broadcasts) to find a server to attach to before they
// know its IPX address. The advertiser:
//
//   - answers SAP nearest-service (type 3) and general-service (type 1) queries
//     for the File Server type (0x0004) on IPX socket 0x0452, and
//   - broadcasts an unsolicited general-service response every sapInterval so a
//     client that missed the query/response handshake still learns the server.
//
// It rides the same core/router/ipx mini-router as the NCP transport: compose hands
// it the router as both the egress (IPXSender) and the identity source (the
// server's IPX network + node), then registers it on the SAP socket. It is the
// SAP analogue of the IPX diagnostic responder.
//
// Reference: Novell SAP (IPX socket 0x0452); mars_nwe / ncpfs (CLAUDE.md #7).

import (
	"sync"
	"time"

	ipxproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx"
	ncpproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ncp"
)

// sapInterval is how often the advertiser broadcasts an unsolicited SAP response.
// NetWare servers advertise every 60 seconds.
const sapInterval = 60 * time.Second

// sapAdvertiser broadcasts and answers SAP for the NCP file server. It is owned by
// the over-IPX transport (which holds it for Props/Stop) and built during compose
// cross-wiring.
type sapAdvertiser struct {
	svc    *Service
	sender IPXSender

	mu      sync.Mutex
	network [4]byte
	node    [6]byte
	stopCh  chan struct{}
	started bool
}

// NewSAP builds the SAP advertiser bound to the service (for the server name +
// counters) and the IPX egress. Compose calls it during cross-wiring, sets the IPX
// identity, hands it to the over-IPX transport (SetSAP), starts it, and registers
// it on the SAP socket. Exported so the SAP type stays unexported while compose can
// build one.
func (s *Service) NewSAP(sender IPXSender) *sapAdvertiser {
	return &sapAdvertiser{svc: s, sender: sender}
}

// Start begins the SAP broadcast loop (exported wrapper for compose).
func (a *sapAdvertiser) Start() { a.start() }

// SetIdentity sets the server's IPX network + node the advertisement carries (the
// address a client should contact). Compose reads these from the mini-router.
func (a *sapAdvertiser) SetIdentity(network [4]byte, node [6]byte) {
	a.mu.Lock()
	a.network = network
	a.node = node
	a.mu.Unlock()
}

// start begins the periodic broadcast loop. Idempotent.
func (a *sapAdvertiser) start() {
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

// stop halts the broadcast loop. Idempotent.
func (a *sapAdvertiser) stop() {
	a.mu.Lock()
	if !a.started {
		a.mu.Unlock()
		return
	}
	a.started = false
	close(a.stopCh)
	a.mu.Unlock()
}

// loop broadcasts an unsolicited general-service response every sapInterval until
// stopped.
func (a *sapAdvertiser) loop(stop chan struct{}) {
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

// entry builds the SAP service entry advertising this file server (type 0x0004,
// the server name, and the server's IPX net/node + NCP socket 0x0451).
func (a *sapAdvertiser) entry() ncpproto.SAPEntry {
	a.mu.Lock()
	net := a.network
	node := a.node
	a.mu.Unlock()
	return ncpproto.SAPEntry{
		Type:    ncpproto.SAPServerTypeFileServer,
		Name:    a.svc.serverName(),
		Network: net,
		Node:    node,
		Socket:  ncpproto.NCPSocket,
		Hops:    1,
	}
}

// broadcast sends an unsolicited SAP general-service response to the IPX broadcast
// address on the SAP socket.
func (a *sapAdvertiser) broadcast() {
	payload := ncpproto.MarshalResponse(ncpproto.SAPGeneralResponse, []ncpproto.SAPEntry{a.entry()}, nil)
	out := &ipxproto.Datagram{
		Type:    0x04, // PEP
		DstNet:  a.network,
		DstNode: [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
		DstSock: ncpproto.SAPSocket,
		SrcSock: ncpproto.SAPSocket,
		Payload: payload,
	}
	if err := a.sender.Send(out); err != nil {
		return
	}
	a.svc.counters.sapBroadcasts.Add(1)
}

// HandleDatagram is the core/router/ipx SocketHandler entry point for the SAP
// socket: a client query. It answers a file-server nearest/general query with a SAP
// response addressed back to the querier (nearest queries get a nearest response,
// general queries a general response).
func (a *sapAdvertiser) HandleDatagram(d *ipxproto.Datagram) {
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
	if !q.WantsType(ncpproto.SAPServerTypeFileServer) {
		return
	}
	payload := ncpproto.MarshalResponse(op, []ncpproto.SAPEntry{a.entry()}, nil)
	out := &ipxproto.Datagram{
		Type:    0x04,
		DstNet:  d.SrcNet,
		DstNode: d.SrcNode,
		DstSock: d.SrcSock,
		SrcSock: ncpproto.SAPSocket,
		Payload: payload,
	}
	_ = a.sender.Send(out)
}
