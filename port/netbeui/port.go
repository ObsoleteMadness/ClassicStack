// Package netbeui is the NetBEUI-on-rawlink port. It owns its own
// read loop on a dedicated rawlink, mirroring EtherTalk's per-protocol
// pcap-handle pattern, and pushes a kernel BPF filter that admits
// 802.2 LLC frames with NetBIOS DSAP/SSAP values.
//
// The port handles only link-level framing: it strips the Ethernet
// header and variable-length LLC header from inbound frames before
// handing the NBF body to protocol/netbeui.Decode, and prepends the
// canonical 3-byte LLC UI header to outbound bytes. Source and
// destination MAC addresses are extracted from the raw frame and
// passed to the delivery callback so the NBF transport layer can
// issue directed replies. NBF protocol semantics live above.
package netbeui

import (
	"errors"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/capture"
	"github.com/ObsoleteMadness/ClassicStack/netlog"
	"github.com/ObsoleteMadness/ClassicStack/port/rawlink"
	"github.com/ObsoleteMadness/ClassicStack/protocol/netbeui"
)

// llcHeader is the canonical 802.2 UI-frame header NetBEUI uses for
// outbound frames in this implementation: DSAP and SSAP both 0xF0,
// control byte 0x03 (UI = unnumbered information).
var llcHeader = [3]byte{0xF0, 0xF0, 0x03}

const ethernetHeaderLen = 14

// NetBEUIBPFFilter is the kernel-level BPF expression NetBEUI pushes to
// its rawlink. It admits 802.3 length-encoded frames (EtherType slot
// holds a length ≤ 0x05DC) whose LLC DSAP is 0xF0 and whose SSAP,
// ignoring the command/response bit, is 0xF0. That admits both the
// common UI frames and session traffic carried with 2-byte LLC control
// fields.
const NetBEUIBPFFilter = "ether[12:2] <= 0x05dc and " +
	"ether[14] = 0xf0 and ether[15] & 0xfe = 0xf0"

func llcPayloadOffset(raw []byte) (int, bool) {
	if len(raw) < ethernetHeaderLen+3 {
		return 0, false
	}
	if raw[14] != 0xF0 || raw[15]&0xFE != 0xF0 {
		return 0, false
	}
	control := raw[16]
	if control&0x03 == 0x03 {
		return ethernetHeaderLen + 3, true
	}
	if len(raw) < ethernetHeaderLen+4 {
		return 0, false
	}
	return ethernetHeaderLen + 4, true
}

// ErrNoSourceMAC is returned by Send when the caller has not supplied
// a source MAC for the port.
var ErrNoSourceMAC = errors.New("netbeui: source MAC not configured")

// DeliveryCallback is invoked for each successfully decoded inbound
// NBF frame. srcMAC and dstMAC are the Ethernet-level addresses
// extracted from the raw frame before the LLC header is stripped.
// The transport layer needs srcMAC for directed replies (e.g.
// NAME_RECOGNIZED → SESSION_INITIALIZE).
type DeliveryCallback func(srcMAC, dstMAC [6]byte, frame *netbeui.Frame)

// Port is the NetBEUI port surface.
type Port interface {
	// Start opens the read loop on the rawlink. It must be called
	// before any inbound frames will be delivered.
	Start() error
	// Stop closes the read loop and the underlying rawlink.
	Stop() error
	// Send transmits an NBF frame to dstMAC. The source MAC must
	// already have been configured via SetSourceMAC.
	Send(dstMAC [6]byte, frame *netbeui.Frame) error
	// SendBroadcast transmits an NBF frame to the NetBIOS multicast
	// address (03:00:00:00:00:01).
	SendBroadcast(frame *netbeui.Frame) error
	SetSourceMAC(mac [6]byte)
	SetDeliveryCallback(cb DeliveryCallback)
	// SetCaptureSink installs an optional raw-frame capture sink.
	SetCaptureSink(sink capture.Sink)
}

// llcConn tracks per-peer LLC Type-2 (802.2 extended) connection state.
type llcConn struct {
	mu     sync.Mutex
	uaSent bool  // true after UA has been sent; suppress SABME retransmit responses
	nS     uint8 // our next send sequence number (mod 128)
	nR     uint8 // expected next from remote (N(R) we put in our ACKs)
}

type portImpl struct {
	link rawlink.RawLink

	mu     sync.RWMutex
	src    [6]byte
	hasSrc bool
	cb     DeliveryCallback
	cs     capture.Sink

	connsMu sync.RWMutex
	conns   map[[6]byte]*llcConn

	stopOnce   sync.Once
	readerStop chan struct{}
	readerDone chan struct{}
}

// NewPort returns a NetBEUI port bound to link. Start must be called
// before inbound frames are delivered.
func NewPort(link rawlink.RawLink) Port {
	return &portImpl{
		link:       link,
		conns:      make(map[[6]byte]*llcConn),
		readerStop: make(chan struct{}),
		readerDone: make(chan struct{}),
	}
}

func (p *portImpl) Start() error {
	if fl, ok := p.link.(rawlink.FilterableLink); ok {
		if err := fl.SetFilter(NetBEUIBPFFilter); err != nil {
			netlog.Warn("[NetBEUI] could not set BPF filter: %v", err)
		}
	}
	go p.readLoop()
	return nil
}

func (p *portImpl) Stop() error {
	p.stopOnce.Do(func() {
		close(p.readerStop)
		<-p.readerDone
		_ = p.link.Close()
	})
	return nil
}

func (p *portImpl) SetSourceMAC(mac [6]byte) {
	p.mu.Lock()
	p.src = mac
	p.hasSrc = true
	p.mu.Unlock()
}

func (p *portImpl) SetDeliveryCallback(cb DeliveryCallback) {
	p.mu.Lock()
	p.cb = cb
	p.mu.Unlock()
}

func (p *portImpl) SetCaptureSink(sink capture.Sink) {
	p.mu.Lock()
	p.cs = sink
	p.mu.Unlock()
}

// LLC unnumbered frame control values.
const (
	llcControlSABME = 0x7F // Set Asynchronous Balanced Mode Extended (P=1)
	llcControlDISC  = 0x43 // Disconnect (P=0)
	llcControlDISCP = 0x53 // Disconnect (P=1)
	llcControlDM    = 0x0F // Disconnected Mode
	llcControlUA    = 0x63 // Unnumbered Acknowledgment (F=0)
	llcControlUAF   = 0x73 // Unnumbered Acknowledgment (F=1)
)

// sendLLCUA transmits a 3-byte LLC UA response (F=1) to dstMAC.
func (p *portImpl) sendLLCUA(dstMAC [6]byte) {
	p.mu.RLock()
	src := p.src
	hasSrc := p.hasSrc
	p.mu.RUnlock()
	if !hasSrc {
		return
	}
	const llcLen = 3
	out := make([]byte, ethernetHeaderLen+llcLen)
	copy(out[0:6], dstMAC[:])
	copy(out[6:12], src[:])
	out[12] = 0x00
	out[13] = llcLen
	out[14] = 0xF0       // DSAP
	out[15] = 0xF1       // SSAP with C/R = response
	out[16] = llcControlUAF // UA with F=1
	p.mu.RLock()
	sink := p.cs
	p.mu.RUnlock()
	capture.Write(sink, time.Now(), out)
	if err := p.link.WriteFrame(out); err != nil {
		netlog.Warn("[NetBEUI] LLC UA send error: %v", err)
	}
}

// sendLLCRR transmits a 4-byte LLC RR supervisory response (F=1) to
// dstMAC, acknowledging all I-frames up to nR-1 from the remote.
func (p *portImpl) sendLLCRR(dstMAC [6]byte, nR uint8) {
	p.mu.RLock()
	src := p.src
	hasSrc := p.hasSrc
	p.mu.RUnlock()
	if !hasSrc {
		return
	}
	const llcLen = 4
	out := make([]byte, ethernetHeaderLen+llcLen)
	copy(out[0:6], dstMAC[:])
	copy(out[6:12], src[:])
	out[12] = 0x00
	out[13] = llcLen
	out[14] = 0xF0             // DSAP
	out[15] = 0xF1             // SSAP response
	out[16] = 0x01             // RR S-frame
	out[17] = (nR << 1) | 0x01 // N(R) and F=1
	p.mu.RLock()
	sink := p.cs
	p.mu.RUnlock()
	capture.Write(sink, time.Now(), out)
	if err := p.link.WriteFrame(out); err != nil {
		netlog.Warn("[NetBEUI] LLC RR send error: %v", err)
	}
}

// sendIFrame transmits body as an LLC Type-2 I-frame to dstMAC using
// the connection's current N(S)/N(R) and then increments N(S).
func (p *portImpl) sendIFrame(dstMAC [6]byte, body []byte, conn *llcConn) error {
	p.mu.RLock()
	src := p.src
	hasSrc := p.hasSrc
	p.mu.RUnlock()
	if !hasSrc {
		return ErrNoSourceMAC
	}
	conn.mu.Lock()
	nS := conn.nS
	nR := conn.nR
	conn.nS = (conn.nS + 1) & 0x7F
	conn.mu.Unlock()
	const llcLen = 4
	total := ethernetHeaderLen + llcLen + len(body)
	out := make([]byte, total)
	copy(out[0:6], dstMAC[:])
	copy(out[6:12], src[:])
	payloadLen := llcLen + len(body)
	out[12] = byte(payloadLen >> 8)
	out[13] = byte(payloadLen)
	out[14] = 0xF0    // DSAP
	out[15] = 0xF0    // SSAP command
	out[16] = nS << 1 // I-frame ctrl0: N(S)<<1 | 0
	out[17] = nR << 1 // I-frame ctrl1: N(R)<<1 | P=0
	copy(out[18:], body)
	p.mu.RLock()
	sink := p.cs
	p.mu.RUnlock()
	capture.Write(sink, time.Now(), out)
	return p.link.WriteFrame(out)
}

// sendUI transmits body as an LLC UI (unnumbered information) frame to dstMAC.
func (p *portImpl) sendUI(dstMAC [6]byte, body []byte) error {
	p.mu.RLock()
	src := p.src
	hasSrc := p.hasSrc
	p.mu.RUnlock()
	if !hasSrc {
		return ErrNoSourceMAC
	}
	total := 14 + len(llcHeader) + len(body)
	out := make([]byte, total)
	copy(out[0:6], dstMAC[:])
	copy(out[6:12], src[:])
	llcLen := len(llcHeader) + len(body)
	out[12] = byte(llcLen >> 8)
	out[13] = byte(llcLen)
	copy(out[14:14+len(llcHeader)], llcHeader[:])
	copy(out[14+len(llcHeader):], body)
	p.mu.RLock()
	sink := p.cs
	p.mu.RUnlock()
	capture.Write(sink, time.Now(), out)
	return p.link.WriteFrame(out)
}

func (p *portImpl) Send(dstMAC [6]byte, frame *netbeui.Frame) error {
	body, err := frame.Encode()
	if err != nil {
		return err
	}
	// Session-layer commands (SESSION_INITIALIZE, DATA_*, SESSION_CONFIRM, etc.)
	// use LLC Type-2 I-framing when a connection is established. Non-session
	// frames (NAME_RECOGNIZED, ADD_NAME_RESPONSE, DATAGRAM, etc.) always use
	// UI framing regardless of connection state.
	if netbeui.IsSessionCommand(frame.Command) {
		p.connsMu.RLock()
		conn := p.conns[dstMAC]
		p.connsMu.RUnlock()
		if conn != nil {
			return p.sendIFrame(dstMAC, body, conn)
		}
	}
	return p.sendUI(dstMAC, body)
}

func (p *portImpl) SendBroadcast(frame *netbeui.Frame) error {
	return p.Send(netbeui.NetBIOSMulticastMAC, frame)
}

// readLoop is the single inbound reader. The kernel BPF filter has
// already discarded everything that isn't an 802.3 NetBIOS LLC frame;
// software then strips the variable-length LLC header and decodes the
// NBF body.
func (p *portImpl) readLoop() {
	defer close(p.readerDone)
	for {
		select {
		case <-p.readerStop:
			return
		default:
		}
		frame, err := p.link.ReadFrame()
		if err != nil {
			if err == rawlink.ErrTimeout {
				continue
			}
			netlog.Warn("[NetBEUI] read error: %v", err)
			continue
		}
		p.mu.RLock()
		sink := p.cs
		p.mu.RUnlock()
		capture.Write(sink, time.Now(), frame)
		p.handleFrame(frame)
	}
}

func (p *portImpl) handleFrame(raw []byte) {
	_, ok := llcPayloadOffset(raw)
	if !ok {
		return
	}

	var dstMAC, srcMAC [6]byte
	copy(dstMAC[:], raw[0:6])
	copy(srcMAC[:], raw[6:12])

	p.mu.RLock()
	ourMAC := p.src
	hasSrc := p.hasSrc
	cb := p.cb
	p.mu.RUnlock()

	ctrl := raw[16]

	// --- U-frames (3-byte LLC, ctrl bits 0,1 = 11) ---
	if ctrl&0x03 == 0x03 {
		switch ctrl {
		case llcControlSABME:
			// Only respond to SABMEs addressed to us.
			if !hasSrc || dstMAC != ourMAC {
				return
			}
			p.connsMu.Lock()
			conn := p.conns[srcMAC]
			if conn != nil {
				conn.mu.Lock()
				if conn.uaSent && conn.nS == 0 && conn.nR == 0 {
					// Retransmit SABME before any data was exchanged — ignore;
					// we already sent UA for this connection setup.
					conn.mu.Unlock()
					p.connsMu.Unlock()
					return
				}
				// Data has been exchanged — treat as reconnect: reset state.
				conn.uaSent = false
				conn.nS = 0
				conn.nR = 0
				conn.mu.Unlock()
			} else {
				conn = &llcConn{}
				p.conns[srcMAC] = conn
			}
			conn.mu.Lock()
			conn.uaSent = true
			conn.mu.Unlock()
			p.connsMu.Unlock()
			netlog.Debug("[NetBEUI] LLC SABME from %02X:%02X:%02X:%02X:%02X:%02X — sending UA",
				srcMAC[0], srcMAC[1], srcMAC[2], srcMAC[3], srcMAC[4], srcMAC[5])
			p.sendLLCUA(srcMAC)

		case llcControlDISC, llcControlDISCP:
			if !hasSrc || dstMAC != ourMAC {
				return
			}
			p.connsMu.Lock()
			delete(p.conns, srcMAC)
			p.connsMu.Unlock()
			netlog.Debug("[NetBEUI] LLC DISC from %02X:%02X:%02X:%02X:%02X:%02X — sending UA",
				srcMAC[0], srcMAC[1], srcMAC[2], srcMAC[3], srcMAC[4], srcMAC[5])
			p.sendLLCUA(srcMAC)

		default:
			// UI (0x03) or other U-frame: decode NBF payload if present and deliver.
			if cb == nil {
				return
			}
			nbfPayload := raw[ethernetHeaderLen+3:]
			if len(nbfPayload) == 0 {
				return
			}
			decoded, err := netbeui.Decode(nbfPayload)
			if err != nil {
				return
			}
			cb(srcMAC, dstMAC, decoded)
		}
		return
	}

	// --- I-frames and S-frames require 4-byte LLC (need at least byte 17) ---
	if len(raw) < ethernetHeaderLen+4 {
		return
	}
	ctrl1 := raw[17]

	// S-frame: ctrl bits 0,1 = 01
	if ctrl&0x03 == 0x01 {
		if !hasSrc || dstMAC != ourMAC {
			return
		}
		// RR (ctrl & 0x0F == 0x01): respond with RR F if P-bit is set.
		if ctrl&0x0F == 0x01 && ctrl1&0x01 != 0 {
			p.connsMu.RLock()
			conn := p.conns[srcMAC]
			p.connsMu.RUnlock()
			var nR uint8
			if conn != nil {
				conn.mu.Lock()
				nR = conn.nR
				conn.mu.Unlock()
			}
			p.sendLLCRR(srcMAC, nR)
		}
		return
	}

	// I-frame: ctrl bit 0 == 0
	if ctrl&0x01 == 0 {
		if !hasSrc || dstMAC != ourMAC || cb == nil {
			return
		}
		p.connsMu.RLock()
		conn := p.conns[srcMAC]
		p.connsMu.RUnlock()
		if conn == nil {
			return // I-frame outside of established connection
		}
		remoteNS := ctrl >> 1
		conn.mu.Lock()
		conn.nR = (remoteNS + 1) & 0x7F
		nR := conn.nR
		conn.mu.Unlock()
		// Acknowledge via RR if peer set the P-bit.
		if ctrl1&0x01 != 0 {
			p.sendLLCRR(srcMAC, nR)
		}
		nbfPayload := raw[ethernetHeaderLen+4:]
		if len(nbfPayload) == 0 {
			return
		}
		decoded, err := netbeui.Decode(nbfPayload)
		if err != nil {
			return
		}
		cb(srcMAC, dstMAC, decoded)
	}
}
