package asp

import (
	"github.com/ObsoleteMadness/ClassicStack/client/atalk"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/asp"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
)

// write.go implements the workstation side of the two-phase ASP Write and the
// background WSS handler that answers server-initiated TReqs.

// Write runs a two-phase ASPWrite: phase 1 sends the AFP command block (an FPWrite
// header naming reqCount data bytes) to the server session socket as an XO TReq; the
// server then pulls the data with a server-initiated aspDataWrite TReq to our WSS,
// which serveWSS answers from the pending buffer registered here; finally the server
// applies the write and replies to the phase-1 TReq, which this call returns.
//
// block is the FPWrite command header (WriteRequest.Header()); data is the fork bytes
// the server will pull. The reply is the FPWrite reply body (lastWritten) and result.
func (s *Session) Write(block, data []byte) (reply []byte, result int32, err error) {
	select {
	case <-s.stop:
		return nil, 0, ErrSessionClosed
	default:
	}
	seq := s.nextSeq()

	// Register the data BEFORE sending phase 1, so the server's data pull (which can
	// arrive before phase 1's requester goroutine has parked) always finds it.
	s.pendingMu.Lock()
	s.pending[seq] = data
	s.pendingMu.Unlock()
	defer func() {
		s.pendingMu.Lock()
		delete(s.pending, seq)
		s.pendingMu.Unlock()
	}()

	ud := asp.WritePacket{SessionID: s.id, SeqNum: seq}.MarshalUserData()
	resp, err := s.atp.Request(s.server, ud, block, true, 8)
	if err != nil {
		return nil, 0, err
	}
	return resp.Data, int32(resp.UserData), nil
}

// serveWSS answers server-initiated TReqs on the workstation session socket for the
// life of the session: WriteContinue (the aspDataWrite data pull), Tickle, Attention,
// and CloseSession. It exits on Close.
func (s *Session) serveWSS(ch <-chan ddp.Datagram) {
	defer s.wg.Done()
	for {
		select {
		case <-s.stop:
			return
		case d, ok := <-ch:
			if !ok {
				return
			}
			req, ok := atalk.DecodeTReq(d)
			if !ok {
				continue
			}
			s.handleWSSReq(req)
		}
	}
}

// handleWSSReq dispatches one server-initiated TReq by its ASP function byte.
func (s *Session) handleWSSReq(req atalk.InboundTReq) {
	fn := uint8(req.UserData >> 24)
	switch fn {
	case asp.SPFuncWriteContinue:
		s.handleWriteContinue(req)
	case asp.SPFuncTickle:
		// Keep-alive: no reply is required, but ack the transaction so the server's
		// requester (if it expects one) is satisfied. A Tickle is ALO with no data.
		_ = s.ep.RespondTReq(req, req.UserData, nil)
	case asp.SPFuncAttention:
		// Server attention (shutdown/message). Ack it; higher layers can later expose
		// the attention code. Answer with an empty TResp.
		_ = s.ep.RespondTReq(req, req.UserData, nil)
	case asp.SPFuncCloseSess:
		// Server-initiated close: ack and stop the session.
		_ = s.ep.RespondTReq(req, req.UserData, nil)
		s.stopOnce.Do(func() { close(s.stop) })
	default:
		// Unknown server-initiated function: ack empty so the server is not left
		// retransmitting.
		_ = s.ep.RespondTReq(req, req.UserData, nil)
	}
}

// handleWriteContinue answers the server's aspDataWrite pull: it looks up the pending
// write data by the request's ASP sequence number and returns it as the TResp data.
// The server splits/reassembles per the ATP bitmap; RespondTReq honours the requester
// bitmap and EOM. The transaction is XO, so the server releases it with a TRel after
// collecting the data (the endpoint ignores the inbound TRel — it carries no data).
func (s *Session) handleWriteContinue(req atalk.InboundTReq) {
	wc, ok := asp.ParseWriteContinue(req.UserData, req.Payload)
	if !ok {
		_ = s.ep.RespondTReq(req, req.UserData, nil)
		return
	}
	s.pendingMu.Lock()
	data := s.pending[wc.SeqNum]
	s.pendingMu.Unlock()

	// Honour the server's requested buffer size (BufferSize): send at most that many
	// bytes in this round. The server pulls again if it wants more (each pull carries
	// the next window); for the ASP quantum (≤ 4624) one pull suffices.
	if wc.BufferSize > 0 && int(wc.BufferSize) < len(data) {
		data = data[:wc.BufferSize]
	}
	_ = s.ep.RespondTReq(req, req.UserData, data)
}
