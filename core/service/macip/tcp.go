package macip

// TCP/IPv4 read helpers for the MacIP data path. Core is reflection-free and may not
// import encoding/binary (it pulls reflect), so every multi-byte field is read
// big-endian by hand — the same discipline core/protocol/ddp follows.
//
// This file is OBSERVATION ONLY. The MacIP gateway forwards a Mac's IP packets
// unmodified in both directions, matching the golden reference (macipgw macip_output
// and the pre-refactor main branch). Earlier revisions rewrote egress-bound segments —
// clamping the inbound TCP MSS option and throttling the Mac's advertised receive
// window down to a few DDP segments — on the theory that a classic MacTCP receiver
// over-advertises and the peer must be paced. That was wrong for NAT mode: the egress
// there is our own OSNAT TCP-terminating proxy (adapter/macipgw/nat), which already
// paces itself on the Mac's REAL advertised window (space = macAck + macWindow −
// ourSeq) and does not retransmit, so a falsified small window starved that loop and a
// single dropped segment DEADLOCKED the flow (capture ltoudp-netboot.pcap). The genuine
// LToUDP burst constraint belongs to the per-node link pace (core/link.Pace), not to a
// TCP rewrite on the data path. The clamps and their TCP-checksum recompute were removed;
// what remains is a read-only observation of the window/ACK a Mac advertises, for
// diagnostics.

const (
	// ipProtoTCP is the IP protocol number for TCP.
	ipProtoTCP = 6

	// tcpFlagACK is the TCP control-bit mask we test.
	tcpFlagACK = 0x10
)

// ipHeaderLen returns the IPv4 header length in bytes (IHL×4), or 0 if pkt is too
// short or not IPv4.
func ipHeaderLen(pkt []byte) int {
	if len(pkt) < 20 || pkt[0]>>4 != 4 {
		return 0
	}
	ihl := int(pkt[0]&0x0f) * 4
	if ihl < 20 || ihl > len(pkt) {
		return 0
	}
	return ihl
}

// tcpSegment returns the TCP header+payload slice of an IPv4/TCP packet, or nil when
// pkt is not a non-fragmented TCP packet with a complete header. A fragment (offset
// > 0) is skipped: only the first fragment carries the TCP header. The segment is
// bounded by the IPv4 total-length field, not the end of the buffer, because a DDP or
// Ethernet frame may carry trailing padding past the IP packet.
func tcpSegment(pkt []byte) []byte {
	ihl := ipHeaderLen(pkt)
	if ihl == 0 || pkt[9] != ipProtoTCP {
		return nil
	}
	fragOff := int(pkt[6]&0x1f)<<8 | int(pkt[7])
	if fragOff != 0 {
		return nil
	}
	totalLen := int(pkt[2])<<8 | int(pkt[3])
	if totalLen < ihl || totalLen > len(pkt) {
		totalLen = len(pkt)
	}
	seg := pkt[ihl:totalLen]
	if len(seg) < 20 {
		return nil
	}
	dataOff := int(seg[12]>>4) * 4
	if dataOff < 20 || dataOff > len(seg) {
		return nil
	}
	return seg
}

// macFlow holds the last receive-window/ACK a Mac advertised, learned from segments the
// Mac SENT (observeFromMac). Observation only — diagnostics and a record of what a Mac
// says about its receive capacity. Bounded by maxTrackedFlows.
type macFlow struct {
	ack    uint32 // last ACK the Mac sent (highest byte it has received + 1)
	window uint16 // last receive window the Mac advertised
}

// observeFromMac extracts the ACK number and advertised receive window from a TCP
// segment the Mac SENT (Mac→peer direction). ok is false when pkt is not a TCP
// segment carrying an ACK. The window is the raw advertised value; window scaling is
// not honoured (MacTCP predates RFC 1323 and never negotiates it).
func observeFromMac(pkt []byte) (macFlow, bool) {
	seg := tcpSegment(pkt)
	if seg == nil {
		return macFlow{}, false
	}
	if seg[13]&tcpFlagACK == 0 {
		return macFlow{}, false
	}
	ack := uint32(seg[8])<<24 | uint32(seg[9])<<16 | uint32(seg[10])<<8 | uint32(seg[11])
	window := uint16(seg[14])<<8 | uint16(seg[15])
	return macFlow{ack: ack, window: window}, true
}
