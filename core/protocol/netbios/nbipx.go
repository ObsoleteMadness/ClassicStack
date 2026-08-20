package netbios

import (
	"errors"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
	ipxproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx"
)

// NetBIOS-over-IPX (NBIPX) packet encoding.
//
// NBIPX uses two IPX packet types depending on purpose:
//
//   - IPX type 20 ("NetBIOS broadcast / forwarding") for name service: name
//     claim, name query, name in conflict. Travels broadcast and may traverse
//     up to 8 routers.
//   - IPX type 4 ("Packet Exchange Protocol") on socket 0x0455 for session
//     traffic: establishment, data, teardown. Carries the 16-byte NB-IPX
//     session header below.
//
// The session-header constants and name-service packet shape are the same on
// the wire whether the sender is OS/2 LAN Server, Win95, or NetWare-based.

// IPXTypeNetBIOS is the IPX packet-type (0x14 = 20) for NBIPX broadcast
// forwarding (name claim / query). It is the IPX-level constant under its
// NBIPX-facing name; core/protocol/ipx holds the single definition.
const IPXTypeNetBIOS = ipxproto.TypeNetBIOS

// IPXTypePEP is the IPX packet-type (0x04) for the NB-IPX session protocol on
// socket 0x0455. See IPXTypeNetBIOS on where the value is defined.
const IPXTypePEP = ipxproto.TypePEP

// NB-IPX socket numbers. NetBIOS-over-IPX (NWLink) uses five sockets; the server's
// session engine (core/service/netbios) registers on each and the client transports
// (client/smb ipx.go / nbipx.go, client/netbios) address them, so they are defined
// once here rather than per side — both used to carry private copies.
//
//	0x0455 — session + the type-20 NBIPX Find-name broadcast
//	0x0550 — the NB-IPX server socket (our claim's source socket); ALSO the socket
//	         direct-hosted SMB listens on (see the SMB service's DirectSMBSocket)
//	0x0551 — NMPI name-query ("where is CLASSICSTACK?")
//	0x0552 — the direct-hosted SMB client's own socket (golden capture
//	         spec/captures/nwlink-win98.pcap frames 14/15/16)
//	0x0553 — NB-IPX datagram (NMPI mailslot sends: browser traffic)
//	0x0554 — name service (alternative path some stacks use)
var (
	NBIPXSessionSocket   = [2]byte{0x04, 0x55}
	NBIPXServerSocket    = [2]byte{0x05, 0x50}
	NBIPXNameQuerySocket = [2]byte{0x05, 0x51}
	NBIPXClientSocket    = [2]byte{0x05, 0x52}
	NBIPXDatagramSocket  = [2]byte{0x05, 0x53}
	NBIPXNameSocket      = [2]byte{0x05, 0x54}
)

// NBIPXUnassignedConnID is the DestConnID sentinel a client stamps on its NetBIOS
// session-request (SESSION_INITIALIZE) DATA frame before the server has assigned a
// connection id, and the value the server keys the request off. Both sides used to
// declare their own copy.
const NBIPXUnassignedConnID uint16 = 0xFFFF

// NBIPXMaxFrameData is the most session data one NB-IPX DATA frame carries: an
// Ethernet II payload (1500) less the IPX header (30) and the NB-IPX session header
// (18). A message larger than this is fragmented across frames via
// TotalDataLen/Offset/DataLen with EOM set only on the last; both the client
// transport and the server engine must agree on the boundary, so the constant is
// shared rather than restated on each side.
const NBIPXMaxFrameData = 1500 - ipxproto.HeaderLen - NBIPXSessionHeaderLen

// NB-IPX session header: data_stream_type values.
//
// ERRATA (captures/ipx.pcap): a real Win98/WfW NWLink client drives session
// traffic with a much smaller DataStreamType set than the 0x14/0x15/0x16
// "DataAck/DataOnlyLast/DataFirstMiddle" numbering originally assumed here (that
// set is a different NWLink dialect this client never emits). On the wire the
// observed session stream types are:
//
//	0x01 FIND.NAME          (name service, ConnCtrlFlag 0x00)
//	0x02 NAME.RECOGNIZED    (name service, ConnCtrlFlag 0x00)
//	0x06 DATA               (session message; ConnCtrlFlag carries EOM 0x10 /
//	                         ACK 0x40 / SYS 0x80 — every SMB rides this type)
//	0x07 SESSION.END        (ConnCtrlFlag 0x40)
//	0x08 SESSION.END.ACK    (ConnCtrlFlag 0x80)
//
// There is NO explicit SESSION.INIT/CONFIRM handshake: the first DATA (an SMB
// negotiate) opens the circuit implicitly. NBIPXSessionData is the canonical
// name for the DATA type; the legacy Confirm/Init aliases are retained for the
// name-service conflict path but are not used to frame session data. See
// spec/errata.md.
const (
	NBIPXFindName       uint8 = 0x01 // name service request
	NBIPXNameRecognized uint8 = 0x02 // name service reply (positive)
	NBIPXCheckName      uint8 = 0x03
	NBIPXNameInUse      uint8 = 0x04
	NBIPXDeregisterName uint8 = 0x05
	NBIPXSessionInit    uint8 = 0x05 // legacy alias; no INIT is seen on the wire
	NBIPXSessionData    uint8 = 0x06 // DATA — the type every SMB session frame uses
	NBIPXSessionConfirm uint8 = 0x06 // legacy alias (== SessionData); unused for framing
	NBIPXSessionEnd     uint8 = 0x07
	NBIPXSessionEndAck  uint8 = 0x08
	NBIPXStatusQuery    uint8 = 0x09
	NBIPXStatusResponse uint8 = 0x0A
	// NBIPXDirectedDatagram tags a raw directed NetBIOS datagram on the datagram
	// socket; it is a datagram-path type, distinct from the session DATA type.
	NBIPXDirectedDatagram uint8 = 0x0B
	// Legacy alternate-dialect data types, retained for reference / other stacks.
	NBIPXDataAck         uint8 = 0x14
	NBIPXDataOnlyLast    uint8 = 0x15
	NBIPXDataFirstMiddle uint8 = 0x16
)

// NB-IPX session header: connection-control flag bits (high nibble of
// conn_ctrl_flag).
const (
	NBIPXConnFlagSYS uint8 = 0x80 // system packet
	NBIPXConnFlagACK uint8 = 0x40 // requesting an ACK
	NBIPXConnFlagATT uint8 = 0x20 // attention
	NBIPXConnFlagEOM uint8 = 0x10 // end of message
	// NBIPXConnFlagRESEND is a resend request: the peer asks us to retransmit our
	// data frames starting from the sequence number in its RecvSeq field.
	// ERRATA (captures ipx.pcap 2026-07-10 frame 278): Win98 NWLink emits
	// SYS|RESEND (0x88) with RecvSeq 0 when a server data frame arrives carrying an
	// unexpected SendSeq — see the sequencing rules on NBIPXSessionHeader.
	NBIPXConnFlagRESEND uint8 = 0x08

	// NBIPXConnFlagCONFIRM is the low bit a server sets on the session-accept DATA
	// frame that confirms a client's SESSION_INITIALIZE. ERRATA (captures/ipx.pcap):
	// a Win98/WfW NWLink client only advances to SMB when the accept carries
	// ConnCtrlFlag = SYS|CONFIRM (0x81) *and* RecvSeq = 1 (see NBIPXSessionAcceptRecvSeq);
	// an accept of bare SYS (0x80) with RecvSeq 0 is treated as unconfirmed and the
	// client retransmits SESSION_INITIALIZE forever. The working WFW-IPX server's
	// accept (frame 367) sets both; ours (frame 332) set neither, so no session ever
	// negotiated over the type-4 path. This is the NBIPX-flattened analogue of NBF's
	// distinct SESSION_CONFIRM command (spec/iee802.md §5.6.16) — NBIPX rides it on
	// DATA (0x06) with this flag rather than a separate DataStreamType.
	NBIPXConnFlagCONFIRM uint8 = 0x01
)

// NBIPXSessionAcceptRecvSeq is the RecvSeq value a server puts in its session-accept
// (SESSION_CONFIRM) DATA frame. ERRATA (captures/ipx.pcap frame 367): the working
// WFW-IPX server sets RecvSeq = 1 on the accept; the client validates it together
// with NBIPXConnFlagCONFIRM before it will send its first SMB frame.
const NBIPXSessionAcceptRecvSeq uint16 = 1

// NBIPXSessionHeaderLen is the wire length of NBIPXSessionHeader.
//
// ERRATA (captures/ipx.pcap): the on-wire session header is 18 bytes, not the 16
// this codec (and the legacy over_ipx transport it was ported from) assumed. See
// the field table on NBIPXSessionHeader below and spec/errata.md. The extra two
// bytes are the Receive-Sequence / Bytes-Received pair at offsets 14-15/16-17;
// SMB data begins at offset 18. Getting this wrong offset the SMB payload by two
// bytes on decode and truncated our replies, so no NB-IPX session ever negotiated.
const NBIPXSessionHeaderLen = 18

// NBIPXSessionHeader is the 18-byte session header that prefixes every NB-IPX
// session-family payload (everything carried over IPX type 4 on socket 0x0455).
//
// ERRATA: all multi-byte fields are LITTLE-endian, not big-endian. The wire (a
// Win98/WfW NWLink client in captures/ipx.pcap) puts SourceConnID/DestConnID and
// the length fields little-endian; a request's SourceConnID is echoed as the
// reply's DestConnID, and TotalDataLen/DataLen equal the SMB payload byte count.
// The field/offset table observed on the wire:
//
//	 0    ConnCtrlFlag    (SYS|ACK|ATT|EOM bitfield)
//	 1    DataStreamType  (NBIPXSessionInit, NBIPXDataOnlyLast, ...)
//	 2-3  SourceConnID    (LE)
//	 4-5  DestConnID      (LE)
//	 6-7  SendSeq         (LE)
//	 8-9  TotalDataLen    (LE) — SMB message length
//	10-11 Offset          (LE)
//	12-13 DataLen         (LE) — bytes carried in this frame
//	14-15 RecvSeq         (LE) — receive sequence number
//	16-17 BytesReceived   (LE)
//	18+   Data (the SMB PDU)
//
// Sequencing rules (ERRATA, observed against WinNT 3.51 / Win98 NWLink clients in
// captures ipx.pcap 2026-07-10; see spec/errata.md):
//
//   - SendSeq is consumed by frames that carry data — the SESSION_INITIALIZE
//     (0x41, seq 0; the client's first SMB frame is seq 1) and every data frame —
//     and by SESSION_END (0x40, zero data). Zero-data SYSTEM/control frames (the
//     0x81 accept, an 0x80 ack, an 0x88 resend request, and NT's 0xC0 probe)
//     carry the sender's CURRENT send counter but consume nothing — so the
//     server's first data frame MUST be seq 0, and a probe is acked with the
//     UNCHANGED RecvSeq (acking a probe as consumed reads as a protocol error:
//     NT aborts the session after ~9 probes, client error 59). Ground truth
//     (ipx.pcap 2026-07-10 frames 488-509, WfW client ↔ NT server): WfW's
//     bare-SYS 0x80 ack (seq 4) did not consume — its next data frame reused
//     seq 4 — while its SESSION_END (0x40, seq 5) did (NT's end-ack said
//     RecvSeq 6).
//   - RecvSeq is the cumulative acknowledgment: the next SendSeq the sender expects
//     from its peer. A data frame whose SendSeq or RecvSeq contradicts the peer's
//     counters is DISCARDED and answered with SYS|RESEND (RecvSeq = resend-from);
//     mirroring the client's SendSeq back (what this engine originally did) reads
//     as "server data frame 0 was lost" and deadlocks the circuit.
//   - BytesReceived is the RECEIVE-WINDOW EDGE: RecvSeq + the number of frames
//     the sender is prepared to accept (the highest peer SendSeq acceptable,
//     plus one). NT-as-server advertises RecvSeq+5 on every frame (accept = 6,
//     then 7/8/9/10 as it consumes); WfW advertises +3. An NT CLIENT will not
//     transmit data while the peer's advertised edge is below its next send
//     sequence: it polls with a zero-data SYS|ACK probe (0xC0, SendSeq 1) every
//     ~600ms, and each probe MUST be answered with a zero-data SYS frame whose
//     BytesReceived opens the window (RecvSeq unchanged). Unanswered, NT retries
//     ~7x and drops the session; answered with a zero window, it re-probes until
//     the client errors with 240 "session cancelled". Win9x/WfW clients ignore
//     the field (they transmit regardless and accept 0 from us).
type NBIPXSessionHeader struct {
	ConnCtrlFlag   uint8 // SYS|ACK|ATT|EOM bitfield
	DataStreamType uint8 // NBIPXFindName, NBIPXSessionInit, ...
	SourceConnID   uint16
	DestConnID     uint16
	SendSeq        uint16
	TotalDataLen   uint16
	Offset         uint16
	DataLen        uint16
	RecvSeq        uint16 // receive sequence number (was mis-modelled as ConnCtrlByte+Reserved)
	BytesReceived  uint16
}

// EncodeSessionHeader serialises an NB-IPX session header (18 bytes, LE). Callers
// typically build a single `[header || payload]` buffer.
func EncodeSessionHeader(h *NBIPXSessionHeader) []byte {
	out := make([]byte, NBIPXSessionHeaderLen)
	out[0] = h.ConnCtrlFlag
	out[1] = h.DataStreamType
	bp.PutLE16(out[2:4], h.SourceConnID)
	bp.PutLE16(out[4:6], h.DestConnID)
	bp.PutLE16(out[6:8], h.SendSeq)
	bp.PutLE16(out[8:10], h.TotalDataLen)
	bp.PutLE16(out[10:12], h.Offset)
	bp.PutLE16(out[12:14], h.DataLen)
	bp.PutLE16(out[14:16], h.RecvSeq)
	bp.PutLE16(out[16:18], h.BytesReceived)
	return out
}

// DecodeSessionHeader parses the first 18 bytes of an NB-IPX session payload.
func DecodeSessionHeader(b []byte) (*NBIPXSessionHeader, error) {
	if len(b) < NBIPXSessionHeaderLen {
		return nil, ErrShortNBIPX
	}
	return &NBIPXSessionHeader{
		ConnCtrlFlag:   b[0],
		DataStreamType: b[1],
		SourceConnID:   bp.LE16(b[2:4]),
		DestConnID:     bp.LE16(b[4:6]),
		SendSeq:        bp.LE16(b[6:8]),
		TotalDataLen:   bp.LE16(b[8:10]),
		Offset:         bp.LE16(b[10:12]),
		DataLen:        bp.LE16(b[12:14]),
		RecvSeq:        bp.LE16(b[14:16]),
		BytesReceived:  bp.LE16(b[16:18]),
	}, nil
}

// NBIPXSessionRequestNameLen is the two 16-byte NetBIOS names that prefix a
// session-request / session-accept DATA payload on the wire.
const NBIPXSessionRequestNameLen = 2 * NameLength

// NBIPXSessionRequest is the payload carried by the DATA frame that OPENS an NB-IPX
// circuit (DestConnID == NBIPXUnassignedConnID) and by the DATA frame that ACCEPTS
// it: two 16-byte NetBIOS names followed by an opaque capability trailer.
//
//	 0:16  Source      — the sender's own name
//	16:32  Destination — the name being called
//	32:    Trailer     — capability bytes ([max frame data LE16][timer][timer]),
//	                     retained and echoed verbatim by the responder
//
// ERRATA: the name order is [SOURCE][DESTINATION] — each sender names ITSELF first.
// Golden capture spec/captures/nbipx-win98.pcap frame 65 (WIN98-2 → WIN98-1) carries
// "WIN98-2"<00> then "WIN98-1"<20>, and the matching accept (frame 66) carries
// "WIN98-1"<20> then "WIN98-2"<00>. Both sides of this stack used to read/write the
// pair as [called][calling], i.e. exactly inverted; because they agreed with each
// other the in-process e2e passed while no real NWLink peer would ever answer, and a
// broadcast SESSION_INITIALIZE read as addressed to our own workstation name was
// silently dropped by Win98. The layout lives here so neither side can re-invert it.
type NBIPXSessionRequest struct {
	Source      Name
	Destination Name
	Trailer     []byte
}

// Encode serialises the session-request/accept payload ([source][destination][trailer]).
func (r *NBIPXSessionRequest) Encode() []byte {
	out := make([]byte, 0, NBIPXSessionRequestNameLen+len(r.Trailer))
	out = append(out, r.Source[:]...)
	out = append(out, r.Destination[:]...)
	return append(out, r.Trailer...)
}

// DecodeSessionRequest parses a session-request/accept payload — the bytes AFTER the
// 18-byte NB-IPX session header. Trailer aliases b (the caller owns b for the dispatch
// lifetime), matching how the responder echoes it straight back.
func DecodeSessionRequest(b []byte) (*NBIPXSessionRequest, error) {
	if len(b) < NBIPXSessionRequestNameLen {
		return nil, ErrShortNBIPX
	}
	r := &NBIPXSessionRequest{Trailer: b[NBIPXSessionRequestNameLen:]}
	copy(r.Source[:], b[:NameLength])
	copy(r.Destination[:], b[NameLength:NBIPXSessionRequestNameLen])
	return r, nil
}

// Accept returns the session-accept payload answering this request: the names swapped
// so the RESPONDER is again the source, with the caller's trailer preserved verbatim
// (golden capture spec/captures/nbipx-win98.pcap frame 66).
func (r *NBIPXSessionRequest) Accept() *NBIPXSessionRequest {
	return &NBIPXSessionRequest{Source: r.Destination, Destination: r.Source, Trailer: r.Trailer}
}

const (
	NBIPXWANRouterCount       = 8
	NBIPXWANRouterBytes       = 4 * NBIPXWANRouterCount
	NBIPXNameServiceHeaderLen = 2 // NameTypeFlag + DataStreamType
	NBIPXNameServiceLen       = NBIPXWANRouterBytes + NBIPXNameServiceHeaderLen + NameLength
	NMPIFixedHeaderLen        = NBIPXWANRouterBytes + 1 + 1 + 2 + NameLength + NameLength
)

// NMPI opcodes used on sockets 0x0551/0x0553.
const (
	NMPIOpNameClaim    uint8 = 0xF1
	NMPIOpNameDelete   uint8 = 0xF2
	NMPIOpNameQuery    uint8 = 0xF3
	NMPIOpNameFound    uint8 = 0xF4
	NMPIOpMsgHangup    uint8 = 0xF5
	NMPIOpMailslotSend uint8 = 0xFC
	NMPIOpMailslotFind uint8 = 0xFD
	NMPIOpMailslotName uint8 = 0xFE
)

const (
	NMPINameTypeMachine   uint8 = 0x01
	NMPINameTypeWorkgroup uint8 = 0x02
	NMPINameTypeBrowser   uint8 = 0x03
)

// NMPIPacket is the Name Management Protocol over IPX payload used by browser
// mailslot and name-query traffic on sockets 0x0551/0x0553.
type NMPIPacket struct {
	Routers       [NBIPXWANRouterCount][4]byte
	Opcode        uint8
	NameType      uint8
	MessageID     uint16 // little-endian on wire
	RequestedName Name
	SourceName    Name
	Payload       []byte
}

// EncodeNMPIPacket serialises an NMPI packet (52-byte fixed header + payload).
func EncodeNMPIPacket(p *NMPIPacket) []byte {
	out := make([]byte, NMPIFixedHeaderLen+len(p.Payload))
	off := 0
	for i := range NBIPXWANRouterCount {
		copy(out[off:off+4], p.Routers[i][:])
		off += 4
	}
	out[off] = p.Opcode
	off++
	out[off] = p.NameType
	off++
	bp.PutLE16(out[off:off+2], p.MessageID)
	off += 2
	copy(out[off:off+NameLength], p.RequestedName[:])
	off += NameLength
	copy(out[off:off+NameLength], p.SourceName[:])
	off += NameLength
	copy(out[off:], p.Payload)
	return out
}

// DecodeNMPIPacket parses an NMPI packet (52-byte header + optional payload).
func DecodeNMPIPacket(b []byte) (*NMPIPacket, error) {
	if len(b) < NMPIFixedHeaderLen {
		return nil, ErrShortNBIPX
	}
	var p NMPIPacket
	off := 0
	for i := range NBIPXWANRouterCount {
		copy(p.Routers[i][:], b[off:off+4])
		off += 4
	}
	p.Opcode = b[off]
	off++
	p.NameType = b[off]
	off++
	p.MessageID = bp.LE16(b[off : off+2])
	off += 2
	copy(p.RequestedName[:], b[off:off+NameLength])
	off += NameLength
	copy(p.SourceName[:], b[off:off+NameLength])
	off += NameLength
	p.Payload = make([]byte, len(b)-off)
	copy(p.Payload, b[off:])
	return &p, nil
}

// NBIPXNameServicePacket is the body carried inside an IPX type-20 WAN-broadcast
// name packet:
//
//	32 bytes: 8 router network numbers (4 bytes each)
//	1 byte:  NameTypeFlag
//	1 byte:  DataStreamType
//	16 bytes: NetBIOS name
//
// Router entries are zero-filled for same-segment broadcasts.
//
// ERRATA (captures/ipx.pcap, Win98 NWLink): on a NAME_RECOGNIZED **reply** the
// leading 32-byte area is NOT a zero-filled router list — the real client fills it
// with a self-identifying prefix the querier validates before it proceeds to
// SESSION_INITIALIZE. Observed layout of that 32-byte prefix (frames 40/54, byte-
// identical regardless of the queried name):
//
//	[0]     0x10          leading status flag
//	[1]     0x02          DataStreamType (NAME_RECOGNIZED, echoed)
//	[2:18]  responder own NetBIOS name (16B, suffix 0x00 = unique/workstation)
//	[18:32] responder workgroup (14 bytes, space-padded)
//
// then the usual [32]=NameTypeFlag [33]=DataStreamType [34:50]=queried name. A
// same-segment FIND.NAME *query* / name-claim leaves the prefix effectively unused
// (the querier does not validate it), so EncodeNameService keeps zero-filling it;
// EncodeNameRecognized fills it. The status flag on a positive reply is 0x44
// (In-use 0x40 | Registered 0x04); a bare zero here is what made our earlier reply
// be ignored (the client never sent SESSION_INITIALIZE). See spec/errata.md.
type NBIPXNameServicePacket struct {
	Routers        [NBIPXWANRouterCount][4]byte
	NameTypeFlag   uint8
	DataStreamType uint8
	Name           Name
}

// Name-service leading-prefix constants (the 32-byte area a NAME_RECOGNIZED reply
// fills; see NBIPXNameServicePacket ERRATA). Offsets are within the name-service
// body (after the IPX header).
const (
	NBIPXNameRecogLeadStatus uint8 = 0x10 // reply prefix byte 0
	// NBIPXNameRecogNameFlag is the [32] NameTypeFlag on a positive reply:
	// In-use (0x40) | Registered (0x04). A zero here makes the client ignore the
	// reply (no SESSION_INITIALIZE follows).
	NBIPXNameRecogNameFlag   uint8 = 0x44
	nbipxNameRecogOwnNameOff       = 2                                              // own-name offset in the 32-byte prefix
	nbipxNameRecogWorkgrpOff       = 2 + NameLength                                 // workgroup offset (== 18)
	nbipxNameRecogWorkgrpLen       = NBIPXWANRouterBytes - nbipxNameRecogWorkgrpOff // 14 bytes
)

// EncodeNameRecognized serialises a NAME_RECOGNIZED (0x02) reply carrying the
// self-identifying leading prefix a Win98 NWLink client validates before it opens a
// session: [0x10][0x02][own-name:16][workgroup:14] then [0x44][0x02][queried-name:16].
// own is the responder's own NetBIOS name (workstation form); workgroup is the
// responder's workgroup (space-padded/truncated to 14 bytes); queried is the name the
// client asked to resolve (echoed in the trailing name field). The result is the same
// 50-byte length as EncodeNameService, sent as an IPX type-4 (PEP) datagram — NOT
// type-20 — matching the observed reply. See NBIPXNameServicePacket ERRATA.
func EncodeNameRecognized(own Name, workgroup string, queried Name) []byte {
	out := make([]byte, NBIPXNameServiceLen)
	out[0] = NBIPXNameRecogLeadStatus
	out[1] = NBIPXNameRecognized
	copy(out[nbipxNameRecogOwnNameOff:nbipxNameRecogOwnNameOff+NameLength], own[:])
	wg := padName(workgroup, nbipxNameRecogWorkgrpLen)
	copy(out[nbipxNameRecogWorkgrpOff:nbipxNameRecogWorkgrpOff+nbipxNameRecogWorkgrpLen], wg)
	out[NBIPXWANRouterBytes] = NBIPXNameRecogNameFlag // [32]
	out[NBIPXWANRouterBytes+1] = NBIPXNameRecognized  // [33]
	copy(out[NBIPXWANRouterBytes+2:], queried[:])     // [34:50]
	return out
}

// padName upper-cases, space-pads and truncates s to exactly n bytes, matching how a
// NetBIOS name/workgroup rides the wire (space-filled, no NUL terminator).
func padName(s string, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = ' '
	}
	up := []byte(toUpperASCII(s))
	if len(up) > n {
		up = up[:n]
	}
	copy(b, up)
	return b
}

// toUpperASCII upper-cases the ASCII letters of s (NetBIOS names are upper-cased on
// the wire); non-letters pass through. Avoids a strings import in the protocol ring.
func toUpperASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - ('a' - 'A')
		}
	}
	return string(b)
}

// EncodeNameService serialises a name-service body to the canonical 50-byte
// WAN-broadcast form. The IPX header (Type=20) is the caller's job.
func EncodeNameService(p *NBIPXNameServicePacket) []byte {
	out := make([]byte, NBIPXNameServiceLen)
	off := 0
	for i := range NBIPXWANRouterCount {
		copy(out[off:off+4], p.Routers[i][:])
		off += 4
	}
	out[off] = p.NameTypeFlag
	off++
	out[off] = p.DataStreamType
	off++
	copy(out[off:off+NameLength], p.Name[:])
	return out
}

// DecodeNameService parses a name-service body. It accepts both the canonical
// 50-byte WAN-broadcast form and the legacy 16-byte name-only form.
func DecodeNameService(b []byte) (*NBIPXNameServicePacket, error) {
	if len(b) < NameLength {
		return nil, ErrShortNBIPX
	}
	var p NBIPXNameServicePacket
	if len(b) >= NBIPXNameServiceLen {
		off := 0
		for i := range NBIPXWANRouterCount {
			copy(p.Routers[i][:], b[off:off+4])
			off += 4
		}
		p.NameTypeFlag = b[off]
		off++
		p.DataStreamType = b[off]
		off++
		copy(p.Name[:], b[off:off+NameLength])
		return &p, nil
	}
	// Legacy: payload carried only the 16-byte NetBIOS name.
	p.DataStreamType = NBIPXFindName
	copy(p.Name[:], b[:NameLength])
	return &p, nil
}

// ErrShortNBIPX indicates an NB-IPX packet body too short to contain the header
// (or, for name-service packets, the name).
var ErrShortNBIPX = errors.New("netbios: short NB-IPX packet")
