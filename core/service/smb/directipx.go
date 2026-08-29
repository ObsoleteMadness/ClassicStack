package smb

// directipx.go is the SMB direct-hosted-over-IPX transport: Microsoft "NWLink
// direct host" — SMB framed straight onto IPX with NO NetBIOS name/session layer
// (contrast NBIPX, which rides the NetBIOS session engine on socket 0x0455). It
// listens on IPX socket 0x0550, type 4 (PEP); each datagram carries one whole SMB
// message (connectionless — the IPX datagram is the framing, no reassembly), and
// the transport drives the SAME transport-agnostic SMB SessionConsumer seam
// (NewConn/ServeMessage/Close, conn.go) that NBF/NBIPX/NBT use. It is the core
// re-home of the legacy service/smb/over_ipx_direct transport, stripped of the
// netbios SessionContext coupling and the encoding/binary import.
//
// Ring: CORE (stdlib only, reflection-free, no net). It reaches the IPX wire only
// through the DirectIPXSender seam (the core/router/ipx mini-router's Send
// satisfies it structurally), so SMB never imports the mini-router or a port — the
// same acyclicity discipline as the NetBIOS engines.
//
// Connection model ([MS-CIFS] §2.2.1.6.4): the server allocates a Connection ID
// (CID) on the client's NEGOTIATE, keyed by the remote IPX endpoint
// (network+node), and stamps it into the SMB header SecurityFeatures field
// (bytes 18-19) of every response; the client echoes it on later messages. There
// is no explicit session teardown on the wire, so a circuit lives as long as its
// remote endpoint is seen — the Conn per endpoint holds the smbSession (UID/TID/
// FID) across messages exactly like a NetBIOS circuit.

import (
	"sync"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
	ipxproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx"
	nbproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbios"
	protocol "github.com/ObsoleteMadness/ClassicStack/core/protocol/smb"
)

// DirectSMBSocket is the IPX socket SMB direct-hosting listens on (0x0550). Compose
// registers the transport as the core/router/ipx SocketHandler for this socket. The
// value is the shared NB-IPX server socket (core/protocol/netbios): direct-hosted SMB
// and the NB-IPX name claim use the same well-known socket, and the direct-hosted
// CLIENT (client/smb/ipx.go) addresses it from the same definition.
var DirectSMBSocket = nbproto.NBIPXServerSocket

// SMB header fields the connectionless framing reads and writes ([MS-CIFS] §2.2.3.1)
// come from core/protocol/smb — the command byte (MessageCommand), the FLAGS reply
// bit (IsResponseMessage), the WCT offset (WordCountOffset) and the CID /
// SequenceNumber inside SecurityFeatures (ConnectionlessCIDOffset /
// StampConnectionless). They used to be private copies HERE and in the client, and
// the two drifted — the client never wrote SequenceNumber at all.
const (
	smbCIDOffset      = protocol.ConnectionlessCIDOffset
	smbWordCountStart = protocol.WordCountOffset
)

// directIPXClientLabel formats a remote IPX node as the "xx:xx:xx:xx:xx:xx.0550"
// client label the management view groups sessions under (the .0550 socket suffix
// marks the direct-hosted transport). Reflection-free (no fmt), core-ring safe.
func directIPXClientLabel(node [6]byte) string {
	const hexdigits = "0123456789abcdef"
	b := make([]byte, 0, 6*3+4)
	for i, x := range node {
		if i > 0 {
			b = append(b, ':')
		}
		b = append(b, hexdigits[x>>4], hexdigits[x&0x0f])
	}
	b = append(b, '.', '0', '5', '5', '0')
	return string(b)
}

// DirectIPXSender is the IPX datagram egress the transport drives: fill source
// addressing and write one datagram. The core/router/ipx mini-router's
// Send(*ipxproto.Datagram) satisfies it exactly, so compose registers the
// transport on the mini-router (SocketHandler on 0x0550) and hands it the router
// as the sender. The transport never imports the mini-router — only this seam.
type DirectIPXSender interface {
	Send(d *ipxproto.Datagram) error
}

// directIPXEndpoint keys a circuit by the remote IPX address (network+node). The
// socket is fixed (0x0550) so it is not part of the key.
type directIPXEndpoint struct {
	net  [4]byte
	node [6]byte
}

// DirectIPX is the SMB direct-hosting-over-IPX transport. It owns one SMB circuit
// (Conn) per remote endpoint plus that endpoint's server-assigned CID, and routes
// each inbound SMB message to the circuit. Safe for concurrent inbound datagrams.
type DirectIPX struct {
	svc    *Service
	sender DirectIPXSender

	mu      sync.Mutex
	conns   map[directIPXEndpoint]*directIPXConn
	nextCID uint16
}

// directIPXConn is one remote endpoint's state: the server-assigned CID and the
// SMB circuit carrying its UID/TID/FID across messages.
type directIPXConn struct {
	cid  uint16
	conn *Conn
}

// NewDirectIPX builds the transport bound to svc (the SMB command core) and sender
// (the IPX mini-router). Compose registers the returned transport on the mini-
// router as the SocketHandler for DirectSMBSocket.
func (s *Service) NewDirectIPX(sender DirectIPXSender) *DirectIPX {
	t := &DirectIPX{
		svc:     s,
		sender:  sender,
		conns:   make(map[directIPXEndpoint]*directIPXConn),
		nextCID: 1, // 0x0000 and 0xFFFF reserved.
	}
	s.mu.Lock()
	s.closers = append(s.closers, t)
	s.mu.Unlock()
	return t
}

// HandleDatagram is the core/router/ipx mini-router SocketHandler entry point: an
// IPX datagram delivered to the direct-SMB socket. It accepts only PEP (type 4)
// datagrams carrying a whole SMB request (\xffSMB, response bit clear), dispatches
// it through the endpoint's circuit, and sends the response back stamped with the
// CID and the request's SequenceNumber.
func (t *DirectIPX) HandleDatagram(d *ipxproto.Datagram) {
	if d == nil || d.Type != ipxproto.TypePEP {
		return
	}
	msg := d.Payload
	if !protocol.HasProtocolID(msg) {
		return
	}
	// Drop SMB responses arriving on ingress — only requests are dispatched.
	if protocol.IsResponseMessage(msg) {
		return
	}

	allocate := protocol.MessageCommand(msg) == protocol.CommandNegotiate
	if allocate {
		// A real NWLink redirector appends [SOURCE][DESTINATION] NetBIOS names after
		// the NEGOTIATE message, outside ByteCount — the only naming this
		// session-layer-less transport has (see protocol.AppendNameTrailer). Strip it
		// so the command core sees a plain SMB message; the endpoint address, not the
		// name, keys the circuit, so the names are informational here.
		msg, _, _, _ = protocol.SplitNameTrailer(msg)
	}
	conn, cid := t.connFor(d.SrcNet, d.SrcNode, allocate)

	resp := conn.ServeMessage(msg)
	if len(resp) == 0 {
		return
	}

	// SMB_COM_ECHO may request multiple responses; every other command sends one.
	count := echoResponseCount(msg, resp)
	if count <= 1 {
		t.sendResponse(d, resp, msg, cid)
		return
	}
	for seq := uint16(1); seq <= count; seq++ {
		out := append([]byte(nil), resp...)
		// ECHO response Words carries SequenceNumber at WCT+1 (one word).
		if len(out) >= smbWordCountStart+3 {
			bp.PutLE16(out[smbWordCountStart+1:smbWordCountStart+3], seq)
		}
		t.sendResponse(d, out, msg, cid)
	}
}

// connFor returns the circuit + CID for a remote endpoint, opening the circuit and
// (when allocate) assigning a CID on the first NEGOTIATE. A non-NEGOTIATE message
// from an unknown endpoint still opens a circuit (CID 0) so a mid-stream client is
// not dropped — the legacy transport did the same.
func (t *DirectIPX) connFor(network [4]byte, node [6]byte, allocate bool) (*Conn, uint16) {
	key := directIPXEndpoint{net: network, node: node}
	t.mu.Lock()
	defer t.mu.Unlock()
	c := t.conns[key]
	if c == nil {
		c = &directIPXConn{conn: t.svc.NewConn(directIPXClientLabel(node))}
		if allocate {
			c.cid = t.allocCIDLocked()
		}
		t.conns[key] = c
		// Install the server-push writer for asynchronous completions
		// (NOTIFY_CHANGE): stamp the circuit's CID and send to the retained peer
		// address. The CID is read live from the circuit at push time (a NEGOTIATE
		// after a non-NEGOTIATE first message may assign it later).
		pushKey := key
		c.conn.SetPushWriter(func(frame []byte) {
			t.pushResponse(pushKey, frame)
		})
	} else if allocate && c.cid == 0 {
		c.cid = t.allocCIDLocked()
	}
	return c.conn, c.cid
}

// pushResponse sends a server-initiated SMB frame (an asynchronous NOTIFY_CHANGE
// completion, §10d) to a circuit's peer, stamping the circuit's CID. There is no
// request to echo a SequenceNumber from, so it is left zero. Drops cleanly if the
// circuit closed or no sender is wired.
func (t *DirectIPX) pushResponse(key directIPXEndpoint, frame []byte) {
	t.mu.Lock()
	c := t.conns[key]
	var cid uint16
	if c != nil {
		cid = c.cid
	}
	sender := t.sender
	t.mu.Unlock()
	if c == nil || sender == nil {
		return
	}
	payload := append([]byte(nil), frame...)
	if len(payload) >= smbWordCountStart {
		bp.PutLE16(payload[smbCIDOffset:smbCIDOffset+2], cid)
	}
	_ = sender.Send(&ipxproto.Datagram{
		Type:    ipxproto.TypePEP,
		DstNet:  key.net,
		DstNode: key.node,
		DstSock: DirectSMBSocket,
		SrcSock: DirectSMBSocket,
		Payload: payload,
	})
}

// allocCIDLocked hands out the next CID, skipping the reserved 0x0000 and 0xFFFF.
// Caller holds t.mu.
func (t *DirectIPX) allocCIDLocked() uint16 {
	cid := t.nextCID
	t.nextCID++
	if t.nextCID == protocol.ConnectionlessCIDReserved {
		t.nextCID = 1
	}
	return cid
}

// sendResponse stamps the connectionless header (CID + echoed SequenceNumber) and
// writes the response datagram back to the requesting endpoint, swapping sockets.
func (t *DirectIPX) sendResponse(in *ipxproto.Datagram, resp, req []byte, cid uint16) {
	if t.sender == nil {
		return
	}
	payload := append([]byte(nil), resp...)
	stampConnectionless(payload, req, cid)
	_ = t.sender.Send(&ipxproto.Datagram{
		Type:    ipxproto.TypePEP,
		DstNet:  in.SrcNet,
		DstNode: in.SrcNode,
		DstSock: in.SrcSock,
		SrcSock: in.DstSock,
		Payload: payload,
	})
}

// closeCircuits drops every circuit, closing its SMB conn so no file handles leak.
// Called from the SMB service's Stop (the service tracks the transport as a
// circuitCloser). Idempotent.
func (t *DirectIPX) closeCircuits() {
	t.mu.Lock()
	conns := make([]*Conn, 0, len(t.conns))
	for _, c := range t.conns {
		conns = append(conns, c.conn)
	}
	t.conns = make(map[directIPXEndpoint]*directIPXConn)
	t.mu.Unlock()
	for _, c := range conns {
		c.Close()
	}
}

// stampConnectionless writes the CID and the request's SequenceNumber into the
// response SMB header SecurityFeatures field ([MS-CIFS] §2.2.3.1), as required for
// connectionless transports. A non-reserved CID the client already carries wins
// over the freshly-allocated one; the Key field (bytes 14-17) stays zero (no
// connection-level signing over IPX).
func stampConnectionless(resp, req []byte, cid uint16) {
	if len(resp) < smbWordCountStart || len(req) < smbWordCountStart {
		return
	}
	if reqCID := protocol.ConnectionlessCID(req); reqCID != 0 && reqCID != protocol.ConnectionlessCIDReserved {
		cid = reqCID
	}
	// Echo the request's SequenceNumber back verbatim.
	protocol.StampConnectionless(resp, cid, protocol.ConnectionlessSequence(req))
}

// echoResponseCount returns the number of responses an SMB_COM_ECHO exchange wants
// (the EchoCount the request carries), or 1 for any non-ECHO or unsuccessful
// exchange. Multi-response applies only to a successful single-word ECHO.
func echoResponseCount(req, resp []byte) uint16 {
	if len(req) < smbWordCountStart+3 || len(resp) < smbWordCountStart+1 {
		return 1
	}
	if protocol.MessageCommand(req) != protocol.CommandEcho || protocol.MessageCommand(resp) != protocol.CommandEcho {
		return 1
	}
	if protocol.MessageStatus(resp) != protocol.StatusSuccess {
		return 1
	}
	if req[smbWordCountStart] != 1 || resp[smbWordCountStart] != 1 {
		return 1
	}
	c := bp.LE16(req[smbWordCountStart+1 : smbWordCountStart+3])
	if c == 0 {
		return 1
	}
	return c
}
