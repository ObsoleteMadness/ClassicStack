// Package ipx is the real (M3) IPX port: IPX datagrams over Ethernet. Unlike
// the AppleTalk ports it does not ride the DDP router — IPX has its own mini-
// router (§3), so this port exchanges raw frames via the frameport base and
// decodes/encodes the Ethernet encapsulation here.
//
// Inbound, all three legacy framings are accepted (Ethernet II 0x8137, raw
// 802.3, and 802.2 LLC with DSAP=SSAP=0xE0); outbound uses Ethernet II by
// default. Decoded datagrams are handed to an installed DeliveryCallback (the
// IPX mini-router wires this in M4).
package ipx

import (
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
	"github.com/ObsoleteMadness/ClassicStack/core/port/internal/frameport"
	ipxproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx"
)

// Name is the component/section key for the IPX port.
const Name = "IPX"

// etherTypeIPX is the Ethernet II type for IPX (0x8137). For 802.3 length-typed
// frames the type field is the length (≤ 0x05DC) and the encapsulation is told
// apart by the body's first bytes.
const etherTypeIPX = 0x8137

const ethHdrLen = 14

// llcIPX is the 802.2 LLC UI header for IPX (DSAP=SSAP=0xE0, control=0x03).
var llcIPX = [3]byte{0xE0, 0xE0, 0x03}

// DeliveryCallback is invoked for each successfully decoded inbound IPX
// datagram. It runs on the read goroutine; decode-and-hand-off, do not block.
type DeliveryCallback func(d *ipxproto.Datagram)

// Port is the real IPX port. It embeds the frameport base and adds IPX/Ethernet
// encapsulation plus the delivery callback.
type Port struct {
	*frameport.Port

	srcMAC [6]byte
	cb     atomicCallback
}

// New builds the real IPX port. frame is the Ethernet FrameLink (nil → inert
// until compose injects a device link). srcMAC is this station's hardware
// address, stamped on outbound Ethernet frames. Returns (nil, nil) when the
// section is disabled.
func New(m *config.Model, frame link.FrameLink, srcMAC [6]byte, logger log.Logger) (component.Component, error) {
	return NewInstance(port.SectionFromModel(m, Name), frame, srcMAC, logger)
}

// NewInstance builds an IPX port from an already-resolved section — the
// repeated-INSTANCE form (§M11): the compose factory resolves one instance from
// Model.Lists and hands it here, so the port names itself from the instance's
// InstanceName(). Returns (nil, nil) when the section is disabled.
func NewInstance(sec *port.Section, frame link.FrameLink, srcMAC [6]byte, logger log.Logger) (component.Component, error) {
	if !sec.IsEnabled {
		return nil, nil
	}
	p := &Port{srcMAC: srcMAC}
	open := func() (link.FrameLink, error) { return frame, nil }
	if frame == nil {
		open = func() (link.FrameLink, error) { return nil, nil }
	}
	p.Port = frameport.New(sec, open, p.onFrame, logger)
	return p, nil
}

// SetDeliveryCallback installs the inbound delivery callback. May be called
// before or after Start.
func (p *Port) SetDeliveryCallback(cb DeliveryCallback) { p.cb.store(cb) }

// onFrame is the frameport FrameSink: demux the Ethernet encapsulation, decode
// the IPX datagram, and deliver it.
func (p *Port) onFrame(frame link.Frame) {
	payload, ok := stripEncapsulation(frame)
	if !ok {
		return
	}
	d, err := ipxproto.Decode(payload)
	if err != nil {
		p.CountDecodeError()
		return
	}
	if cb := p.cb.load(); cb != nil {
		cb(d)
	}
}

// stripEncapsulation returns the IPX datagram bytes from an Ethernet frame,
// accepting Ethernet II (0x8137), raw 802.3 (0xFFFF magic), and 802.2 LLC
// (DSAP=SSAP=0xE0). The bool is false when the frame is not a recognised IPX
// encapsulation. (Ported from the legacy port/ipx handleFrame.)
func stripEncapsulation(frame link.Frame) ([]byte, bool) {
	if len(frame) < ethHdrLen {
		return nil, false
	}
	etherType := uint16(frame[12])<<8 | uint16(frame[13])
	switch {
	case etherType == etherTypeIPX:
		return frame[ethHdrLen:], true
	case etherType <= 0x05DC: // 802.3 length-typed
		if len(frame) < ethHdrLen+3 {
			return nil, false
		}
		body := frame[ethHdrLen:]
		if body[0] == 0xFF && body[1] == 0xFF {
			return body, true // raw 802.3 IPX (no checksum → 0xFFFF magic)
		}
		if body[0] == llcIPX[0] && body[1] == llcIPX[1] && body[2] == llcIPX[2] {
			return body[3:], true // 802.2 LLC UI
		}
	}
	return nil, false
}

// Send encapsulates and transmits an IPX datagram as an Ethernet II frame to
// dstMAC. The IPX dst node is NOT consulted for the Ethernet dest here — the
// caller (mini-router, M4) supplies the resolved MAC; pass the broadcast MAC
// for broadcast traffic.
func (p *Port) Send(dstMAC [6]byte, d *ipxproto.Datagram) error {
	ipxBytes, err := d.Encode(nil)
	if err != nil {
		return err
	}
	frame := make([]byte, 0, ethHdrLen+len(ipxBytes))
	frame = append(frame, dstMAC[:]...)
	frame = append(frame, p.srcMAC[:]...)
	frame = append(frame, byte(etherTypeIPX>>8), byte(etherTypeIPX&0xFF))
	frame = append(frame, ipxBytes...)
	return p.Port.Send(frame)
}

// atomicCallback is a tiny lock-protected DeliveryCallback holder (atomic.Value
// rejects nil typed funcs, so a small mutex is simpler and reflection-free).
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
