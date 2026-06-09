// Package netbeui is the real (M3) NetBEUI port: NBF frames over 802.2 LLC on
// Ethernet (DSAP=SSAP=0xF0). Like IPX it does not ride the DDP router — NetBEUI
// is a NetBIOS transport (§3, §11d) with its own dispatch; this port exchanges
// raw frames via the frameport base and handles the LLC/NBF encapsulation here.
//
// M3 scope is the UI-frame path (name management, datagrams, name resolution):
// decode the NBF body after the 3-byte LLC UI header and deliver it. The LLC
// Type-2 connection state machine (SABME/UA/I-frame/DISC for session data) is
// session-layer logic that lands with the NetBIOS service (M7); it is NOT in
// this port. Inbound non-UI control frames are counted and skipped.
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

const ethHdrLen = 14

// llcNetBIOS is the 802.2 LLC UI header for NetBIOS Frames: DSAP=SSAP=0xF0,
// control=0x03 (UI). Inbound, the control byte's low two bits being 11 marks a
// U-frame; 0x03 specifically is UI.
var llcNetBIOS = [3]byte{0xF0, 0xF0, 0x03}

// DeliveryCallback is invoked for each decoded inbound NBF UI frame, with the
// Ethernet source and destination MACs. It runs on the read goroutine.
type DeliveryCallback func(srcMAC, dstMAC [6]byte, frame *nbf.Frame)

// Port is the real NetBEUI port. It embeds the frameport base and adds the
// LLC/NBF UI encapsulation plus the delivery callback.
type Port struct {
	*frameport.Port

	srcMAC [6]byte
	cb     atomicCallback
}

// New builds the real NetBEUI port. frame is the Ethernet FrameLink (nil →
// inert until compose injects a device link). srcMAC is this station's hardware
// address, stamped on outbound frames. Returns (nil, nil) when disabled.
func New(m *config.Model, frame link.FrameLink, srcMAC [6]byte, logger log.Logger) (component.Component, error) {
	sec := port.SectionFromModel(m, Name)
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

// SetDeliveryCallback installs the inbound delivery callback.
func (p *Port) SetDeliveryCallback(cb DeliveryCallback) { p.cb.store(cb) }

// onFrame is the frameport FrameSink: validate the LLC UI header, decode the NBF
// body, and deliver it. Non-NetBIOS / non-UI frames are skipped.
func (p *Port) onFrame(frame link.Frame) {
	if len(frame) < ethHdrLen+3 {
		return
	}
	body := frame[ethHdrLen:]
	// Require the NetBIOS LLC DSAP/SSAP and a UI control byte (0x03). The
	// Type-2 connection frames (other control values) are M7's concern.
	if body[0] != llcNetBIOS[0] || body[1] != llcNetBIOS[1] || body[2] != llcNetBIOS[2] {
		return
	}
	nbfBody := body[3:]
	if len(nbfBody) == 0 {
		return
	}
	decoded, err := nbf.Decode(nbfBody)
	if err != nil {
		p.CountDecodeError()
		return
	}
	var dstMAC, srcMAC [6]byte
	copy(dstMAC[:], frame[0:6])
	copy(srcMAC[:], frame[6:12])
	if cb := p.cb.load(); cb != nil {
		cb(srcMAC, dstMAC, decoded)
	}
}

// Send encapsulates and transmits an NBF frame as an 802.3 LLC UI frame to
// dstMAC. The 802.3 length field covers the LLC header + NBF body.
func (p *Port) Send(dstMAC [6]byte, frame *nbf.Frame) error {
	body, err := frame.Encode()
	if err != nil {
		return err
	}
	payloadLen := len(llcNetBIOS) + len(body) // 802.3 length value
	out := make([]byte, 0, ethHdrLen+payloadLen)
	out = append(out, dstMAC[:]...)
	out = append(out, p.srcMAC[:]...)
	out = append(out, byte(payloadLen>>8), byte(payloadLen)) // 802.3 length
	out = append(out, llcNetBIOS[:]...)
	out = append(out, body...)
	return p.Port.Send(out)
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
