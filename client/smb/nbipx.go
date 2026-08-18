package smb

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/client/trace"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	ipxport "github.com/ObsoleteMadness/ClassicStack/core/port/ipx"
	ipxproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx"
	nb "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbios"
)

// nbipxTrace narrates the NBIPX (NWLink) session flow at log.Trace through the shared
// client/trace sink, so `csfs -v` shows the SESSION_INITIALIZE / accept / sequenced DATA
// exchange alongside every other transport's trace.
var nbipxTrace = trace.Logger("nbipx")

// nbipxtracef narrates one NBIPX wire-trace line at log.Trace (no-op unless -v is on).
func nbipxtracef(format string, args ...any) {
	if !nbipxTrace.Enabled(log.Trace) {
		return
	}
	nbipxTrace.Log0(log.Trace, fmt.Sprintf(format, args...))
}

// nbipx.go is the SMB-over-NetBIOS-over-IPX (NWLink) CLIENT transport: the client
// mirror of the server's core/service/netbios NB-IPX session engine. Unlike the
// direct-hosted IPX transport (ipx.go), which rides SMB straight on IPX with no session
// layer, this transport establishes an NB-IPX virtual circuit — SESSION_INITIALIZE →
// session-accept — and then carries each SMB message as a sequenced DATA frame,
// reassembling the server's DATA response. It is what a real Windows-for-Workgroups /
// Win9x NWLink redirector speaks to a NetBIOS-over-IPX file server.
//
// Establishment (the client direction of core/service/netbios/nbipx.go
// handleSessionRequest / sendSessionAccept; ERRATA captures/ipx.pcap frames 23-26,
// 366-368):
//   - The client sends a DATA frame (DataStreamType 0x06) whose DestConnID is the
//     unassigned sentinel 0xFFFF and whose SourceConnID is the client's chosen local
//     circuit id, carrying a [called-name(16) || calling-name(16) || 6-byte trailer]
//     payload. ConnCtrlFlag is ACK|CONFIRM (0x41) and SendSeq is 0 (the
//     SESSION_INITIALIZE consumes the client's seq 0, so the first SMB frame is seq 1).
//   - The server answers with a DATA frame flagged SYS|CONFIRM (0x81), RecvSeq 1,
//     assigning its own id in SourceConnID and echoing ours in DestConnID. The client
//     learns the server's node (the first frame arrives from broadcast) and its
//     connection id from this accept.
//
// SMB data path (the client direction of handleData / sendData): each request is one
// DATA frame, SendSeq incrementing from 1, RecvSeq the cumulative ack (next server
// SendSeq expected), EOM set on the last fragment. The server's response arrives as one
// or more DATA frames (its first data frame is SendSeq 0) reassembled by EOM. See the
// sequencing-rules ERRATA on nb.NBIPXSessionHeader.
//
// Ring: CLIENT.

// nbipxSessionSocket is the IPX socket NB-IPX session traffic rides (0x0455), matching
// the server's netbios.NBIPXSessionSocket.
var nbipxSessionSocket = [2]byte{0x04, 0x55}

// nbipxUnassignedConnID is the DestConnID sentinel the client stamps on its
// SESSION_INITIALIZE before the server has assigned a connection id (mirrors the
// server's nbipxUnassignedConnID).
const nbipxUnassignedConnID uint16 = 0xFFFF

// nbipxConnIDs hands out the client's SourceConnID per Dial. 0 means "no connection"
// on the wire, so it is skipped on wrap. A fixed 0x0001 reused across reconnects from
// the same station collided with the server's still-live circuit (same node + remote
// id) and the new SESSION_INITIALIZE was treated as data on the old session.
var nbipxConnIDs atomic.Uint32

func nextNBIPXClientConnID() uint16 {
	for {
		n := uint16(nbipxConnIDs.Add(1))
		if n != 0 {
			return n
		}
	}
}

// nbipxInitCtrl is the ConnCtrlFlag on the client's SESSION_INITIALIZE DATA frame:
// ACK (0x40, request an acknowledgement — the accept) | CONFIRM (0x01). Observed 0x41
// on the wire (ERRATA captures/ipx.pcap frame 366).
const nbipxInitCtrl = nb.NBIPXConnFlagACK | nb.NBIPXConnFlagCONFIRM

// nbipxMaxFrameData is the most SMB data one client DATA frame carries: an Ethernet II
// payload (1500) less the IPX header (30) and the NB-IPX session header (18). A request
// larger than this is fragmented with EOM set only on the last frame — the server's
// handleData c.frag path reassembles it. It matches the server's nbipxMaxFrameData so
// both directions agree on the fragmentation boundary.
const nbipxMaxFrameData = 1500 - ipxproto.HeaderLen - nb.NBIPXSessionHeaderLen

// nbipxMaxResponse is the largest SMB response this transport reports it can carry back
// in one Send. The server reassembles a fragmented response across DATA frames (Offset/
// TotalDataLen), so unlike direct-hosted IPX this transport is NOT limited to a single
// datagram; a whole SMB message up to maxMessage can arrive. But the classic NWLink
// redirectors negotiate a modest buffer, so the session's own MaxBufferSize bounds it in
// practice. Report a generous cap and let the session's negotiated buffer govern.
const nbipxMaxResponse = maxMessage

// nbipxInitTrailer is the 6-byte capability trailer on the SESSION_INITIALIZE:
// [max frame data (LE16)][timer][timer]. We advertise 1440 (0x05A0, the Win98 value)
// and the Win9x-family timer pair (25 00 0d 00), a combination every observed NWLink
// server echoes/accepts (ERRATA captures/ipx.pcap frame 366, sequencing rule 5). The
// server retains and echoes the trailer verbatim, so its exact value is not load-bearing
// for interop with ClassicStack; matching a real client keeps it honest against others.
var nbipxInitTrailer = []byte{0xA0, 0x05, 0x25, 0x00, 0x0D, 0x00}

// nbipxRequestTimeout bounds how long one Send waits for the reassembled DATA response
// (or the accept) before giving up on a lost frame over the connectionless carrier.
const nbipxRequestTimeout = 5 * time.Second

// nbipxTransport is the SMB-over-NBIPX client transport. It owns the pcap FrameLink,
// runs a read loop that reassembles inbound DATA frames into whole SMB messages, and
// drives the NB-IPX session state machine (establishment + per-frame sequencing).
type nbipxTransport struct {
	fl          link.FrameLink
	srcMAC      [6]byte
	srcNet      [4]byte
	calledName  nb.Name // the server's NetBIOS name (\\SERVER<20>), from the URI
	callingName nb.Name // this client's NetBIOS name

	frameType       ipxport.FrameType // outbound encapsulation (learned unless pinned)
	frameTypePinned bool

	mu           sync.Mutex
	serverNode   [6]byte
	serverNet    [4]byte
	serverMAC    [6]byte // Ethernet source MAC of the server's frames — the L2 next hop
	haveServer   bool
	localConnID  uint16 // our SourceConnID, unique per Dial so reconnects do not collide
	remoteConnID uint16 // the server's SourceConnID, learned from the accept
	established  bool

	// Window-of-one sequencing (mirrors the server ipxCircuit): sendSeq is the SendSeq
	// our NEXT data frame carries; recvSeq is the next SendSeq we expect from the server
	// (stamped as RecvSeq on everything we send). The SESSION_INITIALIZE consumes our
	// seq 0, so sendSeq starts at 1; the server's accept consumes nothing and its first
	// data frame is SendSeq 0, so recvSeq starts at 0.
	sendSeq uint16
	recvSeq uint16

	// Reassembly of the server's DATA response for the Send in flight.
	frag     []byte
	acceptCh chan struct{} // closed-style signal: the accept arrived
	respCh   chan []byte   // a fully reassembled SMB response message
	stop     chan struct{}
	closed   bool
}

// DialNBIPX builds an SMB-over-NBIPX client transport in the default (learned) frame
// type. See DialNBIPXFrame to pin a frame type.
func DialNBIPX(fl link.FrameLink, srcMAC [6]byte, serverName string) (Transport, error) {
	return DialNBIPXFrame(fl, srcMAC, serverName, ipxport.DefaultFrameType, false)
}

// DialNBIPXFrame builds an SMB-over-NBIPX client transport over the pcap FrameLink fl and
// establishes the NB-IPX session to serverName (the \\SERVER label from the URI). srcMAC
// is the virtual station's node (RandomMAC() by default). frameType is the encapsulation
// used on the initial broadcast; when pinned is false the transport LEARNS the server's
// frame type from its first frame (so it reaches a server bound on raw-802.3 / 802.2
// rather than Ethernet II). The first SESSION_INITIALIZE is broadcast; the server's node
// is learned from the accept. It returns an error if the session is not accepted within
// the timeout.
func DialNBIPXFrame(fl link.FrameLink, srcMAC [6]byte, serverName string, frameType ipxport.FrameType, pinned bool) (Transport, error) {
	t := &nbipxTransport{
		fl:              fl,
		srcMAC:          srcMAC,
		calledName:      nb.NewName(serverName, nb.NameTypeFileServer),
		callingName:     nb.NewName(nbipxCallingName(srcMAC), nb.NameTypeWorkstation),
		frameType:       frameType,
		frameTypePinned: pinned,
		sendSeq:         1,
		recvSeq:         0,
		localConnID:     nextNBIPXClientConnID(),
		acceptCh:        make(chan struct{}),
		respCh:          make(chan []byte, 2),
		stop:            make(chan struct{}),
	}
	go t.readLoop()
	if err := t.establish(); err != nil {
		_ = t.Close()
		return nil, err
	}
	return t, nil
}

// nbipxCallingName derives a stable-ish NetBIOS workstation name for the client from its
// MAC, so two client stations on one segment present distinct calling names. The server
// does not validate the calling name (it swaps the pair on accept), so any well-formed
// name suffices; deriving it from the MAC keeps it unique without extra config.
func nbipxCallingName(mac [6]byte) string {
	const hex = "0123456789ABCDEF"
	// "CS-" + last 3 MAC octets in hex → e.g. "CS-A1B2C3" (fits 15 chars).
	b := []byte{'C', 'S', '-'}
	for _, o := range mac[3:] {
		b = append(b, hex[o>>4], hex[o&0x0F])
	}
	return string(b)
}

// establish sends the SESSION_INITIALIZE and waits for the server's session-accept,
// retransmitting on timeout (the connectionless carrier may drop the first broadcast).
func (t *nbipxTransport) establish() error {
	nbipxtracef("SESSION_INITIALIZE %q (DestConnID 0xFFFF, SourceConnID %d)", t.calledName.String(), t.localConnID)
	deadline := time.Now().Add(nbipxRequestTimeout)
	for attempt := 0; time.Now().Before(deadline); attempt++ {
		if err := t.sendInit(); err != nil {
			return err
		}
		select {
		case <-t.acceptCh:
			t.mu.Lock()
			srv, rid := t.serverNode, t.remoteConnID
			t.mu.Unlock()
			nbipxtracef("session-accept from %s (server ConnID %d) — circuit established", macTrace(srv), rid)
			return nil
		case <-time.After(500 * time.Millisecond):
			nbipxtracef("no accept yet, retransmitting SESSION_INITIALIZE (attempt %d)", attempt+1)
		case <-t.stop:
			return ErrTransportClosed
		}
	}
	return fmt.Errorf("smb/nbipx: no session-accept within %s", nbipxRequestTimeout)
}

// sendInit transmits one SESSION_INITIALIZE DATA frame: DestConnID = 0xFFFF, our
// SourceConnID, SendSeq 0, ACK|CONFIRM, payload [called || calling || trailer]. It is
// broadcast until the server node is learned.
func (t *nbipxTransport) sendInit() error {
	payload := make([]byte, 0, 2*nb.NameLength+len(nbipxInitTrailer))
	payload = append(payload, t.calledName[:]...)
	payload = append(payload, t.callingName[:]...)
	payload = append(payload, nbipxInitTrailer...)

	h := &nb.NBIPXSessionHeader{
		ConnCtrlFlag:   nbipxInitCtrl,
		DataStreamType: nb.NBIPXSessionData,
		SourceConnID:   t.localConnID,
		DestConnID:     nbipxUnassignedConnID,
		SendSeq:        0, // the INIT consumes seq 0; first SMB frame is seq 1
		TotalDataLen:   uint16(len(payload)),
		DataLen:        uint16(len(payload)),
		RecvSeq:        0,
	}
	return t.sendFrame(nb.EncodeSessionHeader(h), payload)
}

// Send transmits one SMB message over the established circuit as sequenced DATA frame(s)
// (fragmenting if larger than one frame) and returns the reassembled DATA response.
func (t *nbipxTransport) Send(req []byte) ([]byte, error) {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil, ErrTransportClosed
	}
	if !t.established {
		t.mu.Unlock()
		return nil, errors.New("smb/nbipx: session not established")
	}
	// Drain any stale reassembled response left from a timed-out prior Send.
	for {
		select {
		case <-t.respCh:
			continue
		default:
		}
		break
	}
	remoteID := t.remoteConnID
	firstSeq := t.sendSeq
	recvSeq := t.recvSeq
	frames := (len(req) + nbipxMaxFrameData - 1) / nbipxMaxFrameData
	if frames == 0 {
		frames = 1
	}
	t.sendSeq += uint16(frames)
	t.mu.Unlock()

	if err := t.sendDataMessage(req, firstSeq, remoteID, recvSeq); err != nil {
		return nil, err
	}

	select {
	case resp := <-t.respCh:
		return resp, nil
	case <-time.After(nbipxRequestTimeout):
		return nil, fmt.Errorf("smb/nbipx: no response within %s", nbipxRequestTimeout)
	case <-t.stop:
		return nil, ErrTransportClosed
	}
}

// sendDataMessage frames req into one or more DATA frames numbered from firstSeq, EOM on
// the last, stamping recvSeq as the cumulative ack and remoteID as the server's circuit
// id.
func (t *nbipxTransport) sendDataMessage(req []byte, firstSeq, remoteID, recvSeq uint16) error {
	total := uint16(len(req))
	seq := firstSeq
	for off := 0; ; off += nbipxMaxFrameData {
		n := len(req) - off
		last := n <= nbipxMaxFrameData
		if !last {
			n = nbipxMaxFrameData
		}
		var ctrl uint8
		if last {
			ctrl = nb.NBIPXConnFlagEOM | nb.NBIPXConnFlagACK
		}
		h := &nb.NBIPXSessionHeader{
			ConnCtrlFlag:   ctrl,
			DataStreamType: nb.NBIPXSessionData,
			SourceConnID:   t.localConnID,
			DestConnID:     remoteID,
			SendSeq:        seq,
			TotalDataLen:   total,
			Offset:         uint16(off),
			DataLen:        uint16(n),
			RecvSeq:        recvSeq,
		}
		if err := t.sendFrame(nb.EncodeSessionHeader(h), req[off:off+n]); err != nil {
			return err
		}
		seq++
		if last {
			return nil
		}
	}
}

// MaxResponse reports the reassembling transport's large response ceiling (the session's
// own negotiated buffer governs in practice).
func (t *nbipxTransport) MaxResponse() int { return nbipxMaxResponse }

// sendFrame encapsulates an NB-IPX session payload (header || body) in an IPX PEP
// datagram inside an Ethernet II frame and writes it. The destination is the learned
// server node (broadcast before the accept).
func (t *nbipxTransport) sendFrame(header, body []byte) error {
	payload := append(append([]byte(nil), header...), body...)

	t.mu.Lock()
	dstMAC := broadcastNode
	dstNode := broadcastNode
	dstNet := t.srcNet
	if t.haveServer {
		dstMAC = t.serverMAC
		dstNode = t.serverNode
		dstNet = t.serverNet
	}
	frameType := t.frameType
	t.mu.Unlock()

	d := &ipxproto.Datagram{
		Type:    ipxPEPType,
		DstNet:  dstNet,
		DstNode: dstNode,
		DstSock: nbipxSessionSocket,
		SrcNet:  t.srcNet,
		SrcNode: t.srcMAC,
		SrcSock: nbipxSessionSocket,
		Payload: payload,
	}
	ipxBytes, err := d.Encode(nil)
	if err != nil {
		return err
	}
	return t.fl.Write(frameType.Encapsulate(dstMAC, t.srcMAC, ipxBytes))
}

// readLoop reads frames, strips the encapsulation, decodes the NB-IPX session header,
// and drives the client state machine: it accepts the session (learning the server node
// + connection id), reassembles DATA responses by EOM, and answers a server SYS|ACK
// probe. It ignores frames for other circuits.
func (t *nbipxTransport) readLoop() {
	for {
		frame, err := t.fl.Read()
		if err != nil {
			if err == link.ErrTimeout {
				select {
				case <-t.stop:
					return
				default:
					continue
				}
			}
			return
		}
		payload, frameType, ok := ipxport.Strip(frame)
		if !ok {
			continue
		}
		var srcMAC [6]byte
		copy(srcMAC[:], frame[6:12]) // Ethernet source = L2 next hop to the server
		d, err := ipxproto.Decode(payload)
		if err != nil || d.Type != ipxPEPType || d.DstSock != nbipxSessionSocket {
			continue
		}
		if d.DstNode != t.srcMAC { // not addressed to our virtual station
			continue
		}
		hdr, err := nb.DecodeSessionHeader(d.Payload)
		if err != nil || hdr.DataStreamType != nb.NBIPXSessionData {
			continue
		}
		// Only frames on our circuit: the server stamps our SourceConnID as DestConnID.
		if hdr.DestConnID != t.localConnID {
			continue
		}
		t.handleInbound(d, hdr, srcMAC, frameType)
	}
}

// handleInbound processes one inbound DATA frame addressed to our circuit: the
// session-accept (SYS|CONFIRM), a sequenced data fragment, or a zero-data SYS control.
// srcMAC/frameType are the frame's Ethernet source (L2 next hop) and encapsulation,
// learned alongside the server's IPX address on the first frame.
func (t *nbipxTransport) handleInbound(d *ipxproto.Datagram, hdr *nb.NBIPXSessionHeader, srcMAC [6]byte, frameType ipxport.FrameType) {
	sys := hdr.ConnCtrlFlag&nb.NBIPXConnFlagSYS != 0
	confirm := hdr.ConnCtrlFlag&nb.NBIPXConnFlagCONFIRM != 0
	eom := hdr.ConnCtrlFlag&nb.NBIPXConnFlagEOM != 0

	t.mu.Lock()
	// The session-accept: SYS|CONFIRM with RecvSeq 1 (NBIPXSessionAcceptRecvSeq),
	// carrying the server's SourceConnID. RecvSeq 1 distinguishes a fresh accept from
	// a SYS|CONFIRM re-accept of a stale circuit (which keeps the old counters).
	if !t.established && sys && confirm && hdr.RecvSeq == nb.NBIPXSessionAcceptRecvSeq {
		t.serverNode = d.SrcNode
		t.serverNet = d.SrcNet
		t.serverMAC = srcMAC
		if !t.frameTypePinned {
			t.frameType = frameType
		}
		t.haveServer = true
		t.remoteConnID = hdr.SourceConnID
		t.established = true
		accepted := t.acceptCh
		t.mu.Unlock()
		close(accepted)
		return
	}
	if !t.haveServer {
		// Any inbound on our circuit also fixes the server address (defensive).
		t.serverNode = d.SrcNode
		t.serverNet = d.SrcNet
		t.serverMAC = srcMAC
		if !t.frameTypePinned {
			t.frameType = frameType
		}
		t.haveServer = true
	}

	// A zero-data SYS frame is a control/ack (no sequence consumed): nothing to deliver.
	if hdr.DataLen == 0 {
		t.mu.Unlock()
		return
	}

	// Sequenced data. Accept an in-order frame (SendSeq == recvSeq); advance and
	// reassemble. The server's first data frame is SendSeq 0.
	if hdr.SendSeq != t.recvSeq {
		t.mu.Unlock()
		return // out of window — drop; the server retransmits
	}
	t.recvSeq++

	body := d.Payload
	if len(body) >= nb.NBIPXSessionHeaderLen+int(hdr.DataLen) {
		body = body[nb.NBIPXSessionHeaderLen : nb.NBIPXSessionHeaderLen+int(hdr.DataLen)]
	} else {
		body = body[nb.NBIPXSessionHeaderLen:]
	}

	if !eom {
		t.frag = append(t.frag, body...)
		t.mu.Unlock()
		return
	}
	var msg []byte
	if len(t.frag) > 0 {
		msg = append(t.frag, body...)
		t.frag = nil
	} else {
		msg = append([]byte(nil), body...)
	}
	t.mu.Unlock()

	select {
	case t.respCh <- msg:
	case <-t.stop:
	default:
	}
}

// Close tears down the read loop and closes the link. It does not send SESSION_END: the
// session layer's Close already issues TREE_DISCONNECT/LOGOFF, and the server ages out
// an idle circuit; a best-effort end frame is not worth blocking Close on.
func (t *nbipxTransport) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	close(t.stop)
	t.mu.Unlock()
	return t.fl.Close()
}
