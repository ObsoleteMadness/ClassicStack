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
// session, carry SMB over it. The caller (CALL-out) side is not needed by a file
// server. Flow control (NO_RECEIVE/RECEIVE_CONTINUE) and the I-frame
// retransmit machinery the legacy transport carried are an adapter-altitude
// reliability concern; the core engine delivers and replies, and the segment
// reassembly + DATA_ACK that SMB-over-NBF actually depends on live here.

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
	mac       [6]byte
	localNum  uint8
	remoteNum uint8
	active    bool

	frag []byte         // accumulated DATA_FIRST_MIDDLE payload
	conn SessionCircuit // SMB virtual circuit (nil until consumer opens one)
}

// sessionEngine is the NBF responder state machine. It owns the open circuits,
// hands out local session numbers, and routes reassembled messages to the
// consumer. Safe for concurrent inbound frames (the mini-router may deliver from
// the port read loop).
type sessionEngine struct {
	logger   log.Logger
	sender   FrameSender
	consumer func() SessionConsumer // late-bound: the service installs it after wiring
	names    func() []protocol.Name // local names, to answer NAME_QUERY for ours

	mu        sync.Mutex
	circuits  map[circuitKey]*circuit
	nextLocal uint8
}

// newSessionEngine builds an NBF session engine. consumer and names are
// callbacks so the engine reads the live consumer/name set the service owns
// (both can be set after the engine is constructed, e.g. SMB attaches late).
func newSessionEngine(logger log.Logger, sender FrameSender, consumer func() SessionConsumer, names func() []protocol.Name) *sessionEngine {
	return &sessionEngine{
		logger:   logger,
		sender:   sender,
		consumer: consumer,
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
	if frame.Command == nbf.CmdNameQuery {
		e.handleNameQuery(srcMAC, frame)
	}
}

// HandleSessionFrame is the netbeui mini-router SessionHandler entry point: an
// NBF session-command frame (0x14–0x1F). It drives the lifecycle and data paths.
func (e *sessionEngine) HandleSessionFrame(srcMAC, dstMAC [6]byte, frame *nbf.Frame) {
	_ = dstMAC
	switch frame.Command {
	case nbf.CmdSessionInitialize:
		e.handleSessionInitialize(srcMAC, frame)
	case nbf.CmdSessionEnd:
		e.handleSessionEnd(srcMAC, frame)
	case nbf.CmdDataOnlyLast:
		e.handleDataOnlyLast(srcMAC, frame)
	case nbf.CmdDataFirstMiddle:
		e.handleDataFirstMiddle(srcMAC, frame)
	case nbf.CmdSessionAlive, nbf.CmdDataAck:
		// keepalive / our-data acknowledgements need no response.
	}
}

// handleNameQuery answers a CALL (NAME_QUERY with a non-zero caller session
// number in Data2) for one of our names by creating a circuit and replying
// NAME_RECOGNIZED with the local session number. The caller then sends
// SESSION_INITIALIZE to that number to bring the circuit up.
func (e *sessionEngine) handleNameQuery(srcMAC [6]byte, frame *nbf.Frame) {
	if !e.ownsName(protocol.Name(frame.DestinationName)) {
		return
	}
	callerSession := uint8(frame.Data2 & 0xFF)
	if callerSession == 0 {
		return // FIND.NAME, not a CALL — no session to set up
	}

	e.mu.Lock()
	localNum := e.allocLocalNumLocked()
	e.circuits[circuitKey{srcMAC, localNum}] = &circuit{
		mac:       srcMAC,
		localNum:  localNum,
		remoteNum: callerSession,
	}
	e.mu.Unlock()

	resp := &nbf.Frame{
		Command:        nbf.CmdNameRecognized,
		XmitCorrelator: frame.RspCorrelator,
		RspCorrelator:  uint16(localNum),
		Data2:          uint16(localNum), // high byte 0 = unique name
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
// acknowledges receipt with DATA_ACK, serves the message to the SMB circuit, and
// sends the response back as DATA frames. A message on a circuit with no consumer
// is acknowledged and dropped.
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
			c.conn = consumer.NewConn()
		}
	}
	conn := c.conn
	remoteNum := c.remoteNum
	localNum := c.localNum
	e.mu.Unlock()

	// Acknowledge the data segment (spec §5: DATA_ONLY_LAST is ACKed).
	ack := &nbf.Frame{
		Command:        nbf.CmdDataAck,
		XmitCorrelator: frame.RspCorrelator,
		DestNumber:     remoteNum,
		SourceNumber:   localNum,
	}
	e.send(srcMAC, ack, "data-ack")

	if conn == nil {
		return // no consumer wired — message dropped after ACK
	}
	resp := conn.ServeMessage(msg)
	if len(resp) == 0 {
		return // silent-drop command
	}
	e.sendSessionData(srcMAC, localNum, remoteNum, resp)
}

// sendSessionData fragments resp onto DATA_FIRST_MIDDLE/DATA_ONLY_LAST frames at
// the advertised max I-field and sends them in order. An empty payload still
// sends one DATA_ONLY_LAST (an empty message is a valid SMB response framing).
func (e *sessionEngine) sendSessionData(dstMAC [6]byte, localNum, remoteNum uint8, payload []byte) {
	max := int(ethernetMaxIField)
	if len(payload) == 0 {
		e.send(dstMAC, &nbf.Frame{
			Command:      nbf.CmdDataOnlyLast,
			DestNumber:   remoteNum,
			SourceNumber: localNum,
		}, "session-send")
		return
	}
	for off := 0; off < len(payload); off += max {
		end := min(off+max, len(payload))
		cmd := nbf.CmdDataFirstMiddle
		if end == len(payload) {
			cmd = nbf.CmdDataOnlyLast
		}
		e.send(dstMAC, &nbf.Frame{
			Command:      cmd,
			DestNumber:   remoteNum,
			SourceNumber: localNum,
			Payload:      append([]byte(nil), payload[off:end]...),
		}, "session-send")
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

// send writes a directed NBF frame through the sender, logging a send error at
// warn. A nil sender (engine not wired to a router) drops the frame.
func (e *sessionEngine) send(dstMAC [6]byte, frame *nbf.Frame, reason string) {
	if e.sender == nil {
		return
	}
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
