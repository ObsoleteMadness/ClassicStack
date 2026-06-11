package afp

import (
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/protocol/asp"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/atp"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
)

// session is one ASP session: a client that has completed OpenSession. The
// session id (1–255) is the demux key the client stamps into every subsequent
// Command/Write. afp carries the per-session AFP state (the logged-in user and
// the open volumes) so the AFP layer holds no socket knowledge.
type session struct {
	id     uint8
	wss    uint8     // workstation session socket (for server tickles / attention)
	net    uint16    // client network — server-initiated packets address here
	node   uint8     // client node
	lastRx time.Time // updated on every inbound packet for the maintenance timer
	afp    *afpSession
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

// open allocates a session id and registers a new session. It returns ok=false
// if all 255 ids are in use (the client sees SPErrorNoMoreSessions / ServerBusy).
func (t *sessionTable) open(wss uint8, net uint16, node uint8, afp *afpSession) (*session, bool) {
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
	s := &session{id: id, wss: wss, net: net, node: node, lastRx: time.Now(), afp: afp}
	t.byID[id] = s
	t.nextID = id + 1
	return s, true
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

	sess, ok := s.sessions.open(open.WSSSocket, req.d.SrcNetwork, req.d.SrcNode, newAFPSession())
	if !ok {
		reply.ErrorCode = asp.SPErrorServerBusy
		req.respond(s.rtr, reply.MarshalUserData(), nil)
		return
	}
	reply.SessionID = sess.id
	req.respond(s.rtr, reply.MarshalUserData(), nil)
}

// handleCloseSession tears down the session and replies empty (UserData 0). Any
// forks the client left open are closed here so a client that disconnects without
// FPCloseFork does not leak file handles.
func (s *Service) handleCloseSession(req atpRequest) {
	pkt := asp.ParseCloseSessPacket(req.userData)
	if sess, ok := s.sessions.get(pkt.SessionID); ok && sess.afp != nil {
		sess.afp.forks.closeAll()
	}
	s.sessions.close(pkt.SessionID)
	req.respond(s.rtr, asp.CloseSessReplyUserData(), nil)
}

// handleTickle resets the session's maintenance timer. No reply is sent (spec/10
// §aspTickle: "No reply required").
func (s *Service) handleTickle(req atpRequest) {
	sessID := uint8(req.userData >> 16)
	if sess, ok := s.sessions.get(sessID); ok {
		sess.lastRx = time.Now()
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
	sess.lastRx = time.Now()

	reply, result := s.dispatchAFP(sess, cmd.CmdBlock)
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
	sess.lastRx = time.Now()

	want, hdrLen := writeDataCount(pkt.CmdBlock)
	if want <= 0 {
		// No data to fetch (zero-length FPWrite, or a non-write block): run it
		// straight through the dispatcher and reply in one shot.
		reply, result := s.dispatchAFP(sess, pkt.CmdBlock)
		req.respond(s.rtr, int32ToUserData(result), reply)
		return
	}
	if want > writeQuantum {
		want = writeQuantum
	}

	pw := &pendingWrite{orig: req, sess: sess, cmdBlk: pkt.CmdBlock, hdrLen: hdrLen, want: want}
	tid := s.pendingWrites.add(pw)
	s.sendDataWrite(req, sess, pkt.SeqNum, tid, want)
}

// sendDataWrite emits the phase-2a aspDataWrite TReq to the workstation's session
// socket, requesting up to want bytes of write data. It is a server-initiated ATP
// transaction: tid is the transaction id the workstation will echo in its TResp
// (so handleDataResponse can correlate the data back to the pending write), and
// the request bitmap names the response packets the server is prepared to take.
func (s *Service) sendDataWrite(req atpRequest, sess *session, seq uint16, tid uint16, want int) {
	ud := asp.WriteContinuePacket{SessionID: sess.id, SeqNum: seq, BufferSize: uint16(want)}.MarshalUserData()

	nPackets := min(max((want+atp.MaxATPData-1)/atp.MaxATPData, 1), atp.MaxResponsePackets)
	bitmap := uint8((1 << uint(nPackets)) - 1)

	h := atp.Header{Control: atp.TREQ | atp.XO, Bitmap: bitmap, TransID: tid, UserData: ud}
	frame := h.Encode(make([]byte, 0, atp.HeaderSize+2))
	frame = append(frame, asp.WriteContinuePacket{BufferSize: uint16(want)}.MarshalData()...)

	d := ddp.Datagram{
		DestNetwork: sess.net,
		SrcNetwork:  req.d.DestNetwork,
		DestNode:    sess.node,
		SrcNode:     req.d.DestNode,
		DestSocket:  sess.wss,
		SrcSocket:   req.d.DestSocket,
		DDPType:     atp.DDPType,
		Data:        frame,
	}
	req.from.Unicast(sess.net, sess.node, d)
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
	pw.sess.lastRx = time.Now()

	block := appendWriteData(pw.cmdBlk, pw.hdrLen, pw.data)
	reply, result := s.dispatchAFP(pw.sess, block)
	pw.orig.respond(s.rtr, int32ToUserData(result), reply)
}

// int32ToUserData packs a signed AFP/ASP result code into the 4-byte ATP
// UserData field (two's-complement), the form the .XPP driver reads back as an
// OSErr.
func int32ToUserData(code int32) uint32 { return uint32(code) }
