// Package ipxdiag is the IPX Diagnostic Responder (§observation, spec/errata.md): it
// answers Novell IPX/SPX Diagnostic requests on socket 0x0456 — the wire behind the
// IPXPING reachability tool — so a station probing the segment learns ClassicStack is
// alive. It is the IPX analogue of the AppleTalk Echo (AEP) responder: a tiny
// connectionless request→reply service with no per-peer state.
//
// It plugs into the core/router/ipx mini-router as the SocketHandler for socket
// 0x0456 and replies through the Sender seam (the mini-router's Send satisfies it
// structurally), so it never imports the mini-router or a port — the same acyclicity
// discipline as the NetBIOS engines and the direct-IPX transport.
//
// Ring: CORE (stdlib only, reflection-free, no net). The component lifecycle is a
// no-op: like the NetBIOS session engines, the IPX PORT owns Start/Stop, and this
// responder is wired onto the already-running mini-router during compose.
package ipxdiag

import (
	"context"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	ipxproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx/diag"
)

// Name is the component/section key for the IPX Diagnostic Responder.
const Name = "IPXDiag"

// ipxPEPType is the IPX packet-type (4, Packet Exchange Protocol) diagnostic traffic
// rides, matching NBIPX session traffic and direct-hosted SMB.
const ipxPEPType = ipxproto.TypePEP

// Sender is the IPX datagram egress the responder replies through: fill source
// addressing and write one datagram. The core/router/ipx mini-router's
// Send(*ipxproto.Datagram) satisfies it exactly, so compose registers the responder
// on the mini-router (SocketHandler on diag.Socket) and hands it the router as the
// sender. The responder never imports the mini-router — only this seam.
type Sender interface {
	Send(d *ipxproto.Datagram) error
}

// Responder answers IPX Diagnostic requests on socket 0x0456. It holds the egress
// sender and this station's own node ID (so it can stay silent when a broadcast
// request names itself in the exclusion list), and nothing else — every reply is a
// pure function of the request.
type Responder struct {
	logger log.Logger
	sender Sender
	node   [6]byte
}

// New builds a Diagnostic Responder that replies through sender. node is this
// station's IPX node ID (the interface MAC); a request whose exclusion list names it
// is answered with silence, matching the protocol's "do not re-collect known hosts".
func New(logger log.Logger, sender Sender, node [6]byte) *Responder {
	return &Responder{logger: logger, sender: sender, node: node}
}

// Name returns the component name.
func (r *Responder) Name() string { return Name }

// Start is a no-op: the IPX port owns the lifecycle; the responder is wired onto the
// running mini-router. Idempotent.
func (r *Responder) Start(context.Context) error { return nil }

// Stop is a no-op for the same reason. Idempotent.
func (r *Responder) Stop(context.Context) error { return nil }

// SetSender installs the egress seam late, for compose: the responder is built by
// the registry before the IPX mini-router exists (the router is stood up during the
// transport cross-wire), so the cross-wire injects the sender afterwards — mirroring
// how the browser's SetSink binds its mailslot router post-construction. A nil sender
// leaves the responder receive-only (it decodes but emits nothing). Set before the
// port carries traffic. Idempotent.
func (r *Responder) SetSender(sender Sender) { r.sender = sender }

// SetNode updates the station node ID used for the self-exclusion check. Compose
// calls it after the mini-router's identity is set (the MAC is resolved when the port
// opens). Safe before Start.
func (r *Responder) SetNode(node [6]byte) { r.node = node }

// HandleDatagram is the core/router/ipx mini-router SocketHandler entry point: an IPX
// datagram delivered to the Diagnostic socket. It decodes the request, stays silent
// if the request excludes our own node, and otherwise replies with the minimal
// reachability response (a single IPX-component record) to the requesting endpoint,
// swapping sockets.
func (r *Responder) HandleDatagram(d *ipxproto.Datagram) {
	if d == nil {
		return
	}
	req, err := diag.UnmarshalRequest(d.Payload)
	if err != nil {
		return
	}
	if req.Excludes(r.node) {
		return // the requester already knows us; do not re-announce
	}
	if r.sender == nil {
		return
	}
	resp := diag.SimpleResponse().Marshal()
	_ = r.sender.Send(&ipxproto.Datagram{
		Type:    ipxPEPType,
		DstNet:  d.SrcNet,
		DstNode: d.SrcNode,
		DstSock: d.SrcSock,
		SrcSock: diag.Socket,
		Payload: resp,
	})
	r.logf("answered IPX diagnostic request")
}

// logf emits one info line through the logger if configured.
func (r *Responder) logf(msg string) {
	if r.logger == nil || !r.logger.Enabled(log.Info) {
		return
	}
	r.logger.Log1(log.Info, msg, log.Str("scope", Name))
}

// compile-time assertion: the responder is a component.
var _ component.Component = (*Responder)(nil)
