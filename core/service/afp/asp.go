package afp

import (
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/protocol/asp"
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
	case asp.SPFuncCommand, asp.SPFuncWrite:
		s.handleCommand(req)
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

// handleCommand runs an ASPCommand/ASPWrite: it resolves the session, hands the
// command block to the AFP dispatcher, and replies with the AFP result code in
// the ATP UserData plus the AFP reply block as the response data (spec/10
// §aspCommand). A command for an unknown session is answered with SPErrorParamErr
// encoded as the AFP-level result so the client tears the session down.
//
// The two-phase ASPWrite data path (aspDataWrite/WriteContinue) is not yet wired
// in this spine; an ASPWrite is handled as a command carrying only its block,
// which covers FPWrite headers whose data arrives separately in a later slice.
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

// int32ToUserData packs a signed AFP/ASP result code into the 4-byte ATP
// UserData field (two's-complement), the form the .XPP driver reads back as an
// OSErr.
func int32ToUserData(code int32) uint32 { return uint32(code) }
