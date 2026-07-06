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
// The LLC2 state machine is intentionally minimal: it is a half-duplex relay for
// NBF, not a full LLC2 implementation (no windowing beyond one, no I-frame
// retransmit — the NBF layer above supplies its own acknowledgement). Session
// dispatch itself lives in the NetBIOS service; this port only owns the LLC
// connection state and the UI/I-frame (de)framing.
package netbeui

import (
	"sync"

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

const ethHdrLen = 14

// llcNetBIOS is the 802.2 LLC UI header for NetBIOS Frames: DSAP=SSAP=0xF0,
// control=0x03 (UI). Inbound, the control byte's low two bits being 11 marks a
// U-frame; 0x03 specifically is UI.
var llcNetBIOS = [3]byte{0xF0, 0xF0, 0x03}

// LLC unnumbered/supervisory control values used by the NBF Type-2 session
// machine (ISO 8802-2 / IBM SC30-3587 §5). Command frames carry SSAP 0xF0
// (C/R bit clear); our responses carry SSAP 0xF1 (C/R bit set).
const (
	llcCtrlSABME = 0x7F // Set Async Balanced Mode Extended, P=1
	llcCtrlDISC  = 0x43 // Disconnect, P=0
	llcCtrlDISCP = 0x53 // Disconnect, P=1
	llcCtrlUAF   = 0x73 // Unnumbered Acknowledgment, F=1
	llcCtrlRR    = 0x01 // Receive Ready S-frame (ctrl0)
	llcSSAPResp  = 0xF1 // SSAP with C/R = response
	llcSSAPCmd   = 0xF0 // SSAP with C/R = command
	llcDSAP      = 0xF0 // NetBIOS DSAP
	ethMinFrame  = 60   // pad outbound frames to the 802.3 minimum
)

// llcConn tracks per-peer LLC Type-2 (802.2 extended) connection state. The
// sequence numbers are mod-128 (extended control field). See main's port for
// the reference behaviour; this mirrors it.
type llcConn struct {
	mu     sync.Mutex
	uaSent bool  // UA already sent for the current SABME; suppress retransmit answers
	nS     uint8 // our next send sequence number N(S), mod 128
	nR     uint8 // next expected remote N(S); the N(R) we advertise in acks
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
	if body[0] != llcDSAP || body[1]&0xFE != llcDSAP {
		return
	}

	var dstMAC, srcMAC [6]byte
	copy(dstMAC[:], frame[0:6])
	copy(srcMAC[:], frame[6:12])
	ctrl := body[2]

	// --- U-frames (control low two bits = 11): 3-byte LLC ---
	if ctrl&0x03 == 0x03 {
		switch ctrl {
		case llcCtrlSABME:
			p.handleSABME(srcMAC, dstMAC)
		case llcCtrlDISC, llcCtrlDISCP:
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

	// S-frame (control low two bits = 01). We only act on RR to keep the peer's
	// send window open; respond with RR when it polls (P/F bit set).
	if ctrl&0x03 == 0x01 {
		if !p.addressedToUs(dstMAC) {
			return
		}
		if ctrl&0x0F == llcCtrlRR && ctrl1&0x01 != 0 {
			p.sendRR(srcMAC)
		}
		return
	}

	// I-frame (control low bit = 0): a session-command NBF body inside a Type-2
	// connection. Advance N(R), ack with RR if polled, then deliver.
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
		conn.nR = (remoteNS + 1) & 0x7F
		conn.mu.Unlock()
		if ctrl1&0x01 != 0 { // peer polled — acknowledge
			p.sendRR(srcMAC)
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
		conn.uaSent = true
		conn.mu.Unlock()
	} else {
		conn = &llcConn{uaSent: true}
		p.conns[srcMAC] = conn
	}
	p.connsMu.Unlock()
	p.sendUA(srcMAC)
}

// handleDISC tears down the connection and acknowledges with UA.
func (p *Port) handleDISC(srcMAC, dstMAC [6]byte) {
	if !p.addressedToUs(dstMAC) {
		return
	}
	p.connsMu.Lock()
	delete(p.conns, srcMAC)
	p.connsMu.Unlock()
	p.sendUA(srcMAC)
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
	payloadLen := len(llcNetBIOS) + len(body)
	out := make([]byte, ethHdrLen+payloadLen)
	copy(out[0:6], dstMAC[:])
	copy(out[6:12], p.srcMAC[:])
	out[12], out[13] = byte(payloadLen>>8), byte(payloadLen)
	copy(out[14:17], llcNetBIOS[:])
	copy(out[17:], body)
	return p.Port.Send(padTo(out, ethMinFrame))
}

// sendIFrame transmits body as an LLC Type-2 I-frame using the connection's
// current N(S)/N(R), then increments N(S).
func (p *Port) sendIFrame(dstMAC [6]byte, body []byte, conn *llcConn) error {
	conn.mu.Lock()
	nS, nR := conn.nS, conn.nR
	conn.nS = (conn.nS + 1) & 0x7F
	conn.mu.Unlock()

	const llcLen = 4
	payloadLen := llcLen + len(body)
	out := make([]byte, ethHdrLen+payloadLen)
	copy(out[0:6], dstMAC[:])
	copy(out[6:12], p.srcMAC[:])
	out[12], out[13] = byte(payloadLen>>8), byte(payloadLen)
	out[14] = llcDSAP
	out[15] = llcSSAPCmd
	out[16] = nS << 1 // I-frame ctrl0: N(S)<<1, low bit 0
	out[17] = nR << 1 // I-frame ctrl1: N(R)<<1, P bit 0
	copy(out[18:], body)
	return p.Port.Send(padTo(out, ethMinFrame))
}

// sendUA transmits a 3-byte LLC UA (F=1) response to dstMAC, acknowledging a
// SABME (connection open) or DISC (connection close).
func (p *Port) sendUA(dstMAC [6]byte) error {
	out := make([]byte, ethHdrLen+3)
	copy(out[0:6], dstMAC[:])
	copy(out[6:12], p.srcMAC[:])
	out[12], out[13] = 0x00, 0x03 // 802.3 length = 3 (LLC only)
	out[14] = llcDSAP
	out[15] = llcSSAPResp
	out[16] = llcCtrlUAF
	return p.Port.Send(padTo(out, ethMinFrame))
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
	out := make([]byte, ethHdrLen+4)
	copy(out[0:6], dstMAC[:])
	copy(out[6:12], p.srcMAC[:])
	out[12], out[13] = 0x00, 0x04 // 802.3 length = 4 (LLC only)
	out[14] = llcDSAP
	out[15] = llcSSAPResp
	out[16] = llcCtrlRR        // RR S-frame
	out[17] = (nR << 1) | 0x01 // N(R)<<1 | F=1
	return p.Port.Send(padTo(out, ethMinFrame))
}

// padTo zero-extends out to at least n bytes (the 802.3 minimum frame size);
// NICs and emulated adapters drop sub-60-byte runts. Only trailing bytes are
// added — the 802.3 length field already reflects the real payload size.
func padTo(out []byte, n int) []byte {
	if len(out) >= n {
		return out
	}
	return append(out, make([]byte, n-len(out))...)
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
