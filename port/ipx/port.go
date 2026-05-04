// Package ipx is the IPX-on-rawlink port. It is a stub: the wire-
// format encode/decode for the three IPX framings is wired but no
// actual IPX traffic is generated yet.
package ipx

import (
	"errors"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/port/rawlink"
	protocol "github.com/ObsoleteMadness/ClassicStack/protocol/ipx"
)

// ErrNotImplemented is returned by stub call sites that have not yet
// been filled in.
var ErrNotImplemented = errors.New("ipx: not implemented")

// Framing selects which Ethernet encapsulation the port uses on the
// wire. Real Novell installations historically used four, of which we
// implement Ethernet II today and stub the rest.
type Framing uint8

const (
	// FramingEthernetII is the modern default: EtherType 0x8137.
	FramingEthernetII Framing = iota
	// FramingRaw8023 is "Novell raw 802.3": no LLC header, identified
	// only by length-field framing and the 0xFFFF magic at the IPX
	// checksum offset.
	FramingRaw8023
	// FramingLLC is 802.2 LLC with DSAP/SSAP both 0xE0.
	FramingLLC
	// FramingSNAP is 802.2 LLC + SNAP. Defined for completeness; not
	// currently emitted by the stub port.
	FramingSNAP
)

// DeliveryCallback is invoked for each successfully decoded inbound
// IPX datagram.
type DeliveryCallback func(d *protocol.Datagram)

// Port is the IPX-port surface. It is intentionally not the same type
// as the AppleTalk port.Port — IPX rides its own router and does not
// participate in DDP socket dispatch.
type Port interface {
	Send(d *protocol.Datagram) error
	SetDeliveryCallback(cb DeliveryCallback)
	Close() error
}

// portImpl is the rawlink-backed IPX port. It registers three
// FrameConsumers (one per framing) so the rawlink multiplexer can
// fan inbound frames into a single goroutine here.
type portImpl struct {
	rl      rawlink.RawLink
	framing Framing

	mu     sync.RWMutex
	cb     DeliveryCallback
	cancel []func()
}

// NewPort opens an IPX port on rl using the default Ethernet II
// framing for outbound transmit. Inbound frames are accepted in all
// four framings so the port can interoperate with mixed networks.
func NewPort(rl rawlink.RawLink) Port {
	return NewPortWithFraming(rl, FramingEthernetII)
}

// NewPortWithFraming opens an IPX port on rl with the given outbound
// framing. Inbound frames are still accepted in all four framings.
func NewPortWithFraming(rl rawlink.RawLink, framing Framing) Port {
	p := &portImpl{rl: rl, framing: framing}

	// Subscribe to each framing separately so the rawlink multiplexer
	// can route by EtherType / LLC SAP rather than handing every frame
	// to every consumer.
	subscribe := func(f rawlink.FrameFilter, h func(frame []byte)) {
		c := rawlink.RegisterConsumer(rl, f, frameConsumerFunc(h))
		p.cancel = append(p.cancel, c)
	}

	subscribe(rawlink.FrameFilter{EtherType: 0x8137}, p.onEthernetII)
	subscribe(rawlink.FrameFilter{EtherType: 0x00FF}, p.onRaw8023)
	subscribe(rawlink.FrameFilter{IsLLC: true, DSAP: 0xE0, SSAP: 0xE0}, p.onLLC)

	return p
}

func (p *portImpl) Send(d *protocol.Datagram) error {
	payload, err := d.Encode()
	if err != nil {
		return err
	}

	switch p.framing {
	case FramingEthernetII:
		return p.sendEthernetII(d, payload)
	case FramingRaw8023, FramingLLC, FramingSNAP:
		// Stub: encoding for these framings lands when the real port
		// implementation does. Refusing rather than silently sending an
		// Ethernet II frame avoids quietly corrupting a mixed network.
		return ErrNotImplemented
	default:
		return errors.New("ipx: unknown framing")
	}
}

func (p *portImpl) sendEthernetII(d *protocol.Datagram, payload []byte) error {
	frame := make([]byte, 14+len(payload))
	copy(frame[0:6], d.DstNode[:])
	copy(frame[6:12], d.SrcNode[:])
	frame[12] = 0x81
	frame[13] = 0x37
	copy(frame[14:], payload)
	return p.rl.WriteFrame(frame)
}

func (p *portImpl) SetDeliveryCallback(cb DeliveryCallback) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cb = cb
}

func (p *portImpl) Close() error {
	p.mu.Lock()
	cancels := p.cancel
	p.cancel = nil
	p.mu.Unlock()
	for _, c := range cancels {
		c()
	}
	return nil
}

// onEthernetII handles frames matched by EtherType 0x8137: payload
// starts at offset 14.
func (p *portImpl) onEthernetII(frame []byte) {
	if len(frame) < 14 {
		return
	}
	p.deliver(frame[14:])
}

// onRaw8023 handles "Novell raw 802.3" frames matched by length-field
// EtherType 0x00FF: payload starts at offset 14, identified by the
// 0xFFFF "checksum" at the start of the IPX header.
func (p *portImpl) onRaw8023(frame []byte) {
	if len(frame) < 14+2 {
		return
	}
	body := frame[14:]
	// Raw-802.3 IPX always has 0xFFFF at the checksum offset; LLC
	// frames whose length field happens to fall into this filter
	// would not, so bail rather than mis-decode.
	if body[0] != 0xFF || body[1] != 0xFF {
		return
	}
	p.deliver(body)
}

// onLLC handles 802.2 LLC frames with DSAP=SSAP=0xE0. The IPX header
// follows a 3-byte LLC header (DSAP, SSAP, control).
func (p *portImpl) onLLC(frame []byte) {
	const llcOffset = 14 + 3
	if len(frame) < llcOffset {
		return
	}
	p.deliver(frame[llcOffset:])
}

func (p *portImpl) deliver(payload []byte) {
	p.mu.RLock()
	cb := p.cb
	p.mu.RUnlock()
	if cb == nil {
		return
	}
	d, err := protocol.Decode(payload)
	if err != nil {
		return
	}
	cb(d)
}

// frameConsumerFunc adapts a plain function to the rawlink.FrameConsumer
// interface so each framing handler can be a method value on portImpl.
type frameConsumerFunc func(frame []byte)

// OnFrame implements rawlink.FrameConsumer.
func (f frameConsumerFunc) OnFrame(frame []byte) { f(frame) }
