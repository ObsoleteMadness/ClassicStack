package nat

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// TestRawChecksum_KnownVector pins RawChecksum against a textbook worked
// example (RFC 1071 §3): a 20-byte IPv4 header with the checksum field
// zeroed produces the well-known 0xB861 result.
func TestRawChecksum_KnownVector(t *testing.T) {
	hdr := []byte{
		0x45, 0x00, 0x00, 0x73, 0x00, 0x00, 0x40, 0x00,
		0x40, 0x11, 0x00, 0x00, 0xc0, 0xa8, 0x00, 0x01,
		0xc0, 0xa8, 0x00, 0xc7,
	}
	if got := RawChecksum(hdr); got != 0xb861 {
		t.Errorf("RawChecksum = %#04x, want 0xb861", got)
	}
}

// TestRawChecksum_SelfVerifies checks the defining property of the IPv4
// checksum: computing it over data that already carries its own correct
// checksum field yields zero (ones-complement of an all-ones sum).
func TestRawChecksum_SelfVerifies(t *testing.T) {
	hdr := []byte{
		0x45, 0x00, 0x00, 0x14, 0x00, 0x00, 0x00, 0x00,
		0x40, 0x06, 0x00, 0x00, 0x0a, 0x00, 0x00, 0x01,
		0x0a, 0x00, 0x00, 0x02,
	}
	binary.BigEndian.PutUint16(hdr[10:12], RawChecksum(hdr))
	if got := RawChecksum(hdr); got != 0 {
		t.Errorf("RawChecksum of a self-checksummed header = %#04x, want 0", got)
	}
}

// TestRawChecksum_OddLength checks the odd-length-final-byte padding path
// (the final byte is treated as the high byte of a padded 16-bit word).
func TestRawChecksum_OddLength(t *testing.T) {
	odd := []byte{0x00, 0x01, 0xff}
	even := []byte{0x00, 0x01, 0xff, 0x00}
	if got, want := RawChecksum(odd), RawChecksum(even); got != want {
		t.Errorf("RawChecksum(odd) = %#04x, RawChecksum(zero-padded even) = %#04x, want equal", got, want)
	}
}

// TestBuildIPv4Packet_ChecksumValid checks BuildIPv4Packet always produces a
// self-verifying header checksum and the fixed fields (version/IHL, TTL,
// protocol, addresses, total length) it documents.
func TestBuildIPv4Packet_ChecksumValid(t *testing.T) {
	src := []byte{10, 0, 0, 1}
	dst := []byte{10, 0, 0, 2}
	payload := []byte("hello, world")

	pkt := BuildIPv4Packet(src, dst, 17, payload)

	if pkt[0] != 0x45 {
		t.Errorf("version/IHL byte = %#02x, want 0x45", pkt[0])
	}
	if got := binary.BigEndian.Uint16(pkt[2:4]); int(got) != len(pkt) {
		t.Errorf("total length = %d, want %d", got, len(pkt))
	}
	if pkt[8] != 64 {
		t.Errorf("TTL = %d, want 64", pkt[8])
	}
	if pkt[9] != 17 {
		t.Errorf("protocol = %d, want 17", pkt[9])
	}
	if !bytes.Equal(pkt[12:16], src) {
		t.Errorf("src = %v, want %v", pkt[12:16], src)
	}
	if !bytes.Equal(pkt[16:20], dst) {
		t.Errorf("dst = %v, want %v", pkt[16:20], dst)
	}
	if !bytes.Equal(pkt[20:], payload) {
		t.Errorf("payload = %q, want %q", pkt[20:], payload)
	}
	if RawChecksum(pkt[:20]) != 0 {
		t.Error("header checksum does not self-verify")
	}
}

// TestTransportChecksum_MatchesPseudoHeaderByHand recomputes the UDP
// checksum by hand (pseudo-header + segment, the RFC 768 definition) and
// checks TransportChecksum agrees, pinning the pseudo-header field order and
// the length-in-checksum convention.
func TestTransportChecksum_MatchesPseudoHeaderByHand(t *testing.T) {
	src := []byte{192, 168, 1, 10}
	dst := []byte{192, 168, 1, 20}
	segment := []byte{
		0x04, 0x00, // src port 1024
		0x00, 0x35, // dst port 53
		0x00, 0x0c, // length 12
		0x00, 0x00, // checksum placeholder
		'p', 'i', 'n', 'g',
	}

	var pseudo []byte
	pseudo = append(pseudo, src...)
	pseudo = append(pseudo, dst...)
	pseudo = append(pseudo, 0, 17)
	pseudo = binary.BigEndian.AppendUint16(pseudo, uint16(len(segment)))
	pseudo = append(pseudo, segment...)
	want := RawChecksum(pseudo)

	if got := TransportChecksum(src, dst, 17, segment); got != want {
		t.Errorf("TransportChecksum = %#04x, want %#04x (hand-computed pseudo-header sum)", got, want)
	}
}

// TestTransportChecksum_SelfVerifies mirrors the IPv4-header self-verify
// property for the pseudo-header checksum: stamping the computed value back
// into the segment's checksum field makes a re-computation (which now
// implicitly covers the checksum field itself) come out to zero.
func TestTransportChecksum_SelfVerifies(t *testing.T) {
	src := []byte{10, 0, 0, 1}
	dst := []byte{10, 0, 0, 2}
	segment := make([]byte, 8+5)
	binary.BigEndian.PutUint16(segment[4:6], uint16(len(segment)))
	copy(segment[8:], "hello")

	sum := TransportChecksum(src, dst, 17, segment)
	binary.BigEndian.PutUint16(segment[6:8], sum)

	if got := TransportChecksum(src, dst, 17, segment); got != 0 {
		t.Errorf("TransportChecksum after stamping = %#04x, want 0", got)
	}
}

// TestFragmentIPv4_FitsInOnePiece checks a packet already within maxSize is
// returned as a single fragment, byte-for-byte, not re-encoded.
func TestFragmentIPv4_FitsInOnePiece(t *testing.T) {
	pkt := BuildIPv4Packet([]byte{1, 2, 3, 4}, []byte{5, 6, 7, 8}, 17, []byte("small"))
	frags := FragmentIPv4(pkt, 100)
	if len(frags) != 1 {
		t.Fatalf("got %d fragments, want 1", len(frags))
	}
	if !bytes.Equal(frags[0], pkt) {
		t.Error("single-fragment result does not match the original packet")
	}
}

// TestFragmentIPv4_DFBitBlocksFragmentation checks a packet exceeding
// maxSize with the Don't-Fragment bit set is refused (nil) rather than
// fragmented anyway.
func TestFragmentIPv4_DFBitBlocksFragmentation(t *testing.T) {
	pkt := BuildIPv4Packet([]byte{1, 2, 3, 4}, []byte{5, 6, 7, 8}, 17, make([]byte, 200))
	pkt[6] |= 0x40 // DF bit
	if frags := FragmentIPv4(pkt, 100); frags != nil {
		t.Errorf("got %d fragments for a DF packet exceeding maxSize, want nil", len(frags))
	}
}

// TestFragmentIPv4_SplitsAndReassembles splits an oversized packet, checks
// every fragment (with the possible exception of the last) is a multiple of
// 8 bytes of payload (the IPv4 fragmentation granularity), the offsets and
// MF flag chain correctly, and the fragments' payloads concatenate back to
// the original.
func TestFragmentIPv4_SplitsAndReassembles(t *testing.T) {
	payload := bytes.Repeat([]byte{0xAB}, 1000)
	pkt := BuildIPv4Packet([]byte{1, 2, 3, 4}, []byte{5, 6, 7, 8}, 17, payload)

	const maxSize = 100
	frags := FragmentIPv4(pkt, maxSize)
	if len(frags) < 2 {
		t.Fatalf("got %d fragments, want at least 2", len(frags))
	}

	var reassembled []byte
	for i, f := range frags {
		if len(f) > maxSize {
			t.Errorf("fragment %d length %d exceeds maxSize %d", i, len(f), maxSize)
		}
		ihl := int(f[0]&0xf) * 4
		if ihl != 20 {
			t.Fatalf("fragment %d IHL = %d, want 20 (header copied verbatim)", i, ihl)
		}
		fragPayloadLen := len(f) - ihl
		isLast := i == len(frags)-1
		if !isLast && fragPayloadLen%8 != 0 {
			t.Errorf("fragment %d payload length %d not a multiple of 8", i, fragPayloadLen)
		}
		fo := binary.BigEndian.Uint16(f[6:8])
		offset := int(fo&0x1FFF) * 8
		mf := fo&0x2000 != 0
		if offset != len(reassembled) {
			t.Errorf("fragment %d offset = %d, want %d (running total)", i, offset, len(reassembled))
		}
		if isLast && mf {
			t.Errorf("last fragment has MF set, want clear")
		}
		if !isLast && !mf {
			t.Errorf("fragment %d (not last) has MF clear, want set", i)
		}
		if RawChecksum(f[:ihl]) != 0 {
			t.Errorf("fragment %d header checksum does not self-verify", i)
		}
		reassembled = append(reassembled, f[ihl:]...)
	}
	if !bytes.Equal(reassembled, payload) {
		t.Error("reassembled fragment payloads do not match the original")
	}
}

// TestFragmentIPv4_TooShortToFragment checks a malformed packet too short
// to even hold an IPv4 header is refused rather than causing an out-of-range
// panic.
func TestFragmentIPv4_TooShortToFragment(t *testing.T) {
	short := make([]byte, 10)
	if frags := FragmentIPv4(short, 5); frags != nil {
		t.Errorf("got %d fragments for a 10-byte packet with maxSize 5, want nil", len(frags))
	}
}
