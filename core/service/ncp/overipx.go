package ncp

// overipx.go is the NCP-over-IPX transport: NetWare Core Protocol framed straight
// onto IPX (connectionless — one IPX datagram carries one whole NCP request, no
// reassembly). It listens on IPX socket 0x0451, the well-known NCP socket NETx /
// VLM / Client32 send to. It is the structural twin of the SMB direct-hosted-over-
// IPX transport (core/service/smb/directipx.go): it drives the transport-agnostic
// NCP command engine (Conn.ServeRequest, dispatch.go) and reaches the IPX wire only
// through the IPXSender seam, so this package never imports the mini-router or a
// port (the same acyclicity discipline as SMB's direct-IPX).
//
// Connection model: the client's first packet is a create-connection (type
// 0x1111); the server allocates a numbered service connection keyed by the client's
// IPX endpoint (network+node) and echoes the number in the reply header. Subsequent
// requests (type 0x2222) carry that number; the transport finds the connection
// from the header (falling back to the endpoint) and routes the request to its
// circuit. A destroy-connection (type 0x5555) tears the connection down.
//
// Reference: Novell NCP over IPX (socket 0x0451); mars_nwe / ncpfs (CLAUDE.md #7).

import (
	"sync"

	ipxproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx"
	ncpproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ncp"
)

// ipxNCPType is the IPX packet type NCP rides (0x11, NCP). NetWare clients send NCP
// as type 0x11; some send type 0 — the transport accepts either.
const ipxNCPType uint8 = 0x11

// IPXSender is the IPX datagram egress the transport drives: fill source addressing
// and write one datagram. The core/router/ipx mini-router's Send satisfies it
// exactly, so compose registers the transport on the mini-router (SocketHandler on
// 0x0451) and hands it the router as the sender. The transport never imports the
// mini-router — only this seam.
type IPXSender interface {
	Send(d *ipxproto.Datagram) error
}

// OverIPX is the NCP-over-IPX transport. It owns one NCP circuit (Conn) per remote
// endpoint — bound to that endpoint's service connection — and routes each inbound
// NCP request to the circuit. Safe for concurrent inbound datagrams.
type OverIPX struct {
	svc    *Service
	sender IPXSender

	mu    sync.Mutex
	conns map[endpoint]*Conn

	// sap is the optional SAP advertiser handle (set by SetSAP); it makes the server
	// discoverable. Held here so Props can report SAP state and Stop can halt it. It
	// is the SHARED core/service/sap advertiser (one per runtime, on socket 0x0452),
	// through which NCP and NB-IPX both advertise — the transport keeps only the handle
	// so it never owns the SAP socket.
	sap sapHandle
}

// sapHandle is the minimal SAP-advertiser surface the NCP transport needs: stop it on
// teardown. The shared core/service/sap.Advertiser satisfies it (Stop). Keeping it an
// interface lets the transport hold the shared advertiser without importing it.
type sapHandle interface {
	Stop()
}

// NewOverIPX builds the transport bound to svc (the NCP command core) and sender
// (the IPX mini-router). Compose registers the returned transport on the mini-
// router as the SocketHandler for ncpproto.NCPSocket and registers it for teardown.
func (s *Service) NewOverIPX(sender IPXSender) *OverIPX {
	t := &OverIPX{
		svc:    s,
		sender: sender,
		conns:  make(map[endpoint]*Conn),
	}
	s.mu.Lock()
	s.closers = append(s.closers, t)
	s.mu.Unlock()
	return t
}

// HandleDatagram is the core/router/ipx mini-router SocketHandler entry point: an
// IPX datagram delivered to the NCP socket. It decodes the NCP request, dispatches
// the framing verb (create/destroy connection) or routes an ordinary request to the
// endpoint's circuit, and sends the reply back stamped with the connection number.
func (t *OverIPX) HandleDatagram(d *ipxproto.Datagram) {
	if d == nil {
		return
	}
	if d.Type != ipxNCPType && d.Type != 0 {
		return
	}
	t.svc.observeRX(len(d.Payload))

	req, err := ncpproto.UnmarshalRequest(d.Payload)
	if err != nil {
		t.svc.counters.decodeErrors.Add(1)
		return
	}

	ep := endpoint{net: d.SrcNet, node: d.SrcNode}
	switch req.Type {
	case ncpproto.TypeCreateConnection:
		t.handleCreate(d, ep, req)
	case ncpproto.TypeDestroyConnection:
		t.handleDestroy(d, ep, req)
	case ncpproto.TypeRequest:
		t.handleRequest(d, ep, req)
	default:
		// TypeReply / TypeBurst / unknown verbs are not server-handled.
	}
}

// handleCreate allocates (or reuses) the endpoint's service connection and replies
// with the assigned number.
func (t *OverIPX) handleCreate(d *ipxproto.Datagram, ep endpoint, req *ncpproto.RequestHeader) {
	c, ok := t.svc.conns.Create(ep.net, ep.node)
	if !ok {
		// Connection cap reached: reply with an error completion and conn 0.
		t.reply(d, ncpproto.Reply(req, 0, ncpproto.CompletionInvalidConn), nil)
		return
	}
	t.mu.Lock()
	t.conns[ep] = t.svc.NewConn(c)
	t.mu.Unlock()
	t.svc.pushStats()

	r := ncpproto.Reply(req, c.number, ncpproto.CompletionSuccess)
	r.Type = ncpproto.TypeReply
	t.reply(d, r, nil)
}

// handleDestroy tears down the endpoint's service connection, closing any open file
// handles, and replies success.
func (t *OverIPX) handleDestroy(d *ipxproto.Datagram, ep endpoint, req *ncpproto.RequestHeader) {
	num := req.ConnectionNumber()
	if c, ok := t.svc.conns.Destroy(num); ok {
		closeConnFiles(c)
	}
	t.mu.Lock()
	delete(t.conns, ep)
	t.mu.Unlock()
	t.svc.pushStats()
	t.reply(d, ncpproto.Reply(req, num, ncpproto.CompletionSuccess), nil)
}

// handleRequest routes an ordinary NCP request to the endpoint's circuit and sends
// the reply. A request from an unknown endpoint (no prior create-connection) is
// answered with a not-logged-in completion.
func (t *OverIPX) handleRequest(d *ipxproto.Datagram, ep endpoint, req *ncpproto.RequestHeader) {
	t.mu.Lock()
	cn := t.conns[ep]
	t.mu.Unlock()
	if cn == nil {
		t.reply(d, ncpproto.Reply(req, req.ConnectionNumber(), ncpproto.CompletionInvalidConn), nil)
		return
	}
	completion, body := cn.ServeRequest(req)
	t.reply(d, ncpproto.Reply(req, req.ConnectionNumber(), completion), body)
}

// reply marshals the NCP reply header + body and sends it back to the datagram's
// source endpoint on the NCP socket.
func (t *OverIPX) reply(in *ipxproto.Datagram, hdr ncpproto.ReplyHeader, body []byte) {
	payload := hdr.Marshal(make([]byte, 0, ncpproto.ReplyHeaderLen+len(body)))
	payload = append(payload, body...)

	out := &ipxproto.Datagram{
		Type:    ipxNCPType,
		DstNet:  in.SrcNet,
		DstNode: in.SrcNode,
		DstSock: in.SrcSock,
		SrcSock: ncpproto.NCPSocket,
		Payload: payload,
	}
	if err := t.sender.Send(out); err != nil {
		return
	}
	t.svc.observeTX(len(payload))
}

// closeConnFiles closes every open file handle on a torn-down connection so no seam
// handle leaks.
func closeConnFiles(c *connection) {
	c.mu.Lock()
	files := make([]*openFile, 0, len(c.files))
	for _, of := range c.files {
		files = append(files, of)
	}
	c.files = make(map[uint16]*openFile)
	c.mu.Unlock()
	for _, of := range files {
		if f, ok := of.handle.(interface{ Close() error }); ok {
			_ = f.Close()
		}
	}
}

// closeCircuits tears down every live circuit's connection on service Stop
// (circuitCloser). Open file handles are released so nothing leaks.
func (t *OverIPX) closeCircuits() {
	for _, c := range t.svc.conns.All() {
		closeConnFiles(c)
		t.svc.conns.Destroy(c.number)
	}
	t.mu.Lock()
	t.conns = make(map[endpoint]*Conn)
	sap := t.sap
	t.mu.Unlock()
	if sap != nil {
		sap.Stop()
	}
}

// SetSAP installs the shared SAP advertiser handle the transport reports/stops (set
// during compose cross-wiring). It enables the dashboard's "sap: advertising" prop;
// the shared advertiser (core/service/sap) does the periodic broadcast / query answers
// for this server's registered entry.
func (t *OverIPX) SetSAP(sap sapHandle) {
	t.mu.Lock()
	t.sap = sap
	t.mu.Unlock()
}

// advertising reports whether a SAP advertiser is installed and running
// (sapAdvertiserState, read by Service.Props).
func (t *OverIPX) advertising() bool {
	t.mu.Lock()
	sap := t.sap
	t.mu.Unlock()
	return sap != nil
}
