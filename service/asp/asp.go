/*
Package asp implements the AppleTalk Session Protocol (ASP) as a omnitalk
service. The ATP transaction layer is provided by go/service/atp; this file
is concerned only with ASP semantics — session lifecycle, command/write
dispatch, tickle keep-alives, attentions — and delegates all retry, XO
duplicate filtering, and TRel handling to atp.Endpoint.

Inside Macintosh: Networking, Chapter 8.
*/
package asp

import (
	"context"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/pgodw/omnitalk/appletalk"
	"github.com/pgodw/omnitalk/netlog"
	"github.com/pgodw/omnitalk/port"
	"github.com/pgodw/omnitalk/service"
	"github.com/pgodw/omnitalk/service/afp"
	"github.com/pgodw/omnitalk/service/atp"
	"github.com/pgodw/omnitalk/service/zip"
)

// ServerSocket is the well-known AppleTalk socket for the AFP/ASP server.
const ServerSocket = 252

// nbpType is the NBP entity type that Finder uses to discover AFP servers.
const nbpType = "AFPServer"

// Service implements ASP on top of an atp.Endpoint.
type Service struct {
	serverName     string
	commandHandler afp.CommandHandler
	nbp            *zip.NameInformationService
	zoneName       []byte

	// SPGetParms results.
	maxCmdSize  int
	quantumSize int

	router          service.Router
	registeredZones [][]byte

	endpoint *atp.Endpoint
	sm       *SessionManager

	onSessionOpen     func(*Session)
	onSessionClose    func(*Session)
	onSessionActivity func(*Session)
}

// Spec-to-implementation mapping notes:
//   - No separate SPGetSession method: session acceptance is handled inside
//     handleOpenSession.
//   - No separate SPGetRequest/SPCmdReply/SPWrtReply/SPWrtContinue methods:
//     these are represented by handleCommand/handleASPWrite/completeWrite.
//   - No separate SPNewStatus method: status is sourced from
//     commandHandler.GetStatus() when servicing SPGetStatus.

// requestContext is what the host service threads through atp.HandleInbound
// so the Sender bridge can use router.Reply on the way out.
type requestContext struct {
	d appletalk.Datagram
	p port.Port
}

// New creates an ASP service.
func New(serverName string, handler afp.CommandHandler, nbp *zip.NameInformationService, zone []byte) *Service {
	s := &Service{
		serverName:     serverName,
		commandHandler: handler,
		nbp:            nbp,
		zoneName:       append([]byte(nil), zone...),
	}
	s.sm = NewSessionManager(s.sendTickle)
	s.sm.SetOnClose(func(sess *Session) {
		if s.onSessionClose != nil {
			s.onSessionClose(sess)
		}
	})
	return s
}

// SetSessionLifecycleHooks registers callbacks for ASP session open/close/activity.
func (s *Service) SetSessionLifecycleHooks(onOpen, onClose, onActivity func(*Session)) {
	s.onSessionOpen = onOpen
	s.onSessionClose = onClose
	s.onSessionActivity = onActivity
}

// SetCommandHandler assigns the AFP command handler to this service.
func (s *Service) SetCommandHandler(handler afp.CommandHandler) {
	s.commandHandler = handler
}

// Socket returns the socket number this service listens on.
func (s *Service) Socket() uint8 { return ServerSocket }

// Start performs server-side initialization corresponding to:
//   - SPGetParms (server end; server ASP client -> ASP)
//   - SPInit (server end; server ASP client -> ASP)
//
// In this implementation, SPInit is represented by wiring the SLS endpoint and
// validating ServiceStatusBlock size against QuantumSize before accepting
// traffic.
func (s *Service) Start(router service.Router) error {
	s.router = router

	parms := s.SPGetParms()
	s.maxCmdSize = int(parms.MaxCmdSize)
	s.quantumSize = int(parms.QuantumSize)
	netlog.Info("[ASP] SPGetParms: MaxCmdSize=%d QuantumSize=%d", s.maxCmdSize, s.quantumSize)

	if s.commandHandler != nil {
		status := s.commandHandler.GetStatus()
		if len(status) > s.quantumSize {
			return fmt.Errorf("ASP SPInit: ServiceStatusBlock size %d exceeds QuantumSize %d",
				len(status), s.quantumSize)
		}
		netlog.Info("[ASP] SPInit: SLS socket=%d status=%d bytes", ServerSocket, len(status))
		// Inform the AFP handler of our quantum so it can cap per-read allocations
		// (e.g. HTTP range requests for virtual filesystems). DSI leaves this unset.
		type readLimiter interface{ SetMaxReadSize(int) }
		if rl, ok := s.commandHandler.(readLimiter); ok {
			rl.SetMaxReadSize(s.quantumSize)
			netlog.Debug("[ASP] SetMaxReadSize=%d on command handler", s.quantumSize)
		}
	}

	// The Endpoint's "local" address has its socket field set; the network
	// and node fields are filled per-call by the Sender bridge from the
	// inbound datagram (the router knows our address, not us).
	s.endpoint = atp.NewEndpoint(
		atp.Address{Socket: ServerSocket},
		atp.SenderFunc(s.sendBridge),
	)
	s.endpoint.Listen(s.handleATPRequest)

	if len(s.zoneName) > 0 {
		s.registerInZone(s.zoneName)
	} else {
		zones := router.Zones()
		if len(zones) == 0 {
			s.registerInZone(nil)
		} else {
			for _, z := range zones {
				s.registerInZone(z)
			}
		}
	}
	return nil
}

func (s *Service) registerInZone(zone []byte) {
	s.nbp.RegisterName([]byte(s.serverName), []byte(nbpType), zone, ServerSocket)
	s.registeredZones = append(s.registeredZones, append([]byte(nil), zone...))
	netlog.Info("AFP: registered NBP %q:%s@%q socket=%d", s.serverName, nbpType, zone, ServerSocket)
}

// Stop unregisters NBP and shuts everything down.
// Before teardown, it sends a best-effort SPAttention(ServerGoingDown) to
// active sessions so workstation clients can terminate cleanly.
func (s *Service) Stop() error {
	for _, sessID := range s.sm.SessionIDs() {
		if err := s.SendAttention(sessID, AspAttnServerGoingDown); err != nil {
			netlog.Debug("[ASP] Stop: SendAttention failed for sess=%d: %v", sessID, err)
		}
	}
	for _, z := range s.registeredZones {
		s.nbp.UnregisterName([]byte(s.serverName), []byte(nbpType), z)
	}
	s.sm.Stop()
	return nil
}

// Inbound accepts an incoming DDP datagram. ATP type only.
func (s *Service) Inbound(d appletalk.Datagram, p port.Port) {
	if d.DDPType != atp.DDPTypeATP {
		return
	}
	if s.endpoint == nil {
		return
	}
	src := atp.Address{Net: d.SourceNetwork, Node: d.SourceNode, Socket: d.SourceSocket}
	local := atp.Address{Net: d.DestinationNetwork, Node: d.DestinationNode, Socket: d.DestinationSocket}
	hint := &requestContext{d: d, p: p}
	s.endpoint.HandleInbound(d.Data, src, local, hint)
}

// sendBridge is the atp.Sender implementation. It maps engine outbound
// packets to router calls. For responder-side sends (TResp / cached XO
// replays) hint != nil and we use router.Reply so non-extended LToUDP
// broadcasts work correctly. For requester-side sends (TReq, TRel) hint is
// nil and we use router.Route with explicit src/dst.
func (s *Service) sendBridge(src, dst atp.Address, payload []byte, hint any) error {
	if rc, ok := hint.(*requestContext); ok && rc != nil {
		// Use router.Reply so it can pick the correct outbound port and
		// handle the unnumbered-network broadcast case.
		s.router.Reply(rc.d, rc.p, atp.DDPTypeATP, payload)
		return nil
	}
	dg := appletalk.Datagram{
		HopCount:           0,
		DestinationNetwork: dst.Net,
		DestinationNode:    dst.Node,
		DestinationSocket:  dst.Socket,
		SourceNetwork:      src.Net,
		SourceNode:         src.Node,
		SourceSocket:       src.Socket,
		DDPType:            atp.DDPTypeATP,
		Data:               append([]byte(nil), payload...),
	}
	return s.router.Route(dg, true)
}

// handleATPRequest is the server-side dispatcher for ASP network requests.
// Direction by SPFunction per spec:
//   - workstation -> server: OpenSess, GetStatus, Command, Write, CloseSess
//   - both directions: Tickle
//
// It demultiplexes on the ASP function code in the user-data MSB.
func (s *Service) handleATPRequest(in atp.IncomingRequest, reply atp.Replier) {
	aspCmd := uint8((in.UserBytes >> 24) & 0xFF)
	netlog.Debug("[ASP] cmd=%d from %s tid=%d", aspCmd, in.Src, in.TID)
	switch aspCmd {
	case SPFuncGetStatus:
		s.handleGetStatus(in, reply)
	case SPFuncOpenSess:
		s.handleOpenSession(in, reply)
	case SPFuncCommand:
		s.handleCommand(in, reply)
	case SPFuncWrite:
		s.handleASPWrite(in, reply)
	case SPFuncTickle:
		// Client keepalive; update activity, no reply needed (ATP TReq with
		// no buffers reserved is invalid, but the engine will still create
		// an RspCB for XO; we reply with an empty message to drain it).
		sessID := uint8((in.UserBytes >> 16) & 0xFF)
		if sess := s.sm.Get(sessID); sess != nil {
			sess.touchActivity()
			if s.onSessionActivity != nil {
				s.onSessionActivity(sess)
			}
		}
		reply(atp.ResponseMessage{Buffers: [][]byte{nil}})
	case SPFuncCloseSess:
		s.handleCloseSession(in, reply)
	default:
		netlog.Debug("[ASP] unhandled cmd %d", aspCmd)
		reply(atp.ResponseMessage{Buffers: [][]byte{nil}})
	}
}

// bitmapMaxBytes returns the maximum bytes the workstation can receive for a
// given ATP receive bitmap: each set bit represents one TResp slot of ATPMaxData
// bytes. A zero bitmap is treated as unconstrained (returns 0 to signal "use
// server max").
func bitmapMaxBytes(bitmap uint8) int {
	n := 0
	for b := bitmap; b != 0; b >>= 1 {
		n += int(b & 1)
	}
	return n * ATPMaxData
}

// chunkResponse splits raw response data into <= ATPMaxData byte buffers.
// The effective cap is the smaller of the server's QuantumSize and the
// workstation's receive capacity derived from its ATP request bitmap.
func (s *Service) chunkResponse(data []byte, bitmap uint8) [][]byte {
	effective := s.quantumSize
	if ws := bitmapMaxBytes(bitmap); ws > 0 && ws < effective {
		effective = ws
	}
	if len(data) > effective {
		data = data[:effective]
	}
	if len(data) == 0 {
		return [][]byte{nil}
	}
	n := (len(data) + ATPMaxData - 1) / ATPMaxData
	bufs := make([][]byte, n)
	for i := range n {
		start := i * ATPMaxData
		end := min(start+ATPMaxData, len(data))
		bufs[i] = data[start:end]
	}
	return bufs
}

// handleGetStatus implements SPGetStatus servicing on the server side
// (workstation ASP client -> server SLS).
//
// Related server-end calls from the spec:
//   - SPInit provides initial ServiceStatusBlock.
//   - SPNewStatus updates status for later SPGetStatus calls.
//
// In this code, status comes from commandHandler.GetStatus() at request time.
func (s *Service) handleGetStatus(in atp.IncomingRequest, reply atp.Replier) {
	var status []byte
	if s.commandHandler != nil {
		status = s.commandHandler.GetStatus()
	}
	if len(status) > s.effectiveQuantumSize() {
		netlog.Info("[ASP] GetStatus: ServiceStatusBlockSize=%d exceeds QuantumSize=%d (SPErrorSizeErr)",
			len(status), s.effectiveQuantumSize())
		reply(atp.ResponseMessage{
			Buffers:   [][]byte{nil},
			UserBytes: []uint32{errToUserBytes(SPErrorSizeErr)},
		})
		return
	}
	reply(atp.ResponseMessage{Buffers: s.chunkResponse(status, in.Bitmap)})
}

// handleOpenSession implements SPOpenSession handling at the server side
// (workstation ASP client -> server SLS).
//
// Spec note: classic ASP may gate acceptance on pending SPGetSession calls.
// This implementation models SPGetSession implicitly by accepting while session
// capacity is available.
func (s *Service) handleOpenSession(in atp.IncomingRequest, reply atp.Replier) {
	pkt := ParseOpenSessPacket(in.UserBytes)

	if pkt.VersionNum != ASPVersion {
		netlog.Info("[ASP] OpenSess: bad version 0x%04X from %s", pkt.VersionNum, in.Src)
		r := OpenSessReplyPacket{SSSSocket: ServerSocket, ErrorCode: SPErrorBadVersNum}
		reply(atp.ResponseMessage{
			Buffers:   [][]byte{nil},
			UserBytes: []uint32{r.MarshalUserData()},
		})
		return
	}

	sess := s.sm.Open(in.Src.Net, in.Src.Node, pkt.WSSSocket, in.Local.Net, in.Local.Node)
	if sess == nil {
		r := OpenSessReplyPacket{SSSSocket: ServerSocket, ErrorCode: SPErrorTooManyClients}
		reply(atp.ResponseMessage{
			Buffers:   [][]byte{nil},
			UserBytes: []uint32{r.MarshalUserData()},
		})
		return
	}
	netlog.Info("[ASP] OpenSess: sess=%d from %s wss=%d", sess.ID, in.Src, pkt.WSSSocket)
	if s.onSessionOpen != nil {
		s.onSessionOpen(sess)
	}
	r := OpenSessReplyPacket{SSSSocket: ServerSocket, SessionID: sess.ID, ErrorCode: SPErrorNoError}
	reply(atp.ResponseMessage{
		Buffers:   [][]byte{nil},
		UserBytes: []uint32{r.MarshalUserData()},
	})
}

// handleCloseSession handles CloseSess packets from workstation -> server and
// maps them to server-side SPCloseSession semantics.
func (s *Service) handleCloseSession(in atp.IncomingRequest, reply atp.Replier) {
	pkt := ParseCloseSessPacket(in.UserBytes)
	if s.sm.Get(pkt.SessionID) == nil {
		netlog.Debug("[ASP] CloseSess: unknown SessRefNum=%d", pkt.SessionID)
		reply(atp.ResponseMessage{
			Buffers:   [][]byte{nil},
			UserBytes: []uint32{errToUserBytes(SPErrorParamErr)},
		})
		return
	}
	s.sm.Close(pkt.SessionID)
	reply(atp.ResponseMessage{
		Buffers:   [][]byte{nil},
		UserBytes: []uint32{CloseSessReplyUserData()},
	})
}

// handleCommand implements the SPCommand/SPCmdReply transaction path:
//  1. workstation -> server Command request
//  2. server -> workstation CmdReply result
//
// In classic server-end API terms, this combines SPGetRequest (Command type)
// and SPCmdReply.
func (s *Service) handleCommand(in atp.IncomingRequest, reply atp.Replier) {
	receivedAt := time.Now()
	pkt := ParseCommandPacket(in.UserBytes, in.Data)
	if len(pkt.CmdBlock) > s.effectiveMaxCmdSize() {
		netlog.Debug("[ASP] Command: CmdBlockSize=%d exceeds MaxCmdSize=%d (SPErrorSizeErr)",
			len(pkt.CmdBlock), s.effectiveMaxCmdSize())
		reply(atp.ResponseMessage{
			Buffers:   [][]byte{nil},
			UserBytes: []uint32{errToUserBytes(SPErrorSizeErr)},
		})
		return
	}
	sess := s.sm.Get(pkt.SessionID)
	if sess == nil {
		netlog.Debug("[ASP] Command: unknown SessRefNum=%d", pkt.SessionID)
		reply(atp.ResponseMessage{
			Buffers:   [][]byte{nil},
			UserBytes: []uint32{errToUserBytes(SPErrorParamErr)},
		})
		return
	}
	sess.touchActivity()
	if s.onSessionActivity != nil {
		s.onSessionActivity(sess)
	}
	if !sess.CheckDuplicate(pkt.SeqNum, in.TID) {
		netlog.Debug("[ASP] Command: ASP-level duplicate seqNum=%d on sess=%d, dropping",
			pkt.SeqNum, pkt.SessionID)
		// We must still respond — the ATP engine will use cached response
		// from the RspCB if it sees a true ATP retransmit; for ASP-level
		// duplicates we send an empty result.
		reply(atp.ResponseMessage{Buffers: [][]byte{nil}})
		return
	}

	var replyData []byte
	var errCode int32
	if s.commandHandler != nil {
		replyData, errCode = s.commandHandler.HandleCommand(pkt.CmdBlock)
	}

	// Per AFP-over-ASP spec: FPRead, FPWrite, FPEnumerate can succeed partially.
	// If the reply exceeds QuantumSize, truncate it here but preserve the original
	// AFP error code (e.g., ErrEOFErr or NoErr). The workstation will make
	// additional requests at adjusted offsets to retrieve the rest.
	if len(replyData) > s.effectiveQuantumSize() {
		netlog.Debug("[ASP] Command: SessRefNum=%d CmdReplyDataSize=%d exceeds QuantumSize=%d (truncating, preserving errCode=%d)",
			pkt.SessionID, len(replyData), s.effectiveQuantumSize(), errCode)
		replyData = replyData[:s.effectiveQuantumSize()]
	}
	bufs := s.chunkResponse(replyData, in.Bitmap)
	reply(atp.ResponseMessage{
		Buffers:   bufs,
		UserBytes: []uint32{errToUserBytes(errCode)},
	})
	elapsed := time.Since(receivedAt)
	replyBytes := 0
	for _, b := range bufs {
		replyBytes += len(b)
	}
	if replyBytes > 0 {
		netlog.Debug("[ASP] sess=%d seq=%d: replied %d bytes in %v (%.1f KB/s processing)",
			pkt.SessionID, pkt.SeqNum, replyBytes, elapsed.Round(time.Millisecond),
			float64(replyBytes)/elapsed.Seconds()/1024)
	}
}

// handleASPWrite implements SPWrite handling (phase 1 of 2) on the server side:
//
//  1. workstation -> server: Write TReq with command block
//  2. server -> workstation: SPWrtContinue (WriteContinue TReq)
//  3. workstation -> server: WriteContinue TResp with write data
//  4. server -> workstation: SPWrtReply for the original Write TReq
//
// We capture `reply` from step 1 and invoke it in step 4 once the
// WriteContinue Pending resolves with the data.
//
// In classic server-end API terms, this combines SPGetRequest (Write type)
// with SPWrtContinue and SPWrtReply.
func (s *Service) handleASPWrite(in atp.IncomingRequest, reply atp.Replier) {
	receivedAt := time.Now()
	pkt := ParseWritePacket(in.UserBytes, in.Data)
	if len(pkt.CmdBlock) > s.effectiveMaxCmdSize() {
		netlog.Debug("[ASP] Write: CmdBlockSize=%d exceeds MaxCmdSize=%d (SPErrorSizeErr)",
			len(pkt.CmdBlock), s.effectiveMaxCmdSize())
		reply(atp.ResponseMessage{
			Buffers:   [][]byte{nil},
			UserBytes: []uint32{errToUserBytes(SPErrorSizeErr)},
		})
		return
	}
	sess := s.sm.Get(pkt.SessionID)
	if sess == nil {
		netlog.Debug("[ASP] Write: unknown SessRefNum=%d", pkt.SessionID)
		reply(atp.ResponseMessage{
			Buffers:   [][]byte{nil},
			UserBytes: []uint32{errToUserBytes(SPErrorParamErr)},
		})
		return
	}
	sess.touchActivity()
	if s.onSessionActivity != nil {
		s.onSessionActivity(sess)
	}
	if !sess.CheckDuplicate(pkt.SeqNum, in.TID) {
		netlog.Debug("[ASP] Write: duplicate seqNum=%d on sess=%d, dropping",
			pkt.SeqNum, pkt.SessionID)
		reply(atp.ResponseMessage{Buffers: [][]byte{nil}})
		return
	}

	var wantBytes uint32
	if len(pkt.CmdBlock) >= 12 {
		rawWantBytes := int32(binary.BigEndian.Uint32(pkt.CmdBlock[8:12]))
		if rawWantBytes < 0 {
			netlog.Debug("[ASP] Write: negative BufferSize=%d in SPWrtContinue request metadata (SPErrorParamErr)",
				rawWantBytes)
			reply(atp.ResponseMessage{
				Buffers:   [][]byte{nil},
				UserBytes: []uint32{errToUserBytes(SPErrorParamErr)},
			})
			return
		}
		wantBytes = uint32(rawWantBytes)
	}
	if max := uint32(s.quantumSize); wantBytes > max {
		netlog.Info("[ASP] Write sess=%d: clamping wantBytes %d→%d",
			pkt.SessionID, wantBytes, max)
		wantBytes = max
	}

	// Number of TResp packets we expect from the workstation.
	numPkts := int((wantBytes + ATPMaxData - 1) / ATPMaxData)
	if numPkts == 0 {
		numPkts = 1
	}
	if numPkts > ATPMaxPackets {
		numPkts = ATPMaxPackets
	}

	wcPkt := WriteContinuePacket{
		SessionID:  pkt.SessionID,
		SeqNum:     pkt.SeqNum,
		BufferSize: uint16(wantBytes),
	}
	wcData := wcPkt.MarshalData()

	// Issue the WriteContinue TReq from the server's address as the Mac
	// knows it (the destination of the original Write).
	src := atp.Address{Net: in.Local.Net, Node: in.Local.Node, Socket: ServerSocket}
	dst := atp.Address{Net: sess.WSNet, Node: sess.WSNode, Socket: sess.WSSkt}

	pending, err := s.endpoint.SendRequest(atp.Request{
		Src:          src,
		Dst:          dst,
		UserBytes:    wcPkt.MarshalUserData(),
		Data:         wcData,
		NumBuffers:   numPkts,
		XO:           true,
		TRelTO:       atp.TRel30s,
		RetryTimeout: 2 * time.Second,
		MaxRetries:   8,
	})
	if err != nil {
		netlog.Debug("[ASP] Write sess=%d: WriteContinue SendRequest failed: %v", pkt.SessionID, err)
		reply(atp.ResponseMessage{
			Buffers:   [][]byte{nil},
			UserBytes: []uint32{errToUserBytes(SPErrorParamErr)},
		})
		return
	}

	// Stash the in-flight write so CloseSess can cancel it.
	sess.writeMu.Lock()
	sess.write = &writeState{
		seqNum:    pkt.SeqNum,
		cmdBlock:  pkt.CmdBlock,
		wantBytes: wantBytes,
		reply:     reply,
		pending:   pending,
	}
	sess.writeMu.Unlock()

	wcSentAt := time.Now()

	// Wait for the data in a goroutine so we don't block the engine.
	// Pass the original Write TReq bitmap so the final reply respects the
	// workstation's receive capacity.
	go s.completeWrite(sess, pkt.CmdBlock, wantBytes, pending, reply, in.Bitmap, receivedAt, wcSentAt)
}

// completeWrite finalizes the server-side SPWrite flow after SPWrtContinue has
// returned write data, then sends the SPWrtReply-equivalent result.
func (s *Service) completeWrite(sess *Session, cmdBlock []byte, wantBytes uint32,
	pending *atp.Pending, reply atp.Replier, bitmap uint8, receivedAt, wcSentAt time.Time) {
	resp, err := pending.Wait(context.Background())
	wcRTT := time.Since(wcSentAt)
	// Clear the pending state regardless of outcome.
	sess.writeMu.Lock()
	sess.write = nil
	sess.writeMu.Unlock()

	if err != nil {
		netlog.Debug("[ASP] Write sess=%d: WriteContinue failed after %v: %v", sess.ID, wcRTT.Round(time.Millisecond), err)
		reply(atp.ResponseMessage{
			Buffers:   [][]byte{nil},
			UserBytes: []uint32{errToUserBytes(SPErrorParamErr)},
		})
		return
	}

	// Reassemble the write data in sequence order.
	var writeData []byte
	for _, b := range resp.Buffers {
		writeData = append(writeData, b...)
	}
	if uint32(len(writeData)) > wantBytes {
		writeData = writeData[:wantBytes]
	}
	netlog.Debug("[ASP] Write sess=%d: WriteContinue RTT=%v got %d bytes",
		sess.ID, wcRTT.Round(time.Millisecond), len(writeData))

	full := make([]byte, len(cmdBlock)+len(writeData))
	copy(full, cmdBlock)
	copy(full[len(cmdBlock):], writeData)

	var replyData []byte
	var errCode int32
	if s.commandHandler != nil {
		replyData, errCode = s.commandHandler.HandleCommand(full)
	}

	// Per AFP-over-ASP spec: FPRead, FPWrite, FPEnumerate can succeed partially.
	// If the reply exceeds QuantumSize, truncate it here but preserve the original
	// AFP error code. The workstation will make additional requests at adjusted
	// offsets to retrieve the rest.
	if len(replyData) > s.effectiveQuantumSize() {
		netlog.Debug("[ASP] Write: SessRefNum=%d WrtReplyDataSize=%d exceeds QuantumSize=%d (truncating, preserving errCode=%d)",
			sess.ID, len(replyData), s.effectiveQuantumSize(), errCode)
		replyData = replyData[:s.effectiveQuantumSize()]
	}
	bufs := s.chunkResponse(replyData, bitmap)
	reply(atp.ResponseMessage{
		Buffers:   bufs,
		UserBytes: []uint32{errToUserBytes(errCode)},
	})
	totalElapsed := time.Since(receivedAt)
	replyBytes := 0
	for _, b := range bufs {
		replyBytes += len(b)
	}
	netlog.Debug("[ASP] Write sess=%d: total latency=%v (WriteContinue RTT=%v + handler); replied %d bytes",
		sess.ID, totalElapsed.Round(time.Millisecond), wcRTT.Round(time.Millisecond), replyBytes)
}

// sendTickle sends an ASP Tickle as an ATP-ALO TReq with infinite retries.
// We don't actually want infinite retries (the maintenance loop will time
// the session out anyway); 1 retry is enough to detect responsiveness.
func (s *Service) sendTickle(sess *Session) {
	if s.endpoint == nil {
		return
	}
	src := atp.Address{Net: sess.SrvNet, Node: sess.SrvNode, Socket: ServerSocket}
	dst := atp.Address{Net: sess.WSNet, Node: sess.WSNode, Socket: sess.WSSkt}
	tp := TicklePacket{SessionID: sess.ID}
	pending, err := s.endpoint.SendRequest(atp.Request{
		Src: src, Dst: dst,
		UserBytes:    tp.MarshalUserData(),
		NumBuffers:   1,
		RetryTimeout: 5 * time.Second,
		MaxRetries:   1,
	})
	if err != nil {
		return
	}
	// Drain in the background — we don't actually need the response, but
	// we must release the TCB.
	go func() { _, _ = pending.Wait(context.Background()) }()
}

// errToUserBytes converts a (possibly negative) ASP error constant into the
// uint32 wire encoding without tripping Go's constant-overflow check.
func errToUserBytes(code int32) uint32 { return uint32(code) }

func (s *Service) effectiveQuantumSize() int {
	if s.quantumSize > 0 {
		return s.quantumSize
	}
	return QuantumSize
}

func (s *Service) effectiveMaxCmdSize() int {
	if s.maxCmdSize > 0 {
		return s.maxCmdSize
	}
	return ATPMaxData
}

// SPGetParms implements SPGetParms (both ends): ASP client -> ASP local query
// for MaxCmdSize and QuantumSize.
func (s *Service) SPGetParms() GetParmsResult {
	return GetParmsResult{MaxCmdSize: ATPMaxData, QuantumSize: QuantumSize}
}

// SendAttention implements server-side SPAttention
// (server ASP client -> workstation end of an open session).
func (s *Service) SendAttention(sessID uint8, code uint16) error {
	if code == 0 {
		return fmt.Errorf("ASP: attention code must be non-zero")
	}
	sess := s.sm.Get(sessID)
	if sess == nil {
		netlog.Debug("[ASP] Attention: unknown SessRefNum=%d", sessID)
		return fmt.Errorf("ASP SPAttention: unknown SessRefNum=%d (SPErrorParamErr=%d)", sessID, SPErrorParamErr)
	}
	if s.endpoint == nil {
		return fmt.Errorf("ASP: not started")
	}
	src := atp.Address{Net: sess.SrvNet, Node: sess.SrvNode, Socket: ServerSocket}
	dst := atp.Address{Net: sess.WSNet, Node: sess.WSNode, Socket: sess.WSSkt}
	ap := AttentionPacket{SessionID: sessID, AttentionCode: code}
	pending, err := s.endpoint.SendRequest(atp.Request{
		Src: src, Dst: dst,
		UserBytes:    ap.MarshalUserData(),
		NumBuffers:   1,
		RetryTimeout: 2 * time.Second,
		MaxRetries:   3,
	})
	if err != nil {
		return err
	}
	go func() { _, _ = pending.Wait(context.Background()) }()
	netlog.Debug("[ASP] SendAttention: sess=%d code=0x%04X", sessID, code)
	return nil
}
