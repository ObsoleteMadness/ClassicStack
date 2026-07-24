package smb

import (
	"fmt"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/client/trace"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	nbfproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbeui"
	nb "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbios"
)

// nbfTrace narrates the NBF caller flow at log.Trace through the shared client/trace
// sink, so `csfs -v` shows the NAME_QUERY/SABME/SESSION handshake and per-frame
// sequencing alongside every other transport's trace.
var nbfTrace = trace.Logger("nbf")

// nbftracef narrates one NBF wire-trace line at log.Trace (no-op unless -v is on).
func nbftracef(format string, args ...any) {
	if !nbfTrace.Enabled(log.Trace) {
		return
	}
	nbfTrace.Log0(log.Trace, fmt.Sprintf(format, args...))
}

// nbf.go is the SMB-over-NBF (NetBIOS Frames / NetBEUI) CLIENT transport: the CALLER
// side of the NBF session stack, the mirror of the responder engine in
// core/service/netbios/nbf.go + the LLC2 responder in core/port/netbeui. NBF rides on
// 802.2 LLC directly over Ethernet (DSAP=SSAP=0xF0), with a connection-oriented Type-2
// LLC session (SABME/UA + I-frames with N(S)/N(R) sequencing and RR acks) carrying the
// NetBIOS session-layer commands. This transport drives the whole caller flow so a raw
// pcap NIC reaches an NBF file server the same way a DOS/WfW/OS-2 redirector does.
//
// The caller flow (ground truth captures/netbeui.pcap; [IBM SC30-3587] §5.6):
//  1. NAME_QUERY locate — broadcast a NAME_QUERY for SERVER<20> with Local Session
//     No. 0 ("FIND.NAME"); the server answers NAME_RECOGNIZED, teaching us its MAC.
//  2. NAME_QUERY CALL — unicast a NAME_QUERY carrying our chosen local session number;
//     the server answers NAME_RECOGNIZED whose Data2 low byte is ITS session number.
//  3. LLC2 connect — send SABME (P=1) to the server MAC; it answers UA.
//  4. SESSION_INITIALIZE (I-frame) → the server answers SESSION_CONFIRM (I-frame),
//     completing establishment.
//  5. SMB data — each request is one DATA_ONLY_LAST (fragmented across
//     DATA_FIRST_MIDDLE if larger than the max I-field); the server's DATA frames are
//     reassembled by command (DATA_ONLY_LAST ends the message). All ride LLC2 I-frames,
//     so N(S)/N(R) sequencing and RR acknowledgment run underneath.
//
// This is a MINIMAL caller LLC2: it sequences its own I-frames and acks the server's
// with RR, but implements no T1/checkpoint retransmit recovery (the session's own
// request/response serialisation plus a bounded per-Send wait suffice for a client — a
// lost frame surfaces as a Send timeout the caller can retry, and the classic servers
// this targets do not stress a client's recovery path). The wire framing (control-byte
// values, SSAP command/response split, extended mod-128 sequence numbers) matches
// core/port/netbeui exactly so the server accepts every frame.
//
// Ring: CLIENT.

// NBF 802.2 LLC constants (mirror core/port/netbeui): DSAP/SSAP 0xF0 (NetBIOS), the
// SSAP C/R bit distinguishing command (0xF0) from response (0xF1), and the LLC control
// values the Type-2 machine uses.
const (
	nbfLLCDSAP    = 0xF0 // NetBIOS DSAP
	nbfLLCSSAPCmd = 0xF0 // SSAP with C/R = command
	nbfLLCSSAPRsp = 0xF1 // SSAP with C/R = response
	nbfLLCUI      = 0x03 // Unnumbered Information control (connectionless)
	nbfLLCSABME   = 0x7F // Set Async Balanced Mode Extended, P=1
	nbfLLCDISCP   = 0x53 // Disconnect, P=1
	nbfLLCUAF     = 0x73 // Unnumbered Acknowledgment, F=1
	nbfLLCRR      = 0x01 // Receive Ready S-frame (ctrl0)
	nbfEthHdrLen  = 14
	nbfEthMin     = 60
)

// nbfBPF is the kernel capture filter for the NBF carrier: libpcap's "llc" primitive
// (all 802.2 LLC frames), matching core/port/netbeui.BPFFilter. The read loop
// re-validates the NetBIOS DSAP/SSAP so IPX (0xE0) / SNAP (0xAA) LLC frames are dropped.
const nbfBPF = "llc"

// nbfMaxIField is the largest NBF payload one I-frame carries: the Ethernet MTU (1500)
// less the 4-byte extended LLC header, matching the server's ethernetMaxIField (1464). A
// larger SMB request is fragmented across DATA_FIRST_MIDDLE frames with DATA_ONLY_LAST
// closing it.
const nbfMaxIField = 1464

// nbfMaxResponse is the reassembling transport's response ceiling (the session's own
// negotiated buffer governs in practice).
const nbfMaxResponse = maxMessage

// nbfClientSessionNum is the local NetBIOS session number the caller assigns itself
// (Local Session No. in the CALL NAME_QUERY). Any non-zero value works; the server
// echoes it as DestNumber on session frames.
const nbfClientSessionNum uint8 = 0x01

// nbfRequestTimeout bounds each phase wait (name-recognized, UA, session-confirm, and a
// Send's data response).
const nbfRequestTimeout = 5 * time.Second

// nbfTransport is the SMB-over-NBF client transport. It owns the pcap FrameLink, drives
// the caller LLC2 + NBF session state machine, and reassembles the server's DATA
// response for each Send.
type nbfTransport struct {
	fl          link.FrameLink
	srcMAC      [6]byte
	calledName  nb.Name // the server's file-server name (\\SERVER<20>)
	callingName nb.Name // this client's workstation name

	mu         sync.Mutex
	serverMAC  [6]byte
	haveServer bool
	localNum   uint8 // our NetBIOS session number
	remoteNum  uint8 // the server's NetBIOS session number (from NAME_RECOGNIZED / SESSION_CONFIRM)

	// LLC2 caller sequence state (mod-128, extended control field). nS is our next
	// send sequence N(S); nR is the next expected server N(S) (the N(R) we advertise).
	nS uint8
	nR uint8

	// respCorrelator is the NBF-layer request id set in each request DATA frame's Response
	// Correlator field; the server echoes it in the reply's Transmit Correlator. It must be
	// NON-ZERO and increment per request — the MS redirector sends 0x0001, 0x0002, …
	// (captures/nt-98-nbf.pcap frames 214/217). Starts at 0 and is pre-incremented, so the
	// first request carries 1.
	respCorrelator uint16

	// Phase signalling and response reassembly.
	recognizedCh chan uint8    // server session number from NAME_RECOGNIZED (CALL phase)
	uaCh         chan struct{} // UA received (LLC2 up)
	rrCh         chan struct{} // RR received (the server's F-response to our poll)
	confirmCh    chan struct{} // SESSION_CONFIRM received
	frag         []byte        // accumulated DATA_FIRST_MIDDLE payload
	respCh       chan []byte   // a reassembled SMB response message

	stop   chan struct{}
	closed bool
}

// DialNBF builds an SMB-over-NBF client transport over the pcap FrameLink fl and runs
// the full caller flow to serverName (the \\SERVER label). srcMAC is the virtual
// station's MAC (RandomMAC() by default). It returns an error if any phase does not
// complete within the timeout.
func DialNBF(fl link.FrameLink, srcMAC [6]byte, serverName string) (Transport, error) {
	t := &nbfTransport{
		fl:           fl,
		srcMAC:       srcMAC,
		calledName:   nb.NewName(serverName, nb.NameTypeFileServer),
		callingName:  nb.NewName(nbipxCallingName(srcMAC), nb.NameTypeWorkstation),
		localNum:     nbfClientSessionNum,
		recognizedCh: make(chan uint8, 1),
		uaCh:         make(chan struct{}, 1),
		rrCh:         make(chan struct{}, 1),
		confirmCh:    make(chan struct{}, 1),
		respCh:       make(chan []byte, 2),
		stop:         make(chan struct{}),
	}
	go t.readLoop()
	if err := t.establish(); err != nil {
		_ = t.Close()
		return nil, err
	}
	return t, nil
}

// establish runs the caller flow: NAME_QUERY CALL (locates the server and learns its
// session number), SABME/UA (LLC2 up), then SESSION_INITIALIZE/SESSION_CONFIRM.
func (t *nbfTransport) establish() error {
	// 1+2. NAME_QUERY CALL: broadcast a NAME_QUERY carrying our local session number.
	// The server answers NAME_RECOGNIZED with its session number, and the reply's
	// Ethernet source teaches us the server MAC. Retransmit until answered.
	nbftracef("NAME_QUERY %q (CALL, local session %d)", t.calledName.String(), t.localNum)
	remoteNum, err := t.queryName()
	if err != nil {
		return err
	}
	t.mu.Lock()
	t.remoteNum = remoteNum
	server := t.serverMAC
	t.mu.Unlock()
	nbftracef("NAME_RECOGNIZED from %s, remote session %d", macTrace(server), remoteNum)

	// 3. LLC2 connect: SABME → UA.
	nbftracef("SABME → %s (LLC2 connect)", macTrace(server))
	if err := t.waitFor(t.sendSABME, t.uaCh, "UA (LLC2 connect)"); err != nil {
		return err
	}
	nbftracef("UA received (LLC2 up)")

	// 3a. RR poll → RR final. Ground truth (captures/nt-98-nbf.pcap, frames 208/209:
	// WINNT351-NBF → WIN98-NBF): immediately after UA the caller sends an RR command with
	// the Poll bit set and the server answers RR with the Final bit set, BEFORE any
	// I-frame flows. This resets the LLC2 checkpoint (N(R) exchange) so the send window is
	// open; Win98 does not process the SESSION_INITIALIZE I-frame until this poll/final
	// round has completed. (The older code went straight from UA to the I-frame, which
	// Win98 silently dropped — no SESSION_CONFIRM ever came back.)
	nbftracef("RR (poll) → server, awaiting RR (final)")
	if err := t.waitFor(t.sendRRPoll, t.rrCh, "RR final (LLC2 checkpoint)"); err != nil {
		return err
	}
	nbftracef("RR (final) received — LLC2 window open")

	// 4. SESSION_INITIALIZE → SESSION_CONFIRM. The INITIALIZE I-frame carries the Poll bit
	// (frame 210 is "I P"): the server checkpoints on it and answers with its
	// SESSION_CONFIRM I-frame.
	nbftracef("SESSION_INITIALIZE (I-frame, poll)")
	if err := t.waitFor(t.sendSessionInitialize, t.confirmCh, "SESSION_CONFIRM"); err != nil {
		return err
	}
	nbftracef("SESSION_CONFIRM received — circuit established")
	return nil
}

// macTrace renders a MAC as aa:bb:cc:dd:ee:ff for trace lines.
func macTrace(mac [6]byte) string {
	const hexd = "0123456789abcdef"
	b := make([]byte, 0, 17)
	for i, x := range mac {
		if i > 0 {
			b = append(b, ':')
		}
		b = append(b, hexd[x>>4], hexd[x&0x0f])
	}
	return string(b)
}

// queryName broadcasts a CALL NAME_QUERY (carrying our local session number) for the
// server name and waits for the NAME_RECOGNIZED reply, returning the server's session
// number. It retransmits on timeout.
func (t *nbfTransport) queryName() (uint8, error) {
	deadline := time.Now().Add(nbfRequestTimeout)
	for time.Now().Before(deadline) {
		if err := t.sendNameQuery(); err != nil {
			return 0, err
		}
		select {
		case remoteNum := <-t.recognizedCh:
			return remoteNum, nil
		case <-time.After(500 * time.Millisecond):
		case <-t.stop:
			return 0, ErrTransportClosed
		}
	}
	return 0, fmt.Errorf("smb/nbf: no NAME_RECOGNIZED for %q within %s", t.calledName.String(), nbfRequestTimeout)
}

// waitFor sends a phase frame (send) and waits on its completion channel, retransmitting
// on a sub-timeout until the overall deadline. name labels the phase for the error.
func (t *nbfTransport) waitFor(send func() error, done chan struct{}, name string) error {
	deadline := time.Now().Add(nbfRequestTimeout)
	for time.Now().Before(deadline) {
		if err := send(); err != nil {
			return err
		}
		select {
		case <-done:
			return nil
		case <-time.After(500 * time.Millisecond):
		case <-t.stop:
			return ErrTransportClosed
		}
	}
	return fmt.Errorf("smb/nbf: no %s within %s", name, nbfRequestTimeout)
}

// Send transmits one SMB message as DATA_ONLY_LAST (fragmenting across
// DATA_FIRST_MIDDLE if larger than the max I-field), all inside LLC2 I-frames, and
// returns the reassembled DATA response.
func (t *nbfTransport) Send(req []byte) ([]byte, error) {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil, ErrTransportClosed
	}
	remoteNum, localNum := t.remoteNum, t.localNum
	// Drain any stale reassembled response from a timed-out prior Send.
	for {
		select {
		case <-t.respCh:
			continue
		default:
		}
		break
	}
	t.mu.Unlock()

	if err := t.sendSMB(req, localNum, remoteNum); err != nil {
		return nil, err
	}
	select {
	case resp := <-t.respCh:
		return resp, nil
	case <-time.After(nbfRequestTimeout):
		return nil, fmt.Errorf("smb/nbf: no response within %s", nbfRequestTimeout)
	case <-t.stop:
		return nil, ErrTransportClosed
	}
}

// MaxResponse reports the reassembling transport's response ceiling.
func (t *nbfTransport) MaxResponse() int { return nbfMaxResponse }

// sendSMB frames req into DATA_FIRST_MIDDLE/DATA_ONLY_LAST NBF frames at the max I-field
// and sends each as an LLC2 I-frame. The final frame is DATA_ONLY_LAST, sent with the LLC
// Poll bit set so the server checkpoints and reliably returns its response (matching the
// MS redirector, captures/nt-98-nbf.pcap frame 214). The DATA_ONLY_LAST carries the NBF
// ACK_WITH_DATA_ALLOWED flag (Data1 0x04) so the server may acknowledge with its response
// data frame, as the redirector does.
func (t *nbfTransport) sendSMB(req []byte, localNum, remoteNum uint8) error {
	// Allocate this request's NBF Response Correlator (non-zero, incrementing). The server
	// echoes it in the reply's Transmit Correlator; a zero correlator is why Win98 DATA_ACKed
	// the request without returning a data reply.
	t.mu.Lock()
	t.respCorrelator++
	if t.respCorrelator == 0 {
		t.respCorrelator = 1 // never wrap to 0
	}
	rsp := t.respCorrelator
	t.mu.Unlock()

	if len(req) == 0 {
		f := &nbfproto.Frame{
			Command:       nbfproto.CmdDataOnlyLast,
			Data1:         nbfproto.DataAckWithDataAllowed,
			RspCorrelator: rsp,
			DestNumber:    remoteNum,
			SourceNumber:  localNum,
		}
		return t.sendSessionCtl(f, true /*poll*/)
	}
	for off := 0; off < len(req); off += nbfMaxIField {
		end := off + nbfMaxIField
		last := end >= len(req)
		if last {
			end = len(req)
		}
		cmd := nbfproto.CmdDataFirstMiddle
		var data1 uint8
		var rspCorr uint16
		if last {
			cmd = nbfproto.CmdDataOnlyLast
			data1 = nbfproto.DataAckWithDataAllowed
			rspCorr = rsp // the correlator rides the completing (LAST) frame
		}
		f := &nbfproto.Frame{
			Command:       cmd,
			Data1:         data1,
			RspCorrelator: rspCorr,
			DestNumber:    remoteNum,
			SourceNumber:  localNum,
			Payload:       req[off:end],
		}
		// Poll on the last frame only (the checkpoint that flushes the response).
		if err := t.sendSessionCtl(f, last); err != nil {
			return err
		}
	}
	return nil
}

// --- frame construction (caller direction) ---

// sendNameQuery broadcasts a CALL NAME_QUERY for the server name carrying our local
// session number in Data2's low byte (spec §5.6.8: a CALL sets Local Session No. != 0).
func (t *nbfTransport) sendNameQuery() error {
	f := &nbfproto.Frame{
		Command: nbfproto.CmdNameQuery,
		Data2:   uint16(t.localNum), // low byte = local session number (a CALL, not a locate)
	}
	copy(f.DestinationName[:], t.calledName[:])
	copy(f.SourceName[:], t.callingName[:])
	return t.sendUINBF(nbfproto.NetBIOSMulticastMAC, f)
}

// sendSABME sends a Type-2 SABME (P=1) to the server MAC to open the LLC2 connection.
func (t *nbfTransport) sendSABME() error {
	return t.sendU(nbfLLCSABME)
}

// SESSION_INITIALIZE / SESSION_CONFIRM Data1 option flags (IBM SC30-3587 Table 5-28;
// ground truth captures/nt-98-nbf.pcap frame 210, WINNT351 → WIN98). Bit layout
// B'wxxxxxxv': w = HANDLE SEND.NO.ACK supported, xxxx = Largest-Frame code (7 =
// 65535/no limit), v = NetBIOS 2.00-or-higher. The MS redirector caller sends 0x8f;
// we advertise Largest-Frame + version 2.00 but NOT SEND.NO.ACK, so the conventional
// DATA_ACK contract this transport implements is preserved.
const (
	nbfInitLargestFrameMax uint8 = 0x0E // xxxx = 111. → Largest-Frame code 7 (65535)
	nbfInitVersion2        uint8 = 0x01 // v → NetBIOS 2.00 or higher
	nbfInitFlags                 = nbfInitLargestFrameMax | nbfInitVersion2
)

// nbfMaxRecvSize is the "Maximum data receive size" advertised in SESSION_INITIALIZE /
// SESSION_CONFIRM (Data2). It bounds a single received I-field; the MS caller advertises
// 1482. We advertise our own max I-field so the server never sends a segment we cannot
// hold in one frame.
const nbfMaxRecvSize = nbfMaxIField

// sendSessionInitialize sends SESSION_INITIALIZE as an LLC2 I-frame WITH THE POLL BIT SET
// (frame 210 is "I P"): the server checkpoints on the poll and answers SESSION_CONFIRM.
// Data1 carries the option flags (Largest-Frame + version 2.00); Data2 the max receive
// size; the session numbers address the half-open circuit from the CALL NAME_RECOGNIZED.
func (t *nbfTransport) sendSessionInitialize() error {
	t.mu.Lock()
	remoteNum, localNum := t.remoteNum, t.localNum
	t.mu.Unlock()
	f := &nbfproto.Frame{
		Command:      nbfproto.CmdSessionInitialize,
		Data1:        nbfInitFlags,
		Data2:        nbfMaxRecvSize,
		DestNumber:   remoteNum,
		SourceNumber: localNum,
	}
	body, err := f.Encode()
	if err != nil {
		return err
	}
	return t.sendIFramePoll(body)
}

// sendSession encodes an NBF session-command frame and transmits it as an LLC2 I-frame.
func (t *nbfTransport) sendSession(f *nbfproto.Frame) error {
	return t.sendSessionCtl(f, false)
}

// sendSessionCtl encodes an NBF session-command frame and transmits it as an LLC2 I-frame,
// with the LLC Poll bit set when poll is true (used on the final DATA_ONLY_LAST of an SMB
// request so the server checkpoints and flushes its response).
func (t *nbfTransport) sendSessionCtl(f *nbfproto.Frame, poll bool) error {
	body, err := f.Encode()
	if err != nil {
		return err
	}
	if poll {
		return t.sendIFramePoll(body)
	}
	return t.sendIFrame(body)
}

// sendUINBF encodes an NBF non-session frame and transmits it as an 802.2 LLC UI frame.
func (t *nbfTransport) sendUINBF(dstMAC [6]byte, f *nbfproto.Frame) error {
	body, err := f.Encode()
	if err != nil {
		return err
	}
	return t.sendUIRaw(dstMAC, body)
}

// dstMACLocked returns the destination MAC for a directed frame: the learned server MAC,
// or broadcast before it is known.
func (t *nbfTransport) dstMAC() [6]byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.haveServer {
		return t.serverMAC
	}
	return nbfproto.NetBIOSMulticastMAC
}

// sendUIRaw writes an NBF body as an 802.3 LLC UI frame (3-byte LLC: DSAP 0xF0, SSAP
// 0xF0, control UI). The 802.3 length field covers the LLC header + body.
func (t *nbfTransport) sendUIRaw(dstMAC [6]byte, body []byte) error {
	payloadLen := 3 + len(body)
	out := make([]byte, nbfEthHdrLen+payloadLen)
	copy(out[0:6], dstMAC[:])
	copy(out[6:12], t.srcMAC[:])
	out[12], out[13] = byte(payloadLen>>8), byte(payloadLen)
	out[14], out[15], out[16] = nbfLLCDSAP, nbfLLCSSAPCmd, nbfLLCUI
	copy(out[17:], body)
	return t.fl.Write(nbfPad(out))
}

// sendU writes a 3-byte LLC unnumbered command frame (SABME/DISC) to the server MAC.
func (t *nbfTransport) sendU(ctrl byte) error {
	dst := t.dstMAC()
	out := make([]byte, nbfEthHdrLen+3)
	copy(out[0:6], dst[:])
	copy(out[6:12], t.srcMAC[:])
	out[12], out[13] = 0x00, 0x03
	out[14], out[15], out[16] = nbfLLCDSAP, nbfLLCSSAPCmd, ctrl
	return t.fl.Write(nbfPad(out))
}

// sendIFrame writes an NBF session body as an LLC2 I-frame with the current N(S)/N(R)
// and the Poll bit clear — the normal data path.
func (t *nbfTransport) sendIFrame(body []byte) error {
	return t.sendIFrameCtl(body, false)
}

// sendIFramePoll writes an I-frame with the Poll bit SET, prompting the peer to
// checkpoint and respond. SESSION_INITIALIZE uses this (frame 210 = "I P").
func (t *nbfTransport) sendIFramePoll(body []byte) error {
	return t.sendIFrameCtl(body, true)
}

// sendIFrameCtl writes an NBF session body as an LLC2 I-frame with the current N(S)/N(R),
// advancing N(S). ctrl0 = N(S)<<1 (low bit 0 marks an I-frame); ctrl1 = N(R)<<1 | P, so
// poll sets bit 0 of the second control byte.
func (t *nbfTransport) sendIFrameCtl(body []byte, poll bool) error {
	dst := t.dstMAC()
	const llcLen = 4
	payloadLen := llcLen + len(body)
	out := make([]byte, nbfEthHdrLen+payloadLen)
	copy(out[0:6], dst[:])
	copy(out[6:12], t.srcMAC[:])
	out[12], out[13] = byte(payloadLen>>8), byte(payloadLen)
	out[14], out[15] = nbfLLCDSAP, nbfLLCSSAPCmd
	copy(out[18:], body)

	t.mu.Lock()
	nS, nR := t.nS, t.nR
	t.nS = (t.nS + 1) & 0x7F
	t.mu.Unlock()
	out[16] = nS << 1 // I-frame ctrl0: N(S)<<1, low bit 0
	out[17] = nR << 1 // I-frame ctrl1: N(R)<<1
	if poll {
		out[17] |= 0x01 // P=1
	}
	return t.fl.Write(nbfPad(out))
}

// sendRRPoll writes a 4-byte LLC RR COMMAND with the Poll bit set, advertising our
// current N(R). The caller sends this right after UA (frame 208) to prompt the server's
// RR-final, opening the LLC2 send window before the SESSION_INITIALIZE I-frame flows.
func (t *nbfTransport) sendRRPoll() error {
	dst := t.dstMAC()
	t.mu.Lock()
	nR := t.nR
	t.mu.Unlock()
	out := make([]byte, nbfEthHdrLen+4)
	copy(out[0:6], dst[:])
	copy(out[6:12], t.srcMAC[:])
	out[12], out[13] = 0x00, 0x04
	out[14], out[15] = nbfLLCDSAP, nbfLLCSSAPCmd // SSAP command (C/R = command) for a poll
	out[16] = nbfLLCRR
	out[17] = (nR << 1) | 0x01 // N(R)<<1 | P=1
	return t.fl.Write(nbfPad(out))
}

// sendRR writes a 4-byte LLC RR COMMAND (P=0) advertising our current N(R),
// acknowledging the server's I-frames.
//
// CRITICAL: this MUST be an RR COMMAND with the Poll bit clear (SSAP 0xF0, ctrl1 =
// N(R)<<1), NOT an RR response with Final set. Ground truth captures/nt-98-nbf.pcap
// (WINNT351-NBF → WIN98-NBF, frames 214–266): the caller acks the server's data frames
// by carrying N(R) on its own command frames and, when it must ack standalone, sends an
// RR/DATA_ACK COMMAND — never an unsolicited RR RESPONSE with F=1. An RR with F=1 is a
// checkpoint RESPONSE, valid only as the answer to a command carrying Poll=1; sending one
// unsolicited desynchronises the peer's LLC2 machine. Against real Win98 this wedged the
// link right after NEGOTIATE (whose response was already in flight), so SESSION_SETUP's
// reply was never delivered — "no response within 5s". (Our own responder tolerated the
// stray F-bit, so the e2e never caught it.)
func (t *nbfTransport) sendRR() error {
	dst := t.dstMAC()
	t.mu.Lock()
	nR := t.nR
	t.mu.Unlock()
	out := make([]byte, nbfEthHdrLen+4)
	copy(out[0:6], dst[:])
	copy(out[6:12], t.srcMAC[:])
	out[12], out[13] = 0x00, 0x04
	out[14], out[15] = nbfLLCDSAP, nbfLLCSSAPCmd // SSAP command (C/R = command)
	out[16] = nbfLLCRR
	out[17] = nR << 1 // N(R)<<1, P=0 — a plain ack, not a checkpoint response
	return t.fl.Write(nbfPad(out))
}

// sendRRFinal writes a 4-byte LLC RR RESPONSE with the Final bit set, advertising our
// current N(R). This is the ANSWER to an inbound RR-command-with-Poll (the peer's
// checkpoint of us) — the sole legitimate use of the F-bit RR. Win98 polls after acking a
// request and blocks on this response before sending the reply data (live capture, frame
// 16).
func (t *nbfTransport) sendRRFinal() error {
	dst := t.dstMAC()
	t.mu.Lock()
	nR := t.nR
	t.mu.Unlock()
	out := make([]byte, nbfEthHdrLen+4)
	copy(out[0:6], dst[:])
	copy(out[6:12], t.srcMAC[:])
	out[12], out[13] = 0x00, 0x04
	out[14], out[15] = nbfLLCDSAP, nbfLLCSSAPRsp // SSAP response (C/R = response)
	out[16] = nbfLLCRR
	out[17] = (nR << 1) | 0x01 // N(R)<<1 | F=1
	return t.fl.Write(nbfPad(out))
}

// --- inbound path ---

// readLoop reads frames, validates the NetBIOS LLC header, and dispatches by LLC frame
// type: UA (LLC2 up), UI-carried NAME_RECOGNIZED (server located), and I-frame-carried
// NBF session commands (SESSION_CONFIRM, DATA). It acks inbound I-frames with RR.
func (t *nbfTransport) readLoop() {
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
		t.handleFrame(frame)
	}
}

// handleFrame classifies one inbound 802.2 LLC frame and dispatches it.
func (t *nbfTransport) handleFrame(frame []byte) {
	if len(frame) < nbfEthHdrLen+3 {
		return
	}
	body := frame[nbfEthHdrLen:]
	// NetBIOS DSAP 0xF0, SSAP 0xF0/0xF1 (ignore C/R bit); drop IPX/SNAP LLC.
	if body[0] != nbfLLCDSAP || body[1]&0xFE != nbfLLCDSAP {
		return
	}
	var dstMAC, srcMAC [6]byte
	copy(dstMAC[:], frame[0:6])
	copy(srcMAC[:], frame[6:12])
	ctrl := body[2]

	// U-frames (control low two bits = 11): 3-byte LLC. We only care about UA (the
	// answer to our SABME) and UI (connectionless NBF, e.g. NAME_RECOGNIZED).
	if ctrl&0x03 == 0x03 {
		switch ctrl {
		case nbfLLCUAF:
			t.handleUA(srcMAC, dstMAC)
		default: // UI and other U-frames: connectionless NBF body.
			t.deliverNBF(srcMAC, body[3:])
		}
		return
	}
	if len(body) < 4 {
		return
	}

	// S-frames (control low two bits = 01): RR/RNR/REJ. In the extended (mod-128) control
	// field the P/F bit and N(R) live in the SECOND control byte (body[3]); the SSAP C/R
	// bit (body[1]) distinguishes a command from a response.
	if ctrl&0x03 == 0x01 {
		if dstMAC != t.srcMAC {
			return
		}
		isCommand := body[1] == nbfLLCSSAPCmd // 0xF0 command vs 0xF1 response
		pollFinal := body[3]&0x01 != 0
		// An RR COMMAND with the Poll bit set is the peer checkpointing US: it wants an RR
		// RESPONSE carrying our N(R) with the Final bit set before it will proceed. Ground
		// truth (live /tmp/live.pcap, WIN98 → us, frame 16): after acking our NEGOTIATE
		// request Win98 polls with "RR cmd P=1" and WAITS for our RR-final before sending the
		// NEGOTIATE response. Not answering wedges the exchange — Win98 retransmits the poll
		// forever and the response never comes. This is the ONE legitimate use of an F-bit
		// RR (answering a poll); we must not send F=1 unsolicited (see sendRR).
		if isCommand && pollFinal {
			_ = t.sendRRFinal()
			return
		}
		// Otherwise it is an ack (RR command P=0) or the server's Final response to our own
		// poll (RR response F=1) — the post-UA checkpoint that gates SESSION_INITIALIZE.
		// Signal establish() so it proceeds; harmless after establishment (nothing reads).
		select {
		case t.rrCh <- struct{}{}:
		default:
		}
		return
	}

	// I-frame (control low bit = 0): a session-command NBF body inside the LLC2
	// connection. Advance N(R) to ack it, deliver, then acknowledge at the LLC layer. If
	// the inbound I-frame set the Poll bit (body[3] bit 0), the peer is checkpointing us
	// and REQUIRES an RR-response-final; otherwise a plain RR-command ack suffices.
	if ctrl&0x01 == 0 {
		if dstMAC != t.srcMAC {
			return
		}
		poll := body[3]&0x01 != 0
		remoteNS := ctrl >> 1
		t.mu.Lock()
		expected := t.nR
		if remoteNS == t.nR {
			t.nR = (remoteNS + 1) & 0x7F
		}
		t.mu.Unlock()
		// A retransmit of a frame we already consumed (the server's LLC2 T1 checkpoint
		// re-sent an I-frame whose RR ack it had not yet seen): re-ack but do NOT re-deliver,
		// or the SMB response would be doubled into the stream and every later Send would read
		// the wrong (shifted) reply. Only an in-order frame advances N(R) and is delivered.
		if remoteNS != expected {
			nbftracef("duplicate I-frame N(S)=%d (expected %d) — re-ack, drop (pcap-duplicate artifact)", remoteNS, expected)
			t.ackInbound(poll)
			return
		}
		t.deliverNBF(srcMAC, body[4:])
		t.ackInbound(poll)
	}
}

// ackInbound acknowledges an inbound I-frame at the LLC layer: an RR-response-final when
// the frame carried the Poll bit (the peer is checkpointing us), else a plain RR-command
// ack. Answering a poll with anything but an F-response, or sending F=1 unsolicited, both
// desync the peer's LLC2 machine (see sendRR / sendRRFinal).
func (t *nbfTransport) ackInbound(poll bool) {
	if poll {
		_ = t.sendRRFinal()
		return
	}
	_ = t.sendRR()
}

// handleUA signals the LLC2 connection is up (the server acknowledged our SABME).
func (t *nbfTransport) handleUA(srcMAC, dstMAC [6]byte) {
	if dstMAC != t.srcMAC {
		return
	}
	t.mu.Lock()
	if !t.haveServer {
		t.serverMAC = srcMAC
		t.haveServer = true
	}
	t.mu.Unlock()
	select {
	case t.uaCh <- struct{}{}:
	default:
	}
}

// deliverNBF decodes an NBF body (from either a UI frame — name management — or an LLC2
// I-frame — session commands) and dispatches the command to the caller state machine.
func (t *nbfTransport) deliverNBF(srcMAC [6]byte, nbfBody []byte) {
	if len(nbfBody) == 0 {
		return
	}
	f, err := nbfproto.Decode(nbfBody)
	if err != nil {
		return
	}
	switch f.Command {
	case nbfproto.CmdNameRecognized:
		t.handleNameRecognized(srcMAC, f)
	case nbfproto.CmdSessionConfirm:
		t.handleSessionConfirm(f)
	case nbfproto.CmdDataFirstMiddle:
		t.handleData(f, false)
	case nbfproto.CmdDataOnlyLast:
		t.handleData(f, true)
	case nbfproto.CmdSessionEnd:
		// server closed the session; nothing to reassemble.
	}
}

// handleNameRecognized records the server MAC and its session number from a
// NAME_RECOGNIZED reply (the CALL phase answer), signalling the establish flow. Only a
// positive reply (Data2 low byte != 0, a real session number) advances the CALL.
func (t *nbfTransport) handleNameRecognized(srcMAC [6]byte, f *nbfproto.Frame) {
	remoteNum := uint8(f.Data2 & 0xFF)
	t.mu.Lock()
	if !t.haveServer {
		t.serverMAC = srcMAC
		t.haveServer = true
	}
	t.mu.Unlock()
	if remoteNum == 0 {
		return // a FIND.NAME/locate answer with no session number: not our CALL answer
	}
	select {
	case t.recognizedCh <- remoteNum:
	default:
	}
}

// handleSessionConfirm signals that establishment completed (the server accepted our
// SESSION_INITIALIZE), learning the server's session number.
func (t *nbfTransport) handleSessionConfirm(f *nbfproto.Frame) {
	t.mu.Lock()
	if f.SourceNumber != 0 {
		t.remoteNum = f.SourceNumber
	}
	t.mu.Unlock()
	select {
	case t.confirmCh <- struct{}{}:
	default:
	}
}

// sendDataAck sends an NBF DATA_ACK (0x14) whose Transmit Correlator echoes the server's
// Response Correlator, acknowledging a server DATA frame that asked to be acked. The MS
// redirector piggybacks this ACK_INCLUDED on its NEXT request; a standalone DATA_ACK is the
// equivalent explicit form. CRITICAL against real Win98: its NEGOTIATE response carries a
// non-zero Response Correlator (0x28) and it WITHHOLDS the reply to the next request
// (SESSION_SETUP) until that response is acknowledged — without this it DATA_ACKs our
// SESSION_SETUP but never sends the SMB reply.
func (t *nbfTransport) sendDataAck(xmitCorrelator uint16) error {
	t.mu.Lock()
	remoteNum, localNum := t.remoteNum, t.localNum
	t.mu.Unlock()
	f := &nbfproto.Frame{
		Command:        nbfproto.CmdDataAck,
		XmitCorrelator: xmitCorrelator,
		DestNumber:     remoteNum,
		SourceNumber:   localNum,
	}
	return t.sendSession(f)
}

// handleData accumulates a DATA_FIRST_MIDDLE segment or, on DATA_ONLY_LAST, completes
// the SMB response message and delivers it to the pending Send.
func (t *nbfTransport) handleData(f *nbfproto.Frame, last bool) {
	t.mu.Lock()
	if !last {
		t.frag = append(t.frag, f.Payload...)
		t.mu.Unlock()
		return
	}
	var msg []byte
	if len(t.frag) > 0 {
		msg = append(t.frag, f.Payload...)
		t.frag = nil
	} else {
		msg = append([]byte(nil), f.Payload...)
	}
	t.mu.Unlock()

	// If the server's response asked to be acknowledged (non-zero Response Correlator),
	// send a DATA_ACK echoing it. Win98 withholds the NEXT request's reply until its prior
	// response is acked, so this must happen as the response arrives, not lazily.
	if f.RspCorrelator != 0 {
		_ = t.sendDataAck(f.RspCorrelator)
	}

	select {
	case t.respCh <- msg:
	case <-t.stop:
	default:
	}
}

// nbfPad zero-extends out to the 802.3 minimum frame size (NICs drop sub-60-byte runts).
func nbfPad(out []byte) []byte {
	if len(out) >= nbfEthMin {
		return out
	}
	return append(out, make([]byte, nbfEthMin-len(out))...)
}

// Close tears down the read loop and closes the link. It does not send DISC/SESSION_END:
// the SMB session layer's Close already issues TREE_DISCONNECT/LOGOFF and the server ages
// out an idle circuit; a best-effort teardown is not worth blocking Close on.
func (t *nbfTransport) Close() error {
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
