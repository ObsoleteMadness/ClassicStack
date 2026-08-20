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
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/log"
	ipxproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx"
	protocol "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbios"
)

// ErrNameInUse is returned by ClaimName when another node on the segment already
// holds the name being claimed (a name-service conflict).
var ErrNameInUse = errors.New("netbios: NB-IPX name already in use on segment")

// ipxDatagramType aliases the IPX datagram the mini-router hands the engine, so
// the exported IPXEngine method signature matches the core/router/ipx
// SocketHandler interface exactly (HandleDatagram(*ipxproto.Datagram)) without
// restating the import path.
type ipxDatagramType = ipxproto.Datagram

// NB-IPX socket numbers, re-exported under the names compose registers the IPXEngine
// as the core/router/ipx SocketHandler on. The VALUES live in core/protocol/netbios —
// the client transports (client/smb, client/netbios) address the same sockets and used
// to carry their own literal copies, so the wire numbers are defined once in the
// protocol ring and named here.
var (
	NBIPXSessionSocket   = protocol.NBIPXSessionSocket
	NBIPXServerSocket    = protocol.NBIPXServerSocket
	NBIPXNameQuerySocket = protocol.NBIPXNameQuerySocket
	NBIPXDatagramSocket  = protocol.NBIPXDatagramSocket
	NBIPXNameSocket      = protocol.NBIPXNameSocket
)

// DatagramSender is the IPX datagram egress the NBIPX engine drives: fill source
// addressing and write one datagram, and report the router's own network/node so
// the engine can drop self-looped broadcasts and address directed replies. The
// core/router/ipx mini-router's Send/Network/Node satisfy it exactly, so compose
// registers the engine on the mini-router (as a SocketHandler) and hands it the
// router as the sender. The engine never imports the mini-router or a port — only
// this seam.
type DatagramSender interface {
	Send(d *ipxproto.Datagram) error
	Network() [4]byte
	Node() [6]byte
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

	// Sliding-window-of-one sequencing state (see the sequencing-rules ERRATA on
	// protocol.NBIPXSessionHeader). sendSeq is the SendSeq our NEXT data frame will
	// carry; recvSeq is the next SendSeq expected from the peer (the cumulative ack
	// we stamp as RecvSeq on everything we send). The client's SESSION_INITIALIZE
	// consumes its seq 0, so recvSeq starts at 1; our SYS accept consumes nothing,
	// so sendSeq starts at 0.
	sendSeq uint16
	recvSeq uint16

	// Retained last response message for retransmission: a peer SYS|RESEND (or a
	// duplicate of the request frame we already consumed) is answered by re-framing
	// lastResp from lastRespSeq without re-serving the SMB command.
	lastResp    []byte
	lastRespSeq uint16

	frag []byte         // accumulated DATA_FIRST_MIDDLE payload
	conn SessionCircuit // SMB virtual circuit (nil until consumer opens one)
}

// ipxSessionEngine is the NB-IPX responder state machine. It owns the open
// circuits, hands out local connection IDs, and routes reassembled messages to the
// consumer. Safe for concurrent inbound datagrams (the mini-router may deliver from
// the port read loop).
type ipxSessionEngine struct {
	logger    log.Logger
	sender    DatagramSender
	consumer  func() SessionConsumer  // late-bound: the service installs it after wiring
	dgram     func() DatagramConsumer // late-bound connectionless-datagram sink (browser)
	names     func() []protocol.Name  // local names, to answer the client's NB-IPX name query
	workgroup func() string           // configured workgroup, for the NAME_RECOGNIZED reply prefix

	mu       sync.Mutex
	circuits map[ipxCircuitKey]*ipxCircuit
	nextID   uint16

	// Name-claim state (only live during ClaimName). claiming is the name we are
	// broadcasting a claim for; a matching inbound name-service packet from another
	// node signals objection so the claim aborts. claimSelf is our own IPX node, so a
	// looped-back self-broadcast is not mistaken for a conflict. Guarded by claimMu.
	claimMu   sync.Mutex
	claiming  protocol.Name
	claimSelf [6]byte
	objection chan struct{}
}

// newIPXSessionEngine builds an NB-IPX session engine. consumer, dgram, names and
// workgroup are callbacks so the engine reads the live consumer / datagram sink /
// name set / workgroup the service owns (all can be set after the engine is
// constructed, e.g. SMB and the browser attach late). NB-IPX answers the client's
// name query (NMPI Query-name / NBIPX Find-name) from the name set here, stamping the
// NAME_RECOGNIZED reply with our own name + workgroup, and delivers inbound browser
// mailslot datagrams to the datagram consumer. A nil names callback answers no name
// query; a nil workgroup callback yields an empty (space-filled) workgroup; a nil
// dgram callback drops datagrams after decode.
func newIPXSessionEngine(logger log.Logger, sender DatagramSender, consumer func() SessionConsumer, dgram func() DatagramConsumer, names func() []protocol.Name, workgroup func() string) *ipxSessionEngine {
	return &ipxSessionEngine{
		logger:    logger,
		sender:    sender,
		consumer:  consumer,
		dgram:     dgram,
		names:     names,
		workgroup: workgroup,
		circuits:  make(map[ipxCircuitKey]*ipxCircuit),
	}
}

// ownsName reports whether requested matches one of our local NetBIOS names, so a
// name query for a foreign name is ignored (it is not addressed to us). A nil names
// callback owns nothing.
func (e *ipxSessionEngine) ownsName(requested protocol.Name) bool {
	if e.names == nil {
		return false
	}
	return slices.Contains(e.names(), requested)
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
// IPX datagram delivered to one of the NB-IPX sockets (0x0455 session, 0x0551 name
// query, 0x0553 datagram, 0x0554 name service). It dispatches by IPX packet-type
// and socket, mirroring the legacy over_ipx transport's HandleDatagram:
//
//   - NMPI packets (0x0551 name query / 0x0553 mailslot) — a Query-name for our
//     name is answered here, a MailslotSend (0xFC) is routed to the datagram
//     consumer (the browser).
//   - Type-20 (NetBIOS broadcast) name service — a name-claim conflict probe and
//     the NBIPX Find-name path.
//   - Type-4 (PEP) — the session family (SESSION_INIT/END and DATA frames) and the
//     raw directed datagram (NBIPXDirectedDatagram).
//
// A self-looped broadcast (our own network+node) is dropped so a name claim does
// not object to itself.
func (e *ipxSessionEngine) HandleDatagram(d *ipxproto.Datagram) {
	if d == nil {
		return
	}
	if e.sender != nil && d.SrcNet == e.sender.Network() && d.SrcNode == e.sender.Node() {
		return // our own looped-back broadcast
	}
	// NMPI on the name-query / datagram sockets: a Query-name (0xF3) answered here,
	// a MailslotSend (0xFC) routed to the datagram consumer.
	if d.DstSock == NBIPXNameQuerySocket || d.DstSock == NBIPXDatagramSocket {
		if e.handleNMPIPayload(d) {
			return
		}
	}
	switch d.Type {
	case protocol.IPXTypeNetBIOS:
		// A type-20 name-service packet: a Find-name (0x01) for one of our names is
		// answered with a Name-recognized (0x02) reply — the resolution path a WfW/
		// Win9x client that broadcasts on 0x0455 uses (as opposed to the NMPI Query
		// on 0x0551, above). A Name-recognized/Name-in-use naming a name we are
		// claiming signals a conflict that aborts the claim.
		e.handleNameService(d)
		return
	case protocol.IPXTypePEP:
		e.handlePEP(d)
	}
}

// handlePEP dispatches a PEP (type-4) NB-IPX packet: a raw directed datagram
// (NBIPXDirectedDatagram) on the datagram socket, or the session family on the
// session socket. Mirrors the legacy over_ipx handlePEP.
func (e *ipxSessionEngine) handlePEP(d *ipxproto.Datagram) {
	if len(d.Payload) < 2 {
		return
	}
	// A raw directed datagram on the datagram socket: a bare NetBIOS datagram
	// (dest name, source name, payload) tagged NBIPXDirectedDatagram, routed to the
	// consumer — the raw-datagram analogue of a mailslot send.
	if d.DstSock == NBIPXDatagramSocket && d.Payload[1] == protocol.NBIPXDirectedDatagram {
		e.deliverRawDatagram(d)
		return
	}
	if d.DstSock != NBIPXSessionSocket {
		return
	}
	hdr, err := protocol.DecodeSessionHeader(d.Payload)
	if err != nil {
		return
	}
	switch hdr.DataStreamType {
	case protocol.NBIPXSessionEnd:
		e.handleSessionEnd(d, hdr)
	case protocol.NBIPXSessionEndAck:
		// our SESSION.END was acknowledged: nothing to do.
	case protocol.NBIPXSessionData:
		// DATA (0x06) carries both session-establishment and SMB messages. A frame
		// whose DestConnID is the unassigned sentinel (0xFFFF, or 0 before a circuit
		// exists) is a NetBIOS session request; anything else is an SMB message on an
		// open circuit. (ERRATA captures/ipx.pcap: there is no distinct SESSION.INIT
		// stream type — establishment rides DATA with the 0xFFFF sentinel.)
		if hdr.DestConnID == protocol.NBIPXUnassignedConnID {
			e.handleSessionRequest(d, hdr)
			return
		}
		e.handleData(d, hdr)
	}
}

// keyFor builds the circuit key from an inbound datagram + its session header. The
// remote's SourceConnID identifies the circuit within the peer's address.
func keyFor(d *ipxproto.Datagram, hdr *protocol.NBIPXSessionHeader) ipxCircuitKey {
	return ipxCircuitKey{net: d.SrcNet, node: d.SrcNode, sock: d.SrcSock, remote: hdr.SourceConnID}
}

// handleSessionRequest completes NB-IPX session establishment. A client opens a
// circuit with a DATA frame whose DestConnID is the unassigned sentinel (0xFFFF),
// carrying a [called-name || calling-name || trailer] payload. The engine allocates
// a local connection ID, opens the circuit keyed by the peer's address +
// SourceConnID, and replies with a DATA frame that assigns our ID (SourceConnID) and
// echoes the client's (DestConnID), swapping the two names (the wire's session-accept
// form: [calling || called || trailer]). A request whose called-name is not one of
// ours is ignored (same rule as Find-name): a broadcast SESSION_INITIALIZE for a
// neighbour must not be stolen. A repeated request for a still-unused circuit
// re-accepts with the same local ID (idempotent retransmit handling). A request that
// collides with a circuit that has already carried data is a reconnect: the old
// circuit is torn down and a fresh one accepted (clients reuse SourceConnID 0x0001
// across Dial, and Close does not send SESSION_END).
func (e *ipxSessionEngine) handleSessionRequest(d *ipxproto.Datagram, hdr *protocol.NBIPXSessionHeader) {
	if len(d.Payload) < protocol.NBIPXSessionHeaderLen {
		return
	}
	// [SOURCE][DESTINATION][trailer] — see the ERRATA on protocol.NBIPXSessionRequest
	// for why the order is the caller first and what inverting it cost.
	req, err := protocol.DecodeSessionRequest(d.Payload[protocol.NBIPXSessionHeaderLen:])
	if err != nil {
		return
	}

	// A SESSION_INITIALIZE names the *called* server in its DESTINATION slot. An
	// in-process Finder client on this same pcap station used to have its WIN98-1
	// call accepted here (we ignored the called name), so NetShareEnum ran against
	// CLASSICSTACK and returned only IPC$ (captures/ipx.pcap frames 768–781).
	if !e.ownsName(req.Destination) {
		e.logf("NBIPX session-request ignored (not our name) " + req.Destination.String())
		return
	}

	key := keyFor(d, hdr)
	e.mu.Lock()
	c := e.circuits[key]
	var stale SessionCircuit
	if c != nil && ipxCircuitUsed(c) {
		stale = c.conn
		delete(e.circuits, key)
		c = nil
	}
	if c == nil {
		c = &ipxCircuit{
			net:      d.SrcNet,
			node:     d.SrcNode,
			sock:     d.SrcSock,
			localID:  e.allocLocalIDLocked(),
			remoteID: hdr.SourceConnID,
			// The SESSION_INITIALIZE consumed the client's seq 0; our accept is a
			// SYS frame and consumes none of ours.
			recvSeq: 1,
		}
		e.circuits[key] = c
	}
	localID, sendSeq, recvSeq := c.localID, c.sendSeq, c.recvSeq
	e.mu.Unlock()
	if stale != nil {
		stale.Close()
	}

	// Session-accept payload: swap the pair so WE are the source again — [our called
	// name][the caller's name] — preserving the trailer verbatim (golden capture
	// frame 66).
	e.sendSessionAccept(d, hdr, localID, sendSeq, recvSeq, req.Accept().Encode())
	e.logf("NBIPX circuit established")
}

// ipxCircuitUsed reports whether c has carried session data (or opened an SMB conn)
// so a new SESSION_INITIALIZE with the same remote id is a reconnect, not an INIT
// retransmit. A fresh accept has recvSeq 1, sendSeq 0, and no conn.
func ipxCircuitUsed(c *ipxCircuit) bool {
	return c.conn != nil || c.sendSeq != 0 || c.recvSeq != 1 || len(c.frag) > 0 || len(c.lastResp) > 0
}

// sendSessionAccept replies to a session request with a DATA frame that assigns our
// connection id (SourceConnID) and echoes the client's (DestConnID), carrying the
// swapped-name accept payload. This is the NBIPX SESSION_CONFIRM: ConnCtrlFlag is
// SYS|CONFIRM and RecvSeq is 1, both of which a Win98/WfW NWLink client validates
// before it will send its first SMB frame — an accept of bare SYS with RecvSeq 0 is
// treated as unconfirmed and the client retransmits SESSION_INITIALIZE forever
// (ERRATA captures/ipx.pcap frames 331-340 vs the working WFW server frame 367).
func (e *ipxSessionEngine) sendSessionAccept(in *ipxproto.Datagram, inHdr *protocol.NBIPXSessionHeader, localID, sendSeq, recvSeq uint16, payload []byte) {
	h := &protocol.NBIPXSessionHeader{
		ConnCtrlFlag:   protocol.NBIPXConnFlagSYS | protocol.NBIPXConnFlagCONFIRM,
		DataStreamType: protocol.NBIPXSessionData,
		SourceConnID:   localID,
		DestConnID:     inHdr.SourceConnID,
		SendSeq:        sendSeq,
		TotalDataLen:   uint16(len(payload)),
		DataLen:        uint16(len(payload)),
		RecvSeq:        recvSeq, // protocol.NBIPXSessionAcceptRecvSeq (1) on a fresh circuit
		BytesReceived:  recvSeq + nbipxRecvWindow,
	}
	e.send(in, append(protocol.EncodeSessionHeader(h), payload...), "session-accept")
}

// handleSessionEnd tears down a circuit: close its SMB conn (releasing handles),
// drop it, and acknowledge with SESSION_END_ACK. A duplicate SESSION_END still
// re-ACKs (the peer may have lost the first ACK) but closes nothing twice.
func (e *ipxSessionEngine) handleSessionEnd(d *ipxproto.Datagram, hdr *protocol.NBIPXSessionHeader) {
	key := keyFor(d, hdr)
	e.mu.Lock()
	c := e.circuits[key]
	var localID, sendSeq uint16
	if c != nil {
		localID, sendSeq = c.localID, c.sendSeq
		delete(e.circuits, key)
	}
	e.mu.Unlock()

	if c != nil && c.conn != nil {
		c.conn.Close()
	}
	// SESSION_END carries the ACK-required bit and consumes a sequence number,
	// so the ack's RecvSeq acknowledges it (NT's own end-ack does the same:
	// ipx.pcap 2026-07-10 frames 508/509).
	e.sendControl(d, hdr, localID, sendSeq, hdr.SendSeq+1, protocol.NBIPXSessionEndAck)
}

// handleData drives the sequenced data path of an open circuit (see the
// sequencing-rules ERRATA on protocol.NBIPXSessionHeader):
//
//   - A zero-data frame is session control, consuming no sequence number: a
//     SYS|RESEND asks us to retransmit the retained response from RecvSeq; a
//     SYS|ACK probe (NT sends 0xC0 right after the accept) is answered with a
//     zero-data SYS frame carrying our current counters; a bare ACK is state we
//     already have.
//   - An in-order data frame (SendSeq == recvSeq) advances recvSeq; without EOM it
//     buffers as a fragment, with EOM it completes a message that is served to the
//     lazily-opened SMB circuit and answered with sequenced DATA frames.
//   - A duplicate of the frame we just consumed (SendSeq == recvSeq-1, the client
//     retransmitting because our response was lost) re-sends the retained response
//     without re-serving the SMB command.
func (e *ipxSessionEngine) handleData(d *ipxproto.Datagram, hdr *protocol.NBIPXSessionHeader) {
	if len(d.Payload) < protocol.NBIPXSessionHeaderLen+int(hdr.DataLen) {
		return
	}
	body := d.Payload[protocol.NBIPXSessionHeaderLen : protocol.NBIPXSessionHeaderLen+int(hdr.DataLen)]

	key := keyFor(d, hdr)
	// A message is complete when the EOM bit is set in ConnCtrlFlag. A single-frame
	// SMB reply (the common case) sets EOM on its one DATA frame; a fragmented
	// message clears EOM on all but the last.
	eom := hdr.ConnCtrlFlag&protocol.NBIPXConnFlagEOM != 0

	e.mu.Lock()
	c := e.circuits[key]
	if c == nil {
		e.mu.Unlock()
		return
	}

	// Zero-data session-control (probe / ACK / resend request). These consume no
	// sequence number: NT's post-accept probe (0xC0, SendSeq 1) is acked with the
	// UNCHANGED RecvSeq (1) — acking it as consumed (RecvSeq 2) reads as a
	// protocol error and NT aborts after ~9 probes (client error 59). What the
	// probe actually polls for is the receive-window advertisement in the
	// BytesReceived field; see nbipxRecvWindow.
	if hdr.DataLen == 0 {
		sendSeq, recvSeq := c.sendSeq, c.recvSeq
		lastResp, lastRespSeq := c.lastResp, c.lastRespSeq
		e.mu.Unlock()
		if hdr.ConnCtrlFlag&protocol.NBIPXConnFlagRESEND != 0 && len(lastResp) > 0 {
			e.resendData(d, c, lastResp, lastRespSeq, hdr.RecvSeq, recvSeq)
			return
		}
		if hdr.ConnCtrlFlag&protocol.NBIPXConnFlagACK != 0 {
			e.sendSystemAck(d, c.localID, hdr.SourceConnID, sendSeq, recvSeq)
		}
		return
	}

	// Sequenced data frame.
	if hdr.SendSeq != c.recvSeq {
		// The retransmit of a frame we already consumed: our response was lost (or
		// rejected) — re-send it rather than re-serving the command.
		dup := hdr.SendSeq == c.recvSeq-1
		lastResp, lastRespSeq, recvSeq := c.lastResp, c.lastRespSeq, c.recvSeq
		e.mu.Unlock()
		if dup && len(lastResp) > 0 {
			e.resendData(d, c, lastResp, lastRespSeq, lastRespSeq, recvSeq)
		}
		return // anything else is out of window — drop, the peer recovers
	}
	c.recvSeq++

	if !eom {
		c.frag = append(c.frag, body...)
		sendSeq, recvSeq := c.sendSeq, c.recvSeq
		e.mu.Unlock()
		// A fragment produces no data response to carry the ack, so honour an
		// explicit ACK request with a system frame.
		if hdr.ConnCtrlFlag&protocol.NBIPXConnFlagACK != 0 {
			e.sendSystemAck(d, c.localID, hdr.SourceConnID, sendSeq, recvSeq)
		}
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
			c.conn = consumer.NewConn(nbipxClientLabel(c.node, c.sock))
			// Install the server-push writer for asynchronous completions
			// (NOTIFY_CHANGE), framing SMB bytes onto sequenced DATA frames
			// addressed from the circuit's retained peer address + connection ids.
			c.conn.SetPushWriter(func(frame []byte) {
				e.pushData(key, frame)
			})
		}
	}
	conn := c.conn
	e.mu.Unlock()

	if conn == nil {
		return // no consumer wired — message dropped
	}
	resp := conn.ServeMessage(msg)
	if len(resp) == 0 {
		// Silent-drop command: still answer an explicit ACK request so the client
		// releases its send window.
		if hdr.ConnCtrlFlag&protocol.NBIPXConnFlagACK != 0 {
			e.mu.Lock()
			sendSeq, recvSeq := c.sendSeq, c.recvSeq
			e.mu.Unlock()
			e.sendSystemAck(d, c.localID, hdr.SourceConnID, sendSeq, recvSeq)
		}
		return
	}
	e.sendData(d, c, resp)
}

// sendControl emits an NB-IPX session-control packet (SESSION_END_ACK) back to
// the peer, mirroring the connection IDs (our localID as SourceConnID, the peer's
// as DestConnID), carrying our send counter and the cumulative ack, and
// reflecting the IPX addressing.
func (e *ipxSessionEngine) sendControl(in *ipxproto.Datagram, inHdr *protocol.NBIPXSessionHeader, localID, sendSeq, recvSeq uint16, streamType uint8) {
	h := &protocol.NBIPXSessionHeader{
		ConnCtrlFlag:   protocol.NBIPXConnFlagSYS,
		DataStreamType: streamType,
		SourceConnID:   localID,
		DestConnID:     inHdr.SourceConnID,
		SendSeq:        sendSeq,
		RecvSeq:        recvSeq,
		BytesReceived:  recvSeq + nbipxRecvWindow,
	}
	e.send(in, protocol.EncodeSessionHeader(h), "session-control")
}

// nbipxMaxFrameData is the most message data one DATA frame carries. A response
// larger than this is fragmented across frames via TotalDataLen/Offset/DataLen with
// EOM set only on the last — the receive side of the same scheme handleData's c.frag
// path already reassembles. The boundary is shared with the NB-IPX client transport
// (client/smb/nbipx.go), so it is defined once in the protocol ring.
const nbipxMaxFrameData = protocol.NBIPXMaxFrameData

// nbipxRecvWindow is the receive window we advertise in the BytesReceived field
// of every session frame we send: BytesReceived = RecvSeq + window, the highest
// peer SendSeq we are prepared to accept plus one (the "window edge"). An NT
// NWLink client will NOT transmit data while the peer's advertised edge is below
// its next send sequence — with a zero advertisement it polls with zero-data
// SYS|ACK probes (0xC0) every ~600ms until the client errors out, while Win9x/WfW
// clients ignore the field entirely. 5 mirrors NT's own advertisement (its accept
// carries RecvSeq 1 + 5 = 6, then 7/8/9/10 as it consumes frames; ipx.pcap
// 2026-07-10 frames 488-509). We serve every message as it arrives, so the
// window never actually closes.
const nbipxRecvWindow = 5

// sendData sends a reassembled response back as sequenced DATA (0x06) frames, EOM
// on the last, allocating one SendSeq per frame from the circuit and retaining the
// message for RESEND/duplicate recovery. An empty payload still sends one DATA
// frame so an empty SMB reply is framed.
func (e *ipxSessionEngine) sendData(in *ipxproto.Datagram, c *ipxCircuit, payload []byte) {
	frames := (len(payload) + nbipxMaxFrameData - 1) / nbipxMaxFrameData
	if frames == 0 {
		frames = 1
	}
	e.mu.Lock()
	firstSeq := c.sendSeq
	c.sendSeq += uint16(frames)
	c.lastResp = payload
	c.lastRespSeq = firstSeq
	recvSeq := c.recvSeq
	e.mu.Unlock()
	e.sendDataFrames(in, c, payload, firstSeq, firstSeq, recvSeq)
}

// resendData retransmits the retained response message from the peer-requested
// sequence number (a SYS|RESEND, or a duplicate request frame whose response was
// lost) without consuming new sequence numbers or touching circuit state. A
// request outside the retained message's frame range resends the whole message.
func (e *ipxSessionEngine) resendData(in *ipxproto.Datagram, c *ipxCircuit, payload []byte, firstSeq, fromSeq, recvSeq uint16) {
	if fromSeq < firstSeq || fromSeq > firstSeq+uint16(len(payload)/nbipxMaxFrameData) {
		fromSeq = firstSeq
	}
	e.sendDataFrames(in, c, payload, firstSeq, fromSeq, recvSeq)
}

// sendDataFrames frames payload into DATA frames numbered from firstSeq, emitting
// those at/after fromSeq (== firstSeq sends the whole message), stamping recvSeq as
// the cumulative ack. EOM is set only on the final frame.
func (e *ipxSessionEngine) sendDataFrames(in *ipxproto.Datagram, c *ipxCircuit, payload []byte, firstSeq, fromSeq, recvSeq uint16) {
	total := uint16(len(payload))
	seq := firstSeq
	for off := 0; ; off += nbipxMaxFrameData {
		n := len(payload) - off
		last := n <= nbipxMaxFrameData
		if !last {
			n = nbipxMaxFrameData
		}
		if seq >= fromSeq {
			var ctrl uint8
			if last {
				ctrl = protocol.NBIPXConnFlagEOM
			}
			h := &protocol.NBIPXSessionHeader{
				ConnCtrlFlag:   ctrl,
				DataStreamType: protocol.NBIPXSessionData,
				SourceConnID:   c.localID,
				DestConnID:     c.remoteID,
				SendSeq:        seq,
				TotalDataLen:   total,
				Offset:         uint16(off),
				DataLen:        uint16(n),
				RecvSeq:        recvSeq,
				BytesReceived:  recvSeq + nbipxRecvWindow,
			}
			e.send(in, append(protocol.EncodeSessionHeader(h), payload[off:off+n]...), "session-send")
		}
		seq++
		if last {
			return
		}
	}
}

// sendSystemAck answers a zero-data SYS|ACK probe (and acks a frame that produced
// no data response) with a zero-data SYS frame carrying our current send counter
// and cumulative ack. NT 3.51 probes every fresh circuit this way and drops the
// session if the probe goes unanswered.
func (e *ipxSessionEngine) sendSystemAck(in *ipxproto.Datagram, localID, remoteID, sendSeq, recvSeq uint16) {
	h := &protocol.NBIPXSessionHeader{
		ConnCtrlFlag:   protocol.NBIPXConnFlagSYS,
		DataStreamType: protocol.NBIPXSessionData,
		SourceConnID:   localID,
		DestConnID:     remoteID,
		SendSeq:        sendSeq,
		RecvSeq:        recvSeq,
		BytesReceived:  recvSeq + nbipxRecvWindow,
	}
	e.send(in, protocol.EncodeSessionHeader(h), "session-ack")
}

// pushData sends a server-initiated reassembled message (an asynchronous
// NOTIFY_CHANGE completion, §10d wire push) to the circuit's peer as sequenced
// DATA frames. Unlike sendData it has no inbound datagram to swap addressing from,
// so it synthesizes the addressing from the circuit's retained net/node/sock; the
// circuit is looked up live so a push after SESSION_END is dropped and the
// sequence counters stay coherent with the request/response path.
func (e *ipxSessionEngine) pushData(key ipxCircuitKey, payload []byte) {
	if e.sender == nil {
		return
	}
	frames := (len(payload) + nbipxMaxFrameData - 1) / nbipxMaxFrameData
	if frames == 0 {
		frames = 1
	}
	e.mu.Lock()
	c := e.circuits[key]
	if c == nil {
		e.mu.Unlock()
		return
	}
	firstSeq := c.sendSeq
	c.sendSeq += uint16(frames)
	recvSeq := c.recvSeq
	e.mu.Unlock()

	in := &ipxproto.Datagram{
		SrcNet:  c.net,
		SrcNode: c.node,
		SrcSock: c.sock,
		DstSock: NBIPXSessionSocket,
	}
	e.sendDataFrames(in, c, payload, firstSeq, firstSeq, recvSeq)
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

// ipxBroadcastNode is the IPX node-ID broadcast address (all-ones); a browser
// group datagram fans to it. The value lives in the IPX protocol codec, which every
// IPX-carried transport (and the client mirrors) shares.
var ipxBroadcastNode = ipxproto.BroadcastNode

// handleNMPIPayload decodes an NMPI packet on the name-query (0x0551) or datagram
// (0x0553) socket and dispatches it, reporting true when it consumed the datagram.
// A non-NMPI payload returns false so the caller can try other paths. Mirrors the
// legacy over_ipx handleNMPIPayload.
func (e *ipxSessionEngine) handleNMPIPayload(d *ipxproto.Datagram) bool {
	if len(d.Payload) < 2 {
		return false
	}
	p, err := protocol.DecodeNMPIPacket(d.Payload)
	if err != nil {
		return false
	}
	e.handleNMPI(d, p)
	return true
}

// handleNMPI dispatches a decoded NMPI packet. A Query-name (opcode 0xF3) for one
// of our names is answered with a Name-found (0xF4) reply, echoing the message ID /
// name type so the querier can correlate it, unicast back to the source — this is
// how a WfW/Win9x client locates CLASSICSTACK before opening an NB-IPX session. A
// MailslotSend (0xFC) — the wire form of a browser HostAnnounce / AnnouncementRequest
// / GetBackupList over NB-IPX — is decoded to its inner NetBIOS datagram and routed
// to the connectionless-datagram consumer (the browser), which is how ClassicStack
// appears in an IPX client's browse list ("net view"). Mirrors the legacy over_ipx
// handleNMPI.
func (e *ipxSessionEngine) handleNMPI(d *ipxproto.Datagram, p *protocol.NMPIPacket) {
	if p == nil {
		return
	}
	switch p.Opcode {
	case protocol.NMPIOpMailslotSend:
		e.deliverMailslot(d, p)
	case protocol.NMPIOpNameQuery:
		if !e.ownsName(p.RequestedName) {
			return
		}
		resp := protocol.EncodeNMPIPacket(&protocol.NMPIPacket{
			Opcode:        protocol.NMPIOpNameFound,
			NameType:      p.NameType,
			MessageID:     p.MessageID,
			RequestedName: p.RequestedName,
			SourceName:    p.RequestedName,
		})
		e.sendNameReply(d, resp)
		e.logf("NBIPX name-found " + p.RequestedName.String())
	}
}

// deliverMailslot routes an NMPI MailslotSend's inner browser datagram to the
// datagram consumer, tagging ReplyTo with the sender's IPX address so the consumer
// (browser) can answer a specific requester (GetBackupList / AnnouncementRequest)
// directed rather than broadcast. A nil consumer drops it after decode.
func (e *ipxSessionEngine) deliverMailslot(d *ipxproto.Datagram, p *protocol.NMPIPacket) {
	if e.dgram == nil {
		return
	}
	consumer := e.dgram()
	if consumer == nil {
		return
	}
	consumer.HandleDatagram(Datagram{
		Source:      p.SourceName,
		Destination: p.RequestedName,
		Payload:     append([]byte(nil), p.Payload...),
		Broadcast:   p.RequestedName.Type() == protocol.NameTypeGroup || p.NameType == protocol.NMPINameTypeWorkgroup,
		ReplyTo:     e.replyEndpoint(d),
	})
}

// deliverRawDatagram routes a raw directed NB-IPX datagram (NBIPXDirectedDatagram, a
// bare dest/source/payload NetBIOS datagram, NOT NMPI-wrapped) to the consumer, the
// raw-datagram analogue of deliverMailslot. Mirrors the legacy over_ipx handlePEP
// directed-datagram path.
func (e *ipxSessionEngine) deliverRawDatagram(d *ipxproto.Datagram) {
	if e.dgram == nil {
		return
	}
	consumer := e.dgram()
	if consumer == nil {
		return
	}
	dg, err := protocol.DecodeDatagram(d.Payload[2:])
	if err != nil {
		return
	}
	consumer.HandleDatagram(Datagram{
		Source:      dg.Source,
		Destination: dg.Destination,
		Payload:     dg.Payload,
		Broadcast:   dg.Destination.Type() == protocol.NameTypeGroup,
		ReplyTo:     e.replyEndpoint(d),
	})
}

// replyEndpoint captures the inbound datagram's IPX address as a transport-tagged
// DatagramEndpoint, so a consumer answering a specific requester replies directed to
// that node (via SendDatagram → emitDatagram) rather than broadcasting.
func (e *ipxSessionEngine) replyEndpoint(d *ipxproto.Datagram) *DatagramEndpoint {
	return &DatagramEndpoint{
		Transport: TransportIPX,
		Network:   d.SrcNet,
		Node:      d.SrcNode,
		Socket:    d.SrcSock,
	}
}

// handleNameService examines an inbound type-20 name-service packet. It has two
// roles on a listening server:
//
//   - Find-name resolution: a WfW/Win9x NWLink client (e.g. WIN98-2 in
//     captures/ipx.pcap) locates a server by broadcasting a type-20 Find-name
//     (0x01) on socket 0x0455 — NOT the NMPI Query on 0x0551 (which a different
//     client dialect uses). If the queried name is one of ours we must answer
//     with a Name-recognized (0x02) reply, unicast to the querier; without it the
//     client never resolves CLASSICSTACK and no SMB-over-IPX session opens.
//     (ERRATA captures/ipx.pcap frame 21+: this is the sole name-resolution path
//     WIN98-2 emits.)
//   - Claim-conflict detection: while we are claiming a name, a positive reply
//     (Name-recognized / Name-in-use) naming it from another node means the name
//     is already in use, so we signal the objection to abort the claim. A bare
//     Find-name query does not object to a claim (a query is not a claim).
//
// Mirrors the legacy over_ipx handleNameService, extended with the Find-name
// responder observed on the wire.
func (e *ipxSessionEngine) handleNameService(d *ipxproto.Datagram) {
	pkt, err := protocol.DecodeNameService(d.Payload)
	if err != nil {
		return
	}
	switch pkt.DataStreamType {
	case protocol.NBIPXFindName:
		if e.ownsName(pkt.Name) {
			e.replyNameRecognized(d, pkt.Name)
		}
	case protocol.NBIPXNameRecognized, protocol.NBIPXNameInUse:
		e.noteClaimConflict(pkt.Name, d.SrcNode)
	}
}

// replyNameRecognized answers a type-20 Find-name for one of our names with a
// Name-recognized (0x02) name-service packet, unicast back to the querier's IPX
// node/socket (the Find-name arrives broadcast; the reply is directed). This is
// how a WfW/Win9x client that resolves via type-20 Find-name (rather than the
// NMPI Query on 0x0551) locates CLASSICSTACK before opening an NB-IPX session.
//
// ERRATA (captures/ipx.pcap): the reply must (1) carry the self-identifying leading
// prefix — our own name + workgroup + the 0x44 (In-use|Registered) status flag —
// that the Win98 NWLink client validates, and (2) be sent as an IPX type-4 (PEP)
// datagram, NOT type-20. An earlier zero-prefixed type-20 reply was ignored by the
// client (it never followed up with SESSION_INITIALIZE / Session-data). See
// EncodeNameRecognized and spec/errata.md.
func (e *ipxSessionEngine) replyNameRecognized(in *ipxproto.Datagram, name protocol.Name) {
	if e.sender == nil {
		return
	}
	own := e.ownName()
	body := protocol.EncodeNameRecognized(own, e.workgroupName(), name)
	_ = e.sender.Send(&ipxproto.Datagram{
		Type:    protocol.IPXTypePEP,
		DstNet:  in.SrcNet,
		DstNode: in.SrcNode,
		DstSock: in.SrcSock,
		SrcSock: in.DstSock,
		Payload: body,
	})
	e.logf("NBIPX name-recognized " + name.String())
}

// ownName returns our own NetBIOS name in workstation form (suffix 0x00) for the
// NAME_RECOGNIZED reply prefix, taken from the first local name (its base string).
// Falls back to an empty name if none is registered.
func (e *ipxSessionEngine) ownName() protocol.Name {
	if e.names == nil {
		return protocol.Name{}
	}
	names := e.names()
	if len(names) == 0 {
		return protocol.Name{}
	}
	return protocol.NewName(names[0].String(), protocol.NameTypeWorkstation)
}

// workgroupName returns the configured workgroup for the NAME_RECOGNIZED reply
// prefix. A nil callback (or empty result) yields an empty workgroup, which the
// encoder space-fills.
func (e *ipxSessionEngine) workgroupName() string {
	if e.workgroup == nil {
		return ""
	}
	return e.workgroup()
}

// noteClaimConflict signals the claim goroutine that name is contested, when an
// inbound name-service packet from a node other than ourselves names the name we are
// currently claiming. A self-looped broadcast (claimSelf) is ignored.
func (e *ipxSessionEngine) noteClaimConflict(name protocol.Name, srcNode [6]byte) {
	e.claimMu.Lock()
	claiming, self, obj := e.claiming, e.claimSelf, e.objection
	e.claimMu.Unlock()
	var zero protocol.Name
	if obj == nil || claiming == zero || name != claiming || srcNode == self {
		return
	}
	select {
	case obj <- struct{}{}:
	default:
	}
}

// ClaimName broadcasts a name-claim for name on the segment (a type-20 Find-name plus
// an NMPI ClaimName, retries × interval) and reports whether it was uncontested (nil)
// or another node objected (ErrNameInUse). self is our own IPX node so a looped-back
// self-broadcast is not mistaken for a conflict. Compose calls this on start, once per
// local name, to gate the SAP advertisement — the legacy over_ipx claim-then-advertise
// ordering.
func (e *ipxSessionEngine) ClaimName(ctx context.Context, self [6]byte, name protocol.Name, retries int, interval time.Duration) error {
	obj := make(chan struct{}, 1)
	e.claimMu.Lock()
	e.claiming, e.claimSelf, e.objection = name, self, obj
	e.claimMu.Unlock()
	defer func() {
		e.claimMu.Lock()
		e.claiming, e.objection = protocol.Name{}, nil
		e.claimMu.Unlock()
	}()

	for range retries {
		e.broadcastFindName(name)
		e.broadcastNMPIClaim(name)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-obj:
			return ErrNameInUse
		case <-time.After(interval):
		}
	}
	return nil
}

// broadcastFindName emits one IPX type-20 Find-name carrying name to every node on
// the segment (the name-claim broadcast form). Mirrors the legacy over_ipx
// broadcastFindName.
func (e *ipxSessionEngine) broadcastFindName(name protocol.Name) {
	if e.sender == nil {
		return
	}
	body := protocol.EncodeNameService(&protocol.NBIPXNameServicePacket{
		NameTypeFlag:   0x00,
		DataStreamType: protocol.NBIPXFindName,
		Name:           name,
	})
	_ = e.sender.Send(&ipxproto.Datagram{
		Type:    protocol.IPXTypeNetBIOS,
		DstNet:  e.sender.Network(),
		DstNode: ipxBroadcastNode,
		DstSock: NBIPXSessionSocket,
		SrcSock: NBIPXSessionSocket,
		Payload: body,
	})
}

// broadcastNMPIClaim emits one NMPI ClaimName (opcode 0xF1) for name to the segment,
// sourced from the server socket (0x0550) to the name-query socket (0x0551). Mirrors
// the legacy over_ipx broadcastNMPIClaim.
func (e *ipxSessionEngine) broadcastNMPIClaim(name protocol.Name) {
	if e.sender == nil {
		return
	}
	body := protocol.EncodeNMPIPacket(&protocol.NMPIPacket{
		Opcode:        protocol.NMPIOpNameClaim,
		NameType:      protocol.NMPINameTypeMachine,
		RequestedName: name,
		SourceName:    name,
	})
	_ = e.sender.Send(&ipxproto.Datagram{
		Type:    protocol.IPXTypeNetBIOS,
		DstNet:  e.sender.Network(),
		DstNode: ipxBroadcastNode,
		DstSock: NBIPXNameQuerySocket,
		SrcSock: NBIPXServerSocket,
		Payload: body,
	})
}

// sendNameReply unicasts a name-resolution reply back to the querier as an IPX PEP
// datagram (type 4), swapping source/destination sockets. A nil sender drops it.
func (e *ipxSessionEngine) sendNameReply(in *ipxproto.Datagram, body []byte) {
	if e.sender == nil {
		return
	}
	_ = e.sender.Send(&ipxproto.Datagram{
		Type:    protocol.IPXTypePEP,
		DstNet:  in.SrcNet,
		DstNode: in.SrcNode,
		DstSock: in.SrcSock,
		SrcSock: in.DstSock,
		Payload: body,
	})
}

// emitDatagram sends a connectionless NetBIOS datagram (a browser HostAnnounce /
// election / backup-list frame) over NB-IPX as an NMPI MailslotSend (opcode 0xFC)
// on the datagram socket (0x0553), IPX type 20. The browser's payload is the SMB
// mailslot transaction; it rides the NMPI Payload field with the source/destination
// NetBIOS names in the NMPI header. A broadcast (ReplyTo nil) fans to the IPX
// broadcast node; a directed reply (ReplyTo set by the inbound datagram — a browser
// GetBackupList / AnnouncementRequest answer tagged TransportIPX) is unicast to the
// requester's IPX node/socket, so the answer reaches the one station that asked.
func (e *ipxSessionEngine) emitDatagram(d Datagram) error {
	if e.sender == nil {
		return nil
	}
	body := protocol.EncodeNMPIPacket(&protocol.NMPIPacket{
		Opcode:        protocol.NMPIOpMailslotSend,
		NameType:      nmpiNameType(d.Destination),
		RequestedName: d.Destination,
		SourceName:    d.Source,
		Payload:       d.Payload,
	})
	out := &ipxproto.Datagram{
		Type:    protocol.IPXTypeNetBIOS,
		DstNode: ipxBroadcastNode,
		DstSock: NBIPXDatagramSocket,
		SrcSock: NBIPXDatagramSocket,
		Payload: body,
	}
	if r := d.ReplyTo; r != nil && r.Transport == TransportIPX && r.Node != ([6]byte{}) {
		out.DstNet = r.Network
		out.DstNode = r.Node
		if r.Socket != ([2]byte{}) {
			out.DstSock = r.Socket
		}
		// A DIRECTED answer switches IPX packet type: the fan-out half of a browser
		// exchange is type 20 (NetBIOS broadcast/forwarding, so routers propagate it),
		// but the master's unicast reply to the one station that asked goes out as type
		// 4 (PEP) — golden spec/captures/nbipx-win98.pcap frame 60 and
		// nwlink-win98.pcap frame 41, both "Get Backup List Response" on socket 0x0553.
		// It needs no broadcast forwarding, so it does not claim the type that requests it.
		out.Type = protocol.IPXTypePEP
	}
	return e.sender.Send(out)
}

// nmpiNameType maps a NetBIOS name to the NMPI name-type byte: a group name is a
// workgroup, anything else a machine.
func nmpiNameType(name protocol.Name) uint8 {
	if name.Type() == protocol.NameTypeGroup {
		return protocol.NMPINameTypeWorkgroup
	}
	return protocol.NMPINameTypeMachine
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
