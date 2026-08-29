package smb_test

// nbipx_recovery_test.go pins the NB-IPX client transport's session-layer recovery
// behaviour against a SCRIPTED peer, so each frame the server puts on the wire (and
// each frame the client answers with) is exact. The in-process e2e gate in
// nbipx_e2e_test.go runs the happy path over the real server engine; this file drives
// the paths that engine never exercises because it never loses a frame.
//
// Ground truth is captures/nbipx-disconnect.pcap (2026-08-20, our client ↔ WIN98-NBIPX-2,
// 7871 frames). What that capture shows, and what each test here pins:
//
//   - Frame 7841, the EOM tail of a 2852-byte Read AndX response, was the ONE server
//     frame lost across 2510 client data frames. The client had no explicit-ack path
//     at all — it acknowledged only by piggybacking RecvSeq on its next request — so
//     the incomplete message meant no request went out, the server's nine ACK-required
//     retransmits (7842-7851, 500ms apart) went unanswered, and it killed the circuit
//     with SESSION_END at 7853. Worse, the retransmits were of the frame we ALREADY
//     had and were dropped silently at the window check, so the server could never
//     learn which frame we were missing: the circuit could not have recovered however
//     long it retried. TestNBIPXAcksAckRequiredRetransmit.
//   - The client ignored the inbound SESSION_END and kept issuing SMB requests into
//     the dead circuit (frames 7855/7856/7859/7860, one per 5s request timeout). The
//     END_ACK that capture does show (7854) came from IPX net 3 — ClassicStack's own
//     server-side NBIPX service on the same station — not from this transport.
//     TestNBIPXAnswersPeerSessionEnd.
//   - Every one of the 2510 client frames advertised BytesReceived 0, a closed receive
//     window. Win9x ignores the field, but an NT peer will not transmit past it.
//     TestNBIPXAdvertisesReceiveWindow.

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/adapter/link/inmem"
	clientsmb "github.com/ObsoleteMadness/ClassicStack/client/smb"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	ipxport "github.com/ObsoleteMadness/ClassicStack/core/port/ipx"
	ipxproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx"
	nb "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbios"
)

// scriptMAC / scriptClientMAC are the scripted peer's and the client's stations.
var (
	scriptMAC       = [6]byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x98}
	scriptClientMAC = [6]byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}
)

// scriptPeer is a hand-driven NB-IPX server: it reads the client's frames off an
// in-memory link and writes back exactly the frames a test dictates, including ones a
// correct server would never send. Its own circuit id is peerConnID.
type scriptPeer struct {
	t          *testing.T
	fl         link.FrameLink
	in         chan scriptFrame // session frames from the client, fed by readInto
	peerConnID uint16
	clientID   uint16 // learned from the SESSION_INITIALIZE
	sendSeq    uint16 // our next SendSeq
	recvSeq    uint16 // next client SendSeq we expect
}

type scriptFrame struct {
	hdr  *nb.NBIPXSessionHeader
	body []byte
}

// readInto drains the link into p.in for the life of the test. It runs on its own
// goroutine because inmem.Link.Read has no deadline — a bare Read blocks forever, so a
// test asserting SILENCE cannot call it directly.
func (p *scriptPeer) readInto() {
	for {
		frame, err := p.fl.Read()
		if err != nil {
			if errors.Is(err, link.ErrTimeout) {
				continue
			}
			close(p.in)
			return
		}
		payload, _, ok := ipxport.Strip(frame)
		if !ok {
			continue
		}
		d, err := ipxproto.Decode(payload)
		if err != nil || d.DstSock != nb.NBIPXSessionSocket || d.Type != nb.IPXTypePEP {
			continue
		}
		hdr, err := nb.DecodeSessionHeader(d.Payload)
		if err != nil {
			continue // Find-name and other name-service traffic
		}
		body := d.Payload[nb.NBIPXSessionHeaderLen:]
		if int(hdr.DataLen) <= len(body) {
			body = body[:hdr.DataLen]
		}
		p.in <- scriptFrame{hdr, body}
	}
}

// recv returns the next NB-IPX session frame from the client, failing the test if none
// arrives within the deadline.
func (p *scriptPeer) recv(within time.Duration) (*nb.NBIPXSessionHeader, []byte) {
	p.t.Helper()
	select {
	case f, ok := <-p.in:
		if !ok {
			p.t.Fatalf("scriptPeer: link closed while awaiting a session frame")
		}
		return f.hdr, f.body
	case <-time.After(within):
		p.t.Fatalf("scriptPeer: no session frame from client within %s", within)
	}
	return nil, nil
}

// recvNone asserts the client puts no session frame on the wire for the given window.
func (p *scriptPeer) recvNone(within time.Duration) {
	p.t.Helper()
	select {
	case f, ok := <-p.in:
		if !ok {
			return // link closed — silence by definition
		}
		p.t.Fatalf("scriptPeer: expected silence, got DataStreamType 0x%02x SendSeq %d",
			f.hdr.DataStreamType, f.hdr.SendSeq)
	case <-time.After(within):
	}
}

// send puts one NB-IPX session frame on the wire, verbatim — no sequencing help.
func (p *scriptPeer) send(h *nb.NBIPXSessionHeader, body []byte) {
	p.t.Helper()
	d := &ipxproto.Datagram{
		Type:    nb.IPXTypePEP,
		DstSock: nb.NBIPXSessionSocket,
		SrcSock: nb.NBIPXSessionSocket,
		DstNode: scriptClientMAC,
		SrcNode: scriptMAC,
		Payload: append(nb.EncodeSessionHeader(h), body...),
	}
	raw, err := d.Encode(nil)
	if err != nil {
		p.t.Fatalf("encode: %v", err)
	}
	if err := p.fl.Write(ipxport.DefaultFrameType.Encapsulate(scriptClientMAC, scriptMAC, raw)); err != nil {
		p.t.Fatalf("write: %v", err)
	}
}

// data sends one sequenced DATA frame from the peer's own counters.
func (p *scriptPeer) data(ctrl uint8, total, off uint16, body []byte) {
	p.t.Helper()
	p.send(&nb.NBIPXSessionHeader{
		ConnCtrlFlag:   ctrl,
		DataStreamType: nb.NBIPXSessionData,
		SourceConnID:   p.peerConnID,
		DestConnID:     p.clientID,
		SendSeq:        p.sendSeq,
		TotalDataLen:   total,
		Offset:         off,
		DataLen:        uint16(len(body)),
		RecvSeq:        p.recvSeq,
		BytesReceived:  p.recvSeq + nb.NBIPXRecvWindow,
	}, body)
	p.sendSeq++
}

// dialScripted brings up a client transport against a scripted peer, completing the
// SESSION_INITIALIZE → accept handshake. It returns the live transport, the peer, and
// the INIT header the client sent (so a test can assert on the handshake itself).
func dialScripted(t *testing.T) (clientsmb.Transport, *scriptPeer, *nb.NBIPXSessionHeader) {
	t.Helper()
	clientEnd, peerEnd := inmem.Pair(64)
	p := &scriptPeer{t: t, fl: peerEnd, in: make(chan scriptFrame, 64), peerConnID: 0x0009}
	go p.readInto()

	type dialed struct {
		tr  clientsmb.Transport
		err error
	}
	done := make(chan dialed, 1)
	go func() {
		// KnownServer skips Find-name: the scripted peer answers the INIT directly,
		// so the locate would only cost the test nbipxFindNameWindow.
		tr, err := clientsmb.DialNBIPXWithOpts(clientEnd, scriptClientMAC, "SCRIPTED",
			ipxport.DefaultFrameType, true, clientsmb.DialNBIPXOpts{KnownServer: true})
		done <- dialed{tr, err}
	}()

	init, _ := p.recv(3 * time.Second)
	if init.DataStreamType != nb.NBIPXSessionData || init.DestConnID != nb.NBIPXUnassignedConnID {
		t.Fatalf("expected SESSION_INITIALIZE (DATA, DestConnID 0xFFFF), got type 0x%02x DestConnID 0x%04x",
			init.DataStreamType, init.DestConnID)
	}
	p.clientID = init.SourceConnID
	p.recvSeq = init.SendSeq + 1 // the INIT consumes the client's seq 0

	// The accept: SYS|CONFIRM with RecvSeq 1 — both are validated by the client.
	p.send(&nb.NBIPXSessionHeader{
		ConnCtrlFlag:   nb.NBIPXConnFlagSYS | nb.NBIPXConnFlagCONFIRM,
		DataStreamType: nb.NBIPXSessionData,
		SourceConnID:   p.peerConnID,
		DestConnID:     p.clientID,
		SendSeq:        0,
		RecvSeq:        nb.NBIPXSessionAcceptRecvSeq,
		BytesReceived:  nb.NBIPXSessionAcceptRecvSeq + nb.NBIPXRecvWindow,
	}, nil)

	got := <-done
	if got.err != nil {
		t.Fatalf("DialNBIPX: %v", got.err)
	}
	t.Cleanup(func() { _ = got.tr.Close() })
	return got.tr, p, init
}

// TestNBIPXAcksAckRequiredRetransmit replays the exact shape of the
// captures/nbipx-disconnect.pcap kill sequence: a two-fragment response whose EOM tail
// is lost, then the server's ACK-required retransmit of the fragment the client already
// holds (frames 7840/7841 then 7842). The client must answer that retransmit with a
// system ack naming the frame it is actually waiting for, which is the only thing that
// can tell the server to send the tail instead of the head. Before the fix it stayed
// silent and Win98 tore the circuit down after nine tries.
func TestNBIPXAcksAckRequiredRetransmit(t *testing.T) {
	tr, p, _ := dialScripted(t)

	head := bytes.Repeat([]byte{0xAA}, 1440)
	tail := bytes.Repeat([]byte{0xBB}, 600)
	total := uint16(len(head) + len(tail))

	type sent struct {
		resp []byte
		err  error
	}
	done := make(chan sent, 1)
	go func() {
		resp, err := tr.Send([]byte("REQUEST"))
		done <- sent{resp, err}
	}()

	req, _ := p.recv(3 * time.Second)
	if req.SendSeq != p.recvSeq {
		t.Fatalf("request SendSeq = %d, want %d", req.SendSeq, p.recvSeq)
	}
	p.recvSeq++

	headSeq := p.sendSeq
	p.data(0x00, total, 0, head) // fragment 1, no EOM — accepted
	tailSeq := p.sendSeq
	p.sendSeq++ // fragment 2 (EOM) is DROPPED on the floor, as frame 7841 was

	// The client is now blocked mid-message with nothing to piggyback an ack on.
	// Poll it exactly as Win98 does: resend the HEAD with ACK required (frame 7842).
	p.send(&nb.NBIPXSessionHeader{
		ConnCtrlFlag:   nb.NBIPXConnFlagACK,
		DataStreamType: nb.NBIPXSessionData,
		SourceConnID:   p.peerConnID,
		DestConnID:     p.clientID,
		SendSeq:        headSeq,
		TotalDataLen:   total,
		Offset:         0,
		DataLen:        uint16(len(head)),
		RecvSeq:        p.recvSeq,
		BytesReceived:  p.recvSeq + nb.NBIPXRecvWindow,
	}, head)

	ack, _ := p.recv(2 * time.Second)
	if ack.ConnCtrlFlag&nb.NBIPXConnFlagSYS == 0 || ack.DataLen != 0 {
		t.Fatalf("answer to ACK-required retransmit = ConnCtrlFlag 0x%02x DataLen %d, "+
			"want a zero-data SYS frame", ack.ConnCtrlFlag, ack.DataLen)
	}
	// The whole point: RecvSeq must name the TAIL, so the server retransmits the
	// frame we are missing rather than the one we already have.
	if ack.RecvSeq != tailSeq {
		t.Fatalf("ack RecvSeq = %d, want %d (the missing tail) — the peer cannot "+
			"recover the circuit unless the ack names the frame we still need",
			ack.RecvSeq, tailSeq)
	}
	if ack.BytesReceived != ack.RecvSeq+nb.NBIPXRecvWindow {
		t.Errorf("ack BytesReceived = %d, want RecvSeq+%d = %d",
			ack.BytesReceived, nb.NBIPXRecvWindow, ack.RecvSeq+nb.NBIPXRecvWindow)
	}
	// A control frame consumes no sequence number, so the ack must not claim one.
	if ack.SendSeq != req.SendSeq+1 {
		t.Errorf("ack SendSeq = %d, want the client's unchanged next-to-send %d "+
			"(zero-data control frames consume no sequence number)", ack.SendSeq, req.SendSeq+1)
	}

	// Now honour the ack: send the tail the client asked for. The message completes
	// and the circuit survives — which is what the capture could never reach.
	p.sendSeq = tailSeq
	p.data(nb.NBIPXConnFlagEOM, total, uint16(len(head)), tail)

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("Send after recovery: %v", got.err)
		}
		if want := append(append([]byte(nil), head...), tail...); !bytes.Equal(got.resp, want) {
			t.Fatalf("reassembled response = %d bytes, want %d", len(got.resp), len(want))
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Send did not complete after the missing tail was retransmitted")
	}
}

// TestNBIPXAcksAckRequiredProbe covers the other explicit-ack shape: a zero-data
// SYS|ACK probe (0xC0), which is what an NT peer sends to poll a quiet circuit. It
// consumes no sequence number, so the ack must carry the counters UNCHANGED — acking a
// probe as consumed reads as a protocol error and NT aborts the session.
func TestNBIPXAcksAckRequiredProbe(t *testing.T) {
	_, p, _ := dialScripted(t)

	p.send(&nb.NBIPXSessionHeader{
		ConnCtrlFlag:   nb.NBIPXConnFlagSYS | nb.NBIPXConnFlagACK,
		DataStreamType: nb.NBIPXSessionData,
		SourceConnID:   p.peerConnID,
		DestConnID:     p.clientID,
		SendSeq:        p.sendSeq,
		RecvSeq:        p.recvSeq,
		BytesReceived:  p.recvSeq + nb.NBIPXRecvWindow,
	}, nil)

	ack, _ := p.recv(2 * time.Second)
	if ack.ConnCtrlFlag&nb.NBIPXConnFlagSYS == 0 || ack.DataLen != 0 {
		t.Fatalf("probe answer = ConnCtrlFlag 0x%02x DataLen %d, want zero-data SYS",
			ack.ConnCtrlFlag, ack.DataLen)
	}
	if ack.RecvSeq != p.sendSeq {
		t.Errorf("ack RecvSeq = %d, want %d unchanged (a probe consumes no sequence number)",
			ack.RecvSeq, p.sendSeq)
	}
}

// TestNBIPXAnswersPeerSessionEnd asserts a server-initiated SESSION_END is answered with
// SESSION_END_ACK and kills the circuit, so a Send blocked on the dead session fails at
// once instead of burning the full request timeout — and every later Send fails too,
// rather than pumping SMB requests into a circuit the peer has forgotten (the
// captures/nbipx-disconnect.pcap frames 7855/7856/7859/7860 behaviour).
func TestNBIPXAnswersPeerSessionEnd(t *testing.T) {
	tr, p, _ := dialScripted(t)

	type sent struct {
		err error
	}
	done := make(chan sent, 1)
	go func() {
		_, err := tr.Send([]byte("REQUEST"))
		done <- sent{err}
	}()

	req, _ := p.recv(3 * time.Second)
	p.recvSeq = req.SendSeq + 1

	// Tear the circuit down under the in-flight request.
	endSeq := p.sendSeq
	p.send(&nb.NBIPXSessionHeader{
		ConnCtrlFlag:   nb.NBIPXConnFlagACK, // an END asks for the END_ACK
		DataStreamType: nb.NBIPXSessionEnd,
		SourceConnID:   p.peerConnID,
		DestConnID:     p.clientID,
		SendSeq:        endSeq,
		RecvSeq:        p.recvSeq,
		BytesReceived:  p.recvSeq + nb.NBIPXRecvWindow,
	}, nil)

	endAck, _ := p.recv(2 * time.Second)
	if endAck.DataStreamType != nb.NBIPXSessionEndAck {
		t.Fatalf("answer to SESSION_END = DataStreamType 0x%02x, want SESSION_END_ACK (0x%02x)",
			endAck.DataStreamType, nb.NBIPXSessionEndAck)
	}
	if endAck.ConnCtrlFlag&nb.NBIPXConnFlagSYS == 0 {
		t.Errorf("END_ACK ConnCtrlFlag = 0x%02x, want the SYS bit set", endAck.ConnCtrlFlag)
	}
	// SESSION_END consumes a sequence number, unlike a zero-data probe.
	if endAck.RecvSeq != endSeq+1 {
		t.Errorf("END_ACK RecvSeq = %d, want %d (SESSION_END consumes a sequence number)",
			endAck.RecvSeq, endSeq+1)
	}

	// The blocked Send must fail on the teardown, not on the 5s request timeout.
	select {
	case got := <-done:
		if !errors.Is(got.err, clientsmb.ErrNBIPXSessionEnded) {
			t.Fatalf("in-flight Send error = %v, want ErrNBIPXSessionEnded", got.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Send still blocked after SESSION_END — it is waiting out the request timeout")
	}

	// And the circuit stays dead: no further SMB goes on the wire.
	if _, err := tr.Send([]byte("AGAIN")); !errors.Is(err, clientsmb.ErrNBIPXSessionEnded) {
		t.Fatalf("Send after SESSION_END = %v, want ErrNBIPXSessionEnded", err)
	}
	p.recvNone(200 * time.Millisecond)
}

// TestNBIPXDataFrameIsNotMistakenForNameService is the regression gate for the actual
// cause of both disconnect captures.
//
// Session and name-service traffic share IPX type 4 on socket 0x0455, and
// nb.DecodeNameService reads DataStreamType from payload byte 33 — which on a session
// DATA frame is byte 15 of the SMB payload, i.e. arbitrary file content. A frame whose
// byte 15 happened to be NBIPXNameRecognized (0x02) was classified as a name-service
// reply, found not to match the called name, and swallowed before ever reaching the
// session path. Being content-derived it is deterministic, so every retransmit of that
// frame was swallowed too and the circuit could never recover.
//
// Ground truth: captures/nbipx-disconnect2.pcap frame 9091 and its nine retransmits
// (9093-9102) all carry payload[33] = 0x02 and were all discarded; frame 9084, the same
// fragment shape with payload[33] = 0xff, was accepted normally.
//
// The payload here reproduces that exactly: a two-fragment response whose TAIL carries
// 0x02 at the poisoned offset.
func TestNBIPXDataFrameIsNotMistakenForNameService(t *testing.T) {
	tr, p, _ := dialScripted(t)

	head := bytes.Repeat([]byte{0xAA}, 1440)
	// Byte 33 of the frame payload = byte 15 of this fragment's data (the 18-byte
	// session header precedes it). 0x02 is nb.NBIPXNameRecognized.
	tail := bytes.Repeat([]byte{0xBB}, 600)
	const poisonedOffset = nb.NBIPXNameServiceDataStreamTypeOffset - nb.NBIPXSessionHeaderLen
	tail[poisonedOffset] = nb.NBIPXNameRecognized
	total := uint16(len(head) + len(tail))

	type sent struct {
		resp []byte
		err  error
	}
	done := make(chan sent, 1)
	go func() {
		resp, err := tr.Send([]byte("REQUEST"))
		done <- sent{resp, err}
	}()

	p.recv(3 * time.Second)
	p.recvSeq++

	p.data(0x00, total, 0, head)
	p.data(nb.NBIPXConnFlagEOM, total, uint16(len(head)), tail)

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("Send: %v — a DATA fragment carrying 0x%02x at payload byte %d was "+
				"swallowed as a name-service reply", got.err,
				nb.NBIPXNameRecognized, nb.NBIPXNameServiceDataStreamTypeOffset)
		}
		want := append(append([]byte(nil), head...), tail...)
		if !bytes.Equal(got.resp, want) {
			t.Fatalf("response = %d bytes, want %d", len(got.resp), len(want))
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("Send never completed: the tail fragment carrying 0x%02x at payload "+
			"byte %d never reached the session path",
			nb.NBIPXNameRecognized, nb.NBIPXNameServiceDataStreamTypeOffset)
	}
}

// TestNBIPXStillLocatesServerByName guards the other side of that fix: tightening the
// name-service check must not stop a real NAME_RECOGNIZED from being recognised. A
// genuine reply is exactly nb.NBIPXNameServiceLen bytes and arrives before the circuit is
// established, which is precisely what handleNameRecognized now requires.
func TestNBIPXStillLocatesServerByName(t *testing.T) {
	clientEnd, peerEnd := inmem.Pair(64)
	p := &scriptPeer{t: t, fl: peerEnd, in: make(chan scriptFrame, 64), peerConnID: 0x0009}
	go p.readInto()

	type dialed struct {
		tr  clientsmb.Transport
		err error
	}
	done := make(chan dialed, 1)
	go func() {
		// KnownServer false: this dial MUST go through Find-name / NAME_RECOGNIZED.
		tr, err := clientsmb.DialNBIPXWithOpts(clientEnd, scriptClientMAC, "SCRIPTED",
			ipxport.DefaultFrameType, true, clientsmb.DialNBIPXOpts{})
		done <- dialed{tr, err}
	}()

	// Answer the Find-name broadcast with a well-formed NAME_RECOGNIZED.
	body := nb.EncodeNameService(&nb.NBIPXNameServicePacket{
		NameTypeFlag:   nb.NBIPXNameRecogNameFlag,
		DataStreamType: nb.NBIPXNameRecognized,
		Name:           nb.NewName("SCRIPTED", nb.NameTypeFileServer),
	})
	if len(body) != nb.NBIPXNameServiceLen {
		t.Fatalf("encoded NAME_RECOGNIZED is %d bytes, want the wire length %d",
			len(body), nb.NBIPXNameServiceLen)
	}
	d := &ipxproto.Datagram{
		Type:    nb.IPXTypePEP,
		DstSock: nb.NBIPXSessionSocket,
		SrcSock: nb.NBIPXSessionSocket,
		DstNode: scriptClientMAC,
		SrcNode: scriptMAC,
		Payload: body,
	}
	raw, err := d.Encode(nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if err := p.fl.Write(ipxport.DefaultFrameType.Encapsulate(scriptClientMAC, scriptMAC, raw)); err != nil {
		t.Fatalf("write: %v", err)
	}

	// A located server is unicast the INIT; without the locate the client would spend
	// nbipxFindNameWindow first, so arriving inside it proves the reply was accepted.
	init, _ := p.recv(nbipxLocateProof)
	p.clientID = init.SourceConnID
	p.recvSeq = init.SendSeq + 1
	p.send(&nb.NBIPXSessionHeader{
		ConnCtrlFlag:   nb.NBIPXConnFlagSYS | nb.NBIPXConnFlagCONFIRM,
		DataStreamType: nb.NBIPXSessionData,
		SourceConnID:   p.peerConnID,
		DestConnID:     p.clientID,
		SendSeq:        0,
		RecvSeq:        nb.NBIPXSessionAcceptRecvSeq,
		BytesReceived:  nb.NBIPXSessionAcceptRecvSeq + nb.NBIPXRecvWindow,
	}, nil)

	got := <-done
	if got.err != nil {
		t.Fatalf("DialNBIPX after NAME_RECOGNIZED: %v", got.err)
	}
	_ = got.tr.Close()
}

// nbipxLocateProof is comfortably under the transport's 2s Find-name window, so an INIT
// seen within it proves the NAME_RECOGNIZED was accepted rather than timed out.
const nbipxLocateProof = 1500 * time.Millisecond

// TestNBIPXAdvertisesReceiveWindow asserts every outbound frame carries a receive-window
// edge in BytesReceived. All 2510 client frames in captures/nbipx-disconnect.pcap
// advertised 0; a Win9x peer ignores the field, but an NT NWLink peer will not transmit
// past the advertised edge and polls until it errors out. Ground truth for the values is
// the NT 3.51 station in spec/captures/nbipx-nt351-win98.pcap (INIT RecvSeq 0 /
// BytesReceived 1; everything after the handshake RecvSeq+5).
func TestNBIPXAdvertisesReceiveWindow(t *testing.T) {
	tr, p, init := dialScripted(t)

	if init.BytesReceived != nb.NBIPXInitRecvWindow {
		t.Errorf("SESSION_INITIALIZE BytesReceived = %d, want %d (the accept is the only "+
			"frame acceptable next)", init.BytesReceived, nb.NBIPXInitRecvWindow)
	}

	go func() { _, _ = tr.Send([]byte("REQUEST")) }()

	req, _ := p.recv(3 * time.Second)
	if req.BytesReceived != req.RecvSeq+nb.NBIPXRecvWindow {
		t.Errorf("DATA frame BytesReceived = %d, want RecvSeq+%d = %d",
			req.BytesReceived, nb.NBIPXRecvWindow, req.RecvSeq+nb.NBIPXRecvWindow)
	}
}
