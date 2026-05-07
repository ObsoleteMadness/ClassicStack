// Package ipx is the IPX-on-rawlink port. It owns its own read loop on
// a dedicated rawlink, mirroring the per-protocol-handle pattern that
// EtherTalk and MacIP use. The kernel BPF filter (when supported by
// the underlying rawlink) restricts inbound traffic to the three IPX
// framings; the second-level demux (Ethernet II vs raw 802.3 vs LLC)
// happens here in software because all three sit under a single
// pcap handle but identify themselves at different byte offsets.
package ipx

import (
	"errors"
	"hash/fnv"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/capture"
	"github.com/ObsoleteMadness/ClassicStack/netlog"
	"github.com/ObsoleteMadness/ClassicStack/port/rawlink"
	protocol "github.com/ObsoleteMadness/ClassicStack/protocol/ipx"
)

// ErrNotImplemented is returned by stub call sites that have not yet
// been filled in.
var ErrNotImplemented = errors.New("ipx: not implemented")

// IPXBPFFilter is the kernel-level BPF expression IPX pushes to its
// rawlink. It admits Ethernet II 0x8137 frames, 802.3 raw IPX frames
// (length-encoded with the 0xFFFF magic at the IPX checksum offset),
// and 802.2 LLC frames with DSAP/SSAP both 0xE0 and a UI control byte.
//
// The kernel does the gross-cut filtering; deliver() does the
// second-level decision based on the bytes that survive.
const IPXBPFFilter = "ether proto 0x8137 or " +
	"(ether[12:2] <= 0x05dc and " +
	"((ether[14:2] = 0xffff) or (ether[14:2] = 0xe0e0 and ether[16] = 0x03)))"

// Framing selects which Ethernet encapsulation the port uses on the
// wire. Inbound, all four framings are accepted (SNAP currently
// stubbed). Outbound is whichever framing was passed to the
// constructor.
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
	// Start opens the read loop on the rawlink. It must be called
	// before any inbound frames will be delivered.
	Start() error
	// Stop closes the read loop and the underlying rawlink.
	Stop() error
	// Send transmits an IPX datagram in the configured outbound
	// framing.
	Send(d *protocol.Datagram) error
	// SetDeliveryCallback installs the inbound delivery callback. May
	// be called before or after Start.
	SetDeliveryCallback(cb DeliveryCallback)
	// SetCaptureSink installs an optional raw-frame capture sink.
	SetCaptureSink(sink capture.Sink)
}

// portImpl is the rawlink-backed IPX port.
type portImpl struct {
	link    rawlink.RawLink
	framing Framing

	mu sync.RWMutex
	cb DeliveryCallback
	cs capture.Sink

	dedupMu      sync.Mutex
	recentFrames map[uint64]time.Time

	stopOnce   sync.Once
	readerStop chan struct{}
	readerDone chan struct{}
}

const inboundFrameDedupWindow = 25 * time.Millisecond
const inboundFrameDedupTTL = 100 * time.Millisecond

// NewPort opens an IPX port on link using the default Ethernet II
// framing for outbound transmit. Inbound frames are accepted in all
// three documented framings.
func NewPort(link rawlink.RawLink) Port {
	return NewPortWithFraming(link, FramingEthernetII)
}

// NewPortWithFraming opens an IPX port on link with the given outbound
// framing.
func NewPortWithFraming(link rawlink.RawLink, framing Framing) Port {
	return &portImpl{
		link:         link,
		framing:      framing,
		recentFrames: make(map[uint64]time.Time),
		readerStop:   make(chan struct{}),
		readerDone:   make(chan struct{}),
	}
}

func (p *portImpl) Start() error {
	if fl, ok := p.link.(rawlink.FilterableLink); ok {
		if err := fl.SetFilter(IPXBPFFilter); err != nil {
			netlog.Warn("[IPX] could not set BPF filter: %v", err)
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
		// implementation does. Refusing rather than silently sending
		// an Ethernet II frame avoids quietly corrupting a mixed
		// network.
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
	p.mu.RLock()
	sink := p.cs
	p.mu.RUnlock()
	capture.Write(sink, time.Now(), frame)
	return p.link.WriteFrame(frame)
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

// readLoop is the single inbound reader. It demultiplexes by EtherType
// / length / LLC SAP and hands the IPX body to deliver().
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
			netlog.Warn("[IPX] read error: %v", err)
			continue
		}
		if p.isDuplicateFrame(frame) {
			continue
		}
		p.mu.RLock()
		sink := p.cs
		p.mu.RUnlock()
		capture.Write(sink, time.Now(), frame)
		p.handleFrame(frame)
	}
}

func (p *portImpl) isDuplicateFrame(frame []byte) bool {
	h := fnv.New64a()
	_, _ = h.Write(frame)
	key := h.Sum64()
	now := time.Now()

	p.dedupMu.Lock()
	defer p.dedupMu.Unlock()

	if seenAt, ok := p.recentFrames[key]; ok && now.Sub(seenAt) <= inboundFrameDedupWindow {
		return true
	}
	p.recentFrames[key] = now
	for k, ts := range p.recentFrames {
		if now.Sub(ts) > inboundFrameDedupTTL {
			delete(p.recentFrames, k)
		}
	}
	return false
}

// handleFrame inspects the Ethernet header and routes the surviving
// bytes through the matching framing decoder. The kernel filter has
// already discarded everything that doesn't match one of the three
// framings, so the discriminator here is just byte arithmetic.
func (p *portImpl) handleFrame(frame []byte) {
	if len(frame) < 14 {
		return
	}
	etherType := uint16(frame[12])<<8 | uint16(frame[13])
	switch {
	case etherType == 0x8137:
		// Ethernet II: payload starts at offset 14.
		p.deliver(frame[14:])
	case etherType <= 0x05DC:
		// 802.3 length-encoded. Either raw IPX (0xFFFF magic at the
		// payload start) or 802.2 LLC.
		if len(frame) < 14+3 {
			return
		}
		body := frame[14:]
		if body[0] == 0xFF && body[1] == 0xFF {
			p.deliver(body)
			return
		}
		if body[0] == 0xE0 && body[1] == 0xE0 && body[2] == 0x03 {
			// LLC UI frame with DSAP=SSAP=0xE0; IPX body follows the
			// 3-byte LLC header.
			p.deliver(body[3:])
			return
		}
	}
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
