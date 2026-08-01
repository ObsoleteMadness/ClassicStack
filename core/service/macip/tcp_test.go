package macip

import (
	"encoding/hex"
	"testing"
)

// The MacIP data path forwards a Mac's IP packets unmodified (matching the golden
// macipgw macip_output / pre-refactor main). The former MSS/window clamps and their
// TCP-checksum recompute were removed after they deadlocked NAT-mode TCP (they starved
// the OSNAT proxy's own window-based pacing — see tcp.go). What remains under test is
// the read-only helpers: tcpSegment bounding/fragment handling and observeFromMac.

// mustHex decodes a hex string to bytes, failing the test on a bad literal.
func mustHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

// tcpOptNopByte / tcpOptEndByte are TCP option kinds used only to pad a test packet's
// options to a 4-byte boundary (the production clamps that walked options are gone).
const tcpOptNopByte = 1

// ip16 folds a 16-bit ones-complement sum over data (for building valid test IP/TCP
// checksums locally — the production checksum helpers were removed with the clamps).
func ip16(sum uint32, data []byte) uint16 {
	for i := 0; i+1 < len(data); i += 2 {
		sum += uint32(data[i])<<8 | uint32(data[i+1])
	}
	if len(data)%2 == 1 {
		sum += uint32(data[len(data)-1]) << 8
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

// buildTCP builds a minimal IPv4/TCP packet with the given flags, window, ack, and
// TCP options block, computing valid IP and TCP checksums. src/dst are 4-byte IPs.
func buildTCP(src, dst [4]byte, srcPort, dstPort uint16, flags uint8, window uint16, ack uint32, opts []byte) []byte {
	for len(opts)%4 != 0 {
		opts = append(opts, tcpOptNopByte)
	}
	tcpLen := 20 + len(opts)
	dataOff := tcpLen / 4
	pkt := make([]byte, 20+tcpLen)

	// IPv4 header.
	pkt[0] = 0x45
	total := len(pkt)
	pkt[2] = byte(total >> 8)
	pkt[3] = byte(total)
	pkt[8] = 64 // TTL
	pkt[9] = ipProtoTCP
	copy(pkt[12:16], src[:])
	copy(pkt[16:20], dst[:])
	ipsum := ip16(0, pkt[:20])
	pkt[10] = byte(ipsum >> 8)
	pkt[11] = byte(ipsum)

	// TCP header.
	seg := pkt[20:]
	seg[0] = byte(srcPort >> 8)
	seg[1] = byte(srcPort)
	seg[2] = byte(dstPort >> 8)
	seg[3] = byte(dstPort)
	seg[8] = byte(ack >> 24)
	seg[9] = byte(ack >> 16)
	seg[10] = byte(ack >> 8)
	seg[11] = byte(ack)
	seg[12] = byte(dataOff << 4)
	seg[13] = flags
	seg[14] = byte(window >> 8)
	seg[15] = byte(window)
	copy(seg[20:], opts)
	// TCP checksum over the IPv4 pseudo-header + segment.
	var psum uint32
	psum += uint32(src[0])<<8 | uint32(src[1])
	psum += uint32(src[2])<<8 | uint32(src[3])
	psum += uint32(dst[0])<<8 | uint32(dst[1])
	psum += uint32(dst[2])<<8 | uint32(dst[3])
	psum += uint32(ipProtoTCP)
	psum += uint32(len(seg))
	c := ip16(psum, seg)
	seg[16] = byte(c >> 8)
	seg[17] = byte(c)
	return pkt
}

var (
	macIP  = [4]byte{192, 168, 0, 104}
	peerIP = [4]byte{192, 168, 0, 1}
)

// tcpFlagSYN is used only by the fragment/segment tests here (production no longer
// tests SYN flags — it does not rewrite SYNs).
const tcpFlagSYN = 0x02

// TestTCPSegment_BoundedByIPTotalLen is the regression from ltoudp-netboot.pcap frame
// 32: the inbound SYN-ACK carried 2 bytes of DDP padding past the IP total-length. The
// segment must be bounded by the IP total-length (44), not the buffer end (46) — a
// property observeFromMac still relies on so it never reads padding as segment bytes.
func TestTCPSegment_BoundedByIPTotalLen(t *testing.T) {
	pkt := mustHex("4500002c000040007206586112dcdc7ec0a80068005007b69bcab7c05ef186b16012f507b46f0000020402220090")
	seg := tcpSegment(pkt)
	if seg == nil {
		t.Fatal("expected a TCP segment")
	}
	if len(seg) != 24 {
		t.Fatalf("segment length = %d, want 24 (bounded by IP total-length, not buffer 46)", len(seg))
	}
}

func TestTCPSegment_SkipsFragment(t *testing.T) {
	pkt := buildTCP(peerIP, macIP, 80, 1750, tcpFlagSYN, 8192, 0, []byte{2, 4, 0x05, 0xB4})
	// Set a non-zero fragment offset (byte 7) → not the first fragment.
	pkt[7] = 1
	if tcpSegment(pkt) != nil {
		t.Fatal("a non-first fragment must not be treated as a TCP segment")
	}
}

func TestObserveFromMac(t *testing.T) {
	pkt := buildTCP(macIP, peerIP, 1750, 80, 0x10 /*ACK*/, 4096, 0xDEADBEEF, nil)
	f, ok := observeFromMac(pkt)
	if !ok {
		t.Fatal("expected an observation from an ACK segment")
	}
	if f.window != 4096 {
		t.Fatalf("window = %d, want 4096", f.window)
	}
	if f.ack != 0xDEADBEEF {
		t.Fatalf("ack = %#x, want 0xDEADBEEF", f.ack)
	}
}

func TestObserveFromMac_NoACKFlag(t *testing.T) {
	pkt := buildTCP(macIP, peerIP, 1750, 80, tcpFlagSYN /*no ACK*/, 4096, 0, nil)
	if _, ok := observeFromMac(pkt); ok {
		t.Fatal("a bare SYN carries no meaningful ACK; observation should report !ok")
	}
}

func TestObserveMacTCP_BoundedAndKeyed(t *testing.T) {
	s := &Service{flows: make(map[flowKey]macFlow)}
	pkt := buildTCP(macIP, peerIP, 1750, 80, 0x10, 4096, 42, nil)
	s.observeMacTCP(pkt)
	if len(s.flows) != 1 {
		t.Fatalf("flows = %d, want 1", len(s.flows))
	}
	key := flowKey{macIP: IPv4(macIP), peerIP: IPv4(peerIP), macPort: 1750, peerPort: 80}
	if f, ok := s.flows[key]; !ok || f.window != 4096 || f.ack != 42 {
		t.Fatalf("flow not recorded under expected key: %+v ok=%v", f, ok)
	}
}
