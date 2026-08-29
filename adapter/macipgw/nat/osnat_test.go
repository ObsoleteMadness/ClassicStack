package nat

import (
	"encoding/binary"
	"io"
	"log/slog"
	"testing"
)

// discardLogger returns a logger that writes nowhere, matching what New()
// substitutes for a nil logger — used throughout so these tests can
// construct an *OSNAT directly (skipping New's real ICMP raw socket and
// background goroutines, neither available/wanted in a unit test) while
// still exercising code paths that log.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestAllocICMPNatID_Unique checks successive allocations never collide with
// an id already recorded in icmpByNatID.
func TestAllocICMPNatID_Unique(t *testing.T) {
	n := &OSNAT{icmpByNatID: make(map[uint16]*icmpFwdEntry), icmpNextID: 1000}
	seen := make(map[uint16]bool)
	for i := 0; i < 100; i++ {
		id := n.allocICMPNatID()
		if seen[id] {
			t.Fatalf("allocICMPNatID returned duplicate id %d on call %d", id, i)
		}
		seen[id] = true
		n.icmpByNatID[id] = &icmpFwdEntry{}
	}
}

// TestAllocICMPNatID_SkipsUsedID checks allocation skips over an id already
// present in icmpByNatID rather than handing it out again.
func TestAllocICMPNatID_SkipsUsedID(t *testing.T) {
	n := &OSNAT{icmpByNatID: make(map[uint16]*icmpFwdEntry), icmpNextID: 1000}
	n.icmpByNatID[1000] = &icmpFwdEntry{} // pre-occupy the first candidate
	id := n.allocICMPNatID()
	if id == 1000 {
		t.Error("allocICMPNatID returned an id already present in icmpByNatID")
	}
}

// TestAllocICMPNatID_WrapsAtZero checks the counter wraps back to 1000
// (skipping the 0-999 range reserved below the NAT pool) instead of
// overflowing into it when icmpNextID reaches 0.
func TestAllocICMPNatID_WrapsAtZero(t *testing.T) {
	n := &OSNAT{icmpByNatID: make(map[uint16]*icmpFwdEntry), icmpNextID: 65535}
	first := n.allocICMPNatID() // 65535, then counter wraps to 0 -> reset to 1000
	if first != 65535 {
		t.Fatalf("first id = %d, want 65535", first)
	}
	n.icmpByNatID[first] = &icmpFwdEntry{}
	second := n.allocICMPNatID()
	if second != 1000 {
		t.Errorf("second id = %d, want 1000 (wrapped)", second)
	}
}

// newTestOSNAT builds an *OSNAT with no real sockets and no background
// goroutines (bypassing New), recording every emitted packet in *emitted.
func newTestOSNAT(emitted *[][]byte) *OSNAT {
	return &OSNAT{
		deliver:      func(pkt []byte) { *emitted = append(*emitted, pkt) },
		log:          discardLogger(),
		icmpByClient: make(map[icmpClientKey]*icmpFwdEntry),
		icmpByNatID:  make(map[uint16]*icmpFwdEntry),
		icmpNextID:   1000,
		udpFlows:     make(map[osFlowKey]*udpFwdFlow),
		tcpFlows:     make(map[osFlowKey]*tcpFwdFlow),
		stop:         make(chan struct{}),
	}
}

// TestForward_DropsTooShort checks a packet shorter than a minimal IPv4
// header is dropped silently (no emit, no panic) rather than read out of
// bounds.
func TestForward_DropsTooShort(t *testing.T) {
	var emitted [][]byte
	n := newTestOSNAT(&emitted)
	n.Forward(make([]byte, 10))
	if len(emitted) != 0 {
		t.Errorf("got %d emitted packets, want 0", len(emitted))
	}
}

// TestForward_DropsUnsupportedProtocol checks a packet with a protocol
// number Forward doesn't handle (none of ICMP/UDP/TCP) is dropped rather
// than causing a panic or spurious emit.
func TestForward_DropsUnsupportedProtocol(t *testing.T) {
	var emitted [][]byte
	n := newTestOSNAT(&emitted)
	pkt := BuildIPv4Packet([]byte{1, 2, 3, 4}, []byte{5, 6, 7, 8}, 47 /* GRE */, nil)
	n.Forward(pkt)
	if len(emitted) != 0 {
		t.Errorf("got %d emitted packets for an unsupported protocol, want 0", len(emitted))
	}
}

// TestForward_ICMPWithNoRawSocketIsNoop checks forwarding an ICMP echo
// request when icmpConn is nil (the New() fallback when a raw ICMP socket
// isn't available — e.g. no privileges) is a safe no-op, not a nil-pointer
// panic.
func TestForward_ICMPWithNoRawSocketIsNoop(t *testing.T) {
	var emitted [][]byte
	n := newTestOSNAT(&emitted)
	icmpEcho := []byte{8, 0, 0, 0, 0, 1, 0, 1} // type=8 (echo request), id=1, seq=1
	pkt := BuildIPv4Packet([]byte{1, 2, 3, 4}, []byte{5, 6, 7, 8}, 1, icmpEcho)
	n.Forward(pkt)
	if len(emitted) != 0 {
		t.Errorf("got %d emitted packets with icmpConn nil, want 0", len(emitted))
	}
}

// tcpTestFlow builds a minimal tcpFwdFlow for the segment-building tests
// below, which never touch flow.conn.
func tcpTestFlow() *tcpFwdFlow {
	return &tcpFwdFlow{
		clientIP:   [4]byte{10, 0, 0, 5},
		serverIP:   [4]byte{93, 184, 216, 34},
		clientPort: 1234,
		serverPort: 80,
		mss:        1460,
	}
}

// parseTCPFromIPv4 pulls apart an IPv4+TCP packet the way a real receiver
// would, for the assertions below: IHL, protocol, addresses, and the fixed
// TCP header fields.
type parsedTCP struct {
	srcIP, dstIP           [4]byte
	proto                  byte
	srcPort, dstPort       uint16
	seq, ack               uint32
	flags                  byte
	window                 uint16
	payload                []byte
	ipChecksumSelfVerifies bool
}

func parseTCPFromIPv4(t *testing.T, pkt []byte) parsedTCP {
	t.Helper()
	if len(pkt) < 20 {
		t.Fatalf("packet too short: %d bytes", len(pkt))
	}
	ihl := int(pkt[0]&0xf) * 4
	var p parsedTCP
	copy(p.srcIP[:], pkt[12:16])
	copy(p.dstIP[:], pkt[16:20])
	p.proto = pkt[9]
	p.ipChecksumSelfVerifies = RawChecksum(pkt[:ihl]) == 0
	tcp := pkt[ihl:]
	if len(tcp) < 20 {
		t.Fatalf("TCP segment too short: %d bytes", len(tcp))
	}
	p.srcPort = binary.BigEndian.Uint16(tcp[0:2])
	p.dstPort = binary.BigEndian.Uint16(tcp[2:4])
	p.seq = binary.BigEndian.Uint32(tcp[4:8])
	p.ack = binary.BigEndian.Uint32(tcp[8:12])
	p.flags = tcp[13]
	p.window = binary.BigEndian.Uint16(tcp[14:16])
	tcpHdrLen := int(tcp[12]>>4) * 4
	p.payload = tcp[tcpHdrLen:]
	return p
}

// TestSendTCPSYNACK checks the synthetic SYN-ACK OSNAT sends back to the Mac
// on a new flow: ports swapped (server is the wire source), correct
// seq/ack, SYN+ACK flags, and the MSS carried in the options.
func TestSendTCPSYNACK(t *testing.T) {
	var emitted [][]byte
	n := newTestOSNAT(&emitted)
	flow := tcpTestFlow()
	flow.macSeq = 5000 // macISN + 1

	n.sendTCPSYNACK(flow, 9000)

	if len(emitted) != 1 {
		t.Fatalf("got %d emitted packets, want 1", len(emitted))
	}
	p := parseTCPFromIPv4(t, emitted[0])
	if p.srcIP != flow.serverIP || p.dstIP != flow.clientIP {
		t.Errorf("addrs = %v -> %v, want %v -> %v", p.srcIP, p.dstIP, flow.serverIP, flow.clientIP)
	}
	if p.srcPort != flow.serverPort || p.dstPort != flow.clientPort {
		t.Errorf("ports = %d -> %d, want %d -> %d", p.srcPort, p.dstPort, flow.serverPort, flow.clientPort)
	}
	if p.seq != 9000 {
		t.Errorf("seq = %d, want 9000 (our ISN)", p.seq)
	}
	if p.ack != 5000 {
		t.Errorf("ack = %d, want 5000 (macISN+1)", p.ack)
	}
	const synAck = 0x12
	if p.flags != synAck {
		t.Errorf("flags = %#02x, want %#02x (SYN+ACK)", p.flags, synAck)
	}
	if !p.ipChecksumSelfVerifies {
		t.Error("IP header checksum does not self-verify")
	}
	// MSS option: kind=2 len=4 value=flow.mss, at TCP option offset 0 (byte 20 of the segment).
	tcp := emitted[0][20:]
	if tcp[20] != 2 || tcp[21] != 4 {
		t.Fatalf("MSS option header = %02x %02x, want 02 04", tcp[20], tcp[21])
	}
	if got := binary.BigEndian.Uint16(tcp[22:24]); got != flow.mss {
		t.Errorf("MSS value = %d, want %d", got, flow.mss)
	}
}

// TestSendTCPSegment checks a data/ACK segment carries the given seq/ack/
// flags/payload verbatim and a self-verifying checksum.
func TestSendTCPSegment(t *testing.T) {
	var emitted [][]byte
	n := newTestOSNAT(&emitted)
	flow := tcpTestFlow()

	n.sendTCPSegment(flow, 100, 200, 0x18 /* PSH+ACK */, []byte("payload"))

	p := parseTCPFromIPv4(t, emitted[0])
	if p.seq != 100 || p.ack != 200 {
		t.Errorf("seq/ack = %d/%d, want 100/200", p.seq, p.ack)
	}
	if p.flags != 0x18 {
		t.Errorf("flags = %#02x, want 0x18 (PSH+ACK)", p.flags)
	}
	if string(p.payload) != "payload" {
		t.Errorf("payload = %q, want %q", p.payload, "payload")
	}
	if !p.ipChecksumSelfVerifies {
		t.Error("IP header checksum does not self-verify")
	}
}

// TestSendTCPRST checks the reset segment OSNAT emits when a dial fails:
// correct ports/ack, RST+ACK flags, and an empty payload.
func TestSendTCPRST(t *testing.T) {
	var emitted [][]byte
	n := newTestOSNAT(&emitted)
	server := [4]byte{93, 184, 216, 34}
	client := [4]byte{10, 0, 0, 5}

	n.sendTCPRST(server, client, 80, 1234, 777)

	p := parseTCPFromIPv4(t, emitted[0])
	if p.srcIP != server || p.dstIP != client {
		t.Errorf("addrs = %v -> %v, want %v -> %v", p.srcIP, p.dstIP, server, client)
	}
	if p.srcPort != 80 || p.dstPort != 1234 {
		t.Errorf("ports = %d -> %d, want 80 -> 1234", p.srcPort, p.dstPort)
	}
	if p.ack != 777 {
		t.Errorf("ack = %d, want 777", p.ack)
	}
	const rstAck = 0x14
	if p.flags != rstAck {
		t.Errorf("flags = %#02x, want %#02x (RST+ACK)", p.flags, rstAck)
	}
	if len(p.payload) != 0 {
		t.Errorf("payload len = %d, want 0", len(p.payload))
	}
}
