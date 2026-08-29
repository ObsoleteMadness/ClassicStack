package smb

import (
	"errors"
	"fmt"
	"sync"
	"time"

	clientlink "github.com/ObsoleteMadness/ClassicStack/client/link"
	"github.com/ObsoleteMadness/ClassicStack/client/trace"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	ipxport "github.com/ObsoleteMadness/ClassicStack/core/port/ipx"
	ipxproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx"
	nb "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbios"
	proto "github.com/ObsoleteMadness/ClassicStack/core/protocol/smb"
)

// ipxTrace narrates the direct-hosted-SMB-over-IPX transport at log.Trace through the
// shared client/trace sink (scope "ipx"), so `csfs -v` shows the connectionless
// server-node/CID learning alongside every other transport's trace.
var ipxTrace = trace.Logger("ipx")

// ipxtracef narrates one direct-IPX wire-trace line at log.Trace (no-op unless -v is on).
func ipxtracef(format string, args ...any) {
	if !ipxTrace.Enabled(log.Trace) {
		return
	}
	ipxTrace.Log0(log.Trace, fmt.Sprintf(format, args...))
}

// ipx.go is the SMB direct-hosted-over-IPX CLIENT transport: the client mirror of the
// server's core/service/smb.DirectIPX. SMB rides straight on IPX (socket 0x0550, type 4
// PEP) with NO NetBIOS name/session layer — one IPX datagram carries one whole SMB
// message, connectionless ([MS-CIFS] §2.2.1.6.4). It reuses the shared IPX datagram
// codec (core/protocol/ipx) and speaks the same Ethernet II encapsulation the server's
// core/port/ipx port uses, over a raw pcap FrameLink.
//
// Connection model: the client LOCATES the server with an NMPI Query-name (see
// DialIPXWithOpts) and addresses every SMB datagram to the node that answered, falling
// back to broadcast-and-learn only when dialled with no server name. NEGOTIATE carries
// a [SOURCE][DESTINATION] NetBIOS name trailer after the SMB message — the transport's
// only naming, since it has no session layer (proto.AppendNameTrailer). The server assigns a
// Connection ID (CID) on NEGOTIATE and stamps it into the SMB header SecurityFeatures
// field (bytes 18-19); the client echoes it on subsequent messages, which the server's
// stampConnectionless honours. This transport tracks the learned server node + CID and
// applies them transparently, so the session layer above sees a plain request→response
// Transport.

// The IPX sockets this transport addresses (0x0550 direct-hosted SMB, 0x0551 NMPI
// name service) and its own source socket (0x0552) are the shared NB-IPX socket
// numbers from core/protocol/netbios — the same values the server registers on
// (core/service/smb.DirectSMBSocket, core/service/netbios.NBIPXNameQuerySocket). They
// used to be restated here as literals.
//
// directSMBClientSocket (0x0552) is the client's own socket: the source of both the
// NMPI Query-name and every SMB datagram, and the destination the server's replies come
// back to. Golden capture spec/captures/nwlink-win98.pcap frames 14/15/16 show a real
// NWLink redirector using 0x0552 throughout while addressing 0x0551 for the locate and
// 0x0550 for SMB. Our own server echoes sockets on a reply (sendResponse swaps
// in.SrcSock/in.DstSock), so this stays compatible with ClassicStack too.
var (
	directSMBSocket       = nb.NBIPXServerSocket
	nmpiNameSocket        = nb.NBIPXNameQuerySocket
	directSMBClientSocket = nb.NBIPXClientSocket
)

// ipxLocateWindow bounds the NMPI Query-name phase before the dial gives up.
const ipxLocateWindow = 2 * time.Second

// ipxRequestTimeout bounds how long the client waits for a response datagram before
// giving up on one Send (a lost datagram over the connectionless transport). The
// session layer retries at a higher level if needed; a bounded wait avoids a hang.
const ipxRequestTimeout = 5 * time.Second

// ipxMaxResponse is the largest SMB response this transport can carry back in one IPX
// datagram over Ethernet. Direct-hosted SMB over IPX is connectionless with NO
// reassembly (one datagram = one whole SMB message, [MS-CIFS] §2.2.1.6.4), so a reply
// must fit a single Ethernet frame: 1500-byte payload − 30-byte IPX header leaves ~1470
// for the SMB message. 1400 is a conservative cap that keeps the whole frame under the
// MTU with headroom for the Ethernet/LLC encapsulation, so the server never packs a
// TRANS2/READ reply too big to transmit (the classic DOS/WfW redirectors cap likewise).
// The session bounds TRANS2 MaxDataCount and READ/WRITE sizes by this so a directory
// listing pages through FIND_NEXT2 instead of overflowing one datagram.
const ipxMaxResponse = 1400

// ipxTransport is the direct-hosted-SMB-over-IPX client transport. It owns the pcap
// FrameLink, runs a read loop demultiplexing inbound IPX/SMB datagrams to the pending
// Send, and applies the learned server node + CID to each outbound message.
type ipxTransport struct {
	fl     link.FrameLink
	srcMAC [6]byte
	srcNet [4]byte // client IPX network (0 = unknown; the server replies to our node regardless)

	// calledName / callingName are the NMPI Query-name pair. A zero calledName means
	// "no locate" (the legacy broadcast-and-learn path, kept for in-process tests).
	calledName  nb.Name
	callingName nb.Name
	foundCh     chan struct{} // closed-style signal: NMPI Name-found arrived
	foundOnce   bool

	// frameType is the Ethernet encapsulation used on OUTBOUND messages. It starts at the
	// pinned/default type and, unless pinned, is overwritten with the type learned from the
	// first frame received from the server, so the client reaches a real server bound on
	// raw-802.3 / 802.2 rather than Ethernet II (see the client/ncp transport).
	frameType       ipxport.FrameType
	frameTypePinned bool

	mu         sync.Mutex
	serverNode [6]byte // IPX node of the server (from the response's IPX header)
	serverNet  [4]byte // IPX network of the server (may be an internal net a hop away)
	serverMAC  [6]byte // Ethernet source MAC of the response frame — the L2 next hop
	haveServer bool
	cid        uint16 // server-assigned Connection ID, echoed on later messages
	// seq is the connectionless SequenceNumber stamped on the NEXT request. It starts
	// at proto.FirstSequenceNumber (1) and increments per message; see the ERRATA on
	// that constant.
	seq uint16

	// Pending-request correlation. This connectionless transport carries no per-request
	// demux of its own, so the read loop must match an inbound response to the request
	// currently in flight before delivering it — otherwise a reordered or duplicated
	// datagram (e.g. a NEGOTIATE reply arriving while SESSION_SETUP is outstanding)
	// satisfies the wrong Send. The session layer above serialises Sends, so at most one
	// request is in flight; waitCmd/waitMID name it (waiting is true while a Send waits).
	waiting bool
	waitCmd uint8
	waitMID uint16

	respCh chan []byte
	stop   chan struct{}
	closed bool
}

// RandomMAC generates a locally-administered, unicast MAC address for the client's
// virtual IPX station. The client is a distinct station ON the segment the pcap device
// bridges, NOT the host itself, so it must present its own node address rather than
// borrow the host NIC's MAC — otherwise it collides with the host's own networking
// identity and two client instances on one NIC clash. Delegates to client/link.RandomMAC,
// the shared implementation every client-ring RandomMAC now converges on; kept as a
// wrapper here since it is part of this package's public API (client/smb/nbf_e2e_test.go,
// client/smb/nbipx_e2e_test.go, test/e2e/*).
func RandomMAC() [6]byte {
	return clientlink.RandomMAC()
}

// DialIPXOpts carries optional per-Dial overrides for the direct-hosted IPX transport.
type DialIPXOpts struct {
	// CallingName overrides the MAC-derived NetBIOS name this station puts in the NMPI
	// Query-name's SourceName field. See DialNBIPXOpts.CallingName — same rationale.
	CallingName string
}

// DialIPX builds a direct-hosted-SMB-over-IPX transport over the pcap FrameLink fl in
// the default (learned) frame type. See DialIPXFrame to pin a frame type.
func DialIPX(fl link.FrameLink, srcMAC [6]byte, serverName string) (Transport, error) {
	return DialIPXFrame(fl, srcMAC, serverName, ipxport.DefaultFrameType, false)
}

// DialIPXFrame builds a direct-hosted-SMB-over-IPX transport over the pcap FrameLink fl.
// srcMAC is this virtual station's hardware address (the IPX source node): pass
// RandomMAC() for a synthetic station (the default) or a user-specified MAC to pin the
// address. frameType is the Ethernet encapsulation used on the initial broadcast; when
// pinned is false the transport LEARNS the server's frame type from its first reply (so
// it reaches a server bound on raw-802.3 / 802.2 rather than Ethernet II). The first
// request is broadcast and the server node is learned from its reply. The caller has
// opened fl with an "ipx" BPF filter.
func DialIPXFrame(fl link.FrameLink, srcMAC [6]byte, serverName string, frameType ipxport.FrameType, pinned bool) (Transport, error) {
	return DialIPXWithOpts(fl, srcMAC, serverName, frameType, pinned, DialIPXOpts{})
}

// DialIPXWithOpts is DialIPXFrame with DialIPXOpts overrides.
//
// When serverName is non-empty the dial first LOCATES the holder with an NMPI
// Query-name (0xF3) on socket 0x0551 and waits for its Name-found (0xF4), exactly as a
// real NWLink redirector does (golden capture spec/captures/nwlink-win98.pcap frames
// 14→15→16), then addresses every SMB datagram to the node that answered.
//
// This transport used to skip the locate entirely and simply BROADCAST the first SMB
// message, learning the server from whoever replied. On a segment with more than one
// direct-hosted IPX station that reaches the wrong machine: dialling WIN98-IPX-2
// learned node 00:86:b0:ae:29:6f (WIN98-1) and got ERRSRV 0x12 back, because a
// broadcast NEGOTIATE carries nothing naming its intended recipient. An empty
// serverName keeps the old broadcast-and-learn behaviour for in-process tests that
// have no NMPI responder.
func DialIPXWithOpts(fl link.FrameLink, srcMAC [6]byte, serverName string, frameType ipxport.FrameType, pinned bool, opts DialIPXOpts) (Transport, error) {
	callingName := opts.CallingName
	if callingName == "" {
		callingName = nbipxCallingName(srcMAC)
	}
	t := &ipxTransport{
		fl:              fl,
		srcMAC:          srcMAC,
		callingName:     nb.NewName(callingName, nb.NameTypeWorkstation),
		frameType:       frameType,
		frameTypePinned: pinned,
		foundCh:         make(chan struct{}),
		respCh:          make(chan []byte, 4),
		stop:            make(chan struct{}),
	}
	if serverName != "" {
		t.calledName = nb.NewName(serverName, nb.NameTypeFileServer)
	}
	go t.readLoop()
	if t.calledName != (nb.Name{}) {
		if err := t.locate(); err != nil {
			_ = t.Close()
			return nil, err
		}
	}
	return t, nil
}

// locate broadcasts NMPI Query-name for the called server until Name-found arrives or
// ipxLocateWindow elapses. Unlike NBIPX's Find-name this is NOT best-effort: without a
// located node the transport would fall back to broadcasting SMB, which is exactly the
// bug it exists to fix, so a timeout is returned rather than silently addressing the
// segment at large.
func (t *ipxTransport) locate() error {
	ipxtracef("NMPI Query-name %q", t.calledName.String())
	deadline := time.Now().Add(ipxLocateWindow)
	for attempt := 0; time.Now().Before(deadline); attempt++ {
		if err := t.sendNameQuery(); err != nil {
			return err
		}
		select {
		case <-t.foundCh:
			t.mu.Lock()
			node := t.serverNode
			t.mu.Unlock()
			ipxtracef("NMPI Name-found — server %s", macTrace(node))
			return nil
		case <-time.After(400 * time.Millisecond):
			ipxtracef("no Name-found yet, retransmitting Query-name (attempt %d)", attempt+1)
		case <-t.stop:
			return ErrTransportClosed
		}
	}
	return fmt.Errorf("smb/ipx: no NMPI Name-found for %q within %s", t.calledName.String(), ipxLocateWindow)
}

// sendNameQuery broadcasts one NMPI Query-name (0xF3) for the called server on the name
// socket (0x0551), sourced from our client socket so the holder's Name-found comes back
// to us. Mirrors golden frame 14.
func (t *ipxTransport) sendNameQuery() error {
	t.mu.Lock()
	frameType := t.frameType
	srcNet := t.srcNet
	t.mu.Unlock()

	body := nb.EncodeNMPIPacket(&nb.NMPIPacket{
		Opcode:        nb.NMPIOpNameQuery,
		NameType:      nb.NMPINameTypeMachine,
		RequestedName: t.calledName,
		SourceName:    t.callingName,
	})
	d := &ipxproto.Datagram{
		Type:    ipxproto.TypePEP,
		DstNet:  srcNet,
		DstNode: ipxproto.BroadcastNode,
		DstSock: nmpiNameSocket,
		SrcNet:  srcNet,
		SrcNode: t.srcMAC,
		SrcSock: directSMBClientSocket,
		Payload: body,
	}
	return t.writeDatagram(d, ipxproto.BroadcastNode, frameType)
}

// handleNameFound records the holder's address from an NMPI Name-found (0xF4) for our
// called name. It reports true when the datagram was name-service traffic, so the SMB
// path does not also try to parse it.
func (t *ipxTransport) handleNameFound(d *ipxproto.Datagram, srcMAC [6]byte, frameType ipxport.FrameType) bool {
	pkt, err := nb.DecodeNMPIPacket(d.Payload)
	if err != nil {
		return false
	}
	if pkt.Opcode != nb.NMPIOpNameFound || pkt.RequestedName != t.calledName {
		return true // name-service traffic, but not our answer
	}
	t.mu.Lock()
	t.serverNode = d.SrcNode
	t.serverNet = d.SrcNet
	t.serverMAC = srcMAC
	if !t.frameTypePinned {
		t.frameType = frameType
	}
	t.haveServer = true
	already := t.foundOnce
	t.foundOnce = true
	ch := t.foundCh
	t.mu.Unlock()
	if !already && ch != nil {
		close(ch)
	}
	return true
}

// Send transmits one SMB message as an IPX datagram and returns the matching response.
// The destination is the learned server node (broadcast on the first, pre-NEGOTIATE
// message); the CID the server assigned is stamped into the request header so the
// server correlates the circuit.
func (t *ipxTransport) Send(req []byte) ([]byte, error) {
	t.mu.Lock()
	// L2 (Ethernet) and L3 (IPX) destinations differ once learned: the frame goes to the
	// next-hop MAC we saw the reply from (a router's cable MAC for an internal-net server),
	// while the IPX header is addressed to the server's IPX node.
	dstMAC := ipxproto.BroadcastNode
	dstNode := ipxproto.BroadcastNode
	dstNet := t.srcNet
	if t.haveServer {
		dstMAC = t.serverMAC
		dstNode = t.serverNode
		dstNet = t.serverNet
	}
	frameType := t.frameType
	cid := t.cid
	if t.seq == 0 {
		t.seq = proto.FirstSequenceNumber
	}
	seq := t.seq
	t.seq++
	closed := t.closed
	t.mu.Unlock()
	if closed {
		return nil, ErrTransportClosed
	}

	// Stamp the connectionless SecurityFeatures words: the CID the server assigned (0
	// before NEGOTIATE completes) and this request's SequenceNumber. The sequence
	// number starts at 1 and increments per request — golden capture
	// spec/captures/nwlink-win98.pcap frame 16 shows a real NWLink redirector's
	// NEGOTIATE carrying CID 0 with SequenceNumber 1. We used to leave it zero (only
	// the CID was ever written), which a Win98 direct-hosted server rejects.
	msg := append([]byte(nil), req...)
	proto.StampConnectionless(msg, cid, seq)

	// Register this request as the one in flight so the read loop delivers only its
	// matching response (same command byte and MID). Drain any stale response left in
	// respCh from a prior timed-out Send first, so we never return a bygone reply.
	if len(msg) < proto.HeaderLen {
		return nil, fmt.Errorf("smb/ipx: request shorter than an SMB header")
	}
	reqCmd := proto.MessageCommand(msg)
	reqMID := proto.MessageMID(msg)

	// NEGOTIATE carries the [SOURCE][DESTINATION] name pair after the SMB message —
	// the only thing on this transport that ever names the machine being addressed,
	// since direct-hosted IPX has no NetBIOS session layer. See the ERRATA on
	// proto.AppendNameTrailer. Omitting it is what a Win98 server answers with
	// ERRSRV/18. Only when we located a named server: the broadcast-and-learn path
	// used by in-process tests has no called name to put in it.
	if reqCmd == proto.CommandNegotiate && t.calledName != (nb.Name{}) {
		msg = proto.AppendNameTrailer(msg, t.callingName, t.calledName)
	}
	t.mu.Lock()
	for {
		select {
		case <-t.respCh:
			continue // discard a stale queued response from a previous Send
		default:
		}
		break
	}
	t.waiting = true
	t.waitCmd = reqCmd
	t.waitMID = reqMID
	t.mu.Unlock()
	defer func() {
		t.mu.Lock()
		t.waiting = false
		t.mu.Unlock()
	}()

	d := &ipxproto.Datagram{
		Type:    ipxproto.TypePEP,
		DstNet:  dstNet,
		DstNode: dstNode,
		DstSock: directSMBSocket,
		SrcNet:  t.srcNet,
		SrcNode: t.srcMAC,
		SrcSock: directSMBClientSocket,
		Payload: msg,
	}
	if err := t.writeDatagram(d, dstMAC, frameType); err != nil {
		return nil, err
	}

	select {
	case resp := <-t.respCh:
		return resp, nil
	case <-time.After(ipxRequestTimeout):
		return nil, fmt.Errorf("smb/ipx: no response within %s", ipxRequestTimeout)
	case <-t.stop:
		return nil, ErrTransportClosed
	}
}

// MaxResponse is the datagram-safe reply cap (one IPX datagram over Ethernet, no
// reassembly), used by the session to bound TRANS2 MaxDataCount and READ/WRITE sizes.
func (t *ipxTransport) MaxResponse() int { return ipxMaxResponse }

// writeDatagram encapsulates an IPX datagram in an Ethernet frame of frameType to dstMAC
// and writes it to the link, through the same core/port/ipx framing the server port uses.
func (t *ipxTransport) writeDatagram(d *ipxproto.Datagram, dstMAC [6]byte, frameType ipxport.FrameType) error {
	ipxBytes, err := d.Encode(nil)
	if err != nil {
		return err
	}
	return t.fl.Write(frameType.Encapsulate(dstMAC, t.srcMAC, ipxBytes))
}

// readLoop reads frames, strips the Ethernet/IPX encapsulation, and delivers SMB
// RESPONSE datagrams addressed to our node+socket to the pending Send. It learns the
// server node + CID from the first response.
func (t *ipxTransport) readLoop() {
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
			return // terminal (ErrClosed or other)
		}
		payload, frameType, ok := ipxport.Strip(frame)
		if !ok {
			continue
		}
		var srcMAC [6]byte
		copy(srcMAC[:], frame[6:12]) // Ethernet source = L2 next hop to the server
		d, err := ipxproto.Decode(payload)
		if err != nil || d.Type != ipxproto.TypePEP {
			continue
		}
		if d.DstNode != t.srcMAC {
			continue // not addressed to our virtual station
		}
		// NMPI name service (the locate answer) arrives from socket 0x0551.
		if d.SrcSock == nmpiNameSocket {
			if t.calledName != (nb.Name{}) {
				t.handleNameFound(d, srcMAC, frameType)
			}
			continue
		}
		msg := d.Payload
		if !proto.HasProtocolID(msg) {
			continue
		}
		// Only SMB RESPONSES (reply bit set) on our socket are ours; ignore requests
		// (another client's) and our own echoes.
		if !proto.IsResponseMessage(msg) {
			continue
		}
		// Replies land on our client socket (the server echoes SrcSock/DstSock); accept
		// 0x0550 too, for a server that pushes on the well-known socket instead.
		if d.DstSock != directSMBClientSocket && d.DstSock != directSMBSocket {
			continue
		}

		respCmd := proto.MessageCommand(msg)
		respMID := proto.MessageMID(msg)

		t.mu.Lock()
		// Correlate against the request in flight: a response whose command byte and MID
		// do not match the pending Send is a reordered/duplicated datagram (e.g. a late
		// NEGOTIATE reply arriving during SESSION_SETUP) and must not satisfy it.
		if !t.waiting || respCmd != t.waitCmd || respMID != t.waitMID {
			t.mu.Unlock()
			continue
		}
		if !t.haveServer {
			t.serverNode = d.SrcNode
			t.serverNet = d.SrcNet
			t.serverMAC = srcMAC
			t.haveServer = true
			if !t.frameTypePinned {
				t.frameType = frameType // learn the server's encapsulation
			}
			ipxtracef("learned server node %s (mac %s, frametype %s) from first reply",
				macTrace(d.SrcNode), macTrace(srcMAC), frameType)
		}
		// Learn/refresh the CID the server stamped, so later requests echo it.
		if c := proto.ConnectionlessCID(msg); c != 0 && c != proto.ConnectionlessCIDReserved {
			t.cid = c
		}
		t.mu.Unlock()

		select {
		case t.respCh <- append([]byte(nil), msg...):
		case <-t.stop:
			return
		default:
			// No pending Send (a duplicate/late response): drop it.
		}
	}
}

// (Frame demux is provided by core/port/ipx.Strip, which additionally reports the
// detected frame type so the transport can learn the server's encapsulation. NBIPX in
// this package still uses stripIPXEncapsulation from nbipx.go.)

// Close stops the read loop and closes the link.
func (t *ipxTransport) Close() error {
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
