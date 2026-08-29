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

// rxtracef narrates ONE inbound frame's fate in readLoop — dequeued from the link, and
// then either handled or dropped with the reason. Unlike nbipxtracef it guards the
// Enabled check at the call site via this method so the hot path allocates nothing when
// tracing is off (readLoop runs per frame, thousands per second under a file copy).
//
// It exists to answer a question the wire capture cannot: captures/nbipx-disconnect2.pcap
// shows the server sending the tail fragment of a 2852-byte response ten times over 4.5s
// (frames 9091, 9093-9102, all SendSeq 3261 / offset 1440) with this transport accepting
// none of them and acking none of them, while a byte-identical earlier tail (frame 9084)
// was accepted normally. Frame accounting starts at t.fl.Read(), so the log distinguishes
// "the frame never reached this transport" (delivery: the pcap handle, the uplink) from
// "readLoop read it and a filter threw it away" (a state bug in this file).
func (t *nbipxTransport) rxtracef(format string, args ...any) {
	if !nbipxTrace.Enabled(log.Trace) {
		return
	}
	nbipxTrace.Log0(log.Trace, fmt.Sprintf(format, args...))
}

// traceHdr renders an NB-IPX session header as one compact trace field set, in the same
// order the wire carries them.
func traceHdr(h *nb.NBIPXSessionHeader) string {
	return fmt.Sprintf("cc=0x%02x type=0x%02x src=%d dst=%d sseq=%d tot=%d off=%d dlen=%d rseq=%d brcv=%d",
		h.ConnCtrlFlag, h.DataStreamType, h.SourceConnID, h.DestConnID,
		h.SendSeq, h.TotalDataLen, h.Offset, h.DataLen, h.RecvSeq, h.BytesReceived)
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
// handleNameService / handleSessionRequest / sendSessionAccept; ERRATA
// captures/ipx.pcap frames 23-26, 366-368, and the 2026-08 Finder self-talk
// regression):
//   - The client broadcasts a type-20 Find-name (0x01) for SERVER<20> on socket
//     0x0455. The holder answers with a type-4 NAME_RECOGNIZED (0x02). Skipping
//     Find-name and broadcasting INIT used to let every NWLink listener accept
//     the call — ClassicStack on the same pcap station stole a WIN98-1 session
//     (captures/ipx.pcap frames 768–781) and NetShareEnum listed only IPC$. The
//     server now ignores a SESSION_INITIALIZE whose called-name is not ours.
//   - The client then sends a DATA frame (DataStreamType 0x06) whose DestConnID
//     is the unassigned sentinel 0xFFFF and whose SourceConnID is the client's chosen
//     local circuit id, carrying a [calling-name(16) || called-name(16) || 6-byte
//     trailer] payload — SOURCE name first, DESTINATION second (golden capture
//     spec/captures/nbipx-win98.pcap frames 65/66). ConnCtrlFlag is ACK|CONFIRM
//     (0x41) and SendSeq is 0.
//     It is UNICAST to the node NAME_RECOGNIZED identified, matching the golden
//     Win98↔Win98 open (frames 366–368); only a Find-name that located nobody falls
//     back to broadcasting the INIT.
//   - The server answers with a DATA frame flagged SYS|CONFIRM (0x81), RecvSeq 1,
//     assigning its own id in SourceConnID and echoing ours in DestConnID. The client
//     learns the server's connection id from this accept (the node was already
//     learned from NAME_RECOGNIZED, or from the accept if Find-name timed out).
//
// SMB data path (the client direction of handleData / sendData): each request is one
// DATA frame, SendSeq incrementing from 1, RecvSeq the cumulative ack (next server
// SendSeq expected), EOM set on the last fragment. The server's response arrives as one
// or more DATA frames (its first data frame is SendSeq 0) reassembled by EOM. See the
// sequencing-rules ERRATA on nb.NBIPXSessionHeader.
//
// Ring: CLIENT.

// nbipxSessionSocket is the IPX socket NB-IPX session traffic rides (0x0455). It and
// the unassigned-connection sentinel below are the SAME definitions the server engine
// uses (core/protocol/netbios) — each side used to keep a private literal copy.
var nbipxSessionSocket = nb.NBIPXSessionSocket

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

// nbipxMaxFrameData is the most SMB data one client DATA frame carries. A request
// larger than this is fragmented with EOM set only on the last frame — the server's
// handleData c.frag path reassembles it. It is the shared protocol-ring constant, so
// both directions agree on the fragmentation boundary by construction.
const nbipxMaxFrameData = nb.NBIPXMaxFrameData

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

// ErrNBIPXSessionEnded reports that the SERVER tore the virtual circuit down with a
// SESSION_END — the circuit is gone and no further Send on this transport can succeed.
// It is distinct from ErrTransportClosed (our own Close) so a caller can tell a
// peer-initiated teardown from a local one and reconnect.
var ErrNBIPXSessionEnded = errors.New("smb/nbipx: session ended by server")

// nbipxEndTimeout bounds how long Close waits for SESSION_END_ACK. It is deliberately
// short: the teardown is a courtesy to the peer (so it stops retransmitting on a dead
// circuit), not something a caller should block on. The observed round trip is ~1ms
// (golden capture frames 78→80), so this is generous.
const nbipxEndTimeout = 250 * time.Millisecond

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
	frag         []byte
	acceptCh     chan struct{} // closed-style signal: the accept arrived
	recognizedCh chan struct{} // closed-style signal: NAME_RECOGNIZED arrived
	endAckCh     chan struct{} // closed-style signal: SESSION_END_ACK arrived
	endAcked     bool          // guards the one-shot close of endAckCh
	peerEndCh    chan struct{} // closed-style signal: the peer sent SESSION_END
	peerEnded    bool          // guards the one-shot close of peerEndCh
	respCh       chan []byte   // a fully reassembled SMB response message
	stop         chan struct{}
	closed       bool

	// rxFrames counts every frame readLoop has taken off the link, before any
	// filter. It is touched only by readLoop, so it needs no lock. See rxtracef.
	rxFrames uint64

	// skipLocate skips the Find-name phase in establish() — set when the caller
	// already knows (typically via a local browser.Service that has seen the called
	// name announce itself) that the server is present on the segment, so
	// establish() goes straight to broadcasting SESSION_INITIALIZE with the full
	// establish budget instead of spending nbipxFindNameWindow on a redundant
	// locate. See DialNBIPXOpts.KnownServer.
	skipLocate bool
}

// DialNBIPXOpts carries optional per-Dial overrides for NB-IPX establishment, layered
// on top of DialNBIPXFrame's defaults.
type DialNBIPXOpts struct {
	// CallingName, when non-empty, overrides the MAC-derived NetBIOS calling name
	// (nbipxCallingName) this station presents in the SESSION_INITIALIZE payload. A
	// caller running as part of the ClassicStack server passes its own server
	// identity here, so the outbound client and the server's own NBIPX presence
	// share one NetBIOS name instead of a throwaway "CS-xxxxxx".
	CallingName string
	// KnownServer skips the Find-name locate phase (see nbipxTransport.skipLocate).
	KnownServer bool
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
// rather than Ethernet II). Find-name locates the holder; SESSION_INITIALIZE is
// then broadcast. It returns an error if the session is not accepted within the timeout.
func DialNBIPXFrame(fl link.FrameLink, srcMAC [6]byte, serverName string, frameType ipxport.FrameType, pinned bool) (Transport, error) {
	return DialNBIPXWithOpts(fl, srcMAC, serverName, frameType, pinned, DialNBIPXOpts{})
}

// DialNBIPXWithOpts is DialNBIPXFrame with DialNBIPXOpts overrides.
func DialNBIPXWithOpts(fl link.FrameLink, srcMAC [6]byte, serverName string, frameType ipxport.FrameType, pinned bool, opts DialNBIPXOpts) (Transport, error) {
	callingName := opts.CallingName
	if callingName == "" {
		callingName = nbipxCallingName(srcMAC)
	}
	t := &nbipxTransport{
		fl:              fl,
		srcMAC:          srcMAC,
		calledName:      nb.NewName(serverName, nb.NameTypeFileServer),
		callingName:     nb.NewName(callingName, nb.NameTypeWorkstation),
		frameType:       frameType,
		frameTypePinned: pinned,
		sendSeq:         1,
		recvSeq:         0,
		localConnID:     nextNBIPXClientConnID(),
		acceptCh:        make(chan struct{}),
		recognizedCh:    make(chan struct{}),
		endAckCh:        make(chan struct{}),
		peerEndCh:       make(chan struct{}),
		respCh:          make(chan []byte, 2),
		stop:            make(chan struct{}),
		skipLocate:      opts.KnownServer,
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

// nbipxFindNameWindow is how long establish spends on Find-name before falling
// back to a broadcast SESSION_INITIALIZE (a server that answers INIT without a
// prior locate still works; the locate is what keeps a co-located ClassicStack
// from stealing a neighbour's call).
const nbipxFindNameWindow = 2 * time.Second

// establish locates the server with Find-name (unless skipLocate), then sends
// SESSION_INITIALIZE and waits for the session-accept, retransmitting on timeout.
// skipLocate skips straight to SESSION_INITIALIZE, giving it the full establish
// budget instead of ceding nbipxFindNameWindow to a locate the caller already knows
// is unnecessary — the server-side "not our name" check (handleSessionRequest)
// still guards against a co-located ClassicStack stealing the call, so skipping the
// locate does not reopen that regression.
func (t *nbipxTransport) establish() error {
	deadline := time.Now().Add(nbipxRequestTimeout)
	if t.skipLocate {
		nbipxtracef("known server %q — skipping Find-name locate", t.calledName.String())
	} else if err := t.findName(deadline); err != nil {
		return err
	}
	nbipxtracef("SESSION_INITIALIZE %q (DestConnID 0xFFFF, SourceConnID %d)", t.calledName.String(), t.localConnID)
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

// findName broadcasts type-20 Find-name for the called server until NAME_RECOGNIZED
// arrives or nbipxFindNameWindow elapses. A timeout is not fatal: establish then
// broadcasts SESSION_INITIALIZE the way the older client did.
func (t *nbipxTransport) findName(overall time.Time) error {
	findUntil := time.Now().Add(nbipxFindNameWindow)
	if findUntil.After(overall) {
		findUntil = overall
	}
	nbipxtracef("Find-name %q", t.calledName.String())
	for attempt := 0; time.Now().Before(findUntil); attempt++ {
		if err := t.sendFindName(); err != nil {
			return err
		}
		select {
		case <-t.recognizedCh:
			t.mu.Lock()
			srv := t.serverNode
			t.mu.Unlock()
			nbipxtracef("NAME_RECOGNIZED from %s", macTrace(srv))
			return nil
		case <-time.After(400 * time.Millisecond):
			nbipxtracef("no NAME_RECOGNIZED yet, retransmitting Find-name (attempt %d)", attempt+1)
		case <-t.stop:
			return ErrTransportClosed
		}
	}
	nbipxtracef("no NAME_RECOGNIZED for %q — broadcasting SESSION_INITIALIZE", t.calledName.String())
	return nil
}

// sendInit transmits one SESSION_INITIALIZE DATA frame: DestConnID = 0xFFFF, our
// SourceConnID, SendSeq 0, ACK|CONFIRM, payload [called || calling || trailer].
//
// UNICAST to the holder Find-name located, per the golden Win98↔Win98 handshake
// (spec/errata.md "NBIPX session-request called-name must be one we own": Find name
// X<20> → Name recognized X<20> → unicast SESSION_INITIALIZE to the holder).
// sendFrameTo falls back to broadcast on its own when haveServer is false, so a
// Find-name that timed out still gets the old broadcast behaviour.
//
// This was previously broadcast unconditionally, on the claim that "a Win98 NWLink
// listener does not accept a unicast INIT". That claim was inferred from OUR client
// failing, not from observing a real peer — and it is contradicted by the golden
// capture above. Broadcast does not work either: against WIN98-1 (2026-08-19,
// bridge1) Find-name and NAME_RECOGNIZED both succeed and the broadcast INIT is
// retransmitted ten times with no accept.
func (t *nbipxTransport) sendInit() error {
	// Name order is [SOURCE][DESTINATION] — our own calling name FIRST, the server's
	// called name second. Golden capture spec/captures/nbipx-win98.pcap frame 65
	// (WIN98-2 → WIN98-1) carries "WIN98-2"<00> then "WIN98-1"<20>, and the matching
	// accept (frame 66, WIN98-1 → WIN98-2) carries "WIN98-1"<20> then "WIN98-2"<00> —
	// each sender names itself first. Emitting [called][calling] made Win98 read the
	// datagram as addressed to our workstation name rather than to itself, so it
	// silently dropped every INIT while answering Find-name normally. The layout (and
	// that ERRATA) now lives on nb.NBIPXSessionRequest, shared with the responder.
	payload := (&nb.NBIPXSessionRequest{
		Source:      t.callingName,
		Destination: t.calledName,
		Trailer:     nbipxInitTrailer,
	}).Encode()

	h := &nb.NBIPXSessionHeader{
		ConnCtrlFlag:   nbipxInitCtrl,
		DataStreamType: nb.NBIPXSessionData,
		SourceConnID:   t.localConnID,
		DestConnID:     nb.NBIPXUnassignedConnID,
		SendSeq:        0, // the INIT consumes seq 0; first SMB frame is seq 1
		TotalDataLen:   uint16(len(payload)),
		DataLen:        uint16(len(payload)),
		RecvSeq:        0,
		// Window edge: the only peer frame we can accept next is the accept itself
		// (SendSeq 0), so RecvSeq 0 + 1. See nb.NBIPXInitRecvWindow.
		BytesReceived: nb.NBIPXInitRecvWindow,
	}
	// broadcast=false: unicast to the located holder, or broadcast if none.
	return t.sendFrameTo(nb.EncodeSessionHeader(h), payload, false)
}

// sendFindName broadcasts one IPX type-20 Find-name (0x01) for the called server on
// the session socket. A Win9x NWLink holder answers with a type-4 NAME_RECOGNIZED.
func (t *nbipxTransport) sendFindName() error {
	body := nb.EncodeNameService(&nb.NBIPXNameServicePacket{
		NameTypeFlag:   0x00,
		DataStreamType: nb.NBIPXFindName,
		Name:           t.calledName,
	})
	t.mu.Lock()
	frameType := t.frameType
	srcNet := t.srcNet
	t.mu.Unlock()
	d := &ipxproto.Datagram{
		Type:    nb.IPXTypeNetBIOS,
		DstNet:  srcNet,
		DstNode: ipxproto.BroadcastNode,
		DstSock: nbipxSessionSocket,
		SrcNet:  srcNet,
		SrcNode: t.srcMAC,
		SrcSock: nbipxSessionSocket,
		Payload: body,
	}
	ipxBytes, err := d.Encode(nil)
	if err != nil {
		return err
	}
	return t.fl.Write(frameType.Encapsulate(ipxproto.BroadcastNode, t.srcMAC, ipxBytes))
}

// Send transmits one SMB message over the established circuit as sequenced DATA frame(s)
// (fragmenting if larger than one frame) and returns the reassembled DATA response.
func (t *nbipxTransport) Send(req []byte) ([]byte, error) {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil, ErrTransportClosed
	}
	if t.peerEnded {
		t.mu.Unlock()
		return nil, ErrNBIPXSessionEnded
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
	case <-t.peerEndCh:
		// The server tore the circuit down mid-request: fail now rather than burn
		// the full nbipxRequestTimeout waiting for a reply that cannot come.
		return nil, ErrNBIPXSessionEnded
	case <-time.After(nbipxRequestTimeout):
		return nil, fmt.Errorf("smb/nbipx: no response within %s", nbipxRequestTimeout)
	case <-t.stop:
		return nil, ErrTransportClosed
	}
}

// sendDataMessage frames req into one or more DATA frames numbered from firstSeq, EOM on
// the last, stamping recvSeq as the cumulative ack (and the matching window edge in
// BytesReceived) and remoteID as the server's circuit id.
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
			BytesReceived:  recvSeq + nb.NBIPXRecvWindow,
		}
		if err := t.sendFrameTo(nb.EncodeSessionHeader(h), req[off:off+n], false); err != nil {
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

// sendFrameTo encapsulates an NB-IPX session payload (header || body) in an IPX PEP
// datagram and writes it. broadcast forces the IPX/Ethernet all-ones destination
// (SESSION_INITIALIZE). Otherwise the destination is the learned server node, or
// broadcast before NAME_RECOGNIZED / accept.
func (t *nbipxTransport) sendFrameTo(header, body []byte, broadcast bool) error {
	payload := append(append([]byte(nil), header...), body...)

	t.mu.Lock()
	dstMAC := ipxproto.BroadcastNode
	dstNode := ipxproto.BroadcastNode
	dstNet := t.srcNet
	if !broadcast && t.haveServer {
		dstMAC = t.serverMAC
		dstNode = t.serverNode
		dstNet = t.serverNet
	}
	frameType := t.frameType
	t.mu.Unlock()

	d := &ipxproto.Datagram{
		Type:    nb.IPXTypePEP,
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
// + connection id), reassembles DATA responses by EOM, answers any frame that requests
// an acknowledgement (sendSystemAck), and services a peer SESSION_END (handlePeerEnd).
// It ignores frames for other circuits.
func (t *nbipxTransport) readLoop() {
	for {
		frame, err := t.fl.Read()
		if err != nil {
			if errors.Is(err, link.ErrTimeout) {
				select {
				case <-t.stop:
					return
				default:
					continue
				}
			}
			t.rxtracef("read loop exiting: %v", err)
			return
		}
		// Frame accounting starts at the link, BEFORE every filter below, so a
		// -vv trace distinguishes "the frame never reached this transport" from
		// "this transport read it and threw it away" — see rxtracef.
		t.rxFrames++
		t.rxtracef("rx#%d %d bytes from link", t.rxFrames, len(frame))

		payload, frameType, ok := ipxport.Strip(frame)
		if !ok {
			t.rxtracef("rx#%d DROP: not an IPX encapsulation", t.rxFrames)
			continue
		}
		var srcMAC [6]byte
		copy(srcMAC[:], frame[6:12]) // Ethernet source = L2 next hop to the server
		d, err := ipxproto.Decode(payload)
		if err != nil {
			t.rxtracef("rx#%d DROP: IPX decode (%d bytes): %v", t.rxFrames, len(payload), err)
			continue
		}
		if d.DstSock != nbipxSessionSocket {
			t.rxtracef("rx#%d DROP: socket 0x%02x%02x, want NB-IPX session", t.rxFrames, d.DstSock[0], d.DstSock[1])
			continue
		}
		if d.DstNode != t.srcMAC { // not addressed to our virtual station
			t.rxtracef("rx#%d DROP: node %s, want our station %s", t.rxFrames, macTrace(d.DstNode), macTrace(t.srcMAC))
			continue
		}
		if d.Type != nb.IPXTypePEP {
			t.rxtracef("rx#%d DROP: IPX type 0x%02x, want PEP", t.rxFrames, d.Type)
			continue // session + name-service traffic both ride PEP (type 4)
		}
		if t.handleNameRecognized(d, srcMAC, frameType) {
			t.rxtracef("rx#%d name-service reply", t.rxFrames)
			continue
		}
		hdr, err := nb.DecodeSessionHeader(d.Payload)
		if err != nil {
			t.rxtracef("rx#%d DROP: session header (%d bytes): %v", t.rxFrames, len(d.Payload), err)
			continue
		}
		t.rxtracef("rx#%d %s", t.rxFrames, traceHdr(hdr))
		// Only frames on our circuit: the server stamps our SourceConnID as DestConnID.
		if hdr.DestConnID != t.localConnID {
			t.rxtracef("rx#%d DROP: DestConnID %d, our circuit is %d", t.rxFrames, hdr.DestConnID, t.localConnID)
			continue
		}
		// SESSION_END_ACK (0x08) closes out our teardown — see Close.
		if hdr.DataStreamType == nb.NBIPXSessionEndAck {
			t.signalEndAck()
			continue
		}
		// SESSION_END (0x07): the server is tearing the circuit down under us.
		if hdr.DataStreamType == nb.NBIPXSessionEnd {
			t.handlePeerEnd(hdr)
			continue
		}
		if hdr.DataStreamType != nb.NBIPXSessionData {
			continue
		}
		t.handleInbound(d, hdr, srcMAC, frameType)
	}
}

// handleNameRecognized records the server node from a type-4 NAME_RECOGNIZED for
// our called name. It returns true when the datagram was a name-service reply
// (so the session path must not parse it as DATA).
//
// It must first decide whether the datagram is a name-service packet AT ALL, and that
// cannot be left to nb.DecodeNameService: session traffic and name-service traffic share
// IPX type 4 on one socket, and the decoder reads DataStreamType from payload byte 33 —
// which on a session DATA frame is byte 15 of the SMB payload, arbitrary file bytes.
// Whenever those bytes happened to be 0x02 the frame parsed as NAME_RECOGNIZED, and
// because the embedded "name" then did not match calledName it was swallowed here with
// `return true`, never reaching the session path.
//
// That was the real cause of the disconnects in captures/nbipx-disconnect.pcap and
// nbipx-disconnect2.pcap. Being content-derived it is DETERMINISTIC, so a retransmit of
// the same frame is swallowed every time: in disconnect2 the tail fragment of a
// 2852-byte Read AndX response (frame 9091, payload[33] = 0x02) and all nine of the
// server's retransmits (9093-9102) were discarded here, while a structurally identical
// earlier tail (frame 9084, payload[33] = 0xff) went through. The circuit could not
// recover no matter how long the server retried, and Win98 ended the session.
//
// Two guards, either of which is sufficient, and cheap enough to keep both:
//   - Length: a name-service packet is EXACTLY nb.NBIPXNameServiceLen (50) bytes on the
//     wire (ipx.len 80 in every capture we hold — spec/captures/nbipx-win98.pcap frames
//     52/53, nbipx-disconnect2.pcap frame 8). A fragmented DATA frame is ~1430.
//   - Phase: NAME_RECOGNIZED only answers the Find-name that precedes
//     SESSION_INITIALIZE. Once the circuit is established nothing on it is name service.
func (t *nbipxTransport) handleNameRecognized(d *ipxproto.Datagram, srcMAC [6]byte, frameType ipxport.FrameType) bool {
	if len(d.Payload) != nb.NBIPXNameServiceLen {
		return false
	}
	t.mu.Lock()
	established := t.established
	t.mu.Unlock()
	if established {
		return false
	}
	pkt, err := nb.DecodeNameService(d.Payload)
	if err != nil || pkt.DataStreamType != nb.NBIPXNameRecognized {
		return false
	}
	if pkt.Name != t.calledName {
		return true // a reply for someone else — not session data either
	}
	t.mu.Lock()
	already := t.haveServer
	t.serverNode = d.SrcNode
	t.serverNet = d.SrcNet
	t.serverMAC = srcMAC
	if !t.frameTypePinned {
		t.frameType = frameType
	}
	t.haveServer = true
	ch := t.recognizedCh
	t.mu.Unlock()
	if !already && ch != nil {
		close(ch)
	}
	return true
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

	ackReq := hdr.ConnCtrlFlag&nb.NBIPXConnFlagACK != 0

	// A zero-data SYS frame is a control/ack (no sequence consumed): nothing to
	// deliver. An ACK-requesting probe still has to be answered — see sendSystemAck.
	if hdr.DataLen == 0 {
		sendSeq, recvSeq := t.sendSeq, t.recvSeq
		t.mu.Unlock()
		if ackReq {
			t.sendSystemAck(sendSeq, recvSeq)
		}
		return
	}

	// Sequenced data. Accept an in-order frame (SendSeq == recvSeq); advance and
	// reassemble. The server's first data frame is SendSeq 0.
	if hdr.SendSeq != t.recvSeq {
		sendSeq, recvSeq := t.sendSeq, t.recvSeq
		fragLen := len(t.frag)
		t.mu.Unlock()
		t.rxtracef("rx#%d DROP: out of window — SendSeq %d, expecting %d (frag holds %d bytes)%s",
			t.rxFrames, hdr.SendSeq, recvSeq, fragLen, ackSuffix(ackReq))
		// Out of window — the frame itself is dropped, but a retransmit that ASKS
		// for an ack must still be answered, or the peer never learns which frame
		// we are actually missing. See the deadlock described on sendSystemAck.
		if ackReq {
			t.sendSystemAck(sendSeq, recvSeq)
		}
		return
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
		sendSeq, recvSeq := t.sendSeq, t.recvSeq
		fragLen := len(t.frag)
		t.mu.Unlock()
		t.rxtracef("rx#%d fragment accepted — %d bytes at offset %d, frag now %d/%d%s",
			t.rxFrames, len(body), hdr.Offset, fragLen, hdr.TotalDataLen, ackSuffix(ackReq))
		// A mid-message fragment produces no reply to carry the ack, so honour an
		// explicit request with a system frame (the server engine's handleData does
		// the same on its side).
		if ackReq {
			t.sendSystemAck(sendSeq, recvSeq)
		}
		return
	}
	var msg []byte
	if len(t.frag) > 0 {
		msg = append(t.frag, body...) //nolint:gocritic // t.frag is nilled on the next line, so aliasing its backing array is harmless
		t.frag = nil
	} else {
		msg = append([]byte(nil), body...)
	}
	sendSeq, recvSeq := t.sendSeq, t.recvSeq
	t.mu.Unlock()

	t.rxtracef("rx#%d EOM — message complete, %d bytes (declared %d)%s",
		t.rxFrames, len(msg), hdr.TotalDataLen, ackSuffix(ackReq))

	// The completed message wakes Send, whose next request piggybacks the ack — but
	// that is the CALLER's next request, which may be seconds away or never (the SMB
	// layer may be done). An explicit request cannot wait on it.
	if ackReq {
		t.sendSystemAck(sendSeq, recvSeq)
	}

	select {
	case t.respCh <- msg:
	case <-t.stop:
	default:
		// respCh is buffered and drained by Send; landing here means a completed
		// message was thrown away because nobody was waiting for it.
		t.rxtracef("rx#%d WARN: dropped a complete %d-byte message — no receiver", t.rxFrames, len(msg))
	}
}

// ackSuffix renders whether the frame just traced asked for an acknowledgement, so a
// trace line shows in one place both what we did with the frame and whether we owed the
// peer a reply for it.
func ackSuffix(ackReq bool) string {
	if ackReq {
		return " [ack requested → acking]"
	}
	return ""
}

// sendSystemAck answers a peer frame that set the ACK-required bit (0x40) with a
// zero-data SYS frame carrying our current counters. Per the sequencing ERRATA on
// nb.NBIPXSessionHeader a zero-data control frame consumes NO sequence number, so it
// carries our unchanged sendSeq and the unchanged cumulative recvSeq; acking a probe
// as consumed reads as a protocol error.
//
// This transport used to acknowledge only implicitly, by piggybacking RecvSeq on the
// next outbound request, and had no explicit-ack path at all — over the whole of
// captures/nbipx-disconnect.pcap (2026-08-20, Win98 server, 2510 client data frames)
// it sent zero SYS acks where the server sent 2507. That is fatal, not merely
// impolite: at frame 7841 the EOM tail of a 2852-byte Read AndX response was lost,
// so nothing completed, so no request went out to carry a piggybacked ack, so the
// server's nine ACK-required retransmits (frames 7842-7851, 500ms apart) went
// unanswered and it killed the circuit with SESSION_END. Worse, the retransmits were
// of the frame we ALREADY had, and dropping them silently at the window check meant
// the server could never learn we were missing the NEXT one — the circuit could not
// have recovered however long it retried. Three earlier stalls in the same capture
// (frames 222, 339-365, 2520) survived only because the application happened to emit
// another SMB request before the retry limit.
func (t *nbipxTransport) sendSystemAck(sendSeq, recvSeq uint16) {
	t.mu.Lock()
	remoteID := t.remoteConnID
	t.mu.Unlock()

	h := &nb.NBIPXSessionHeader{
		ConnCtrlFlag:   nb.NBIPXConnFlagSYS,
		DataStreamType: nb.NBIPXSessionData,
		SourceConnID:   t.localConnID,
		DestConnID:     remoteID,
		SendSeq:        sendSeq, // control frames consume no sequence number
		RecvSeq:        recvSeq,
		BytesReceived:  recvSeq + nb.NBIPXRecvWindow,
	}
	nbipxtracef("SYS ack (RecvSeq %d, window edge %d)", recvSeq, recvSeq+nb.NBIPXRecvWindow)
	if err := t.sendFrameTo(nb.EncodeSessionHeader(h), nil, false); err != nil {
		nbipxtracef("SYS ack failed: %v", err)
	}
}

// Close tears down the read loop and closes the link, sending a best-effort
// SESSION_END on an established circuit first (see endSession for why skipping it is
// not free). A circuit the PEER already ended is skipped: handlePeerEnd has answered
// its SESSION_END and cleared established, and ending an already-dead circuit would
// only put a stray frame on the wire.
func (t *nbipxTransport) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true // no further Sends; the read loop keeps running for the END_ACK
	established := t.established && !t.peerEnded
	t.mu.Unlock()

	if established {
		t.endSession()
	}

	t.mu.Lock()
	close(t.stop)
	t.mu.Unlock()
	return t.fl.Close()
}

// endSession performs the NB-IPX teardown: one SESSION_END (0x07) on our circuit,
// then a short wait for the server's SESSION_END_ACK (0x08). Both are best-effort —
// a lost teardown must never make Close fail or block a caller for long.
//
// Skipping it is not free, despite what this transport used to assume ("the server
// ages out an idle circuit"). A real NWLink client always ends its circuit: golden
// capture spec/captures/nbipx-win98.pcap frames 78/80 show SESSION_END (ConnCtrlFlag
// ACK 0x40, DataStreamType 0x07) answered by SESSION_END_ACK (SYS 0x80, 0x08). When
// we vanished instead, WIN98-1 was left retransmitting the last response of the dead
// circuit every 500ms indefinitely, and the NEXT connection from this station would
// intermittently time out ("smb/nbipx: no response within 5s").
func (t *nbipxTransport) endSession() {
	t.mu.Lock()
	h := &nb.NBIPXSessionHeader{
		ConnCtrlFlag:   nb.NBIPXConnFlagACK, // ACK requested: the peer owes us an END_ACK
		DataStreamType: nb.NBIPXSessionEnd,
		SourceConnID:   t.localConnID,
		DestConnID:     t.remoteConnID,
		SendSeq:        t.sendSeq, // SESSION_END consumes a sequence number
		RecvSeq:        t.recvSeq,
		BytesReceived:  t.recvSeq + nb.NBIPXRecvWindow,
	}
	t.sendSeq++
	t.mu.Unlock()

	nbipxtracef("SESSION_END (circuit %d)", h.SourceConnID)
	if err := t.sendFrameTo(nb.EncodeSessionHeader(h), nil, false); err != nil {
		return
	}
	select {
	case <-t.endAckCh:
		nbipxtracef("SESSION_END_ACK — circuit closed")
	case <-time.After(nbipxEndTimeout):
		nbipxtracef("no SESSION_END_ACK within %s — closing anyway", nbipxEndTimeout)
	}
}

// signalEndAck closes endAckCh once, waking a teardown blocked in endSession.
func (t *nbipxTransport) signalEndAck() {
	t.mu.Lock()
	already := t.endAcked
	t.endAcked = true
	ch := t.endAckCh
	t.mu.Unlock()
	if !already && ch != nil {
		close(ch)
	}
}

// handlePeerEnd services a server-initiated SESSION_END (0x07): answer it with
// SESSION_END_ACK (SYS 0x80, 0x08) as the golden Win98↔Win98 teardown does
// (spec/captures/nbipx-win98.pcap frames 78/80), then mark the circuit dead so a
// blocked or subsequent Send fails immediately instead of talking to a peer that has
// already forgotten us.
//
// SESSION_END consumes a sequence number (ERRATA on nb.NBIPXSessionHeader: WfW's
// SESSION_END at seq 5 was answered by NT with RecvSeq 6), so the end-ack acknowledges
// it as consumed — unlike a zero-data probe.
//
// Ignoring the inbound end (readLoop's non-DATA types were all dropped) left the
// transport believing an established circuit was still up: in
// captures/nbipx-disconnect.pcap the SMB layer kept issuing requests into the dead
// session at frames 7855/7856/7859/7860, one per nbipxRequestTimeout, each a
// guaranteed 5s stall. The SESSION_END_ACK that capture does show (frame 7854) came
// from IPX net 3 — ClassicStack's own server-side NBIPX service answering on the same
// station — not from this transport.
func (t *nbipxTransport) handlePeerEnd(hdr *nb.NBIPXSessionHeader) {
	t.mu.Lock()
	if t.peerEnded {
		t.mu.Unlock()
		return // already torn down; a retransmitted end still gets the ack below
	}
	t.peerEnded = true
	t.established = false
	recvSeq := hdr.SendSeq + 1 // the END consumes the peer's sequence number
	t.recvSeq = recvSeq
	sendSeq, remoteID := t.sendSeq, t.remoteConnID
	ch := t.peerEndCh
	t.mu.Unlock()

	nbipxtracef("peer SESSION_END (circuit %d) — answering END_ACK, circuit dead", remoteID)
	h := &nb.NBIPXSessionHeader{
		ConnCtrlFlag:   nb.NBIPXConnFlagSYS,
		DataStreamType: nb.NBIPXSessionEndAck,
		SourceConnID:   t.localConnID,
		DestConnID:     remoteID,
		SendSeq:        sendSeq,
		RecvSeq:        recvSeq,
		BytesReceived:  recvSeq + nb.NBIPXRecvWindow,
	}
	if err := t.sendFrameTo(nb.EncodeSessionHeader(h), nil, false); err != nil {
		nbipxtracef("SESSION_END_ACK failed: %v", err)
	}
	if ch != nil {
		close(ch)
	}
}
