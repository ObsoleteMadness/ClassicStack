// Package over_netbeui adapts a NetBEUI port to the netbios.Transport
// contract. It implements the NBF (NetBIOS Frames Protocol) state
// machine over 802.2 LLC UI frames on Ethernet, providing:
//
//   - Name management: ADD_NAME_QUERY / ADD_NAME_RESPONSE / NAME_IN_CONFLICT
//   - Name resolution: NAME_QUERY / NAME_RECOGNIZED
//   - Datagram delivery: DATAGRAM / DATAGRAM_BROADCAST
//   - Session establishment: NAME_QUERY → NAME_RECOGNIZED → SESSION_INITIALIZE → SESSION_CONFIRM
//   - Session data transfer: DATA_ONLY_LAST / DATA_FIRST_MIDDLE / DATA_ACK
//   - Session teardown: SESSION_END
//   - Keepalive: SESSION_ALIVE
//
// Wire format per IBM SC30-3587 Chapter 5.
package over_netbeui

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/netlog"
	"github.com/ObsoleteMadness/ClassicStack/port/netbeui"
	nbfproto "github.com/ObsoleteMadness/ClassicStack/protocol/netbeui"
	protocol "github.com/ObsoleteMadness/ClassicStack/protocol/netbios"
	nb "github.com/ObsoleteMadness/ClassicStack/service/netbios"
)

// IBM defaults from spec §5.6.1:
// NCB.TRANSMIT.COUNT = 6, NCB.TRANSMIT.TIMEOUT = 500ms.
const (
	defaultTransmitCount   = 6
	defaultTransmitTimeout = 500 * time.Millisecond
)

type transport struct {
	port netbeui.Port

	mu      sync.RWMutex
	handler nb.CommandHandler

	names    *nameTable
	sessions *sessionTable

	// correlator is a monotonically increasing counter for generating
	// unique response correlator values.
	correlator atomic.Uint32

	// srcMAC is cached from the port for building NAME_NUMBER_1.
	srcMAC [6]byte

	fragMu sync.Mutex
	frags  map[[7]byte][]byte

	txMu              sync.Mutex
	txBlocked         map[[7]byte]bool
	txLastFrame       map[[7]byte]*nbfproto.Frame
	txPendingFrames   map[[7]byte][]*nbfproto.Frame
	sessionMaxPayload map[[7]byte]uint16

	cancel context.CancelFunc
}

// NewTransport returns a netbios.Transport backed by an existing
// NetBEUI port. The port must already be configured (source MAC,
// rawlink open) by the caller. srcMAC is the local adapter's MAC
// address, needed for NAME_NUMBER_1 construction and directed replies.
func NewTransport(p netbeui.Port, srcMAC [6]byte) nb.Transport {
	t := &transport{
		port:     p,
		names:    newNameTable(),
		sessions: newSessionTable(),
		srcMAC:   srcMAC,
		frags:    map[[7]byte][]byte{},
		txBlocked:         map[[7]byte]bool{},
		txLastFrame:       map[[7]byte]*nbfproto.Frame{},
		txPendingFrames:   map[[7]byte][]*nbfproto.Frame{},
		sessionMaxPayload: map[[7]byte]uint16{},
	}
	t.correlator.Store(1)
	return t
}

func (t *transport) Start(_ context.Context) error {
	t.port.SetDeliveryCallback(t.onFrame)
	return nil
}

func (t *transport) Stop() error {
	t.port.SetDeliveryCallback(nil)
	if t.cancel != nil {
		t.cancel()
	}
	return nil
}

func (t *transport) SetCommandHandler(h nb.CommandHandler) {
	t.mu.Lock()
	t.handler = h
	t.mu.Unlock()
}

func nbfCommandName(cmd uint8) string {
	switch cmd {
	case nbfproto.CmdAddGroupNameQuery:
		return "ADD_GROUP_NAME_QUERY"
	case nbfproto.CmdAddNameQuery:
		return "ADD_NAME_QUERY"
	case nbfproto.CmdNameInConflict:
		return "NAME_IN_CONFLICT"
	case nbfproto.CmdStatusQuery:
		return "STATUS_QUERY"
	case nbfproto.CmdTerminateTraceRemote:
		return "TERMINATE_TRACE_REMOTE"
	case nbfproto.CmdDatagram:
		return "DATAGRAM"
	case nbfproto.CmdDatagramBroadcast:
		return "DATAGRAM_BROADCAST"
	case nbfproto.CmdNameQuery:
		return "NAME_QUERY"
	case nbfproto.CmdAddNameResponse:
		return "ADD_NAME_RESPONSE"
	case nbfproto.CmdNameRecognized:
		return "NAME_RECOGNIZED"
	case nbfproto.CmdStatusResponse:
		return "STATUS_RESPONSE"
	case nbfproto.CmdTerminateTraceLocal:
		return "TERMINATE_TRACE_LOCAL"
	case nbfproto.CmdDataAck:
		return "DATA_ACK"
	case nbfproto.CmdDataFirstMiddle:
		return "DATA_FIRST_MIDDLE"
	case nbfproto.CmdDataOnlyLast:
		return "DATA_ONLY_LAST"
	case nbfproto.CmdSessionConfirm:
		return "SESSION_CONFIRM"
	case nbfproto.CmdSessionEnd:
		return "SESSION_END"
	case nbfproto.CmdSessionInitialize:
		return "SESSION_INITIALIZE"
	case nbfproto.CmdNoReceive:
		return "NO_RECEIVE"
	case nbfproto.CmdReceiveOutstanding:
		return "RECEIVE_OUTSTANDING"
	case nbfproto.CmdReceiveContinue:
		return "RECEIVE_CONTINUE"
	case nbfproto.CmdSessionAlive:
		return "SESSION_ALIVE"
	default:
		return "UNKNOWN"
	}
}

func (t *transport) sendFrame(dstMAC [6]byte, frame *nbfproto.Frame, reason string) error {
	netlog.Debug("[NetBEUI] tx %s(0x%02X) dst=%02X:%02X:%02X:%02X:%02X:%02X dnum=%d snum=%d data2=0x%04X payload=%d reason=%s",
		nbfCommandName(frame.Command), frame.Command,
		dstMAC[0], dstMAC[1], dstMAC[2], dstMAC[3], dstMAC[4], dstMAC[5],
		frame.DestNumber, frame.SourceNumber, frame.Data2, len(frame.Payload), reason)
	return t.port.Send(dstMAC, frame)
}

func (t *transport) sendBroadcastFrame(frame *nbfproto.Frame, reason string) error {
	netlog.Debug("[NetBEUI] tx %s(0x%02X) dst=broadcast dnum=%d snum=%d data2=0x%04X payload=%d reason=%s",
		nbfCommandName(frame.Command), frame.Command,
		frame.DestNumber, frame.SourceNumber, frame.Data2, len(frame.Payload), reason)
	return t.port.SendBroadcast(frame)
}

// sessionForInbound resolves a session table entry for an inbound
// session frame and enforces expected remote session number/state.
func (t *transport) sessionForInbound(srcMAC [6]byte, destNum, sourceNum uint8, requireActive bool) *session {
	sess := t.sessions.Lookup(srcMAC, destNum)
	if sess == nil {
		return nil
	}

	sess.Mu.Lock()
	defer sess.Mu.Unlock()

	// Once the remote session number is learned, inbound session frames
	// must match it to avoid cross-session confusion.
	if sess.RemoteNum != 0 && sourceNum != 0 && sess.RemoteNum != sourceNum {
		return nil
	}
	if requireActive && sess.State != sessionStateActive {
		return nil
	}
	return sess
}

// nextCorrelator returns a unique 16-bit correlator value.
func (t *transport) nextCorrelator() uint16 {
	for {
		v := t.correlator.Add(1)
		if v != 0 { // avoid zero which means "unused" on the wire
			return uint16(v)
		}
	}
}

// --- Name Service ---

// SendName claims a NetBIOS name on the network by broadcasting
// ADD_NAME_QUERY per spec §5.6.2. Retries defaultTransmitCount
// times at defaultTransmitTimeout intervals. The name is registered
// locally if no ADD_NAME_RESPONSE (conflict) is received.
func (t *transport) SendName(name protocol.Name) error {
	isGroup := false // ADD_NAME_QUERY is for unique names
	entry := t.names.Add(name, isGroup)
	if entry == nil {
		// Already registered.
		return nil
	}

	corr := t.nextCorrelator()

	frame := &nbfproto.Frame{
		Command:       nbfproto.CmdAddNameQuery,
		RspCorrelator: corr,
	}
	copy(frame.SourceName[:], name[:])

	for i := 0; i < defaultTransmitCount; i++ {
		if err := t.sendBroadcastFrame(frame, "name-claim"); err != nil {
			netlog.Warn("[NetBEUI] ADD_NAME_QUERY send error: %v", err)
		}
		time.Sleep(defaultTransmitTimeout)
	}

	// If no conflict was detected during the claim window, mark registered.
	if entry.State == nameStateClaiming {
		t.names.SetState(name, nameStateRegistered)
		netlog.Info("[NetBEUI] name registered: %s", name.String())
	}
	return nil
}

// SendDatagram wraps a NetBIOS datagram in an NBF DATAGRAM (0x08)
// frame and broadcasts it.
func (t *transport) SendDatagram(d *protocol.Datagram) error {
	payload, err := d.Encode()
	if err != nil {
		return err
	}

	frame := &nbfproto.Frame{
		Command: nbfproto.CmdDatagram,
	}
	copy(frame.DestinationName[:], d.Destination[:])
	copy(frame.SourceName[:], d.Source[:])
	frame.Payload = payload[2*protocol.NameLength:] // just user data, names are in header

	return t.sendBroadcastFrame(frame, "datagram")
}

// SendDirectedDatagram sends a NetBIOS datagram directly to a known
// destination MAC (from remote.Node). If remote.Node is empty, it
// falls back to broadcast transport.
func (t *transport) SendDirectedDatagram(d *protocol.Datagram, remote nb.DatagramEndpoint) error {
	payload, err := d.Encode()
	if err != nil {
		return err
	}

	frame := &nbfproto.Frame{
		Command: nbfproto.CmdDatagram,
	}
	copy(frame.DestinationName[:], d.Destination[:])
	copy(frame.SourceName[:], d.Source[:])
	frame.Payload = payload[2*protocol.NameLength:]

	if remote.Node == ([6]byte{}) {
		return t.sendBroadcastFrame(frame, "directed-datagram-fallback")
	}
	return t.sendFrame(remote.Node, frame, "directed-datagram")
}

// SendSession maps a session packet onto NBF DATA_ONLY_LAST frames.
// This is a simplified implementation that sends each packet as a
// single DATA_ONLY_LAST (no segmentation).
func (t *transport) SendSession(s *protocol.SessionPacket) error {
	// Find the first active session to send on. A real implementation
	// would route by session, but we have a single-session stub here.
	sessions := t.sessions.All()
	if len(sessions) == 0 {
		return nb.ErrNotImplemented
	}

	sess := sessions[0]
	sess.Mu.Lock()
	if sess.State != sessionStateActive {
		sess.Mu.Unlock()
		return nb.ErrNotImplemented
	}

	corr := t.nextCorrelator()
	destNum := sess.RemoteNum
	srcNum := sess.LocalNum
	remoteMac := sess.RemoteAddr
	sess.LastXmitCorrelator = corr
	sess.Mu.Unlock()
	return t.sendSessionPayload(remoteMac, srcNum, destNum, s.Payload)
}

// --- Inbound Frame Dispatch ---

func (t *transport) onFrame(srcMAC, dstMAC [6]byte, frame *nbfproto.Frame) {
	netlog.Debug("[NetBEUI] rx %s(0x%02X) src=%02X:%02X:%02X:%02X:%02X:%02X dst=%02X:%02X:%02X:%02X:%02X:%02X dnum=%d snum=%d data2=0x%04X xmit=0x%04X rsp=0x%04X payload=%d",
		nbfCommandName(frame.Command), frame.Command,
		srcMAC[0], srcMAC[1], srcMAC[2], srcMAC[3], srcMAC[4], srcMAC[5],
		dstMAC[0], dstMAC[1], dstMAC[2], dstMAC[3], dstMAC[4], dstMAC[5],
		frame.DestNumber, frame.SourceNumber, frame.Data2, frame.XmitCorrelator, frame.RspCorrelator, len(frame.Payload))
	switch frame.Command {
	// --- Name management ---
	case nbfproto.CmdAddNameQuery, nbfproto.CmdAddGroupNameQuery:
		t.handleAddNameQuery(srcMAC, frame)
	case nbfproto.CmdAddNameResponse:
		t.handleAddNameResponse(frame)
	case nbfproto.CmdNameInConflict:
		t.handleNameInConflict(frame)

	// --- Name resolution / session establishment ---
	case nbfproto.CmdNameQuery:
		t.handleNameQuery(srcMAC, frame)
	case nbfproto.CmdNameRecognized:
		t.handleNameRecognized(srcMAC, frame)

	// --- Session lifecycle ---
	case nbfproto.CmdSessionInitialize:
		t.handleSessionInitialize(srcMAC, frame)
	case nbfproto.CmdSessionConfirm:
		t.handleSessionConfirm(srcMAC, frame)
	case nbfproto.CmdSessionEnd:
		t.handleSessionEnd(srcMAC, frame)
	case nbfproto.CmdSessionAlive:
		t.handleSessionAlive(srcMAC, frame)

	// --- Session data ---
	case nbfproto.CmdDataOnlyLast:
		t.handleDataOnlyLast(srcMAC, frame)
	case nbfproto.CmdDataFirstMiddle:
		t.handleDataFirstMiddle(srcMAC, frame)
	case nbfproto.CmdDataAck:
		t.handleDataAck(srcMAC, frame)

	// --- Datagram ---
	case nbfproto.CmdDatagram:
		t.handleDatagram(srcMAC, frame)
	case nbfproto.CmdDatagramBroadcast:
		t.handleDatagramBroadcast(srcMAC, frame)

	// --- Flow control ---
	case nbfproto.CmdNoReceive:
		t.handleNoReceive(srcMAC, frame)
	case nbfproto.CmdReceiveOutstanding:
		t.handleReceiveOutstanding(srcMAC, frame)
	case nbfproto.CmdReceiveContinue:
		t.handleReceiveContinue(srcMAC, frame)

	// --- Status ---
	case nbfproto.CmdStatusQuery:
		t.handleStatusQuery(srcMAC, frame)
	case nbfproto.CmdStatusResponse:
		t.handleStatusResponse(srcMAC, frame)

	default:
		netlog.Debug("[NetBEUI] unknown command 0x%02X from %02X:%02X:%02X:%02X:%02X:%02X",
			frame.Command, srcMAC[0], srcMAC[1], srcMAC[2], srcMAC[3], srcMAC[4], srcMAC[5])
	}
}

// --- Name Management Handlers ---

// handleAddNameQuery: a remote node is claiming a name. If we own it
// as a unique name, respond with ADD_NAME_RESPONSE.
func (t *transport) handleAddNameQuery(srcMAC [6]byte, frame *nbfproto.Frame) {
	queriedName := frame.SourceName // spec §5.6.2: source name = name being added
	entry := t.names.Lookup(protocol.Name(queriedName))
	if entry == nil || entry.IsGroup {
		return // we don't own it or it's a group name (no conflict)
	}
	if entry.State != nameStateRegistered {
		return
	}

	// Conflict: respond with ADD_NAME_RESPONSE.
	resp := &nbfproto.Frame{
		Command:        nbfproto.CmdAddNameResponse,
		Data1:          0x00, // not in add-name process
		XmitCorrelator: frame.RspCorrelator,
	}
	copy(resp.DestinationName[:], queriedName[:])
	copy(resp.SourceName[:], queriedName[:])

	if err := t.sendFrame(srcMAC, resp, "add-name-response"); err != nil {
		netlog.Warn("[NetBEUI] ADD_NAME_RESPONSE send error: %v", err)
	}
	netlog.Info("[NetBEUI] sent ADD_NAME_RESPONSE for %q to %02X:%02X:%02X:%02X:%02X:%02X",
		protocol.Name(queriedName).String(),
		srcMAC[0], srcMAC[1], srcMAC[2], srcMAC[3], srcMAC[4], srcMAC[5])
}

// handleAddNameResponse: someone else already owns the name we're
// trying to claim. Mark the name as conflicted.
func (t *transport) handleAddNameResponse(frame *nbfproto.Frame) {
	conflictName := frame.DestinationName
	entry := t.names.Lookup(protocol.Name(conflictName))
	if entry == nil {
		return
	}
	if entry.State == nameStateClaiming {
		t.names.SetState(protocol.Name(conflictName), nameStateConflict)
		netlog.Warn("[NetBEUI] name conflict: %q already owned by another node",
			protocol.Name(conflictName).String())
	}
}

// handleNameInConflict: a remote node detected a name conflict.
func (t *transport) handleNameInConflict(frame *nbfproto.Frame) {
	conflictName := frame.DestinationName
	if t.names.IsLocal(protocol.Name(conflictName)) {
		t.names.SetState(protocol.Name(conflictName), nameStateConflict)
		netlog.Warn("[NetBEUI] NAME_IN_CONFLICT received for %q",
			protocol.Name(conflictName).String())
	}
}

// --- Name Resolution / Session Establishment ---

// handleNameQuery: a remote node is looking for a name (CALL or
// FIND.NAME). If we own it, respond with NAME_RECOGNIZED.
func (t *transport) handleNameQuery(srcMAC [6]byte, frame *nbfproto.Frame) {
	destName := frame.DestinationName
	entry := t.names.Lookup(protocol.Name(destName))
	if entry == nil || entry.State != nameStateRegistered {
		return // not our name
	}

	// Determine if this is a session request or just FIND.NAME.
	// The Data2 low byte in NAME_QUERY indicates the caller's
	// local session number (0 = FIND.NAME, >0 = CALL).
	callerSession := uint8(frame.Data2 & 0xFF)

	// Create a session if this is a CALL.
	var localSessionNum uint8
	if callerSession != 0 {
		sess := t.sessions.LookupByRemote(srcMAC, callerSession)
		if sess == nil {
			sess = t.sessions.Create(srcMAC)
			sess.Mu.Lock()
			sess.RemoteNum = callerSession
			sess.Mu.Unlock()
		}
		localSessionNum = sess.LocalNum
	}

	resp := &nbfproto.Frame{
		Command:        nbfproto.CmdNameRecognized,
		XmitCorrelator: frame.RspCorrelator,
		RspCorrelator:  uint16(localSessionNum),
	}
	// DATA2: high byte = name type (0=unique, 1=group),
	//        low byte = session number (0=no listen, 1-FE=session number)
	nameType := uint16(0x00) // unique
	if entry.IsGroup {
		nameType = 0x01
	}
	resp.Data2 = (nameType << 8) | uint16(localSessionNum)
	copy(resp.DestinationName[:], frame.SourceName[:])
	copy(resp.SourceName[:], destName[:])

	if err := t.sendFrame(srcMAC, resp, "name-recognized"); err != nil {
		netlog.Warn("[NetBEUI] NAME_RECOGNIZED send error: %v", err)
	}
	netlog.Info("[NetBEUI] NAME_RECOGNIZED for %q (session=%d) to %02X:%02X:%02X:%02X:%02X:%02X",
		protocol.Name(destName).String(), localSessionNum,
		srcMAC[0], srcMAC[1], srcMAC[2], srcMAC[3], srcMAC[4], srcMAC[5])
}

// handleNameRecognized: response to our NAME_QUERY.
func (t *transport) handleNameRecognized(srcMAC [6]byte, frame *nbfproto.Frame) {
	sessionNum := uint8(frame.Data2 & 0xFF)
	if sessionNum == 0 {
		netlog.Debug("[NetBEUI] NAME_RECOGNIZED (FIND.NAME) from %02X:%02X:%02X:%02X:%02X:%02X",
			srcMAC[0], srcMAC[1], srcMAC[2], srcMAC[3], srcMAC[4], srcMAC[5])
		return
	}
	netlog.Info("[NetBEUI] NAME_RECOGNIZED (session=%d) from %02X:%02X:%02X:%02X:%02X:%02X",
		sessionNum, srcMAC[0], srcMAC[1], srcMAC[2], srcMAC[3], srcMAC[4], srcMAC[5])
}

// --- Session Lifecycle ---

// handleSessionInitialize: the caller sends this after receiving
// NAME_RECOGNIZED to start the session.
func (t *transport) handleSessionInitialize(srcMAC [6]byte, frame *nbfproto.Frame) {
	// Find the session we created during NAME_RECOGNIZED.
	destNum := frame.DestNumber   // our session number
	srcNum := frame.SourceNumber  // caller's session number

	sess := t.sessions.Lookup(srcMAC, destNum)
	if sess == nil {
		netlog.Warn("[NetBEUI] SESSION_INITIALIZE for unknown session %d from %02X:%02X:%02X:%02X:%02X:%02X",
			destNum, srcMAC[0], srcMAC[1], srcMAC[2], srcMAC[3], srcMAC[4], srcMAC[5])
		return
	}

	sess.Mu.Lock()
	sess.RemoteNum = srcNum
	sess.State = sessionStateActive
	sess.Mu.Unlock()
	t.txMu.Lock()
	t.sessionMaxPayload[sessionWireKey(srcMAC, destNum)] = 1464
	t.txMu.Unlock()

	// Reply with SESSION_CONFIRM.
	confirm := &nbfproto.Frame{
		Command:        nbfproto.CmdSessionConfirm,
		XmitCorrelator: frame.RspCorrelator,
		RspCorrelator:  t.nextCorrelator(),
		DestNumber:     srcNum,
		SourceNumber:   destNum,
	}
	// Data2 carries the max receive size (we advertise the spec
	// default maximum I-field for Ethernet: 1500 - LLC overhead).
	confirm.Data2 = 1464

	if err := t.sendFrame(srcMAC, confirm, "session-confirm"); err != nil {
		netlog.Warn("[NetBEUI] SESSION_CONFIRM send error: %v", err)
	}
	netlog.Info("[NetBEUI] session %d↔%d established with %02X:%02X:%02X:%02X:%02X:%02X",
		destNum, srcNum,
		srcMAC[0], srcMAC[1], srcMAC[2], srcMAC[3], srcMAC[4], srcMAC[5])
}

// handleSessionConfirm: the responder confirmed our session.
func (t *transport) handleSessionConfirm(srcMAC [6]byte, frame *nbfproto.Frame) {
	localNum := frame.DestNumber
	remoteNum := frame.SourceNumber

	sess := t.sessions.Lookup(srcMAC, localNum)
	if sess == nil {
		netlog.Warn("[NetBEUI] SESSION_CONFIRM for unknown session %d", localNum)
		return
	}

	sess.Mu.Lock()
	sess.RemoteNum = remoteNum
	sess.State = sessionStateActive
	sess.Mu.Unlock()
	if frame.Data2 != 0 {
		t.txMu.Lock()
		t.sessionMaxPayload[sessionWireKey(srcMAC, localNum)] = frame.Data2
		t.txMu.Unlock()
	}

	netlog.Info("[NetBEUI] session %d↔%d confirmed by %02X:%02X:%02X:%02X:%02X:%02X",
		localNum, remoteNum,
		srcMAC[0], srcMAC[1], srcMAC[2], srcMAC[3], srcMAC[4], srcMAC[5])
}

// handleSessionEnd: peer is tearing down the session.
func (t *transport) handleSessionEnd(srcMAC [6]byte, frame *nbfproto.Frame) {
	localNum := frame.DestNumber
	sess := t.sessionForInbound(srcMAC, localNum, frame.SourceNumber, false)
	if sess == nil {
		return
	}
	sess.Mu.Lock()
	sess.State = sessionStateClosed
	sess.Mu.Unlock()
	t.fragMu.Lock()
	delete(t.frags, sessionFragmentKey(srcMAC, localNum))
	t.fragMu.Unlock()
	t.txMu.Lock()
	key := sessionWireKey(srcMAC, localNum)
	delete(t.txBlocked, key)
	delete(t.txLastFrame, key)
	delete(t.txPendingFrames, key)
	delete(t.sessionMaxPayload, key)
	t.txMu.Unlock()
	t.sessions.Remove(srcMAC, localNum)

	netlog.Info("[NetBEUI] session %d ended by %02X:%02X:%02X:%02X:%02X:%02X",
		localNum, srcMAC[0], srcMAC[1], srcMAC[2], srcMAC[3], srcMAC[4], srcMAC[5])
}

// handleSessionAlive: keepalive probe — just log it.
func (t *transport) handleSessionAlive(srcMAC [6]byte, frame *nbfproto.Frame) {
	if t.sessionForInbound(srcMAC, frame.DestNumber, frame.SourceNumber, false) == nil {
		return
	}
	netlog.Debug("[NetBEUI] SESSION_ALIVE from %02X:%02X:%02X:%02X:%02X:%02X session %d",
		srcMAC[0], srcMAC[1], srcMAC[2], srcMAC[3], srcMAC[4], srcMAC[5],
		frame.DestNumber)
}

// --- Session Data Transfer ---

// handleDataOnlyLast: received a complete data message. Deliver to
// the handler and send DATA_ACK.
func (t *transport) handleDataOnlyLast(srcMAC [6]byte, frame *nbfproto.Frame) {
	localNum := frame.DestNumber
	sess := t.sessionForInbound(srcMAC, localNum, frame.SourceNumber, true)
	if sess == nil {
		return
	}

	payload := frame.Payload
	t.fragMu.Lock()
	if head, ok := t.frags[sessionFragmentKey(srcMAC, localNum)]; ok {
		payload = append(append([]byte(nil), head...), frame.Payload...)
		delete(t.frags, sessionFragmentKey(srcMAC, localNum))
	}
	t.fragMu.Unlock()

	// Send DATA_ACK.
	ack := &nbfproto.Frame{
		Command:        nbfproto.CmdDataAck,
		XmitCorrelator: frame.RspCorrelator,
		DestNumber:     frame.SourceNumber,
		SourceNumber:   localNum,
	}
	if err := t.sendFrame(srcMAC, ack, "data-ack"); err != nil {
		netlog.Warn("[NetBEUI] DATA_ACK send error: %v", err)
	}

	// Deliver to the command handler.
	t.mu.RLock()
	handler := t.handler
	t.mu.RUnlock()
	if handler == nil {
		return
	}

	pkt := &protocol.SessionPacket{
		Type:    protocol.SessionMessage,
		Payload: payload,
	}

	if sh, ok := handler.(nb.ContextualSessionHandler); ok {
		resp, err := sh.HandleSessionContext(pkt, nb.SessionContext{
			Local: nb.DatagramEndpoint{Node: t.srcMAC},
			Remote: nb.DatagramEndpoint{Node: srcMAC},
			SourceConnID: uint16(frame.SourceNumber),
			DestConnID:   uint16(localNum),
		})
		if err != nil {
			netlog.Warn("[NetBEUI] contextual session handler error: %v", err)
			return
		}
		if resp != nil && len(resp.Payload) > 0 {
			if err := t.sendSessionPayload(srcMAC, localNum, frame.SourceNumber, resp.Payload); err != nil {
				netlog.Warn("[NetBEUI] response session send error: %v", err)
			}
		}
		return
	}
	if err := handler.HandleSession(pkt); err != nil {
		netlog.Warn("[NetBEUI] session handler error: %v", err)
	}
}

// handleDataFirstMiddle: first or middle segment of a multi-segment
// message. In this simplified implementation we deliver each segment
// as a standalone packet.
func (t *transport) handleDataFirstMiddle(srcMAC [6]byte, frame *nbfproto.Frame) {
	localNum := frame.DestNumber
	if t.sessionForInbound(srcMAC, localNum, frame.SourceNumber, true) == nil {
		return
	}
	t.fragMu.Lock()
	key := sessionFragmentKey(srcMAC, localNum)
	t.frags[key] = append(t.frags[key], frame.Payload...)
	t.fragMu.Unlock()
}

// handleDataAck: the remote acknowledged our DATA_ONLY_LAST.
func (t *transport) handleDataAck(srcMAC [6]byte, frame *nbfproto.Frame) {
	localNum := frame.DestNumber
	sess := t.sessionForInbound(srcMAC, localNum, frame.SourceNumber, true)
	if sess == nil {
		return
	}
	sess.Mu.Lock()
	defer sess.Mu.Unlock()
	if frame.XmitCorrelator != 0 && sess.LastXmitCorrelator != 0 && frame.XmitCorrelator != sess.LastXmitCorrelator {
		return
	}
	sess.LastXmitCorrelator = 0
	netlog.Debug("[NetBEUI] DATA_ACK for session %d from %02X:%02X:%02X:%02X:%02X:%02X",
		localNum, srcMAC[0], srcMAC[1], srcMAC[2], srcMAC[3], srcMAC[4], srcMAC[5])
}

// --- Datagram Handlers ---

func (t *transport) handleDatagram(srcMAC [6]byte, frame *nbfproto.Frame) {
	t.mu.RLock()
	handler := t.handler
	t.mu.RUnlock()
	if handler == nil {
		return
	}

	d := &protocol.Datagram{
		Destination: protocol.Name(frame.DestinationName),
		Source:      protocol.Name(frame.SourceName),
		Payload:     frame.Payload,
	}
	if ch, ok := handler.(nb.ContextualDatagramHandler); ok {
		if err := ch.HandleDatagramContext(d, nb.DatagramContext{
			Local:  nb.DatagramEndpoint{Node: t.srcMAC},
			Remote: nb.DatagramEndpoint{Node: srcMAC},
		}); err != nil {
			netlog.Warn("[NetBEUI] contextual datagram handler error: %v", err)
		}
		return
	}
	if err := handler.HandleDatagram(d); err != nil {
		netlog.Warn("[NetBEUI] datagram handler error: %v", err)
	}
}

func (t *transport) handleDatagramBroadcast(srcMAC [6]byte, frame *nbfproto.Frame) {
	// Same handling as directed datagram — the broadcast distinction
	// is at the link layer (multicast MAC), not in the handler.
	t.handleDatagram(srcMAC, frame)
}

// --- Status Handlers ---

// handleStatusQuery replies with a minimal STATUS_RESPONSE when the
// query targets a locally registered name.
func (t *transport) handleStatusQuery(srcMAC [6]byte, frame *nbfproto.Frame) {
	queriedName := protocol.Name(frame.DestinationName)
	entry := t.names.Lookup(queriedName)
	if entry == nil || entry.State != nameStateRegistered {
		return
	}
	statusPayload, tooLong, tooBig := t.buildStatusPayload(frame.Data2)
	data2 := uint16(len(statusPayload)) & 0x3FFF
	if tooLong {
		data2 |= 0x8000
	}
	if tooBig {
		data2 |= 0x4000
	}

	resp := &nbfproto.Frame{
		Command:        nbfproto.CmdStatusResponse,
		Data1:          0x00,
		Data2:          data2,
		XmitCorrelator: frame.RspCorrelator,
		RspCorrelator:  t.nextCorrelator(),
		Payload:        statusPayload,
	}
	copy(resp.DestinationName[:], frame.SourceName[:])
	copy(resp.SourceName[:], queriedName[:])

	if err := t.sendFrame(srcMAC, resp, "status-response"); err != nil {
		netlog.Warn("[NetBEUI] STATUS_RESPONSE send error: %v", err)
		return
	}
	netlog.Debug("[NetBEUI] STATUS_RESPONSE for %q to %02X:%02X:%02X:%02X:%02X:%02X",
		queriedName.String(),
		srcMAC[0], srcMAC[1], srcMAC[2], srcMAC[3], srcMAC[4], srcMAC[5])
}

func (t *transport) handleStatusResponse(srcMAC [6]byte, frame *nbfproto.Frame) {
	netlog.Debug("[NetBEUI] STATUS_RESPONSE from %02X:%02X:%02X:%02X:%02X:%02X for %q",
		srcMAC[0], srcMAC[1], srcMAC[2], srcMAC[3], srcMAC[4], srcMAC[5],
		protocol.Name(frame.SourceName).String())
}

func sessionWireKey(srcMAC [6]byte, localNum uint8) [7]byte {
	var k [7]byte
	copy(k[:6], srcMAC[:])
	k[6] = localNum
	return k
}

func (t *transport) sendSessionPayload(remoteMac [6]byte, localNum, remoteNum uint8, payload []byte) error {
	key := sessionWireKey(remoteMac, localNum)
	maxPayload := 1464
	t.txMu.Lock()
	if v, ok := t.sessionMaxPayload[key]; ok && v > 0 {
		maxPayload = int(v)
	}
	t.txMu.Unlock()
	if maxPayload < 1 {
		maxPayload = 1
	}

	frames := make([]*nbfproto.Frame, 0, (len(payload)/maxPayload)+1)
	if len(payload) == 0 {
		frames = append(frames, &nbfproto.Frame{
			Command:      nbfproto.CmdDataOnlyLast,
			DestNumber:   remoteNum,
			SourceNumber: localNum,
		})
	} else {
		for off := 0; off < len(payload); off += maxPayload {
			end := off + maxPayload
			if end > len(payload) {
				end = len(payload)
			}
			cmd := nbfproto.CmdDataFirstMiddle
			if end == len(payload) {
				cmd = nbfproto.CmdDataOnlyLast
			}
			corr := uint16(0)
			if cmd == nbfproto.CmdDataOnlyLast {
				corr = t.nextCorrelator()
			}
			frames = append(frames, &nbfproto.Frame{
				Command:       cmd,
				RspCorrelator: corr,
				DestNumber:    remoteNum,
				SourceNumber:  localNum,
				Payload:       append([]byte(nil), payload[off:end]...),
			})
		}
	}

	t.txMu.Lock()
	if t.txBlocked[key] {
		t.txPendingFrames[key] = append(t.txPendingFrames[key], frames...)
		t.txMu.Unlock()
		return nil
	}
	t.txMu.Unlock()

	return t.sendSessionFramesNow(remoteMac, localNum, frames)
}

func (t *transport) sendSessionFramesNow(remoteMac [6]byte, localNum uint8, frames []*nbfproto.Frame) error {
	key := sessionWireKey(remoteMac, localNum)
	for _, f := range frames {
		if err := t.sendFrame(remoteMac, f, "session-send"); err != nil {
			return err
		}
		t.txMu.Lock()
		cp := *f
		cp.Payload = append([]byte(nil), f.Payload...)
		t.txLastFrame[key] = &cp
		t.txMu.Unlock()
	}
	return nil
}

func (t *transport) handleNoReceive(srcMAC [6]byte, frame *nbfproto.Frame) {
	if t.sessionForInbound(srcMAC, frame.DestNumber, frame.SourceNumber, true) == nil {
		return
	}
	t.txMu.Lock()
	t.txBlocked[sessionWireKey(srcMAC, frame.DestNumber)] = true
	t.txMu.Unlock()
	netlog.Debug("[NetBEUI] NO_RECEIVE from %02X:%02X:%02X:%02X:%02X:%02X session %d",
		srcMAC[0], srcMAC[1], srcMAC[2], srcMAC[3], srcMAC[4], srcMAC[5], frame.DestNumber)
}

func (t *transport) handleReceiveContinue(srcMAC [6]byte, frame *nbfproto.Frame) {
	if t.sessionForInbound(srcMAC, frame.DestNumber, frame.SourceNumber, true) == nil {
		return
	}
	key := sessionWireKey(srcMAC, frame.DestNumber)
	t.txMu.Lock()
	t.txBlocked[key] = false
	pending := t.txPendingFrames[key]
	delete(t.txPendingFrames, key)
	t.txMu.Unlock()
	if len(pending) > 0 {
		if err := t.sendSessionFramesNow(srcMAC, frame.DestNumber, pending); err != nil {
			netlog.Warn("[NetBEUI] pending send on RECEIVE_CONTINUE failed: %v", err)
		}
	}
	netlog.Debug("[NetBEUI] RECEIVE_CONTINUE from %02X:%02X:%02X:%02X:%02X:%02X session %d",
		srcMAC[0], srcMAC[1], srcMAC[2], srcMAC[3], srcMAC[4], srcMAC[5], frame.DestNumber)
}

func (t *transport) handleReceiveOutstanding(srcMAC [6]byte, frame *nbfproto.Frame) {
	if t.sessionForInbound(srcMAC, frame.DestNumber, frame.SourceNumber, true) == nil {
		return
	}
	key := sessionWireKey(srcMAC, frame.DestNumber)
	t.txMu.Lock()
	last := t.txLastFrame[key]
	t.txMu.Unlock()
	if last == nil {
		return
	}
	if err := t.sendFrame(srcMAC, last, "receive-outstanding-retransmit"); err != nil {
		netlog.Warn("[NetBEUI] retransmit on RECEIVE_OUTSTANDING failed: %v", err)
	}
	netlog.Debug("[NetBEUI] RECEIVE_OUTSTANDING from %02X:%02X:%02X:%02X:%02X:%02X session %d",
		srcMAC[0], srcMAC[1], srcMAC[2], srcMAC[3], srcMAC[4], srcMAC[5], frame.DestNumber)
}

func sessionFragmentKey(srcMAC [6]byte, localNum uint8) [7]byte {
	var k [7]byte
	copy(k[:6], srcMAC[:])
	k[6] = localNum
	return k
}

// buildStatusPayload assembles adapter status data. The payload format
// is a compact list of local names where each entry is 16-byte name,
// 1-byte local name number, and 1-byte flags (bit 7 indicates group).
func (t *transport) buildStatusPayload(requestedBufLen uint16) ([]byte, bool, bool) {
	registered := t.names.Registered()
	if len(registered) == 0 {
		return nil, false, false
	}

	full := make([]byte, 0, len(registered)*18)
	for _, e := range registered {
		entry := make([]byte, 18)
		copy(entry[0:16], e.Name[:])
		entry[16] = e.Number
		if e.IsGroup {
			entry[17] = 0x80
		}
		full = append(full, entry...)
	}

	maxLen := int(requestedBufLen)
	if maxLen <= 0 {
		return nil, len(full) > 0, len(full) > 0
	}
	if len(full) <= maxLen {
		return full, false, false
	}
	if maxLen < 18 {
		return nil, true, true
	}
	truncLen := (maxLen / 18) * 18
	if truncLen == 0 {
		return nil, true, true
	}
	out := make([]byte, truncLen)
	copy(out, full[:truncLen])
	return out, true, true
}
