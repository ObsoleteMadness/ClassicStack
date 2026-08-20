// Package netbeui is the real (M3) NetBEUI port: NBF frames over 802.2 LLC on
// Ethernet (DSAP=SSAP=0xF0). Like IPX it does not ride the DDP router — NetBEUI
// is a NetBIOS transport (§3, §11d) with its own dispatch; this port exchanges
// raw frames via the frameport base and handles the LLC/NBF encapsulation here.
//
// The port handles both LLC framing modes NBF uses on the wire:
//
//   - Type-1 (UI, 3-byte LLC, control 0x03): connectionless name management,
//     datagrams and name resolution. The NBF body follows the LLC header; decode
//     and deliver it.
//   - Type-2 (connection-oriented 802.2 extended, 4-byte LLC): the session data
//     path. DOS/WfW clients (see netbeui.pcap) establish a session by sending
//     SABME after a NAME_RECOGNIZED; the port must answer with UA, then carry the
//     session-command NBF bodies (SESSION_INITIALIZE, DATA_ONLY_LAST, …) inside
//     I-frames with N(S)/N(R) sequencing, acking peer I-frames with RR. Without
//     this the client's SABME goes unanswered and no SMB session ever forms — the
//     "MS-DOS clients cannot see the server" symptom.
//
// The LLC2 state machine is minimal but includes Type-2 error recovery
// (ISO 8802-2 §7.5): sent I-frames are retained until the peer's N(R)
// acknowledges them, a checkpoint (an RR with the P/F bit set, or a REJ)
// whose N(R) trails our V(S) retransmits the outstanding frames, and a T1
// reply timer polls the peer (RR command, P=1) when our I-frames go unacked,
// dropping the connection after llcN2 fruitless polls. Recovery matters in
// practice: NBF above cannot recover a frame lost at the LLC layer (the peer's
// session layer never saw it, so it never asks again), and netbeui.pcap shows
// an NT 3.51 client whose NIC dropped an I-frame and then checkpoint-polled
// forever against a server that only echoed RR. Session dispatch itself lives
// in the NetBIOS service; this port owns the LLC connection state, the
// UI/I-frame (de)framing, and this recovery machinery.
package netbeui

import (
	"context"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
	"github.com/ObsoleteMadness/ClassicStack/core/port/internal/frameport"
	nbf "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbeui"
)

// Name is the component/section key for the NetBEUI port.
const Name = "NetBEUI"

// BPFFilter is the kernel capture filter for the NetBEUI port: libpcap's "llc" primitive
// (matching main's NetBEUI default filter), which passes all 802.2 LLC frames. NBF rides
// 802.2 LLC with DSAP=SSAP=0xF0; "llc" also admits the IPX 0xE0 and SNAP 0xAA SAPs, but
// the onFrame path re-validates the full NetBIOS LLC UI header (0xF0/0xF0/0x03) and drops
// the rest, so the read loop still only acts on NBF. Each NIC transport owns its filter;
// this is NetBEUI's — and crucially it is NOT the EtherTalk filter, which dropped every
// NBF frame at the kernel (the reported "netbeui can't see any frames" regression).
const BPFFilter = "llc"

// The 802.2 LLC framing this port speaks — SAP values, control-field constants,
// frame geometry and the frame ENCODERS — lives in core/protocol/netbeui (llc.go),
// shared with the NBF CALLER in client/smb. Both sides used to keep private copies
// of the same literals and hand-roll the same Ethernet+LLC layout; they must agree
// byte for byte, so the definitions are single-sourced there. Only the LLC2 state
// machine below (sequence numbers, T1/N2 recovery) is this port's own.
const ethHdrLen = nbf.EthernetHeaderLen

// llcT1 is the LLC2 reply timer (ISO 8802-2 §7.5.8 T1): how long a sent
// I-frame may sit unacknowledged before we checkpoint-poll the peer with an
// RR command (P=1). A variable so tests can shorten it.
var llcT1 = time.Second

// llcN2 is the LLC2 retry budget (ISO 8802-2 N2): consecutive unanswered T1
// polls before the connection is declared dead and dropped.
const llcN2 = 8

// llcConn tracks per-peer LLC Type-2 (802.2 extended) connection state. The
// sequence numbers are mod-128 (extended control field).
type llcConn struct {
	mu     sync.Mutex
	uaSent bool  // UA already sent for the current SABME; suppress retransmit answers
	nS     uint8 // our next send sequence number N(S), mod 128 (V(S))
	nR     uint8 // next expected remote N(S); the N(R) we advertise in acks (V(R))

	// Type-2 error recovery: every sent I-frame is retained (oldest first,
	// contiguous N(S) up to nS-1) until the peer's N(R) acknowledges it, so a
	// checkpoint or REJ can retransmit what the peer missed. t1 polls the peer
	// while unacked is non-empty; t1Tries counts consecutive unanswered polls.
	unacked []unackedIFrame
	t1      *time.Timer
	t1Tries int
}

// unackedIFrame is one retained I-frame: its send sequence number and the full
// Ethernet frame as transmitted (retransmits patch ctrl1 to the current N(R)).
type unackedIFrame struct {
	nS  uint8
	raw []byte
}

// ackLocked releases retained I-frames acknowledged by the peer's N(R): the
// peer has received everything up to but not including nr. An nr outside the
// va..V(S) window is a protocol error and is ignored. Caller holds c.mu.
func (c *llcConn) ackLocked(nr uint8) {
	if len(c.unacked) == 0 {
		return
	}
	va := c.unacked[0].nS
	if (nr-va)&nbf.LLCSeqMask > (c.nS-va)&nbf.LLCSeqMask {
		return // N(R) outside the transmit window — ignore
	}
	for len(c.unacked) > 0 && c.unacked[0].nS != nr {
		c.unacked = c.unacked[1:]
	}
	if len(c.unacked) == 0 {
		c.stopT1Locked()
	}
}

// stopT1Locked cancels the reply timer and resets the retry budget. Caller
// holds c.mu.
func (c *llcConn) stopT1Locked() {
	if c.t1 != nil {
		c.t1.Stop()
		c.t1 = nil
	}
	c.t1Tries = 0
}

// retransmitCopiesLocked returns send-ready copies of every retained I-frame,
// each with ctrl1 patched to carry the connection's current N(R) (the ack
// state advances even on a retransmit). Caller holds c.mu.
func (c *llcConn) retransmitCopiesLocked() [][]byte {
	if len(c.unacked) == 0 {
		return nil
	}
	out := make([][]byte, 0, len(c.unacked))
	for _, u := range c.unacked {
		cp := make([]byte, len(u.raw))
		copy(cp, u.raw)
		cp[nbf.LLCCtrl1Offset] = c.nR << 1 // refresh N(R), P=0
		out = append(out, cp)
	}
	return out
}

// DeliveryCallback is invoked for each decoded inbound NBF UI frame, with the
// Ethernet source and destination MACs. It runs on the read goroutine.
type DeliveryCallback func(srcMAC, dstMAC [6]byte, frame *nbf.Frame)

// Port is the real NetBEUI port. It embeds the frameport base and adds the
// LLC/NBF UI encapsulation plus the delivery callback.
type Port struct {
	*frameport.Port

	srcMAC [6]byte
	cb     atomicCallback

	connsMu sync.Mutex
	conns   map[[6]byte]*llcConn
}

// New builds the real NetBEUI port. frame is the Ethernet FrameLink (nil →
// inert until compose injects a device link). srcMAC is this station's hardware
// address, stamped on outbound frames. Returns (nil, nil) when disabled.
func New(m *config.Model, frame link.FrameLink, srcMAC [6]byte, logger log.Logger) (component.Component, error) {
	return NewInstance(port.SectionFromModel(m, Name), frame, srcMAC, logger)
}

// NewInstance builds a NetBEUI port from an already-resolved section — the
// repeated-INSTANCE form (§M11): the compose factory resolves one instance from
// Model.Lists and hands it here. frame is a SINGLE pre-opened link (nil → inert); for
// a restartable device link the compose factory uses NewInstanceFromOpener instead.
// Returns (nil, nil) when disabled.
func NewInstance(sec *port.Section, frame link.FrameLink, srcMAC [6]byte, logger log.Logger) (component.Component, error) {
	open := func() (link.FrameLink, error) { return frame, nil }
	if frame == nil {
		open = func() (link.FrameLink, error) { return nil, nil }
	}
	return NewInstanceFromOpener(sec, open, srcMAC, logger)
}

// NewInstanceFromOpener builds a NetBEUI port whose link is opened by a per-Start
// factory (§M11.c device-link injection): the compose factory injects the NIC opener
// resolved from the port's interface kind, so the port opens a FRESH link on every
// Start and survives a UI Stop→Start (a closed pcap handle is terminal). A nil opener
// (or one returning nil,nil) yields the inert-but-configured form. Returns (nil, nil)
// when the section is disabled.
func NewInstanceFromOpener(sec *port.Section, open func() (link.FrameLink, error), srcMAC [6]byte, logger log.Logger) (component.Component, error) {
	if !sec.IsEnabled {
		return nil, nil
	}
	if open == nil {
		open = func() (link.FrameLink, error) { return nil, nil }
	}
	p := &Port{srcMAC: srcMAC, conns: make(map[[6]byte]*llcConn)}
	p.Port = frameport.New(sec, open, p.onFrame, logger)
	return p, nil
}

// SetDeliveryCallback installs the inbound delivery callback.
func (p *Port) SetDeliveryCallback(cb DeliveryCallback) { p.cb.store(cb) }

// onFrame is the frameport FrameSink. It classifies the 802.2 LLC frame by its
// control byte and dispatches: U-frames (SABME/DISC/UI, 3-byte LLC) and
// S/I-frames (RR / session data, 4-byte LLC). SABME/DISC drive the Type-2
// connection machine (answered with UA); UI and I-frames carry an NBF body that
// is decoded and delivered. Frames not addressed to us at the LLC-connection
// layer (SABME/DISC/RR/I to a foreign MAC) are ignored.
func (p *Port) onFrame(frame link.Frame) {
	if len(frame) < ethHdrLen+3 {
		return
	}
	body := frame[ethHdrLen:]
	// Require the NetBIOS DSAP and SSAP (ignoring the C/R bit): DSAP 0xF0,
	// SSAP 0xF0/0xF1. This admits UI, SABME, DISC, UA, RR and I-frames while
	// dropping IPX (0xE0) and SNAP (0xAA) that the "llc" BPF filter also passes.
	if !nbf.IsNetBIOSLLC(body) {
		return
	}

	var dstMAC, srcMAC [6]byte
	copy(dstMAC[:], frame[0:6])
	copy(srcMAC[:], frame[6:12])
	ctrl := body[2]

	// --- U-frames (control low two bits = 11): 3-byte LLC ---
	if ctrl&0x03 == 0x03 {
		switch ctrl {
		case nbf.LLCCtrlSABME:
			p.handleSABME(srcMAC, dstMAC)
		case nbf.LLCCtrlDISC, nbf.LLCCtrlDISCP:
			p.handleDISC(srcMAC, dstMAC)
		default: // UI (0x03) and any other U-frame: connectionless NBF body.
			p.deliverNBF(srcMAC, dstMAC, body[3:])
		}
		return
	}

	// --- S- and I-frames: 4-byte LLC (extended control) ---
	if len(body) < 4 {
		return
	}
	ctrl1 := body[3]

	// S-frame (control low two bits = 01): RR/RNR/REJ carry the peer's N(R),
	// which acknowledges our I-frames. A checkpoint (RR with P/F set — the
	// peer's recovery poll, or the F-response to our own T1 poll) or a REJ
	// whose N(R) trails our V(S) means the peer missed I-frames: retransmit
	// them (ISO 8802-2 §7.5). A command with P=1 is also answered with RR F=1.
	if ctrl&0x03 == 0x01 {
		if !p.addressedToUs(dstMAC) {
			return
		}
		conn := p.lookupConn(srcMAC)
		if conn == nil {
			return
		}
		nr := ctrl1 >> 1
		pf := ctrl1&0x01 != 0
		isCommand := body[1]&0x01 == 0
		sFunc := ctrl & nbf.LLCCtrlSFuncMask

		conn.mu.Lock()
		conn.ackLocked(nr)
		conn.t1Tries = 0 // any S-frame proves the peer is alive
		var retransmits [][]byte
		if sFunc == nbf.LLCCtrlREJ || (sFunc == nbf.LLCCtrlRR && pf) {
			retransmits = conn.retransmitCopiesLocked()
			if len(retransmits) > 0 {
				p.armT1Locked(srcMAC, conn) // recovery in flight — keep the timer running
			}
		}
		conn.mu.Unlock()

		if isCommand && pf {
			_ = p.sendRR(srcMAC) // best-effort LLC2 ack; a lost RR is re-driven by the peer's next poll
		}
		for _, raw := range retransmits {
			_ = p.Port.Send(raw)
		}
		return
	}

	// I-frame (control low bit = 0): a session-command NBF body inside a Type-2
	// connection. Advance N(R), process the piggybacked N(R) ack of our own
	// I-frames, ack with RR if polled, then deliver.
	if ctrl&0x01 == 0 {
		if !p.addressedToUs(dstMAC) {
			return
		}
		conn := p.lookupConn(srcMAC)
		if conn == nil {
			return // I-frame outside an established connection
		}
		remoteNS := ctrl >> 1
		conn.mu.Lock()
		conn.nR = (remoteNS + 1) & nbf.LLCSeqMask
		conn.ackLocked(ctrl1 >> 1)
		conn.mu.Unlock()
		if ctrl1&0x01 != 0 { // peer polled — acknowledge
			_ = p.sendRR(srcMAC) // best-effort LLC2 ack; a lost RR is re-driven by the peer's next poll
		}
		p.deliverNBF(srcMAC, dstMAC, body[4:])
	}
}

// addressedToUs reports whether dstMAC is this station's unicast MAC. SABME/DISC/
// RR/I-frames are only meaningful when addressed to us.
func (p *Port) addressedToUs(dstMAC [6]byte) bool { return dstMAC == p.srcMAC }

// lookupConn returns the LLC connection for peer mac, or nil.
func (p *Port) lookupConn(mac [6]byte) *llcConn {
	p.connsMu.Lock()
	defer p.connsMu.Unlock()
	return p.conns[mac]
}

// dropConn removes the LLC connection for peer mac and stops its reply timer.
func (p *Port) dropConn(mac [6]byte) {
	p.connsMu.Lock()
	conn := p.conns[mac]
	delete(p.conns, mac)
	p.connsMu.Unlock()
	if conn != nil {
		conn.mu.Lock()
		conn.stopT1Locked()
		conn.unacked = nil
		conn.mu.Unlock()
	}
}

// armT1Locked (re)starts the connection's reply timer. Caller holds conn.mu.
func (p *Port) armT1Locked(mac [6]byte, conn *llcConn) {
	if conn.t1 != nil {
		conn.t1.Stop()
	}
	conn.t1 = time.AfterFunc(llcT1, func() { p.onT1(mac) })
}

// onT1 fires when a sent I-frame has gone unacknowledged for llcT1: checkpoint
// the peer with an RR command (P=1) so its RR F=1 response reports which
// frames it is missing (the S-frame path then retransmits them). After llcN2
// consecutive unanswered polls the connection is dead — drop it.
func (p *Port) onT1(mac [6]byte) {
	conn := p.lookupConn(mac)
	if conn == nil {
		return
	}
	conn.mu.Lock()
	if len(conn.unacked) == 0 {
		conn.stopT1Locked()
		conn.mu.Unlock()
		return
	}
	conn.t1Tries++
	if conn.t1Tries > llcN2 {
		conn.stopT1Locked()
		conn.mu.Unlock()
		p.dropConn(mac)
		return
	}
	nR := conn.nR
	p.armT1Locked(mac, conn)
	conn.mu.Unlock()
	_ = p.sendRRPoll(mac, nR)
}

// Stop tears down every LLC connection (stopping the reply timers) before
// stopping the underlying frame port, so no timer outlives the link.
func (p *Port) Stop(ctx context.Context) error {
	p.connsMu.Lock()
	conns := p.conns
	p.conns = make(map[[6]byte]*llcConn)
	p.connsMu.Unlock()
	for _, conn := range conns {
		conn.mu.Lock()
		conn.stopT1Locked()
		conn.unacked = nil
		conn.mu.Unlock()
	}
	return p.Port.Stop(ctx)
}

// deliverNBF decodes an NBF body and hands it to the delivery callback.
func (p *Port) deliverNBF(srcMAC, dstMAC [6]byte, nbfBody []byte) {
	if len(nbfBody) == 0 {
		return
	}
	decoded, err := nbf.Decode(nbfBody)
	if err != nil {
		p.CountDecodeError()
		return
	}
	if cb := p.cb.load(); cb != nil {
		cb(srcMAC, dstMAC, decoded)
	}
}

// handleSABME answers a session-open request. A SABME retransmit that arrives
// before any data has flowed is ignored (we already UA'd); otherwise it is a
// reconnect and the sequence state is reset. Either way we answer with UA so the
// client's LLC2 connection comes up — the fix for DOS/WfW clients that could
// previously never establish an SMB session.
func (p *Port) handleSABME(srcMAC, dstMAC [6]byte) {
	if !p.addressedToUs(dstMAC) {
		return
	}
	p.connsMu.Lock()
	conn := p.conns[srcMAC]
	if conn != nil {
		conn.mu.Lock()
		if conn.uaSent && conn.nS == 0 && conn.nR == 0 {
			conn.mu.Unlock()
			p.connsMu.Unlock()
			return // duplicate SABME before any data — already acknowledged
		}
		conn.nS, conn.nR = 0, 0 // reconnect: reset sequence state
		conn.unacked = nil      // outstanding frames belong to the old connection
		conn.stopT1Locked()
		conn.uaSent = true
		conn.mu.Unlock()
	} else {
		conn = &llcConn{uaSent: true}
		p.conns[srcMAC] = conn
	}
	p.connsMu.Unlock()
	_ = p.sendUA(srcMAC) // best-effort SABME ack; the peer retransmits SABME if the UA is lost
}

// handleDISC tears down the connection (dropping its recovery state and reply
// timer) and acknowledges with UA.
func (p *Port) handleDISC(srcMAC, dstMAC [6]byte) {
	if !p.addressedToUs(dstMAC) {
		return
	}
	p.dropConn(srcMAC)
	_ = p.sendUA(srcMAC) // best-effort DISC ack; the peer retransmits DISC if the UA is lost
}

// Send transmits an NBF frame to dstMAC. Session-layer commands (0x14–0x1F) ride
// LLC Type-2 I-frames when a connection to dstMAC is established, so the peer
// sequences and acknowledges them; every other command — and any session command
// with no established connection — uses UI framing.
func (p *Port) Send(dstMAC [6]byte, frame *nbf.Frame) error {
	body, err := frame.Encode()
	if err != nil {
		return err
	}
	if nbf.IsSessionCommand(frame.Command) {
		if conn := p.lookupConn(dstMAC); conn != nil {
			return p.sendIFrame(dstMAC, body, conn)
		}
	}
	return p.sendUI(dstMAC, body)
}

// sendUI transmits body as an 802.3 LLC UI frame. The 802.3 length field covers
// the 3-byte LLC header + NBF body.
func (p *Port) sendUI(dstMAC [6]byte, body []byte) error {
	return p.Port.Send(nbf.EncodeUIFrame(dstMAC, p.srcMAC, body))
}

// sendIFrame transmits body as an LLC Type-2 I-frame using the connection's
// current N(S)/N(R), increments N(S), and retains the frame for Type-2 error
// recovery until the peer's N(R) acknowledges it (the T1 timer polls while
// anything is outstanding).
func (p *Port) sendIFrame(dstMAC [6]byte, body []byte, conn *llcConn) error {
	conn.mu.Lock()
	nS, nR := conn.nS, conn.nR
	conn.nS = (conn.nS + 1) & nbf.LLCSeqMask
	out := nbf.EncodeIFrame(dstMAC, p.srcMAC, nS, nR, false, body)
	conn.unacked = append(conn.unacked, unackedIFrame{nS: nS, raw: out})
	p.armT1Locked(dstMAC, conn)
	conn.mu.Unlock()

	return p.Port.Send(out)
}

// sendUA transmits a 3-byte LLC UA (F=1) response to dstMAC, acknowledging a
// SABME (connection open) or DISC (connection close).
func (p *Port) sendUA(dstMAC [6]byte) error {
	return p.Port.Send(nbf.EncodeUFrame(dstMAC, p.srcMAC, nbf.LLCSSAPResponse, nbf.LLCCtrlUAF))
}

// sendRR transmits a 4-byte LLC RR (Receive Ready, F=1) supervisory response to
// dstMAC, advertising the connection's N(R) so the peer's send window advances.
func (p *Port) sendRR(dstMAC [6]byte) error {
	var nR uint8
	if conn := p.lookupConn(dstMAC); conn != nil {
		conn.mu.Lock()
		nR = conn.nR
		conn.mu.Unlock()
	}
	return p.sendS(dstMAC, nbf.LLCSSAPResponse, nR, true) // N(R)<<1 | F=1
}

// sendRRPoll transmits an RR command with P=1 — the T1 checkpoint poll asking
// the peer to report its N(R) (its RR F=1 response drives retransmission).
func (p *Port) sendRRPoll(dstMAC [6]byte, nR uint8) error {
	return p.sendS(dstMAC, nbf.LLCSSAPCommand, nR, true) // N(R)<<1 | P=1
}

// sendS transmits a 4-byte LLC RR supervisory frame with the given SSAP
// (command/response), advertising nR, with the P/F bit set when pollFinal.
func (p *Port) sendS(dstMAC [6]byte, ssap, nR uint8, pollFinal bool) error {
	return p.Port.Send(nbf.EncodeSFrame(dstMAC, p.srcMAC, ssap, nbf.LLCCtrlRR, nR, pollFinal))
}

// SendBroadcast transmits frame to the NetBIOS functional multicast address.
func (p *Port) SendBroadcast(frame *nbf.Frame) error {
	return p.Send(nbf.NetBIOSMulticastMAC, frame)
}

// atomicCallback is a small lock-protected DeliveryCallback holder.
type atomicCallback struct {
	mu sync.Mutex
	cb DeliveryCallback
}

func (a *atomicCallback) store(cb DeliveryCallback) {
	a.mu.Lock()
	a.cb = cb
	a.mu.Unlock()
}

func (a *atomicCallback) load() DeliveryCallback {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cb
}
