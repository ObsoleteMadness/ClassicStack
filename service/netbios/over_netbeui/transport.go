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
		if err := t.port.SendBroadcast(frame); err != nil {
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

	return t.port.SendBroadcast(frame)
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
	sess.mu.Lock()
	if sess.state != sessionStateActive {
		sess.mu.Unlock()
		return nb.ErrNotImplemented
	}

	corr := t.nextCorrelator()
	sess.lastXmitCorrelator = corr
	destNum := sess.remoteNum
	srcNum := sess.localNum
	remoteMac := sess.remoteMAC
	sess.mu.Unlock()

	frame := &nbfproto.Frame{
		Command:        nbfproto.CmdDataOnlyLast,
		RspCorrelator:  corr,
		DestNumber:     destNum,
		SourceNumber:   srcNum,
		Payload:        s.Payload,
	}

	return t.port.Send(remoteMac, frame)
}

// --- Inbound Frame Dispatch ---

func (t *transport) onFrame(srcMAC, dstMAC [6]byte, frame *nbfproto.Frame) {
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
		netlog.Debug("[NetBEUI] NO_RECEIVE from %02X:%02X:%02X:%02X:%02X:%02X",
			srcMAC[0], srcMAC[1], srcMAC[2], srcMAC[3], srcMAC[4], srcMAC[5])
	case nbfproto.CmdReceiveOutstanding:
		netlog.Debug("[NetBEUI] RECEIVE_OUTSTANDING from %02X:%02X:%02X:%02X:%02X:%02X",
			srcMAC[0], srcMAC[1], srcMAC[2], srcMAC[3], srcMAC[4], srcMAC[5])
	case nbfproto.CmdReceiveContinue:
		netlog.Debug("[NetBEUI] RECEIVE_CONTINUE from %02X:%02X:%02X:%02X:%02X:%02X",
			srcMAC[0], srcMAC[1], srcMAC[2], srcMAC[3], srcMAC[4], srcMAC[5])

	// --- Status ---
	case nbfproto.CmdStatusQuery:
		netlog.Debug("[NetBEUI] STATUS_QUERY from %02X:%02X:%02X:%02X:%02X:%02X (not implemented)",
			srcMAC[0], srcMAC[1], srcMAC[2], srcMAC[3], srcMAC[4], srcMAC[5])
	case nbfproto.CmdStatusResponse:
		netlog.Debug("[NetBEUI] STATUS_RESPONSE from %02X:%02X:%02X:%02X:%02X:%02X (not implemented)",
			srcMAC[0], srcMAC[1], srcMAC[2], srcMAC[3], srcMAC[4], srcMAC[5])

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

	if err := t.port.Send(srcMAC, resp); err != nil {
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
		sess := t.sessions.Create(srcMAC)
		localSessionNum = sess.localNum
	}

	resp := &nbfproto.Frame{
		Command:        nbfproto.CmdNameRecognized,
		XmitCorrelator: frame.RspCorrelator,
		RspCorrelator:  t.nextCorrelator(),
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

	if err := t.port.Send(srcMAC, resp); err != nil {
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

	sess.mu.Lock()
	sess.remoteNum = srcNum
	sess.state = sessionStateActive
	sess.mu.Unlock()

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

	if err := t.port.Send(srcMAC, confirm); err != nil {
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

	sess.mu.Lock()
	sess.remoteNum = remoteNum
	sess.state = sessionStateActive
	sess.mu.Unlock()

	netlog.Info("[NetBEUI] session %d↔%d confirmed by %02X:%02X:%02X:%02X:%02X:%02X",
		localNum, remoteNum,
		srcMAC[0], srcMAC[1], srcMAC[2], srcMAC[3], srcMAC[4], srcMAC[5])
}

// handleSessionEnd: peer is tearing down the session.
func (t *transport) handleSessionEnd(srcMAC [6]byte, frame *nbfproto.Frame) {
	localNum := frame.DestNumber

	sess := t.sessions.Lookup(srcMAC, localNum)
	if sess == nil {
		return
	}

	sess.mu.Lock()
	sess.state = sessionStateClosed
	sess.mu.Unlock()
	t.sessions.Remove(srcMAC, localNum)

	netlog.Info("[NetBEUI] session %d ended by %02X:%02X:%02X:%02X:%02X:%02X",
		localNum, srcMAC[0], srcMAC[1], srcMAC[2], srcMAC[3], srcMAC[4], srcMAC[5])
}

// handleSessionAlive: keepalive probe — just log it.
func (t *transport) handleSessionAlive(srcMAC [6]byte, frame *nbfproto.Frame) {
	netlog.Debug("[NetBEUI] SESSION_ALIVE from %02X:%02X:%02X:%02X:%02X:%02X session %d",
		srcMAC[0], srcMAC[1], srcMAC[2], srcMAC[3], srcMAC[4], srcMAC[5],
		frame.DestNumber)
}

// --- Session Data Transfer ---

// handleDataOnlyLast: received a complete data message. Deliver to
// the handler and send DATA_ACK.
func (t *transport) handleDataOnlyLast(srcMAC [6]byte, frame *nbfproto.Frame) {
	localNum := frame.DestNumber

	sess := t.sessions.Lookup(srcMAC, localNum)
	if sess == nil {
		return
	}

	// Send DATA_ACK.
	ack := &nbfproto.Frame{
		Command:        nbfproto.CmdDataAck,
		XmitCorrelator: frame.RspCorrelator,
		DestNumber:     frame.SourceNumber,
		SourceNumber:   localNum,
	}
	if err := t.port.Send(srcMAC, ack); err != nil {
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
		Payload: frame.Payload,
	}
	if err := handler.HandleSession(pkt); err != nil {
		netlog.Warn("[NetBEUI] session handler error: %v", err)
	}
}

// handleDataFirstMiddle: first or middle segment of a multi-segment
// message. In this simplified implementation we deliver each segment
// as a standalone packet.
func (t *transport) handleDataFirstMiddle(srcMAC [6]byte, frame *nbfproto.Frame) {
	// For now, deliver segments individually. A proper implementation
	// would reassemble into complete messages.
	t.handleDataOnlyLast(srcMAC, frame)
}

// handleDataAck: the remote acknowledged our DATA_ONLY_LAST.
func (t *transport) handleDataAck(srcMAC [6]byte, frame *nbfproto.Frame) {
	localNum := frame.DestNumber
	sess := t.sessions.Lookup(srcMAC, localNum)
	if sess == nil {
		return
	}
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
	if err := handler.HandleDatagram(d); err != nil {
		netlog.Warn("[NetBEUI] datagram handler error: %v", err)
	}
}

func (t *transport) handleDatagramBroadcast(srcMAC [6]byte, frame *nbfproto.Frame) {
	// Same handling as directed datagram — the broadcast distinction
	// is at the link layer (multicast MAC), not in the handler.
	t.handleDatagram(srcMAC, frame)
}
