package ipx

import (
	"errors"
	"strings"
)

// FrameType is the Ethernet encapsulation an IPX port stamps on OUTBOUND frames
// (Novell's "frame type"). NetWare shipped four framings historically; this port
// implements the three that carry a bare IPX datagram over Ethernet:
//
//   - Ethernet II (DIX): EtherType 0x8137, IPX datagram follows the 14-byte
//     Ethernet header directly. This is what the Macintosh MacIPX control panel
//     speaks, so it is the default (§ default to Ethernet II for MacIPX).
//   - Raw 802.3 (Novell "Ethernet_802.3"): an 802.3 length-typed frame whose body
//     is the IPX datagram, recognised by IPX's own 0xFFFF "no checksum" magic in
//     the first two body bytes. No LLC header.
//   - 802.2 (IEEE "Ethernet_802.2"): an 802.3 length-typed frame carrying an LLC
//     UI header (DSAP=SSAP=0xE0, control=0x03) ahead of the IPX datagram.
//
// (802.2 SNAP — "Ethernet_SNAP" — is not offered; it was rare for IPX and adds
// nothing MacIPX or NetWare 3.x/4.x deployments observed here need.)
type FrameType uint8

const (
	// FrameEthernetII is Ethernet II / DIX (EtherType 0x8137). The default.
	FrameEthernetII FrameType = iota
	// FrameRaw8023 is raw 802.3 (Novell "Ethernet_802.3"), no LLC.
	FrameRaw8023
	// FrameLLC8022 is IEEE 802.2 LLC (DSAP=SSAP=0xE0, UI control 0x03).
	FrameLLC8022
)

// DefaultFrameType is the encapsulation used when the section leaves ipx_frame_type
// empty: Ethernet II, for MacIPX compatibility.
const DefaultFrameType = FrameEthernetII

// ErrBadFrameType reports an ipx_frame_type value that is not one of the
// recognised framings.
var ErrBadFrameType = errors.New("ipx: frame type must be one of ethernet_ii, 802.3, 802.2")

// ParseFrameType maps a config string to a FrameType. It is case-insensitive and
// accepts the common Novell / packet-analyser spellings. An empty string yields
// DefaultFrameType (Ethernet II) with no error.
func ParseFrameType(s string) (FrameType, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return DefaultFrameType, nil
	case "ethernet_ii", "ethernet ii", "ethernetii", "ethernet2", "eth2", "dix", "ii", "8137":
		return FrameEthernetII, nil
	case "802.3", "8023", "raw", "raw_802.3", "ethernet_802.3", "novell", "novell_ether":
		return FrameRaw8023, nil
	case "802.2", "8022", "llc", "802.2_llc", "ethernet_802.2":
		return FrameLLC8022, nil
	}
	return DefaultFrameType, ErrBadFrameType
}

// String returns the canonical config spelling of a FrameType.
func (t FrameType) String() string {
	switch t {
	case FrameRaw8023:
		return "802.3"
	case FrameLLC8022:
		return "802.2"
	default:
		return "ethernet_ii"
	}
}

// encapsulate builds a complete Ethernet frame carrying ipxBytes in this frame
// type, addressed dst←src. For the length-typed framings (raw 802.3 and 802.2
// LLC) the EtherType field is the 802.3 payload length; Ethernet II uses the
// 0x8137 IPX EtherType.
func (t FrameType) encapsulate(dst, src [6]byte, ipxBytes []byte) []byte {
	switch t {
	case FrameRaw8023:
		frame := make([]byte, 0, ethHdrLen+len(ipxBytes))
		frame = append(frame, dst[:]...)
		frame = append(frame, src[:]...)
		frame = appendLen(frame, len(ipxBytes))
		frame = append(frame, ipxBytes...)
		return frame
	case FrameLLC8022:
		body := len(llcIPX) + len(ipxBytes)
		frame := make([]byte, 0, ethHdrLen+body)
		frame = append(frame, dst[:]...)
		frame = append(frame, src[:]...)
		frame = appendLen(frame, body)
		frame = append(frame, llcIPX[:]...)
		frame = append(frame, ipxBytes...)
		return frame
	default: // FrameEthernetII
		frame := make([]byte, 0, ethHdrLen+len(ipxBytes))
		frame = append(frame, dst[:]...)
		frame = append(frame, src[:]...)
		frame = append(frame, byte(etherTypeIPX>>8), byte(etherTypeIPX&0xFF))
		frame = append(frame, ipxBytes...)
		return frame
	}
}

// appendLen appends a 16-bit big-endian 802.3 length field.
func appendLen(dst []byte, n int) []byte {
	return append(dst, byte(n>>8), byte(n))
}
