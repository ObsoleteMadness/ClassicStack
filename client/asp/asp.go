// Package asp is the client-side ASP (AppleTalk Session Protocol) session: it opens a
// session to an AFP server, runs Command transactions, drives the two-phase Write, and
// keeps the session alive — all over the client/atalk ATP requester. It is the
// transport an AFP client (client/afp) sits on.
//
// ASP session flow (Inside AppleTalk, Ch. 11), from the workstation side:
//   - GetStatus (ALO, to the server's SLS) → the FPGetSrvrInfo block.
//   - OpenSession (ALO, to the SLS): the workstation sends its session socket (WSS); the
//     server replies with the server session socket (SSS) and a session id.
//   - Command / Write (XO, to the SSS) carry AFP command blocks.
//   - The server sends server-initiated Tickle/Attention/WriteContinue/CloseSession
//     TReqs to the WSS, which this session answers on a background goroutine.
//   - CloseSession (to the SSS) ends the session.
//
// Ring: CLIENT.
package asp

import (
	"errors"
	"fmt"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/client/atalk"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/asp"
)

// Session is an open ASP session to one AFP server.
type Session struct {
	atp    *atalk.ATP
	ep     *atalk.Endpoint
	server atalk.Addr // the server session socket (SSS) address for Command/Write
	sls    atalk.Addr // the server listening socket (SLS) for GetStatus/OpenSession

	wss uint8 // our workstation session socket
	id  uint8 // session id assigned by the server

	seqMu   sync.Mutex
	seq     uint16 // next ASP request sequence number to use
	seqInit bool   // whether seq has been handed out at least once

	// cmdMu serializes Command and Write. System 7 ASP accepts one Command/Write
	// at a time and silently drops any other in-flight sequence (classicstack-web
	// enqueueCmd; ClassicStack errata on overlapping seqs).
	cmdMu sync.Mutex

	// pending holds the write data awaiting the server's aspDataWrite pull, keyed by
	// the ASP request sequence number the phase-1 ASPWrite used. serveWSS consumes it
	// when the matching WriteContinue TReq arrives.
	pendingMu sync.Mutex
	pending   map[uint16][]byte

	attnMu      sync.Mutex
	onAttention func(code uint16)

	stopOnce sync.Once
	stop     chan struct{}
	wg       sync.WaitGroup
}

// ErrSessionClosed is returned by Command/Write after the session is closed.
var ErrSessionClosed = errors.New("asp: session closed")

// GetStatus runs an ASPGetStatus (ALO) to the server's listening socket and returns the
// FPGetSrvrInfo status block. It needs no open session, so it is a package function
// taking the endpoint + server address.
func GetStatus(a *atalk.ATP, sls atalk.Addr) ([]byte, error) {
	resp, err := a.Request(sls, asp.MarshalGetStatusRequest(), nil, false, 8)
	if err != nil {
		return nil, fmt.Errorf("asp: GetStatus: %w", err)
	}
	return resp.Data, nil
}

// Open runs the OpenSession handshake to the server's listening socket sls and returns
// a live Session (GetStatus is a separate call, not part of Open). The workstation
// session socket (WSS) is bound here; the server's SSS from the reply becomes the
// Command/Write target. On success it starts the background goroutine that answers
// server-initiated TReqs on the WSS.
func Open(ep *atalk.Endpoint, a *atalk.ATP, sls atalk.Addr) (*Session, error) {
	wss, wssRaw := ep.Bind()
	ud := asp.OpenSessPacket{WSSSocket: wss, VersionNum: asp.Version}.MarshalUserData()
	resp, err := a.Request(sls, ud, nil, false, 1)
	if err != nil {
		ep.Unbind(wss)
		return nil, fmt.Errorf("asp: OpenSession: %w", err)
	}
	reply := asp.ParseOpenSessReply(resp.UserData)
	if reply.ErrorCode != asp.SPErrorNoError {
		ep.Unbind(wss)
		return nil, fmt.Errorf("asp: OpenSession rejected: error %d", reply.ErrorCode)
	}

	s := &Session{
		atp: a,
		ep:  ep,
		sls: sls,
		server: atalk.Addr{
			Network: sls.Network,
			Node:    sls.Node,
			Socket:  reply.SSSSocket,
		},
		wss:     wss,
		id:      reply.SessionID,
		pending: make(map[uint16][]byte),
		stop:    make(chan struct{}),
	}
	s.wg.Add(2)
	go s.serveWSS(wssRaw)
	go s.tickleServer()
	return s, nil
}

// SessionID returns the server-assigned session id.
func (s *Session) SessionID() uint8 { return s.id }

// SetAttentionHandler installs h as the callback for server-initiated ASP
// Attention packets. h runs on a new goroutine (it must not block the WSS
// handler) and receives the 16-bit attention code (AspAttn* bits). Passing nil
// clears the handler. The AFP client uses this to fetch FPGetSrvrMsg after an
// AspAttnMsg attention.
func (s *Session) SetAttentionHandler(h func(code uint16)) {
	s.attnMu.Lock()
	s.onAttention = h
	s.attnMu.Unlock()
}

func (s *Session) attentionHandler() func(code uint16) {
	s.attnMu.Lock()
	defer s.attnMu.Unlock()
	return s.onAttention
}

// nextSeq returns the next ASP request sequence number. The FIRST Command/Write on a
// session MUST use sequence number 0 and each subsequent one increments — a real
// System 7.x ASP server tracks the expected sequence and SILENTLY DROPS a Command whose
// sequence it did not expect (ground truth: captures/vmac-to-vmac.pcapng, the real Mac
// workstation's first Command is seq 0, then 1, 2, …). Starting at 1 left every Command
// unanswered. See spec/errata.md.
func (s *Session) nextSeq() uint16 {
	s.seqMu.Lock()
	defer s.seqMu.Unlock()
	if !s.seqInit {
		s.seqInit = true
		s.seq = 0
		return 0
	}
	s.seq++
	return s.seq
}

// Command runs an ASP Command (XO) carrying an AFP command block and returns the reply
// body plus the AFP result code (the signed 32-bit OSErr the server put in the reply
// UserData). Small AFP replies (login, OpenFork, GetFileDirParms, …) fit in one ATP
// packet; callers that expect a larger body (FPEnumerate, FPRead) use CommandMax.
func (s *Session) Command(block []byte) (reply []byte, result int32, err error) {
	return s.CommandMax(block, 1)
}

// CommandMax is Command with an explicit ATP response-slot budget (1..8). The TReq
// bitmap must match the expected payload: System 7 often omits EOM, so asking for 8
// slots on a 20-byte OpenFork reply stalls until ATP retry. classicstack-web defaults
// Command to bitmap 0x01 and sizes FPRead with bitmapForPayload.
func (s *Session) CommandMax(block []byte, maxResp int) (reply []byte, result int32, err error) {
	select {
	case <-s.stop:
		return nil, 0, ErrSessionClosed
	default:
	}
	if maxResp < 1 {
		maxResp = 1
	}
	s.cmdMu.Lock()
	defer s.cmdMu.Unlock()
	select {
	case <-s.stop:
		return nil, 0, ErrSessionClosed
	default:
	}
	seq := s.nextSeq()
	ud := asp.CommandPacket{SessionID: s.id, SeqNum: seq}.MarshalUserData()
	resp, err := s.atp.Request(s.server, ud, block, true, maxResp)
	if err != nil {
		return nil, 0, err
	}
	return resp.Data, int32(resp.UserData), nil
}

// Close ends the session with an ASPCloseSession and stops the WSS goroutine.
func (s *Session) Close() error {
	s.stopOnce.Do(func() {
		// Best-effort CloseSession to the server session socket.
		ud := asp.MarshalCloseSessRequest(s.id)
		_, _ = s.atp.Request(s.server, ud, nil, false, 1)
		close(s.stop)
	})
	s.wg.Wait()
	s.ep.Unbind(s.wss)
	return nil
}
