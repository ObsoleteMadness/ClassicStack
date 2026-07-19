package afp

import (
	"strconv"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/protocol/asp"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/atp"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
)

// session is one ASP session: a client that has completed OpenSession. The
// session id (1–255) is the demux key the client stamps into every subsequent
// Command/Write. The transport-neutral AFP command core lives behind conn (the
// per-circuit Conn from conn.go); the session adds only the ASP transport state
// (socket/address/timer), so the AFP layer holds no socket knowledge.
type session struct {
	id   uint8
	wss  uint8  // workstation session socket (for server tickles / attention)
	net  uint16 // client network — server-initiated packets address here
	node uint8  // client node

	// srvNet/srvNode/srvSocket are the server-side address the workstation reached
	// us on (the OpenSession request's destination). Server-initiated packets
	// (tickle / attention / aspDataWrite / TRel) are routed FROM here so the .XPP
	// driver correlates them to this session. Kept so async sends need not hold
	// the original inbound port.
	srvNet    uint16
	srvNode   uint8
	srvSocket uint8

	conn *Conn // the transport-agnostic AFP command circuit (conn.go)

	mu     sync.Mutex
	lastRx time.Time // updated on every inbound packet for the maintenance timer
	seq    seqFilter // ASP-level duplicate filter (retransmitted TReq must not re-run)
	closed bool      // set once the maintenance loop / CloseSess has torn it down
	stop   chan struct{}
}

// seqFilter is the per-session ASP duplicate filter. A workstation retransmits a
// TReq (same seqNum) with a fresh ATP transaction id when it thinks a reply was
// lost; without this an idempotent-unsafe command (FPWrite, FPCreateFile) would
// run twice. Mirrors main's service/asp seqFilter: a request is a duplicate only
// when the ASP seqNum repeats under a DIFFERENT ATP tid.
type seqFilter struct {
	lastSeq uint16
	lastTID uint16
	inited  bool
}

// accept records (seq, tid) and reports whether the request should be processed.
// False means duplicate — drop.
func (f *seqFilter) accept(seq, tid uint16) bool {
	if f.inited && seq == f.lastSeq && tid != f.lastTID {
		return false
	}
	f.lastSeq, f.lastTID, f.inited = seq, tid, true
	return true
}

// touch updates the activity timestamp and applies the duplicate filter under the
// session lock. It returns false when the request is an ASP-level duplicate that
// must be silently dropped.
func (s *session) touch(seqNum, tid uint16) (fresh bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastRx = time.Now()
	return s.seq.accept(seqNum, tid)
}

// idle reports how long since the last inbound packet on this session.
func (s *session) idle() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Since(s.lastRx)
}

// sessionTable holds the live ASP sessions keyed by session id, and allocates new
// ids. ASP session ids are a single byte (1–255); 0 is reserved. Allocation walks
// from the last id so a busy server reuses freed ids predictably.
type sessionTable struct {
	mu     sync.Mutex
	byID   map[uint8]*session
	nextID uint8
}

func newSessionTable() *sessionTable {
	return &sessionTable{byID: make(map[uint8]*session), nextID: 1}
}

// open allocates a session id and registers a new session bound to the given AFP
// command circuit. wss/net/node address the workstation; srv* is the server-side
// address the client reached us on (threaded onto server-initiated packets). It
// returns ok=false if all 255 ids are in use (the client sees
// SPErrorNoMoreSessions / ServerBusy).
func (t *sessionTable) open(wss uint8, net uint16, node uint8, srvNet uint16, srvNode, srvSocket uint8, conn *Conn) (*session, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.byID) >= 255 {
		return nil, false
	}
	id := t.nextID
	for {
		if id == 0 {
			id = 1
		}
		if _, taken := t.byID[id]; !taken {
			break
		}
		id++
	}
	s := &session{
		id: id, wss: wss, net: net, node: node,
		srvNet: srvNet, srvNode: srvNode, srvSocket: srvSocket,
		lastRx: time.Now(), conn: conn, stop: make(chan struct{}),
	}
	t.byID[id] = s
	t.nextID = id + 1
	return s, true
}

// ids returns a snapshot of the live session ids (for Stop's ServerGoingDown
// sweep, which must not hold the table lock while sending).
func (t *sessionTable) ids() []uint8 {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]uint8, 0, len(t.byID))
	for id := range t.byID {
		out = append(out, id)
	}
	return out
}

// get returns the session for an id, if live.
func (t *sessionTable) get(id uint8) (*session, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	s, ok := t.byID[id]
	return s, ok
}

// close removes a session.
func (t *sessionTable) close(id uint8) {
	t.mu.Lock()
	delete(t.byID, id)
	t.mu.Unlock()
}

// Count returns the number of live sessions (diagnostics / stats).
func (t *sessionTable) Count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.byID)
}

// handleASP demuxes one ATP TReq by its ASP SPFunction (the MSB of UserData) and
// drives the session lifecycle. It runs the ASP responsibilities of spec/10 §
// "Implementation Notes": GetStatus (no session), OpenSession, Command/Write
// demux, Tickle (no reply), CloseSession. Server-initiated functions
// (WriteContinue=7, Attention=8) are never inbound commands and are ignored if
// seen, per the spec's "common mistakes".
func (s *Service) handleASP(req atpRequest) {
	spFunc := uint8(req.userData >> 24)
	switch spFunc {
	case asp.SPFuncGetStatus:
		s.handleGetStatus(req)
	case asp.SPFuncOpenSess:
		s.handleOpenSession(req)
	case asp.SPFuncCloseSess:
		s.handleCloseSession(req)
	case asp.SPFuncCommand:
		s.handleCommand(req)
	case asp.SPFuncWrite:
		s.handleWrite(req)
	case asp.SPFuncTickle:
		s.handleTickle(req)
	default:
		// WriteContinue/Attention are server→workstation only; anything else is
		// malformed. Either way there is nothing to reply to.
	}
}

// handleGetStatus answers ASPGetStatus with the AFP server-information block
// (FPGetSrvrInfo), with no session. The reply UserData is 0 (spec/10 §aspGetStat).
func (s *Service) handleGetStatus(req atpRequest) {
	block := s.serverInfoBlock()
	req.respond(s.rtr, 0, block)
}

// handleOpenSession assigns a session id, records the workstation session socket,
// and replies (SSS, sessID, 0, 0). The server session socket the client should
// send future commands to is this service's own socket (the spine uses one DDP
// socket and demuxes by session id, matching netatalk's single-socket model).
func (s *Service) handleOpenSession(req atpRequest) {
	open := asp.ParseOpenSessPacket(req.userData)
	reply := asp.OpenSessReplyPacket{SSSSocket: s.Socket(), ErrorCode: asp.SPErrorNoError}

	if open.VersionNum != asp.Version {
		reply.ErrorCode = asp.SPErrorBadVersNum
		req.respond(s.rtr, reply.MarshalUserData(), nil)
		return
	}

	sess, ok := s.sessions.open(
		open.WSSSocket, req.d.SrcNetwork, req.d.SrcNode,
		req.d.DestNetwork, req.d.DestNode, req.d.DestSocket,
		s.NewConn(),
	)
	if !ok {
		reply.ErrorCode = asp.SPErrorServerBusy
		req.respond(s.rtr, reply.MarshalUserData(), nil)
		return
	}
	reply.SessionID = sess.id
	req.respond(s.rtr, reply.MarshalUserData(), nil)

	// Start the per-session maintenance loop: it tickles the workstation to keep
	// the session alive and reaps it if the client goes silent past the
	// maintenance timeout (so a vanished client does not leak its forks).
	s.wg.Add(1)
	go s.maintainSession(sess)
}

// teardownSession closes a session exactly once: stops its maintenance loop,
// closes any forks the client left open, and drops it from the table. Safe to
// call from the maintenance loop (idle reap) or an inbound CloseSess; the first
// caller wins and the rest are no-ops.
func (s *Service) teardownSession(sess *session) {
	sess.mu.Lock()
	if sess.closed {
		sess.mu.Unlock()
		return
	}
	sess.closed = true
	close(sess.stop)
	sess.mu.Unlock()

	if sess.conn != nil {
		sess.conn.Close()
	}
	s.sessions.close(sess.id)
}

// handleCloseSession tears down the session and replies empty (UserData 0). Any
// forks the client left open are closed here so a client that disconnects without
// FPCloseFork does not leak file handles.
func (s *Service) handleCloseSession(req atpRequest) {
	pkt := asp.ParseCloseSessPacket(req.userData)
	if sess, ok := s.sessions.get(pkt.SessionID); ok {
		s.teardownSession(sess)
	}
	req.respond(s.rtr, asp.CloseSessReplyUserData(), nil)
}

// handleTickle resets the session's maintenance timer. No reply is sent (spec/10
// §aspTickle: "No reply required").
func (s *Service) handleTickle(req atpRequest) {
	sessID := uint8(req.userData >> 16)
	if sess, ok := s.sessions.get(sessID); ok {
		sess.mu.Lock()
		sess.lastRx = time.Now()
		sess.mu.Unlock()
	}
}

// maintainSession runs the per-session keep-alive + inactivity-reap loop (main's
// SessionManager.maintenance). Every TickleInterval it sends a tickle to the
// workstation; if no inbound packet has arrived within SessionMaintenanceTimeout
// it tears the session down (so a client that vanished without CloseSess does not
// leak its forks). The loop exits when the session is torn down (stop closed) or
// the service drains (drainStop closed).
func (s *Service) maintainSession(sess *session) {
	defer s.wg.Done()
	ticker := time.NewTicker(asp.TickleInterval)
	defer ticker.Stop()
	for {
		select {
		case <-sess.stop:
			return
		case <-s.drainStop:
			return
		case <-ticker.C:
			if sess.idle() > asp.SessionMaintenanceTimeout {
				// strconv, not fmt: core/ bans reflect transitively (archtest §1).
				s.logf("ASP session " + strconv.Itoa(int(sess.id)) +
					" timed out (idle > " + asp.SessionMaintenanceTimeout.String() + "), closing")
				s.teardownSession(sess)
				return
			}
			s.sendTickle(sess)
		}
	}
}

// handleCommand runs an ASPCommand: it resolves the session, hands the command
// block to the AFP dispatcher, and replies with the AFP result code in the ATP
// UserData plus the AFP reply block as the response data (spec/10 §aspCommand). A
// command for an unknown session is answered with SPErrorParamErr encoded as the
// AFP-level result so the client tears the session down.
func (s *Service) handleCommand(req atpRequest) {
	cmd := asp.ParseCommandPacket(req.userData, req.payload)
	sess, ok := s.sessions.get(cmd.SessionID)
	if !ok {
		// No such session: reply with the ASP session-closed error in UserData.
		req.respond(s.rtr, uint32(int32ToUserData(int32(asp.SPErrorParamErr))), nil)
		return
	}
	if len(cmd.CmdBlock) > asp.ATPMaxData {
		// Oversized command block (> one ATP packet): reject with the ASP size
		// error (spec/10; main's effectiveMaxCmdSize == ATPMaxData == 578).
		req.respond(s.rtr, int32ToUserData(int32(asp.SPErrorSizeErr)), nil)
		return
	}
	if !sess.touch(cmd.SeqNum, req.transID) {
		// ASP-level duplicate (retransmitted seq under a new tid): the original is
		// still in flight or just answered; drop rather than re-run the command.
		return
	}

	reply, result := sess.conn.Command(cmd.CmdBlock)
	req.respond(s.rtr, int32ToUserData(result), reply)
}

// handleWrite runs phase 1 of a two-phase ASPWrite (spec/10 §"Two-Phase Write
// Protocol"). The aspWrite TReq carries only the AFP command block (an FPWrite
// header) — the bulk write data has not arrived yet. The server reads the
// FPWrite reqCount, registers the pending write, and issues a server-initiated
// aspDataWrite TReq to the workstation's session socket asking it to send that
// many bytes. The workstation's TResp data is collected in handleDataResponse,
// which then runs the FPWrite and replies to this same (phase-1) TReq.
//
// An unknown session, a non-FPWrite block, or a zero-length write completes
// inline here (no data round-trip): a zero reqCount needs no data, and a command
// the dispatcher can answer without data (it should not happen, but is handled
// for robustness) is answered immediately.
func (s *Service) handleWrite(req atpRequest) {
	pkt := asp.ParseWritePacket(req.userData, req.payload)
	sess, ok := s.sessions.get(pkt.SessionID)
	if !ok {
		req.respond(s.rtr, int32ToUserData(int32(asp.SPErrorParamErr)), nil)
		return
	}
	if len(pkt.CmdBlock) > asp.ATPMaxData {
		req.respond(s.rtr, int32ToUserData(int32(asp.SPErrorSizeErr)), nil)
		return
	}
	if !sess.touch(pkt.SeqNum, req.transID) {
		// Duplicate aspWrite retransmission: the original is in flight (its
		// aspDataWrite is pending). Dropping avoids issuing a second data pull /
		// double-applying the write.
		return
	}

	want, hdrLen := writeDataCount(pkt.CmdBlock)
	if want <= 0 {
		// No data to fetch (zero-length FPWrite, or a non-write block): run it
		// straight through the command circuit and reply in one shot.
		reply, result := sess.conn.Command(pkt.CmdBlock)
		req.respond(s.rtr, int32ToUserData(result), reply)
		return
	}
	if want > writeQuantum {
		want = writeQuantum
	}

	pw := &pendingWrite{orig: req, sess: sess, cmdBlk: pkt.CmdBlock, hdrLen: hdrLen, want: want, seq: pkt.SeqNum}
	tid := s.pendingWrites.add(pw)
	s.sendDataWrite(sess, pkt.SeqNum, tid, want)
	s.wg.Add(1)
	go s.retryDataWrite(pw, tid)
}

// retryDataWrite guards one in-flight aspDataWrite against a lost request/response
// (the spine drives it as a raw TReq, so unlike main's ATP endpoint it has no
// built-in retransmission). It resends the aspDataWrite up to writeMaxRetries
// times, one every writeRetryInterval, until the write completes (the pending
// entry is removed by handleDataResponse) or the service drains. If the
// workstation never answers it abandons the write, cleans up the pending entry,
// and fails the phase-1 aspWrite so the client is not left waiting forever.
func (s *Service) retryDataWrite(pw *pendingWrite, tid uint16) {
	defer s.wg.Done()
	ticker := time.NewTicker(writeRetryInterval)
	defer ticker.Stop()
	for attempt := 0; attempt < writeMaxRetries; attempt++ {
		select {
		case <-pw.sess.stop:
			return
		case <-s.drainStop:
			return
		case <-ticker.C:
			if _, live := s.pendingWrites.get(tid); !live {
				return // handleDataResponse already completed this write
			}
			s.sendDataWrite(pw.sess, pw.seq, tid, pw.want)
		}
	}
	// Exhausted retries: drop the pending write and fail the phase-1 aspWrite so
	// the client stops waiting.
	if _, live := s.pendingWrites.get(tid); live {
		s.pendingWrites.remove(tid)
		pw.orig.respond(s.rtr, int32ToUserData(int32(asp.SPErrorParamErr)), nil)
	}
}

// routeToWorkstation sends a server-initiated ATP frame to the session's
// workstation session socket, sourced from the server address the client reached
// us on. It mirrors main's requester-side send (router.Route with explicit
// src/dst) so tickle / attention / aspDataWrite / TRel all address the .XPP
// driver correctly without holding the original inbound port.
func (s *Service) routeToWorkstation(sess *session, frame []byte) {
	if s.rtr == nil {
		return
	}
	s.rtr.Route(ddp.Datagram{
		DestNetwork: sess.net,
		DestNode:    sess.node,
		DestSocket:  sess.wss,
		SrcNetwork:  sess.srvNet,
		SrcNode:     sess.srvNode,
		SrcSocket:   sess.srvSocket,
		DDPType:     atp.DDPType,
		Data:        frame,
	}, true)
}

// sendDataWrite emits the phase-2a aspDataWrite TReq to the workstation's session
// socket, requesting up to want bytes of write data. It is a server-initiated ATP
// transaction: tid is the transaction id the workstation will echo in its TResp
// (so handleDataResponse can correlate the data back to the pending write), and
// the request bitmap names the response packets the server is prepared to take.
func (s *Service) sendDataWrite(sess *session, seq uint16, tid uint16, want int) {
	ud := asp.WriteContinuePacket{SessionID: sess.id, SeqNum: seq, BufferSize: uint16(want)}.MarshalUserData()

	nPackets := min(max((want+atp.MaxATPData-1)/atp.MaxATPData, 1), atp.MaxResponsePackets)
	bitmap := uint8((1 << uint(nPackets)) - 1)

	// The aspDataWrite is an exactly-once (XO) transaction: the workstation holds
	// the transaction open until the server releases it with a TRel (sent from
	// handleDataResponse once the data is in hand). The TRel-timeout indicator in
	// the control byte tells the .XPP driver how long to wait for that TRel before
	// abandoning the transaction.
	h := atp.Header{Control: atp.TREQ | atp.XO, Bitmap: bitmap, TransID: tid, UserData: ud}
	h.SetTRelTimeout(atp.TRel30s)
	frame := h.Encode(make([]byte, 0, atp.HeaderSize+2))
	frame = append(frame, asp.WriteContinuePacket{BufferSize: uint16(want)}.MarshalData()...)
	s.routeToWorkstation(sess, frame)
}

// sendTRel releases the exactly-once aspDataWrite transaction: after the server
// has collected the workstation's data TResp, it sends a TRel (Transaction
// Release) for tid so the .XPP driver can drop its transaction control block and
// consider the write delivered. Without this the workstation holds the XO
// transaction open, retransmits its data TResp until it gives up, and reports the
// write as failed — even though the server already applied it. main's ATP
// endpoint sends this automatically for an XO requester; the spine's hand-rolled
// aspDataWrite must do it explicitly.
func (s *Service) sendTRel(sess *session, tid uint16) {
	h := atp.Header{Control: atp.TREL, TransID: tid}
	s.routeToWorkstation(sess, h.Encode(make([]byte, 0, atp.HeaderSize)))
}

// sendTickle sends a keep-alive SPTickle TReq to the workstation (main's
// sendTickle). No reply is needed — it exists only to reset the client's own
// session-maintenance timer so an idle-but-live session is not torn down.
func (s *Service) sendTickle(sess *session) {
	ud := asp.TicklePacket{SessionID: sess.id}.MarshalUserData()
	h := atp.Header{Control: atp.TREQ, Bitmap: 0x01, TransID: 0, UserData: ud}
	s.routeToWorkstation(sess, h.Encode(make([]byte, 0, atp.HeaderSize)))
}

// sendAttention sends an SPAttention TReq to the workstation carrying a non-zero
// attention code (e.g. AspAttnServerGoingDown on Stop). Best-effort: it is a
// server-initiated notification, so no reply is awaited.
func (s *Service) sendAttention(sess *session, code uint16) {
	if code == 0 {
		return
	}
	ud := asp.AttentionPacket{SessionID: sess.id, AttentionCode: code}.MarshalUserData()
	h := atp.Header{Control: atp.TREQ, Bitmap: 0x01, TransID: 0, UserData: ud}
	s.routeToWorkstation(sess, h.Encode(make([]byte, 0, atp.HeaderSize)))
}

// sendCloseSession sends a server-initiated SPCloseSession TReq to the
// workstation, ending the session from the server side (operator disconnect,
// service stop) — the sequence an observed AppleShare server performs after its
// final shutdown attention. Best-effort: the workstation TResp-acks it, but no
// reply is awaited.
func (s *Service) sendCloseSession(sess *session) {
	ud := asp.CloseSessPacket{SessionID: sess.id}.MarshalUserData()
	h := atp.Header{Control: atp.TREQ, Bitmap: 0x01, TransID: 0, UserData: ud}
	s.routeToWorkstation(sess, h.Encode(make([]byte, 0, atp.HeaderSize)))
}

// handleDataResponse collects phase-2b write data: the workstation's TResp to the
// aspDataWrite TReq the server sent. Packets are accumulated in arrival order; on
// the end-of-message packet (or once want bytes are in hand) the FPWrite command
// block carrying the assembled data is run through the dispatcher and the result
// is sent back as the phase-3 reply to the original aspWrite TReq.
//
// Like the rest of this spine, it assumes the router drives Inbound serially, so
// the packets of one transaction are accumulated without a per-write lock; the
// pendingWriteTable's own mutex only guards the id→write map.
func (s *Service) handleDataResponse(resp atpResponse) {
	pw, ok := s.pendingWrites.get(resp.transID)
	if !ok {
		return // unknown / already-completed transaction
	}
	pw.data = append(pw.data, resp.payload...)
	if len(pw.data) > pw.want {
		pw.data = pw.data[:pw.want]
	}
	if !resp.eom && len(pw.data) < pw.want {
		return // more data packets to come
	}

	s.pendingWrites.remove(resp.transID)
	pw.sess.mu.Lock()
	pw.sess.lastRx = time.Now()
	pw.sess.mu.Unlock()

	// Release the exactly-once aspDataWrite transaction now that its data is in
	// hand, so the workstation stops holding it open (and stops retransmitting the
	// data TResp). This closes phase 2; the phase-3 reply below answers the
	// separate phase-1 aspWrite transaction.
	s.sendTRel(pw.sess, resp.transID)

	block := appendWriteData(pw.cmdBlk, pw.hdrLen, pw.data)
	reply, result := pw.sess.conn.Command(block)
	pw.orig.respond(s.rtr, int32ToUserData(result), reply)
}

// int32ToUserData packs a signed AFP/ASP result code into the 4-byte ATP
// UserData field (two's-complement), the form the .XPP driver reads back as an
// OSErr.
func int32ToUserData(code int32) uint32 { return uint32(code) }
