package netbeui

// llc.go holds NBF's CARRIER: the IEEE 802.2 LLC header (and the 802.3 Ethernet
// frame it rides in) that wraps every NBF body from commands.go/netbeui.go.
//
// It lives in the protocol ring because BOTH ends of the stack frame it, byte for
// byte, and each used to hand-roll its own copy:
//
//   - the RESPONDER, core/port/netbeui (llcCtrl*/llcSSAP*/llcDSAP + sendUI /
//     sendIFrame / sendUA / sendS), and
//   - the CALLER, client/smb/nbf.go (nbfLLC* + sendUIRaw / sendU / sendIFrameCtl /
//     sendRR / sendRRFinal / sendRRPoll).
//
// The two copies had to agree exactly — a stray Final bit or a wrong 802.3 length
// desynchronises the peer's LLC2 machine (see the ERRATA on the RR helpers) — so
// the encoders are defined once and both sides call them. The LLC2 STATE machines
// (N(S)/N(R) bookkeeping, T1/N2 recovery on the responder, the caller's simpler
// window) legitimately differ and stay where they are; only the framing is shared.
//
// NBF uses three LLC frame shapes ([IBM SC30-3587] §5.5; ISO 8802-2):
//
//	U-frame  3-byte LLC: DSAP, SSAP, control            (UI, SABME, DISC, UA)
//	S-frame  4-byte LLC: DSAP, SSAP, ctrl0, ctrl1       (RR/RNR/REJ, extended)
//	I-frame  4-byte LLC: DSAP, SSAP, N(S)<<1, N(R)<<1|P (session data, extended)
//
// The 802.3 length field covers the LLC header + body only (not the padding), and
// every frame is zero-extended to the 60-byte 802.3 minimum — NICs and emulated
// adapters drop sub-60-byte runts.

// 802.2 LLC SAP values for NetBIOS Frames. The SSAP's low bit is the C/R bit:
// clear on a command, set on a response.
const (
	// LLCDSAP is the NetBIOS DSAP (0xF0). Inbound, a frame is NBF when its DSAP is
	// this and its SSAP is this ignoring the C/R bit (`ssap&0xFE == LLCDSAP`),
	// which is how the "llc" BPF filter's IPX (0xE0) and SNAP (0xAA) frames are
	// dropped.
	LLCDSAP uint8 = 0xF0
	// LLCSSAPCommand is the SSAP with C/R = command (0xF0).
	LLCSSAPCommand uint8 = 0xF0
	// LLCSSAPResponse is the SSAP with C/R = response (0xF1).
	LLCSSAPResponse uint8 = 0xF1
)

// LLC control-field values used by NBF (ISO 8802-2 / [IBM SC30-3587] §5). The
// U-frame values are whole control bytes; the S-frame values are the ctrl0 byte
// (the N(R) and P/F bit live in ctrl1 in the extended, mod-128 format).
const (
	LLCCtrlUI    uint8 = 0x03 // Unnumbered Information (connectionless)
	LLCCtrlSABME uint8 = 0x7F // Set Async Balanced Mode Extended, P=1
	LLCCtrlDISC  uint8 = 0x43 // Disconnect, P=0
	LLCCtrlDISCP uint8 = 0x53 // Disconnect, P=1
	LLCCtrlUAF   uint8 = 0x73 // Unnumbered Acknowledgment, F=1
	LLCCtrlRR    uint8 = 0x01 // Receive Ready S-frame (ctrl0)
	LLCCtrlREJ   uint8 = 0x09 // Reject S-frame (ctrl0): retransmit from N(R)
	LLCCtrlRNR   uint8 = 0x05 // Receive Not Ready S-frame (ctrl0): peer busy
)

// LLC control-field bit masks. A U-frame has the low two bits set; an S-frame has
// them 01; an I-frame has the low bit clear. In the extended (mod-128) format the
// P/F bit and N(R) live in the SECOND control byte.
const (
	LLCCtrlUMask     uint8 = 0x03 // ctrl0 & LLCCtrlUMask == LLCCtrlUMask → U-frame
	LLCCtrlSMask     uint8 = 0x01 // ctrl0 & LLCCtrlUMask == LLCCtrlSMask → S-frame
	LLCCtrlIMask     uint8 = 0x01 // ctrl0 & LLCCtrlIMask == 0 → I-frame
	LLCCtrlSFuncMask uint8 = 0x0F // the S-frame function bits of ctrl0 (RR/RNR/REJ)
	LLCPollFinal     uint8 = 0x01 // the P/F bit in ctrl1 (extended format)
	LLCSeqMask       uint8 = 0x7F // mod-128 sequence-number mask for N(S)/N(R)
)

// Frame-geometry constants.
const (
	// EthernetHeaderLen is the 14-byte Ethernet/802.3 MAC header (dst, src, length).
	EthernetHeaderLen = 14
	// EthernetMinFrame is the 802.3 minimum frame size outbound frames pad to.
	EthernetMinFrame = 60
	// LLCHeaderLen is the 3-byte (basic-format U-frame) LLC header.
	LLCHeaderLen = 3
	// LLCExtHeaderLen is the 4-byte (extended-format I/S-frame) LLC header.
	LLCExtHeaderLen = 4
	// LLCCtrl1Offset is the offset of the second control byte within a whole
	// Ethernet frame — where N(R) and the P/F bit live. The responder patches it in
	// place when it retransmits a retained I-frame with a refreshed N(R).
	LLCCtrl1Offset = EthernetHeaderLen + 3
)

// MaxIField is the NBF payload one I-frame carries and the value both ends
// advertise as their maximum receive size (SESSION_INITIALIZE / SESSION_CONFIRM
// Data2): the Ethernet MTU less LLC/NBF overhead. A larger message is fragmented
// across DATA_FIRST_MIDDLE frames closed by DATA_ONLY_LAST. The responder
// (core/service/netbios) and the caller (client/smb) must agree or one side
// truncates the other's message, so the value is defined once here.
const MaxIField uint16 = 1464

// EncodeUIFrame builds a complete 802.3 frame carrying an NBF body in a Type-1
// (connectionless) LLC UI frame: DSAP 0xF0, SSAP 0xF0 (command), control 0x03. The
// 802.3 length field covers the LLC header + body; the frame is padded to the
// 802.3 minimum.
func EncodeUIFrame(dstMAC, srcMAC [6]byte, body []byte) []byte {
	payloadLen := LLCHeaderLen + len(body)
	out := make([]byte, EthernetHeaderLen+payloadLen)
	putEthernetHeader(out, dstMAC, srcMAC, payloadLen)
	out[14], out[15], out[16] = LLCDSAP, LLCSSAPCommand, LLCCtrlUI
	copy(out[17:], body)
	return padTo(out, EthernetMinFrame)
}

// EncodeUFrame builds a complete 802.3 frame carrying a 3-byte unnumbered LLC
// control frame (SABME/DISC as a command with ssap LLCSSAPCommand, UA as a
// response with LLCSSAPResponse). There is no body.
func EncodeUFrame(dstMAC, srcMAC [6]byte, ssap, ctrl uint8) []byte {
	out := make([]byte, EthernetHeaderLen+LLCHeaderLen)
	putEthernetHeader(out, dstMAC, srcMAC, LLCHeaderLen)
	out[14], out[15], out[16] = LLCDSAP, ssap, ctrl
	return padTo(out, EthernetMinFrame)
}

// EncodeSFrame builds a complete 802.3 frame carrying a 4-byte supervisory LLC
// frame (extended format): ctrl0 is the S-function (LLCCtrlRR / LLCCtrlRNR /
// LLCCtrlREJ) and ctrl1 carries N(R)<<1 plus the P/F bit.
//
// ssap selects command vs response, and the choice is load-bearing: an RR with F=1
// is a checkpoint RESPONSE, valid only as the answer to a command carrying P=1.
// Sending one unsolicited desynchronises the peer's LLC2 machine (against real
// Win98 it wedged the link right after NEGOTIATE), while failing to answer a peer's
// RR-command-with-P leaves it retransmitting the poll forever.
func EncodeSFrame(dstMAC, srcMAC [6]byte, ssap, sFunc, nR uint8, pollFinal bool) []byte {
	out := make([]byte, EthernetHeaderLen+LLCExtHeaderLen)
	putEthernetHeader(out, dstMAC, srcMAC, LLCExtHeaderLen)
	out[14], out[15] = LLCDSAP, ssap
	out[16] = sFunc
	out[17] = nR << 1
	if pollFinal {
		out[17] |= LLCPollFinal
	}
	return padTo(out, EthernetMinFrame)
}

// EncodeIFrame builds a complete 802.3 frame carrying an NBF session body in an
// LLC Type-2 I-frame (extended format): ctrl0 = N(S)<<1 (low bit 0 marks an
// I-frame), ctrl1 = N(R)<<1 with the Poll bit in bit 0. I-frames are always
// commands (SSAP 0xF0). Advancing N(S) is the caller's job — the sequence state
// belongs to the connection, not the codec.
func EncodeIFrame(dstMAC, srcMAC [6]byte, nS, nR uint8, poll bool, body []byte) []byte {
	payloadLen := LLCExtHeaderLen + len(body)
	out := make([]byte, EthernetHeaderLen+payloadLen)
	putEthernetHeader(out, dstMAC, srcMAC, payloadLen)
	out[14], out[15] = LLCDSAP, LLCSSAPCommand
	out[16] = nS << 1
	out[17] = nR << 1
	if poll {
		out[17] |= LLCPollFinal
	}
	copy(out[18:], body)
	return padTo(out, EthernetMinFrame)
}

// putEthernetHeader writes the 14-byte 802.3 MAC header: destination, source, and
// the length field, which counts the LLC header + body WITHOUT any padding.
func putEthernetHeader(out []byte, dstMAC, srcMAC [6]byte, payloadLen int) {
	copy(out[0:6], dstMAC[:])
	copy(out[6:12], srcMAC[:])
	out[12], out[13] = byte(payloadLen>>8), byte(payloadLen)
}

// padTo zero-extends out to at least n bytes (the 802.3 minimum frame size); NICs
// and emulated adapters drop sub-60-byte runts. Only trailing bytes are added — the
// 802.3 length field already reflects the real payload size.
func padTo(out []byte, n int) []byte {
	if len(out) >= n {
		return out
	}
	return append(out, make([]byte, n-len(out))...)
}

// IsNetBIOSLLC reports whether a frame's LLC header carries the NetBIOS SAPs: DSAP
// 0xF0 and SSAP 0xF0/0xF1 (the C/R bit ignored). The "llc" BPF filter both sides
// use also passes IPX (0xE0) and SNAP (0xAA), which this drops. b is the frame body
// AFTER the Ethernet header; a body shorter than an LLC header is not NBF.
func IsNetBIOSLLC(b []byte) bool {
	return len(b) >= LLCHeaderLen && b[0] == LLCDSAP && b[1]&0xFE == LLCDSAP
}
