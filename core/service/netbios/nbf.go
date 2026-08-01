package netbios

// nbf.go is the core NBF (NetBIOS Frames Protocol) session engine: the NetBEUI
// virtual-circuit state machine that turns NAME_QUERY → NAME_RECOGNIZED →
// SESSION_INITIALIZE → SESSION_CONFIRM into an established circuit, reassembles
// the DATA_FIRST_MIDDLE/DATA_ONLY_LAST segments of each SMB message, and feeds
// the whole message to the upper-layer SessionConsumer (SMB), sending the
// response back as DATA frames. It is the core re-home of the legacy
// service/netbios/over_netbeui transport's session half, stripped of netlog and
// the port import: it talks to the world only through the FrameSender seam (the
// core/router/netbeui mini-router satisfies it) and the SessionConsumer seam (the
// SMB command engine satisfies it). It holds no link-layer or storage knowledge.
//
// Ring: CORE (stdlib only, reflection-free). The NBF wire codec is
// core/protocol/netbeui; this engine is the state machine over it.
//
// Scope: the responder (listen) side — answer an inbound CALL, accept the
// session, carry SMB over it. Alongside the session machine the engine also
// answers the two connectionless responder paths (nbf_datagram.go): the
// node-status query (STATUS_QUERY → STATUS_RESPONSE, built from the local name
// set) and the directed/broadcast datagram (decoded and routed to the optional
// DatagramConsumer). The caller (CALL-out) side is not needed by a file server.
// The transmit-side reliability the peer can drive — NO_RECEIVE/RECEIVE_CONTINUE
// flow control and the RECEIVE_OUTSTANDING last-frame retransmit — is carried here
// per-circuit, matching the legacy over_netbeui transport byte-for-byte on the wire:
// a WfW/Win9x peer that closes its receive window mid-reply must be honoured or the
// held frames are lost. The segment reassembly + DATA_ACK that SMB-over-NBF depends
// on live here alongside it.

import (
	"slices"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/core/log"
	nbf "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbeui"
	protocol "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbios"
)

// FrameSender is the NBF frame egress the session engine drives: send a directed
// UI frame to a peer MAC, or broadcast one. The core/router/netbeui mini-router's
// Send/SendBroadcast satisfy it exactly, so compose registers the engine on the
// mini-router (as its NameHandler + SessionHandler) and hands it the router as the
// sender. The engine never imports the mini-router or a port — only this seam.
type FrameSender interface {
	Send(dstMAC [6]byte, frame *nbf.Frame) error
	SendBroadcast(frame *nbf.Frame) error
}

// ethernetMaxIField is the NBF payload the engine advertises in SESSION_CONFIRM:
// the Ethernet MTU (1500) minus LLC/NBF overhead, matching the legacy transport.
const ethernetMaxIField uint16 = 1464

// circuitKey identifies a virtual circuit by peer MAC plus the local session
// number we assigned it — the same (MAC, localNum) tuple the NBF session header
// carries inbound as DestNumber.
type circuitKey struct {
	mac      [6]byte
	localNum uint8
}

// circuit is one NBF virtual circuit: the peer address, the local/remote session
// numbers exchanged during establishment, the partial-message reassembly buffer
// (DATA_FIRST_MIDDLE accumulates here until DATA_ONLY_LAST completes it), and the
// upper-layer SessionCircuit the reassembled SMB messages are served to.
type circuit struct {
	mac        [6]byte
	localNum   uint8
	remoteNum  uint8
	active     bool
	callerName protocol.Name // calling NetBIOS name from the establishing NAME_QUERY (frame.SourceName)

	frag []byte         // accumulated DATA_FIRST_MIDDLE payload
	conn SessionCircuit // SMB virtual circuit (nil until consumer opens one)

	// Transmit-side reliability (NBF flow control, [IBM SC30-3587] §5): a peer
	// throttles the server mid-message with NO_RECEIVE and resumes with
	// RECEIVE_CONTINUE, or asks for the last frame again with RECEIVE_OUTSTANDING.
	// txBlocked holds our sends while the peer's receive window is closed;
	// txPending queues the frames we could not send; txLast is the most recent
	// frame sent, retained for a RECEIVE_OUTSTANDING retransmit request.
	txBlocked bool
	txPending []*nbf.Frame
	txLast    *nbf.Frame
}

// sessionEngine is the NBF responder state machine. It owns the open circuits,
// hands out local session numbers, and routes reassembled messages to the
// consumer. Safe for concurrent inbound frames (the mini-router may deliver from
// the port read loop).
type sessionEngine struct {
	logger   log.Logger
	sender   FrameSender
	consumer func() SessionConsumer  // late-bound: the service installs it after wiring
	dgram    func() DatagramConsumer // late-bound connectionless-datagram sink
	names    func() []protocol.Name  // local names, to answer NAME_QUERY/STATUS_QUERY for ours

	mu        sync.Mutex
	circuits  map[circuitKey]*circuit
	nextLocal uint8
}

// newSessionEngine builds an NBF session engine. consumer, dgram and names are
// callbacks so the engine reads the live consumer/name set the service owns
// (all can be set after the engine is constructed, e.g. SMB attaches late).
func newSessionEngine(logger log.Logger, sender FrameSender, consumer func() SessionConsumer, dgram func() DatagramConsumer, names func() []protocol.Name) *sessionEngine {
	return &sessionEngine{
		logger:   logger,
		sender:   sender,
		consumer: consumer,
		dgram:    dgram,
		names:    names,
		circuits: make(map[circuitKey]*circuit),
	}
}

// allocLocalNum hands out the next non-zero local session number. Number 0 means
// "no session" on the wire, so the allocator skips it on wrap. Caller holds mu.
func (e *sessionEngine) allocLocalNumLocked() uint8 {
	e.nextLocal++
	if e.nextLocal == 0 {
		e.nextLocal++
	}
	return e.nextLocal
}

// ownsName reports whether name is one of the local names this server claims, so
// NAME_QUERY for a foreign name is ignored (it is not addressed to us).
func (e *sessionEngine) ownsName(name protocol.Name) bool {
	return slices.Contains(e.names(), name)
}

// HandleFrame is the netbeui mini-router NameHandler entry point: a non-session
// NBF frame addressed to one of our registered names. The engine answers the
// session-establishment NAME_QUERY (a CALL) with NAME_RECOGNIZED carrying the
// local session number; FIND.NAME (callerSession==0) and other non-session
// frames are left to the name layer / ignored here.
func (e *sessionEngine) HandleFrame(srcMAC, dstMAC [6]byte, frame *nbf.Frame) {
	_ = dstMAC
	e.logFrame("NBF UI frame in", srcMAC, frame.Command)
	switch frame.Command {
	case nbf.CmdNameQuery:
		e.handleNameQuery(srcMAC, frame)
	case nbf.CmdStatusQuery:
		e.handleStatusQuery(srcMAC, frame)
	case nbf.CmdDatagram:
		e.handleDatagram(srcMAC, frame, false)
	case nbf.CmdDatagramBroadcast:
		e.handleDatagram(srcMAC, frame, true)
	}
}

// HandleSessionFrame is the netbeui mini-router SessionHandler entry point: an
// NBF session-command frame (0x14–0x1F). It drives the lifecycle and data paths.
func (e *sessionEngine) HandleSessionFrame(srcMAC, dstMAC [6]byte, frame *nbf.Frame) {
	_ = dstMAC
	e.logFrame("NBF session frame in", srcMAC, frame.Command)
	switch frame.Command {
	case nbf.CmdSessionInitialize:
		e.handleSessionInitialize(srcMAC, frame)
	case nbf.CmdSessionEnd:
		e.handleSessionEnd(srcMAC, frame)
	case nbf.CmdDataOnlyLast:
		e.handleDataOnlyLast(srcMAC, frame)
	case nbf.CmdDataFirstMiddle:
		e.handleDataFirstMiddle(srcMAC, frame)
	case nbf.CmdNoReceive:
		e.handleNoReceive(srcMAC, frame)
	case nbf.CmdReceiveContinue:
		e.handleReceiveContinue(srcMAC, frame)
	case nbf.CmdReceiveOutstanding:
		e.handleReceiveOutstanding(srcMAC, frame)
	case nbf.CmdSessionAlive, nbf.CmdDataAck:
		// keepalive / our-data acknowledgements need no response.
	}
}

// handleNameQuery answers a NAME_QUERY for one of our names with NAME_RECOGNIZED.
// Windows drives a session open in two phases ([IBM SC30-3587] §5.6.8/§5.6.10,
// confirmed against netbeui.pcap): first a broadcast locate carrying Local Session
// No. 0 ("FIND.NAME request"), then a unicast CALL carrying a real session number,
// followed by SESSION_INITIALIZE. Both phases expect a NAME_RECOGNIZED — answering
// only the second (returning silently when the session number is 0) leaves the
// client's initial locate unanswered, so it never learns the name exists and never
// proceeds to the CALL. (This is why an NT 3.51 client could not see the server
// while Win98, whose own server answers the session-0 locate, could.)
//
// For a real CALL (ss != 0) we allocate a circuit and reply with the local session
// number in Data2/RspCorrelator, so the caller's SESSION_INITIALIZE can bring it up.
// For a locate (ss == 0) no circuit is created: we reply with Data2 ss = 0 ("no
// LISTEN pending / FIND.NAME response", spec §5.6.10 Data2).
func (e *sessionEngine) handleNameQuery(srcMAC [6]byte, frame *nbf.Frame) {
	if !e.ownsName(protocol.Name(frame.DestinationName)) {
		return
	}

	var localNum uint8
	if callerSession := uint8(frame.Data2 & 0xFF); callerSession != 0 {
		e.mu.Lock()
		localNum = e.allocLocalNumLocked()
		e.circuits[circuitKey{srcMAC, localNum}] = &circuit{
			mac:        srcMAC,
			localNum:   localNum,
			remoteNum:  callerSession,
			callerName: protocol.Name(frame.SourceName),
		}
		e.mu.Unlock()
	}

	resp := &nbf.Frame{
		Command:        nbf.CmdNameRecognized,
		XmitCorrelator: frame.RspCorrelator,
		RspCorrelator:  uint16(localNum),
		Data2:          uint16(localNum), // high byte 0 = unique name; low byte = session no. (0 = FIND.NAME/no-session)
	}
	copy(resp.DestinationName[:], frame.SourceName[:])
	copy(resp.SourceName[:], frame.DestinationName[:])
	e.send(srcMAC, resp, "name-recognized")
}

// handleSessionInitialize completes establishment: mark the circuit active, learn
// the remote session number, and reply SESSION_CONFIRM advertising the max
// I-field. The circuit is now ready to carry SMB messages.
func (e *sessionEngine) handleSessionInitialize(srcMAC [6]byte, frame *nbf.Frame) {
	localNum := frame.DestNumber
	e.mu.Lock()
	c := e.circuits[circuitKey{srcMAC, localNum}]
	if c != nil {
		c.remoteNum = frame.SourceNumber
		c.active = true
	}
	e.mu.Unlock()
	if c == nil {
		return
	}

	confirm := &nbf.Frame{
		Command:        nbf.CmdSessionConfirm,
		XmitCorrelator: frame.RspCorrelator,
		Data2:          ethernetMaxIField,
		DestNumber:     frame.SourceNumber,
		SourceNumber:   localNum,
	}
	e.send(srcMAC, confirm, "session-confirm")
	e.logf("NBF circuit established")
}

// handleSessionEnd tears down a circuit: close its SMB conn (releasing handles)
// and drop it. A duplicate SESSION_END is a no-op.
func (e *sessionEngine) handleSessionEnd(srcMAC [6]byte, frame *nbf.Frame) {
	key := circuitKey{srcMAC, frame.DestNumber}
	e.mu.Lock()
	c := e.circuits[key]
	delete(e.circuits, key)
	e.mu.Unlock()
	if c != nil && c.conn != nil {
		c.conn.Close()
	}
}

// handleDataFirstMiddle accumulates a non-final SMB message segment. The bytes
// are buffered until DATA_ONLY_LAST completes the message.
func (e *sessionEngine) handleDataFirstMiddle(srcMAC [6]byte, frame *nbf.Frame) {
	e.mu.Lock()
	defer e.mu.Unlock()
	c := e.circuits[circuitKey{srcMAC, frame.DestNumber}]
	if c == nil || !c.active {
		return
	}
	c.frag = append(c.frag, frame.Payload...)
}

// handleDataOnlyLast completes an SMB message (joining any buffered segments),
// acknowledges receipt, serves the message to the SMB circuit, and sends the
// response back as DATA frames. A message on a circuit with no consumer is
// acknowledged and dropped.
//
// Acknowledgment follows the sender's Data1 option bits ([IBM SC30-3587]
// Table 5-25): NO.ACK data is not acknowledged at all; when the sender set
// ACKNOWLEDGE_WITH_DATA_ALLOWED, the acknowledgment rides the first frame of
// our response (ACKNOWLEDGE_INCLUDED + the sender's RSP correlator) instead of
// a separate DATA_ACK. That halves the reply to one frame — verified against
// netbeui.pcap, where an NT 3.51 client's NE2000-class NIC reliably dropped
// the second of our two back-to-back frames (DATA_ACK then DATA_ONLY_LAST)
// and the SMB session never came up. A separate DATA_ACK is still sent when
// the sender did not allow piggybacking or when we have no response to carry it.
func (e *sessionEngine) handleDataOnlyLast(srcMAC [6]byte, frame *nbf.Frame) {
	key := circuitKey{srcMAC, frame.DestNumber}
	e.mu.Lock()
	c := e.circuits[key]
	if c == nil || !c.active {
		e.mu.Unlock()
		return
	}
	var msg []byte
	if len(c.frag) > 0 {
		msg = append(c.frag, frame.Payload...)
		c.frag = nil
	} else {
		msg = frame.Payload
	}
	// Open the SMB circuit lazily on the first message so a circuit that never
	// carries data costs no consumer state.
	if c.conn == nil {
		if consumer := e.consumer(); consumer != nil {
			c.conn = consumer.NewConn(nbfClientLabel(c.mac))
			// Install the server-push writer: a held NOTIFY_CHANGE completes
			// asynchronously by framing SMB bytes onto this circuit's DATA frames,
			// using the circuit's retained (MAC, localNum, remoteNum) addressing.
			cMAC, cLocal, cRemote := c.mac, c.localNum, c.remoteNum
			c.conn.SetPushWriter(func(frame []byte) {
				e.sendSessionData(cMAC, cLocal, cRemote, frame)
			})
			// Pass the calling NetBIOS name through to the consumer's management
			// session view, if it accepts one (SMB's *Conn does; a consumer that
			// doesn't care simply isn't a NetBIOSNamer).
			if namer, ok := c.conn.(NetBIOSNamer); ok && c.callerName != (protocol.Name{}) {
				namer.SetNetBIOSName(c.callerName.String())
			}
		}
	}
	conn := c.conn
	remoteNum := c.remoteNum
	localNum := c.localNum
	e.mu.Unlock()

	wantsAck := frame.Data1&nbf.DataNoAck == 0
	piggyback := wantsAck && frame.Data1&nbf.DataAckWithDataAllowed != 0 && conn != nil
	if wantsAck && !piggyback {
		e.sendDataAck(srcMAC, localNum, remoteNum, frame.RspCorrelator)
	}

	if conn == nil {
		return // no consumer wired — message dropped after ACK
	}
	resp := conn.ServeMessage(msg)
	if len(resp) == 0 {
		// Silent-drop command: nothing to carry a piggybacked ack, so a
		// deferred acknowledgment falls back to a plain DATA_ACK.
		if piggyback {
			e.sendDataAck(srcMAC, localNum, remoteNum, frame.RspCorrelator)
		}
		return
	}
	ackCorrelator := uint16(0)
	if piggyback {
		ackCorrelator = frame.RspCorrelator
	}
	e.sendSessionDataAck(srcMAC, localNum, remoteNum, resp, ackCorrelator, piggyback)
}

// sendDataAck sends a DATA_ACK for a received DATA_ONLY_LAST (spec §5.6.11:
// XMIT correlator echoes the data frame's RSP correlator).
func (e *sessionEngine) sendDataAck(dstMAC [6]byte, localNum, remoteNum uint8, correlator uint16) {
	e.send(dstMAC, &nbf.Frame{
		Command:        nbf.CmdDataAck,
		XmitCorrelator: correlator,
		DestNumber:     remoteNum,
		SourceNumber:   localNum,
	}, "data-ack")
}

// sendSessionData fragments resp onto DATA_FIRST_MIDDLE/DATA_ONLY_LAST frames at
// the advertised max I-field and sends them in order. An empty payload still
// sends one DATA_ONLY_LAST (an empty message is a valid SMB response framing). If
// the circuit's receive window is closed (the peer sent NO_RECEIVE), the frames are
// queued and flushed on RECEIVE_CONTINUE instead of being sent immediately.
func (e *sessionEngine) sendSessionData(dstMAC [6]byte, localNum, remoteNum uint8, payload []byte) {
	e.sendSessionDataAck(dstMAC, localNum, remoteNum, payload, 0, false)
}

// sendSessionDataAck is sendSessionData with an optional piggybacked
// acknowledgment: when ackIncluded is set, the first frame carries
// ACKNOWLEDGE_INCLUDED and ackCorrelator (the peer's RSP correlator), standing
// in for a separate DATA_ACK ([IBM SC30-3587] Table 5-25).
func (e *sessionEngine) sendSessionDataAck(dstMAC [6]byte, localNum, remoteNum uint8, payload []byte, ackCorrelator uint16, ackIncluded bool) {
	max := int(ethernetMaxIField)
	frames := make([]*nbf.Frame, 0, len(payload)/max+1)
	if len(payload) == 0 {
		frames = append(frames, &nbf.Frame{
			Command:      nbf.CmdDataOnlyLast,
			DestNumber:   remoteNum,
			SourceNumber: localNum,
		})
	} else {
		for off := 0; off < len(payload); off += max {
			end := min(off+max, len(payload))
			cmd := nbf.CmdDataFirstMiddle
			if end == len(payload) {
				cmd = nbf.CmdDataOnlyLast
			}
			frames = append(frames, &nbf.Frame{
				Command:      cmd,
				DestNumber:   remoteNum,
				SourceNumber: localNum,
				Payload:      append([]byte(nil), payload[off:end]...),
			})
		}
	}
	if ackIncluded {
		frames[0].Data1 |= nbf.DataAckIncluded
		frames[0].XmitCorrelator = ackCorrelator
	}

	// Hold the frames if the peer's receive window is closed; otherwise send now.
	e.mu.Lock()
	c := e.circuits[circuitKey{dstMAC, localNum}]
	if c != nil && c.txBlocked {
		c.txPending = append(c.txPending, frames...)
		e.mu.Unlock()
		return
	}
	e.mu.Unlock()
	e.sendSessionFramesNow(dstMAC, localNum, frames)
}

// sendSessionFramesNow sends the given session frames in order and records the last
// one on the circuit for a possible RECEIVE_OUTSTANDING retransmit. It bypasses the
// blocked-window check (the caller has decided the frames may go out now).
func (e *sessionEngine) sendSessionFramesNow(dstMAC [6]byte, localNum uint8, frames []*nbf.Frame) {
	for _, f := range frames {
		e.send(dstMAC, f, "session-send")
		cp := *f
		cp.Payload = append([]byte(nil), f.Payload...)
		e.mu.Lock()
		if c := e.circuits[circuitKey{dstMAC, localNum}]; c != nil {
			c.txLast = &cp
		}
		e.mu.Unlock()
	}
}

// handleNoReceive marks the circuit's receive window closed: the peer has no RECEIVE
// posted, so we hold further session data until it sends RECEIVE_CONTINUE. Mirrors the
// legacy over_netbeui handleNoReceive.
func (e *sessionEngine) handleNoReceive(srcMAC [6]byte, frame *nbf.Frame) {
	e.mu.Lock()
	if c := e.circuits[circuitKey{srcMAC, frame.DestNumber}]; c != nil && c.active {
		c.txBlocked = true
	}
	e.mu.Unlock()
}

// handleReceiveContinue reopens the circuit's receive window and flushes any frames
// queued while it was closed. Mirrors the legacy over_netbeui handleReceiveContinue.
func (e *sessionEngine) handleReceiveContinue(srcMAC [6]byte, frame *nbf.Frame) {
	e.mu.Lock()
	c := e.circuits[circuitKey{srcMAC, frame.DestNumber}]
	if c == nil || !c.active {
		e.mu.Unlock()
		return
	}
	c.txBlocked = false
	pending := c.txPending
	c.txPending = nil
	e.mu.Unlock()
	if len(pending) > 0 {
		e.sendSessionFramesNow(srcMAC, frame.DestNumber, pending)
	}
}

// handleReceiveOutstanding retransmits the last session frame we sent on the circuit,
// which the peer is asking for again (it missed our last transmission). Mirrors the
// legacy over_netbeui handleReceiveOutstanding.
func (e *sessionEngine) handleReceiveOutstanding(srcMAC [6]byte, frame *nbf.Frame) {
	e.mu.Lock()
	c := e.circuits[circuitKey{srcMAC, frame.DestNumber}]
	var last *nbf.Frame
	if c != nil && c.active {
		last = c.txLast
	}
	e.mu.Unlock()
	if last != nil {
		e.send(srcMAC, last, "receive-outstanding-retransmit")
	}
}

// closeAll tears down every circuit (called when the service stops), closing the
// SMB conns so no file handles leak.
func (e *sessionEngine) closeAll() {
	e.mu.Lock()
	conns := make([]SessionCircuit, 0, len(e.circuits))
	for _, c := range e.circuits {
		if c.conn != nil {
			conns = append(conns, c.conn)
		}
	}
	e.circuits = make(map[circuitKey]*circuit)
	e.mu.Unlock()
	for _, conn := range conns {
		conn.Close()
	}
}

// emitDatagram sends a connectionless NetBIOS datagram (a browser HostAnnounce /
// election / backup-list frame) as an NBF UI frame carrying the source/destination
// NetBIOS names and the payload. It ALWAYS uses CmdDatagram (0x08), never
// CmdDatagramBroadcast (0x09): a real Windows/WfW/Win98 browser routes an inbound
// datagram by its destination NetBIOS name and dispatches only 0x08 frames — every
// browser datagram in captures/win98nbf-win31nbf.pcapng (Host/Domain announcements,
// GetBackupList request AND response, RequestAnnouncement, LocalMasterAnnounce) is a
// 0x08 Datagram, none is a 0x09 broadcast, and a 0x09 to a group name a client is not
// registered for is silently dropped (the exact failure fixed on the client's
// discovery path). A directed reply (ReplyTo set by the inbound datagram — a browser
// GetBackupList / AnnouncementRequest answer) is unicast to the requester's MAC
// (carried in ReplyTo.Node) so the answer reaches the one station that asked; a
// "broadcast" (ReplyTo nil) goes to the NetBIOS functional multicast MAC, where the
// destination NAME in the frame selects the recipient(s). The wire command is 0x08 in
// both cases — only the L2 destination MAC differs.
func (e *sessionEngine) emitDatagram(d Datagram) error {
	if e.sender == nil {
		return nil
	}
	frame := &nbf.Frame{Payload: d.Payload}
	frame.DestinationName = [16]byte(d.Destination)
	frame.SourceName = [16]byte(d.Source)
	frame.Command = nbf.CmdDatagram

	if r := d.ReplyTo; r != nil && r.Transport == TransportNetBEUI && r.Node != ([6]byte{}) {
		return e.sender.Send(r.Node, frame)
	}
	return e.sender.SendBroadcast(frame)
}

// send writes a directed NBF frame through the sender, logging a send error at
// warn. A nil sender (engine not wired to a router) drops the frame.
func (e *sessionEngine) send(dstMAC [6]byte, frame *nbf.Frame, reason string) {
	if e.sender == nil {
		return
	}
	e.logFrame("NBF frame out ("+reason+")", dstMAC, frame.Command)
	if err := e.sender.Send(dstMAC, frame); err != nil {
		e.logf("NBF send failed: " + reason)
	}
}

// logf emits one info line through the logger if configured.
func (e *sessionEngine) logf(msg string) {
	if e.logger == nil || !e.logger.Enabled(log.Info) {
		return
	}
	e.logger.Log1(log.Info, msg, log.Str("scope", Name))
}

// logFrame narrates one NBF frame (in or out) at debug level: the command mnemonic and
// the peer MAC. Guarded by Enabled so the format cost is skipped when debug is off. This
// is the NetBIOS-layer half of the request/response narration (the SMB-layer half is in
// core/service/smb ServeMessage).
func (e *sessionEngine) logFrame(msg string, mac [6]byte, cmd uint8) {
	if e.logger == nil || !e.logger.Enabled(log.Debug) {
		return
	}
	e.logger.Log(log.Debug, msg,
		log.Str("scope", Name),
		log.Str("command", nbf.CommandName(cmd)),
		log.Str("peer", macString(mac)))
}

// macString formats a 6-byte MAC as aa:bb:cc:dd:ee:ff for log fields (avoids importing
// net/fmt in the core ring for one diagnostics call).
func macString(mac [6]byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, 17)
	for i, b := range mac {
		if i > 0 {
			out = append(out, ':')
		}
		out = append(out, digits[b>>4], digits[b&0x0F])
	}
	return string(out)
}
