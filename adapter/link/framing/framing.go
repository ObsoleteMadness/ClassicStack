package framing

import (
	"errors"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
)

// Ethernet/SNAP EtherTalk constants (Inside AppleTalk, EtherTalk Link Access
// Protocol). A DDP datagram on Ethernet is carried in an 802.3 length-typed
// frame with an IEEE 802.2 LLC header (AA AA 03) and a SNAP header whose PID
// identifies AppleTalk.
var (
	llcSNAP       = []byte{0xAA, 0xAA, 0x03}             // 802.2 LLC: SAP=AA, control=UI
	snapAppleTalk = []byte{0x08, 0x00, 0x07, 0x80, 0x9B} // SNAP OUI+PID for AppleTalk DDP
)

const (
	ethHdrLen   = 14 // dst(6) + src(6) + length(2)
	llcSnapLen  = 8  // 802.2 LLC (3) + SNAP (5)
	minDDPFrame = ethHdrLen + llcSnapLen
)

var (
	// ErrNotAppleTalk is returned (internally; surfaced as a skipped frame) when
	// an inbound frame is not an EtherTalk DDP frame (e.g. AARP, or unrelated
	// traffic the kernel filter didn't drop).
	ErrNotAppleTalk = errors.New("framing: frame is not an EtherTalk DDP datagram")
	// ErrShortFrame is returned for frames too small to hold the LLC/SNAP header.
	ErrShortFrame = errors.New("framing: ethernet frame too short for SNAP DDP")
)

// EtherTalk is a link.Framer that wraps DDP datagrams in Ethernet/SNAP and
// unwraps them. It is the M1 framing seam: encode/decode are real, but address
// resolution (AARP) and node-claim are NOT performed here — see the doc.go and
// TODO(M3) notes. As a result, outbound datagrams are sent to the broadcast MAC
// unless a static peer MAC has been supplied; inbound AARP frames are skipped.
type EtherTalk struct {
	// SrcMAC is this station's 6-byte hardware address, stamped as the Ethernet
	// source on outbound frames. If nil, a zero MAC is used (caller should set it
	// once the port owns an interface).
	SrcMAC []byte

	// BroadcastMAC overrides the destination MAC for outbound frames. M1 has no
	// AARP table, so by default every datagram goes to the AppleTalk broadcast
	// MAC (09:00:07:FF:FF:FF). TODO(M3): resolve dest network/node -> unicast MAC
	// via AARP/AMT.
	BroadcastMAC []byte
}

// appleTalkBroadcastMAC is the EtherTalk (ELAP) broadcast address.
var appleTalkBroadcastMAC = []byte{0x09, 0x00, 0x07, 0xFF, 0xFF, 0xFF}

// Framing wraps a FrameLink as a DatagramLink doing Ethernet/SNAP DDP framing.
// It satisfies link.Framer.
func (e *EtherTalk) Framing(fl link.FrameLink) (link.DatagramLink, error) {
	if fl == nil {
		return nil, errors.New("framing: nil FrameLink")
	}
	src := make([]byte, 6)
	copy(src, e.SrcMAC)
	dst := append([]byte(nil), appleTalkBroadcastMAC...)
	if len(e.BroadcastMAC) == 6 {
		copy(dst, e.BroadcastMAC)
	}
	return &datagramLink{fl: fl, srcMAC: src, dstMAC: dst}, nil
}

// Compile-time assertions.
var (
	_ link.Framer       = (*EtherTalk)(nil)
	_ link.DatagramLink = (*datagramLink)(nil)
)

type datagramLink struct {
	fl      link.FrameLink
	mu      sync.Mutex // guards the scratch encode buffer
	scratch []byte

	srcMAC []byte
	dstMAC []byte
}

// ReadDatagram reads frames until one is a valid EtherTalk DDP datagram, then
// returns the decoded ddp.Datagram. Non-AppleTalk frames (AARP, noise) are
// skipped — surfaced to the caller only as the underlying link's ErrTimeout/
// ErrClosed. TODO(M3): hand AARP frames to a resolver instead of dropping.
func (d *datagramLink) ReadDatagram() (ddp.Datagram, error) {
	for {
		frame, err := d.fl.Read()
		if err != nil {
			return ddp.Datagram{}, err
		}
		dg, err := decode(frame)
		if err != nil {
			// Not a DDP datagram (AARP/other) or malformed: skip and keep reading.
			continue
		}
		return dg, nil
	}
}

// WriteDatagram encodes dg as an Ethernet/SNAP DDP frame and writes it. M1 sends
// to the (broadcast or configured) destination MAC; per-node MAC resolution is
// TODO(M3).
func (d *datagramLink) WriteDatagram(dg ddp.Datagram) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	frame, err := encode(d.scratch[:0], d.srcMAC, d.dstMAC, dg)
	if err != nil {
		return err
	}
	d.scratch = frame // retain capacity for reuse
	return d.fl.Write(frame)
}

func (d *datagramLink) Close() error { return d.fl.Close() }

// encode builds an Ethernet/SNAP frame carrying dg into dst[:0] and returns it.
func encode(dst, srcMAC, dstMAC []byte, dg ddp.Datagram) ([]byte, error) {
	// DDP long-header bytes first, so we know the 802.3 length field.
	ddpBytes, err := dg.Encode(nil)
	if err != nil {
		return nil, err
	}
	payloadLen := llcSnapLen + len(ddpBytes) // 802.2+SNAP+DDP, the 802.3 length value

	dst = append(dst, dstMAC...)
	dst = append(dst, srcMAC...)
	dst = append(dst, byte(payloadLen>>8), byte(payloadLen)) // 802.3 length
	dst = append(dst, llcSNAP...)
	dst = append(dst, snapAppleTalk...)
	dst = append(dst, ddpBytes...)
	return dst, nil
}

// decode parses an Ethernet/SNAP EtherTalk frame into a ddp.Datagram, returning
// ErrNotAppleTalk for frames that are not AppleTalk DDP (including AARP).
func decode(frame []byte) (ddp.Datagram, error) {
	if len(frame) < minDDPFrame {
		return ddp.Datagram{}, ErrShortFrame
	}
	// Validate 802.2 LLC + SNAP AppleTalk PID at the fixed offsets.
	if !equal(frame[ethHdrLen:ethHdrLen+3], llcSNAP) {
		return ddp.Datagram{}, ErrNotAppleTalk
	}
	if !equal(frame[ethHdrLen+3:ethHdrLen+8], snapAppleTalk) {
		return ddp.Datagram{}, ErrNotAppleTalk // could be AARP (80:F3) or other SNAP PID
	}
	// 802.3 length field bounds the payload, guarding against trailing padding.
	length := int(frame[12])<<8 | int(frame[13])
	if length < llcSnapLen || ethHdrLen+length > len(frame) {
		return ddp.Datagram{}, ErrShortFrame
	}
	ddpBytes := frame[ethHdrLen+llcSnapLen : ethHdrLen+length]
	return ddp.Decode(ddpBytes)
}

func equal(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
