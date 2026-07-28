// Package ipx is the real (M3) IPX port: IPX datagrams over Ethernet. Unlike
// the AppleTalk ports it does not ride the DDP router — IPX has its own mini-
// router (§3), so this port exchanges raw frames via the frameport base and
// decodes/encodes the Ethernet encapsulation here.
//
// Inbound, all three legacy framings are accepted (Ethernet II 0x8137, raw
// 802.3, and 802.2 LLC with DSAP=SSAP=0xE0) regardless of configuration. Outbound
// uses the section's ipx_frame_type (Ethernet II by default, for MacIPX
// compatibility — see frametype.go). Decoded datagrams are handed to an installed
// DeliveryCallback (the IPX mini-router wires this in M4).
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

// BPFFilter is the kernel capture filter for the IPX port: libpcap's "ipx" primitive,
// which matches all three legacy IPX framings (Ethernet II 0x8137, raw 802.3, and 802.2
// LLC with DSAP=SSAP=0xE0). Applied at the pcap handle so the read loop is not fed the
// AppleTalk/IPv4/etc. background a promiscuous handle would otherwise surface. Each NIC
// transport owns its filter (§ ports-open-their-own-filters); this is IPX's.
const BPFFilter = "ipx"

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

	srcMAC     [6]byte
	frameType  FrameType   // default outbound encapsulation (§ ipx_frame_type)
	frameTypes []FrameType // every advertised encapsulation (§ ipx_frame_types)
	cb         atomicCallback

	// learned maps a peer MAC to the frame type its last inbound frame used, so a unicast
	// reply is sent in the same framing the request arrived in — the multi-frame-type
	// behaviour of a real NetWare server. Broadcast/unlearned peers use frameType.
	learnMu sync.Mutex
	learned map[[6]byte]FrameType
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
// InstanceName(). frame is a SINGLE pre-opened link (nil → inert); for a
// restartable device link the compose factory uses NewInstanceFromOpener instead.
// Returns (nil, nil) when the section is disabled.
func NewInstance(sec *port.Section, frame link.FrameLink, srcMAC [6]byte, logger log.Logger) (component.Component, error) {
	open := func() (link.FrameLink, error) { return frame, nil }
	if frame == nil {
		open = func() (link.FrameLink, error) { return nil, nil }
	}
	return NewInstanceFromOpener(sec, open, srcMAC, logger)
}

// NewInstanceFromOpener builds an IPX port whose link is opened by a per-Start
// factory (§M11.c device-link injection): the compose factory injects the NIC opener
// resolved from the port's interface kind, so the port opens a FRESH link on every
// Start and therefore survives a UI Stop→Start (a closed pcap handle is terminal —
// see the pcap restart lifecycle). A nil opener (or one returning nil,nil) yields the
// inert-but-configured form. Returns (nil, nil) when the section is disabled.
func NewInstanceFromOpener(sec *port.Section, open func() (link.FrameLink, error), srcMAC [6]byte, logger log.Logger) (component.Component, error) {
	if !sec.IsEnabled {
		return nil, nil
	}
	if open == nil {
		open = func() (link.FrameLink, error) { return nil, nil }
	}
	ft, err := ParseFrameType(sec.IPXFrameType)
	if err != nil {
		return nil, err
	}
	// The advertised frame-type set is the explicit ipx_frame_types list (each parsed),
	// or — when unset — just the single default/ipx_frame_type. SAP/RIP advertisers read
	// FrameTypes so a broadcast reaches clients on every configured framing.
	frameTypes := []FrameType{ft}
	if len(sec.IPXFrameTypes) > 0 {
		frameTypes = frameTypes[:0]
		for _, s := range sec.IPXFrameTypes {
			f, err := ParseFrameType(s)
			if err != nil {
				return nil, err
			}
			frameTypes = append(frameTypes, f)
		}
	}
	p := &Port{srcMAC: srcMAC, frameType: ft, frameTypes: frameTypes, learned: make(map[[6]byte]FrameType)}
	p.Port = frameport.New(sec, open, p.onFrame, logger)
	return p, nil
}

// FrameTypes returns every Ethernet encapsulation the port advertises on (the parsed
// ipx_frame_types list, or the single default frame type). SAP/RIP advertisers emit one
// broadcast per returned frame type so a client bound to any framing discovers the server.
func (p *Port) FrameTypes() []FrameType { return p.frameTypes }

// SetDeliveryCallback installs the inbound delivery callback. May be called
// before or after Start.
func (p *Port) SetDeliveryCallback(cb DeliveryCallback) { p.cb.store(cb) }

// SrcMAC returns the station hardware address used as the Ethernet source.
func (p *Port) SrcMAC() [6]byte { return p.srcMAC }

// onFrame is the frameport FrameSink: demux the Ethernet encapsulation, decode
// the IPX datagram, remember the framing this source used, and deliver it.
func (p *Port) onFrame(frame link.Frame) {
	payload, frameType, ok := Strip(frame)
	if !ok {
		return
	}
	d, err := ipxproto.Decode(payload)
	if err != nil {
		p.CountDecodeError()
		return
	}
	// Remember the frame type this peer speaks so a unicast reply mirrors it — the
	// multi-frame-type behaviour of a real NetWare server. Keyed by the Ethernet source
	// MAC (the L2 identity a reply is addressed to), which for Ethernet IPX equals the
	// source node.
	if len(frame) >= 12 {
		var src [6]byte
		copy(src[:], frame[6:12])
		p.learnMu.Lock()
		p.learned[src] = frameType
		p.learnMu.Unlock()
	}
	if cb := p.cb.load(); cb != nil {
		cb(d)
	}
}

// broadcastMAC is the all-ones Ethernet destination a broadcast IPX datagram targets.
var broadcastMAC = [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}

// Send encapsulates and transmits an IPX datagram to dstMAC. A UNICAST is framed in the
// frame type this peer last spoke to us in (learned per source MAC on inbound), so a
// reply mirrors the request's framing — the multi-frame-type behaviour of a real NetWare
// server; an unheard peer takes the configured default. A BROADCAST (all-ones dest, e.g.
// a SAP/RIP advert) is emitted ONCE PER advertised frame type (ipx_frame_types), so
// clients bound to any framing receive it. The IPX dst node is NOT consulted for the
// Ethernet dest here — the caller (mini-router) supplies the resolved MAC.
func (p *Port) Send(dstMAC [6]byte, d *ipxproto.Datagram) error {
	ipxBytes, err := d.Encode(nil)
	if err != nil {
		return err
	}
	if dstMAC == broadcastMAC {
		var firstErr error
		for _, ft := range p.frameTypes {
			if err := p.Port.Send(ft.encapsulate(dstMAC, p.srcMAC, ipxBytes)); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		return firstErr
	}
	return p.Port.Send(p.replyFrameType(dstMAC).encapsulate(dstMAC, p.srcMAC, ipxBytes))
}

// replyFrameType picks the frame type for a unicast to dstMAC: the framing dstMAC last
// used inbound (learned), else the configured default. A broadcast dest (all-ones) is
// never learned, so it takes the default.
func (p *Port) replyFrameType(dstMAC [6]byte) FrameType {
	p.learnMu.Lock()
	ft, ok := p.learned[dstMAC]
	p.learnMu.Unlock()
	if ok {
		return ft
	}
	return p.frameType
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
