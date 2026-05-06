// Package netbeui is the NetBEUI-on-rawlink port. It owns its own
// read loop on a dedicated rawlink, mirroring EtherTalk's per-protocol
// pcap-handle pattern, and pushes a kernel BPF filter that admits
// only 802.2 LLC frames with DSAP/SSAP both 0xF0 and a UI control
// byte — the canonical NBF link encoding.
//
// The port handles only link-level framing: it strips the 3-byte LLC
// header from inbound frames before handing the NBF body to
// protocol/netbeui.Decode, and prepends the same header to outbound
// bytes. Source and destination MAC addresses are extracted from the
// Ethernet header and passed to the delivery callback so the NBF
// transport layer can issue directed replies. NBF protocol semantics
// live above.
package netbeui

import (
	"errors"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/netlog"
	"github.com/ObsoleteMadness/ClassicStack/port/rawlink"
	"github.com/ObsoleteMadness/ClassicStack/protocol/netbeui"
)

// llcHeader is the fixed 802.2 UI-frame header for NetBEUI: DSAP and
// SSAP both 0xF0, control byte 0x03 (UI = unnumbered information).
var llcHeader = [3]byte{0xF0, 0xF0, 0x03}

// llcOffset is the offset of the NBF body within an inbound frame:
// 14 bytes Ethernet (dst MAC + src MAC + length) + 3 bytes LLC.
const llcOffset = 14 + 3

// NetBEUIBPFFilter is the kernel-level BPF expression NetBEUI pushes to
// its rawlink. It admits 802.3 length-encoded frames (EtherType slot
// holds a length ≤ 0x05DC) whose first three LLC bytes are
// 0xF0 / 0xF0 / 0x03 — the canonical UI-frame DSAP/SSAP/control for
// NBF.
const NetBEUIBPFFilter = "ether[12:2] <= 0x05dc and " +
	"ether[14:2] = 0xf0f0 and ether[16] = 0x03"

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
}

type portImpl struct {
	link rawlink.RawLink

	mu     sync.RWMutex
	src    [6]byte
	hasSrc bool
	cb     DeliveryCallback

	stopOnce   sync.Once
	readerStop chan struct{}
	readerDone chan struct{}
}

// NewPort returns a NetBEUI port bound to link. Start must be called
// before inbound frames are delivered.
func NewPort(link rawlink.RawLink) Port {
	return &portImpl{
		link:       link,
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

func (p *portImpl) Send(dstMAC [6]byte, frame *netbeui.Frame) error {
	p.mu.RLock()
	src := p.src
	hasSrc := p.hasSrc
	p.mu.RUnlock()
	if !hasSrc {
		return ErrNoSourceMAC
	}
	body, err := frame.Encode()
	if err != nil {
		return err
	}
	// 802.3 length-encoded Ethernet: the EtherType slot carries the
	// length of "LLC header + payload" so anything ≤ 0x05DC is
	// interpreted by switches as 802.2/LLC framing.
	total := 14 + len(llcHeader) + len(body)
	out := make([]byte, total)
	copy(out[0:6], dstMAC[:])
	copy(out[6:12], src[:])
	llcLen := len(llcHeader) + len(body)
	out[12] = byte(llcLen >> 8)
	out[13] = byte(llcLen)
	copy(out[14:14+len(llcHeader)], llcHeader[:])
	copy(out[14+len(llcHeader):], body)
	return p.link.WriteFrame(out)
}

func (p *portImpl) SendBroadcast(frame *netbeui.Frame) error {
	return p.Send(netbeui.NetBIOSMulticastMAC, frame)
}

// readLoop is the single inbound reader. The kernel BPF filter has
// already discarded everything that isn't an LLC UI frame with
// DSAP/SSAP both 0xF0; the only thing left for software to do is
// strip the 14+3 byte link header and decode the NBF body.
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
		p.handleFrame(frame)
	}
}

func (p *portImpl) handleFrame(raw []byte) {
	if len(raw) < llcOffset {
		return
	}
	p.mu.RLock()
	cb := p.cb
	p.mu.RUnlock()
	if cb == nil {
		return
	}
	// Extract Ethernet MACs before stripping the link header.
	var dstMAC, srcMAC [6]byte
	copy(dstMAC[:], raw[0:6])
	copy(srcMAC[:], raw[6:12])

	body, err := netbeui.Decode(raw[llcOffset:])
	if err != nil {
		return
	}
	cb(srcMAC, dstMAC, body)
}
