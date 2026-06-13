package netbios

// nbipx.go is the core NBIPX (NetBIOS-over-IPX, "NWLink") session engine: the
// IPX parallel of the NBF engine in nbf.go. It turns the NB-IPX session-protocol
// exchange — SESSION_INIT → SESSION_CONFIRM, then DATA_FIRST_MIDDLE/DATA_ONLY_LAST
// segments reassembled into whole SMB messages, then SESSION_END → SESSION_END_ACK
// — into the same upper-layer SessionConsumer (SMB) seam, sending each response
// back as NB-IPX DATA frames. It is the core re-home of the legacy
// service/netbios/over_ipx transport's session half (its handlePEP path), stripped
// of netlog and the router/SAP imports: it talks to the world only through the
// DatagramSender seam (the core/router/ipx mini-router satisfies it) and the
// SessionConsumer seam (the SMB command engine satisfies it). It holds no
// link-layer or storage knowledge.
//
// Ring: CORE (stdlib only, reflection-free). The NB-IPX wire codec is
// core/protocol/netbios (NBIPXSessionHeader); this engine is the state machine
// over it. The name-service / NMPI name-query / mailslot-datagram paths the legacy
// transport also carried are name-layer and datagram concerns, not the session
// data path SMB rides — they are out of scope for this engine.
//
// Scope: the responder (listen) side — accept an inbound SESSION_INIT, carry SMB
// over the circuit, tear it down on SESSION_END. The caller side and the WAN
// router-list / retransmit machinery are not needed by a listening file server.

import (
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/core/log"
	ipxproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx"
	protocol "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbios"
)

// ipxDatagramType aliases the IPX datagram the mini-router hands the engine, so
// the exported IPXEngine method signature matches the core/router/ipx
// SocketHandler interface exactly (HandleDatagram(*ipxproto.Datagram)) without
// restating the import path.
type ipxDatagramType = ipxproto.Datagram

// NBIPXSessionSocket is the IPX socket NB-IPX session traffic uses (0x0455).
// Compose registers the IPXEngine as the core/router/ipx SocketHandler for this
// socket; it is the one source of truth for where the engine listens.
var NBIPXSessionSocket = [2]byte{0x04, 0x55}

// DatagramSender is the IPX datagram egress the NBIPX session engine drives: fill
// source addressing and write one datagram. The core/router/ipx mini-router's
// Send(*ipxproto.Datagram) satisfies it exactly, so compose registers the engine
// on the mini-router (as a SocketHandler on socket 0x0455) and hands it the router
// as the sender. The engine never imports the mini-router or a port — only this
// seam.
type DatagramSender interface {
	Send(d *ipxproto.Datagram) error
}

// ipxCircuitKey identifies an NB-IPX virtual circuit by the peer's IPX address
// (network+node+socket) plus the remote connection ID it stamped as SourceConnID.
// The tuple is unique per circuit and lets the engine route DATA/END frames to the
// right reassembly buffer and upper-layer conn.
type ipxCircuitKey struct {
	net    [4]byte
	node   [6]byte
	sock   [2]byte
	remote uint16 // peer's SourceConnID
}

// ipxCircuit is one NB-IPX virtual circuit: the peer address, the local/remote
// connection IDs exchanged at SESSION_INIT, the partial-message reassembly buffer
// (DATA_FIRST_MIDDLE accumulates here until DATA_ONLY_LAST/EOM completes it), and
// the upper-layer SessionCircuit the reassembled SMB messages are served to.
type ipxCircuit struct {
	net      [4]byte
	node     [6]byte
	sock     [2]byte
	localID  uint16
	remoteID uint16

	frag []byte         // accumulated DATA_FIRST_MIDDLE payload
	conn SessionCircuit // SMB virtual circuit (nil until consumer opens one)
}

// ipxSessionEngine is the NB-IPX responder state machine. It owns the open
// circuits, hands out local connection IDs, and routes reassembled messages to the
// consumer. Safe for concurrent inbound datagrams (the mini-router may deliver from
// the port read loop).
type ipxSessionEngine struct {
	logger   log.Logger
	sender   DatagramSender
	consumer func() SessionConsumer // late-bound: the service installs it after wiring

	mu       sync.Mutex
	circuits map[ipxCircuitKey]*ipxCircuit
	nextID   uint16
}

// newIPXSessionEngine builds an NB-IPX session engine. consumer is a callback so
// the engine reads the live consumer the service owns (it can be set after the
// engine is constructed, e.g. SMB attaches late). NB-IPX answers NAME_QUERY at the
// name layer, not here, so this engine takes no names callback.
func newIPXSessionEngine(logger log.Logger, sender DatagramSender, consumer func() SessionConsumer) *ipxSessionEngine {
	return &ipxSessionEngine{
		logger:   logger,
		sender:   sender,
		consumer: consumer,
		circuits: make(map[ipxCircuitKey]*ipxCircuit),
	}
}

// allocLocalIDLocked hands out the next non-zero local connection ID. ID 0 means
// "no connection" on the wire, so the allocator skips it on wrap. Caller holds mu.
func (e *ipxSessionEngine) allocLocalIDLocked() uint16 {
	e.nextID++
	if e.nextID == 0 {
		e.nextID++
	}
	return e.nextID
}

// HandleDatagram is the core/router/ipx mini-router SocketHandler entry point: an
// IPX datagram delivered to the NB-IPX session socket. The engine handles only the
// PEP (type 4) session family — SESSION_INIT/END and DATA frames carrying the
// 16-byte NB-IPX session header — and ignores everything else (name service,
// mailslot datagrams) the name/datagram layers own.
func (e *ipxSessionEngine) HandleDatagram(d *ipxproto.Datagram) {
	if d == nil || d.Type != protocol.IPXTypePEP {
		return
	}
	hdr, err := protocol.DecodeSessionHeader(d.Payload)
	if err != nil {
		return
	}
	switch hdr.DataStreamType {
	case protocol.NBIPXSessionInit:
		e.handleSessionInit(d, hdr)
	case protocol.NBIPXSessionEnd:
		e.handleSessionEnd(d, hdr)
	case protocol.NBIPXDataFirstMiddle, protocol.NBIPXDataOnlyLast:
		e.handleData(d, hdr)
	case protocol.NBIPXDataAck:
		// our-data acknowledgement: nothing to do.
	}
}

// keyFor builds the circuit key from an inbound datagram + its session header. The
// remote's SourceConnID identifies the circuit within the peer's address.
func keyFor(d *ipxproto.Datagram, hdr *protocol.NBIPXSessionHeader) ipxCircuitKey {
	return ipxCircuitKey{net: d.SrcNet, node: d.SrcNode, sock: d.SrcSock, remote: hdr.SourceConnID}
}

// handleSessionInit completes establishment: allocate a local connection ID, open
// the circuit keyed by the peer's address + SourceConnID, and reply SESSION_CONFIRM
// carrying our ID. A repeated INIT for an existing circuit re-confirms with the
// same local ID (idempotent retransmit handling). The circuit is now ready to carry
// SMB messages.
func (e *ipxSessionEngine) handleSessionInit(d *ipxproto.Datagram, hdr *protocol.NBIPXSessionHeader) {
	key := keyFor(d, hdr)
	e.mu.Lock()
	c := e.circuits[key]
	if c == nil {
		c = &ipxCircuit{
			net:      d.SrcNet,
			node:     d.SrcNode,
			sock:     d.SrcSock,
			localID:  e.allocLocalIDLocked(),
			remoteID: hdr.SourceConnID,
		}
		e.circuits[key] = c
	}
	localID := c.localID
	e.mu.Unlock()

	e.sendControl(d, hdr, localID, protocol.NBIPXSessionConfirm)
	e.logf("NBIPX circuit established")
}

// handleSessionEnd tears down a circuit: close its SMB conn (releasing handles),
// drop it, and acknowledge with SESSION_END_ACK. A duplicate SESSION_END still
// re-ACKs (the peer may have lost the first ACK) but closes nothing twice.
func (e *ipxSessionEngine) handleSessionEnd(d *ipxproto.Datagram, hdr *protocol.NBIPXSessionHeader) {
	key := keyFor(d, hdr)
	e.mu.Lock()
	c := e.circuits[key]
	var localID uint16
	if c != nil {
		localID = c.localID
		delete(e.circuits, key)
	}
	e.mu.Unlock()

	if c != nil && c.conn != nil {
		c.conn.Close()
	}
	e.sendControl(d, hdr, localID, protocol.NBIPXSessionEndAck)
}

// handleData reassembles an SMB message: DATA_FIRST_MIDDLE (and any DATA_ONLY_LAST
// without the EOM flag) buffers; DATA_ONLY_LAST with EOM completes it. On a
// complete message the engine opens the SMB circuit lazily, serves the message, and
// sends the response back as DATA frames. A message on a circuit with no consumer
// is dropped.
func (e *ipxSessionEngine) handleData(d *ipxproto.Datagram, hdr *protocol.NBIPXSessionHeader) {
	if len(d.Payload) < protocol.NBIPXSessionHeaderLen+int(hdr.DataLen) {
		return
	}
	body := d.Payload[protocol.NBIPXSessionHeaderLen : protocol.NBIPXSessionHeaderLen+int(hdr.DataLen)]

	key := keyFor(d, hdr)
	eom := hdr.DataStreamType == protocol.NBIPXDataOnlyLast || hdr.ConnCtrlFlag&protocol.NBIPXConnFlagEOM != 0

	e.mu.Lock()
	c := e.circuits[key]
	if c == nil {
		e.mu.Unlock()
		return
	}
	if !eom {
		c.frag = append(c.frag, body...)
		e.mu.Unlock()
		return
	}
	var msg []byte
	if len(c.frag) > 0 {
		msg = append(c.frag, body...)
		c.frag = nil
	} else {
		msg = append([]byte(nil), body...)
	}
	// Open the SMB circuit lazily on the first message so a circuit that never
	// carries data costs no consumer state.
	if c.conn == nil {
		if consumer := e.consumer(); consumer != nil {
			c.conn = consumer.NewConn()
		}
	}
	conn := c.conn
	localID := c.localID
	e.mu.Unlock()

	if conn == nil {
		return // no consumer wired — message dropped
	}
	resp := conn.ServeMessage(msg)
	if len(resp) == 0 {
		return // silent-drop command
	}
	e.sendData(d, hdr, localID, resp)
}

// sendControl emits an NB-IPX session-control packet (SESSION_CONFIRM /
// SESSION_END_ACK) back to the peer, mirroring the connection IDs (our localID as
// SourceConnID, the peer's as DestConnID) and reflecting the IPX addressing.
func (e *ipxSessionEngine) sendControl(in *ipxproto.Datagram, inHdr *protocol.NBIPXSessionHeader, localID uint16, streamType uint8) {
	h := &protocol.NBIPXSessionHeader{
		ConnCtrlFlag:   protocol.NBIPXConnFlagSYS,
		DataStreamType: streamType,
		SourceConnID:   localID,
		DestConnID:     inHdr.SourceConnID,
		SendSeq:        inHdr.SendSeq,
		ConnCtrlByte:   inHdr.ConnCtrlByte,
	}
	e.send(in, protocol.EncodeSessionHeader(h), "session-control")
}

// sendData sends a reassembled response back as one EOM-flagged DATA_ONLY_LAST
// frame (a file-server reply fits the IPX datagram; the legacy transport replied
// the same way). An empty payload still sends one DATA_ONLY_LAST so an empty SMB
// reply is framed.
func (e *ipxSessionEngine) sendData(in *ipxproto.Datagram, inHdr *protocol.NBIPXSessionHeader, localID uint16, payload []byte) {
	h := &protocol.NBIPXSessionHeader{
		ConnCtrlFlag:   protocol.NBIPXConnFlagEOM,
		DataStreamType: protocol.NBIPXDataOnlyLast,
		SourceConnID:   localID,
		DestConnID:     inHdr.SourceConnID,
		SendSeq:        inHdr.SendSeq,
		TotalDataLen:   uint16(len(payload)),
		DataLen:        uint16(len(payload)),
		ConnCtrlByte:   inHdr.ConnCtrlByte,
	}
	e.send(in, append(protocol.EncodeSessionHeader(h), payload...), "session-send")
}

// send writes an NB-IPX PEP datagram (type 4) back to the inbound peer, swapping
// the source/destination IPX sockets, through the sender. A nil sender (engine not
// wired to a router) drops the datagram.
func (e *ipxSessionEngine) send(in *ipxproto.Datagram, body []byte, reason string) {
	if e.sender == nil {
		return
	}
	out := &ipxproto.Datagram{
		Type:    protocol.IPXTypePEP,
		DstNet:  in.SrcNet,
		DstNode: in.SrcNode,
		DstSock: in.SrcSock,
		SrcSock: in.DstSock,
		Payload: body,
	}
	if err := e.sender.Send(out); err != nil {
		e.logf("NBIPX send failed: " + reason)
	}
}

// closeAll tears down every circuit (called when the service stops), closing the
// SMB conns so no file handles leak.
func (e *ipxSessionEngine) closeAll() {
	e.mu.Lock()
	conns := make([]SessionCircuit, 0, len(e.circuits))
	for _, c := range e.circuits {
		if c.conn != nil {
			conns = append(conns, c.conn)
		}
	}
	e.circuits = make(map[ipxCircuitKey]*ipxCircuit)
	e.mu.Unlock()
	for _, conn := range conns {
		conn.Close()
	}
}

// logf emits one info line through the logger if configured.
func (e *ipxSessionEngine) logf(msg string) {
	if e.logger == nil || !e.logger.Enabled(log.Info) {
		return
	}
	e.logger.Log1(log.Info, msg, log.Str("scope", Name))
}
