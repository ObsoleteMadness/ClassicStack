// Package netbeui is the NetBEUI-on-rawlink port. It rides 802.2 LLC
// directly on Ethernet (DSAP/SSAP both 0xF0) — there is no IP, no
// IPX, no AppleTalk in the stack below it.
//
// The port handles only link-level framing: it strips the 3-byte LLC
// header from inbound frames before handing the NBF body to
// protocol/netbeui.Decode, and prepends the same header to outbound
// bytes. NBF protocol semantics live above.
package netbeui

import (
	"errors"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/port/rawlink"
	"github.com/ObsoleteMadness/ClassicStack/protocol/netbeui"
)

// llcHeader is the fixed 802.2 UI-frame header for NetBEUI: DSAP and
// SSAP both 0xF0, control byte 0x03 (UI = unnumbered information).
var llcHeader = [3]byte{0xF0, 0xF0, 0x03}

// llcOffset is the offset of the NBF body within an inbound frame:
// 14 bytes Ethernet (dst MAC + src MAC + length) + 3 bytes LLC.
const llcOffset = 14 + 3

// ErrNoSourceMAC is returned by Send when the caller has not supplied
// a source MAC for the port. The stub port leaves MAC selection to
// the caller because the rawlink does not yet expose its own address.
var ErrNoSourceMAC = errors.New("netbeui: source MAC not configured")

// DeliveryCallback is invoked for each successfully decoded inbound
// NBF frame.
type DeliveryCallback func(frame *netbeui.Frame)

// Port is the NetBEUI port surface.
type Port interface {
	// Send transmits an NBF frame to dstMAC. The source MAC must
	// already have been configured via SetSourceMAC.
	Send(dstMAC [6]byte, frame *netbeui.Frame) error
	SetSourceMAC(mac [6]byte)
	SetDeliveryCallback(cb DeliveryCallback)
	Close() error
}

type portImpl struct {
	rl     rawlink.RawLink
	cancel func()

	mu  sync.RWMutex
	src [6]byte
	cb  DeliveryCallback
	hasSrc bool
}

// NewPort opens a NetBEUI port on rl. The port subscribes to inbound
// 802.2 LLC frames with DSAP=SSAP=0xF0 via the rawlink multiplexer.
func NewPort(rl rawlink.RawLink) Port {
	p := &portImpl{rl: rl}
	filter := rawlink.FrameFilter{IsLLC: true, DSAP: 0xF0, SSAP: 0xF0}
	p.cancel = rawlink.RegisterConsumer(rl, filter, p)
	return p
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
	return p.rl.WriteFrame(out)
}

func (p *portImpl) Close() error {
	if p.cancel != nil {
		p.cancel()
	}
	return nil
}

// OnFrame implements rawlink.FrameConsumer. The rawlink multiplexer
// matches by LLC DSAP/SSAP; here we strip the 14+3 byte link header
// and hand the NBF body to the protocol decoder.
func (p *portImpl) OnFrame(frame []byte) {
	if len(frame) < llcOffset {
		return
	}
	p.mu.RLock()
	cb := p.cb
	p.mu.RUnlock()
	if cb == nil {
		return
	}
	body, err := netbeui.Decode(frame[llcOffset:])
	if err != nil {
		return
	}
	cb(body)
}
