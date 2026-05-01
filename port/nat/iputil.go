// Package nat provides shared IP packet utilities and the IP NAT engine used
// by the MacIP gateway. Moving these here isolates the NAT logic from the
// MacIP service package and allows future reuse across ports.
package nat

import "encoding/binary"

// MaxIPPerDDP is the maximum IP payload that fits in a single DDP packet
// (ddp.MaxDataLength = 586 bytes).
const MaxIPPerDDP = 586

// FragmentIPv4 splits pkt into fragments each ≤maxSize bytes.
// Returns nil if pkt is malformed or has the DF bit set and exceeds maxSize.
// The caller owns the returned slices.
func FragmentIPv4(pkt []byte, maxSize int) [][]byte {
	if len(pkt) <= maxSize {
		return [][]byte{append([]byte(nil), pkt...)}
	}
	if len(pkt) < 20 {
		return nil
	}
	if pkt[6]&0x40 != 0 { // DF bit — cannot fragment
		return nil
	}
	ihl := int(pkt[0]&0xf) * 4
	if ihl < 20 || len(pkt) < ihl {
		return nil
	}
	fragPayloadMax := ((maxSize - ihl) / 8) * 8
	if fragPayloadMax <= 0 {
		return nil
	}
	origFO := binary.BigEndian.Uint16(pkt[6:8])
	origOffset := int(origFO&0x1FFF) * 8
	origMF := origFO&0x2000 != 0
	payload := pkt[ihl:]
	var frags [][]byte
	for off := 0; off < len(payload); off += fragPayloadMax {
		end := off + fragPayloadMax
		isLast := end >= len(payload)
		if end > len(payload) {
			end = len(payload)
		}
		frag := make([]byte, ihl+(end-off))
		copy(frag[:ihl], pkt[:ihl])
		copy(frag[ihl:], payload[off:end])
		binary.BigEndian.PutUint16(frag[2:4], uint16(len(frag)))
		fo := uint16((origOffset + off) / 8)
		var flags uint16
		if !isLast || origMF {
			flags = 0x2000 // MF
		}
		binary.BigEndian.PutUint16(frag[6:8], flags|fo)
		binary.BigEndian.PutUint16(frag[10:12], 0)
		binary.BigEndian.PutUint16(frag[10:12], RawChecksum(frag[:ihl]))
		frags = append(frags, frag)
	}
	return frags
}

// RawChecksum computes a ones-complement checksum used for IPv4 headers and ICMP.
func RawChecksum(data []byte) uint16 {
	sum := uint32(0)
	for i := 0; i+1 < len(data); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(data[i:]))
	}
	if len(data)%2 == 1 {
		sum += uint32(data[len(data)-1]) << 8
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

// TransportChecksum computes the TCP/UDP checksum using the IPv4 pseudo-header.
func TransportChecksum(srcIP, dstIP []byte, proto uint8, segment []byte) uint16 {
	sum := uint32(0)
	sum += uint32(binary.BigEndian.Uint16(srcIP[0:2]))
	sum += uint32(binary.BigEndian.Uint16(srcIP[2:4]))
	sum += uint32(binary.BigEndian.Uint16(dstIP[0:2]))
	sum += uint32(binary.BigEndian.Uint16(dstIP[2:4]))
	sum += uint32(proto)
	sum += uint32(len(segment))
	for i := 0; i+1 < len(segment); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(segment[i:]))
	}
	if len(segment)%2 == 1 {
		sum += uint32(segment[len(segment)-1]) << 8
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

// BuildIPv4Packet constructs a minimal IPv4 packet with a valid header checksum.
func BuildIPv4Packet(srcIP, dstIP []byte, proto uint8, payload []byte) []byte {
	pkt := make([]byte, 20+len(payload))
	pkt[0] = 0x45
	binary.BigEndian.PutUint16(pkt[2:4], uint16(len(pkt)))
	pkt[8] = 64
	pkt[9] = proto
	copy(pkt[12:16], srcIP)
	copy(pkt[16:20], dstIP)
	binary.BigEndian.PutUint16(pkt[10:12], 0)
	binary.BigEndian.PutUint16(pkt[10:12], RawChecksum(pkt[:20]))
	copy(pkt[20:], payload)
	return pkt
}
