package framing

import (
	"errors"

	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
)

// LocalTalk LLAP wire constants (spec/09-port-localtalk-base.md §"LLAP Frame
// Format"). An LLAP frame is a 3-byte header — destination node, source node,
// type — followed (for DDP types only) by a short- or long-header DDP datagram.
const (
	llapHdrLen = 3 // dest(1) + src(1) + type(1)

	// LLAP type codes carried in the third header byte.
	llapShortDDP = 0x01 // short-header DDP (intra-network; net numbers implicit)
	llapLongDDP  = 0x02 // long-header DDP (inter-network; full DDP header)
	llapENQ      = 0x81 // node-claim probe (control; no payload)
	llapACK      = 0x82 // node-claim response (control; no payload)

	llapBroadcastNode = 0xFF // LLAP destination selecting every node on the segment

	// ddpShortHdrLen is the DDP short header: length(2) + destSocket(1) +
	// srcSocket(1) + ddpType(1) = 5 bytes (net numbers + nodes are implicit, taken
	// from the LLAP frame). The long header is ddp.headerLen (13), handled by the
	// core ddp codec.
	ddpShortHdrLen = 5
)

var (
	// ErrShortLLAP is returned (and surfaced as a skipped frame) for a frame too
	// small to hold the 3-byte LLAP header.
	ErrShortLLAP = errors.New("framing: LocalTalk frame too short for LLAP header")
	// ErrLLAPControl marks an LLAP control frame (ENQ/ACK/RTS/CTS) — not a DDP
	// datagram. The read loop skips it; node-claim is handled elsewhere (deferred,
	// like EtherTalk AARP).
	ErrLLAPControl = errors.New("framing: LocalTalk LLAP control frame (no DDP)")
	// ErrShortDDPHeader is returned for a short-header payload below the minimum.
	ErrShortDDPHeader = errors.New("framing: LocalTalk short-header DDP payload too short")
)

// Addr supplies the LocalTalk port's live claimed network number and node
// address. The framer reads it for the TWO things the wire genuinely needs from
// port state and that are NOT already in the datagram:
//
//   - the LLAP SOURCE node to stamp on every outbound frame (the port's own
//     claimed node), and
//   - the NETWORK number to reconstruct an inbound SHORT-header datagram, whose
//     header omits the network by definition (it is implicitly the receiving
//     segment's).
//
// It does NOT use Addr to decide short- vs long-header: that is a ROUTING
// decision the AppleTalk router already made when it chose this port and set the
// datagram's Dest/SrcNetwork (router.Route → port.Unicast/Broadcast). The framer
// reads those datagram fields rather than re-judging the network against the port
// — the router is the authority on intra- vs inter-network, not the framer. A nil
// Addr behaves as the unclaimed state (network 0, node 0).
type Addr interface {
	Network() uint16
	Node() uint8
}

// LocalTalk is a link.Framer that wraps DDP datagrams in LLAP and unwraps them,
// for the LocalTalk transports (LToUDP, TashTalk, virtual). Unlike the stateless
// Ethernet/SNAP framer, the short-vs-long header decision and the inbound
// short-header network/node stamping both depend on the port's claimed address,
// so it reads that via Addr.
//
// SCOPE: DDP-data frames (short 0x01 / long 0x02) are real. LLAP control frames
// (ENQ/ACK node-claim) are NOT produced or consumed here — ReadDatagram skips
// them and node acquisition is deferred (the EtherTalk-AARP analogue), matching
// the M3 deferral. Until a node is claimed (Addr reports node 0) the port has no
// address; the runport drops outbound until SetAddress runs.
type LocalTalk struct {
	// Addr is the live node/network source. nil → unclaimed (net 0, node 0).
	Addr Addr
	// CalcChecksum stamps a DDP checksum on outbound long-header frames when true
	// (the spec allows either; false matches the core ddp.Encode default of a zero
	// "checksum disabled" field).
	CalcChecksum bool
}

// staticAddr is a trivial Addr for a fixed network/node (tests, or a port that
// has already claimed). NewStaticAddr wraps a literal pair.
type staticAddr struct {
	net  uint16
	node uint8
}

func (s staticAddr) Network() uint16 { return s.net }
func (s staticAddr) Node() uint8     { return s.node }

// NewStaticAddr returns an Addr reporting a fixed network/node.
func NewStaticAddr(network uint16, node uint8) Addr { return staticAddr{net: network, node: node} }

// Framing wraps a FrameLink as a DatagramLink doing LLAP DDP framing. It
// satisfies link.Framer.
func (e *LocalTalk) Framing(fl link.FrameLink) (link.DatagramLink, error) {
	if fl == nil {
		return nil, errors.New("framing: nil FrameLink")
	}
	return &ltDatagramLink{fl: fl, addr: e.Addr, calcChecksum: e.CalcChecksum}, nil
}

// Compile-time assertions.
var (
	_ link.Framer       = (*LocalTalk)(nil)
	_ link.DatagramLink = (*ltDatagramLink)(nil)
)

type ltDatagramLink struct {
	fl           link.FrameLink
	addr         Addr
	calcChecksum bool
}

// network/node read the live claimed address (0/0 when unclaimed).
func (d *ltDatagramLink) network() uint16 {
	if d.addr == nil {
		return 0
	}
	return d.addr.Network()
}

func (d *ltDatagramLink) node() uint8 {
	if d.addr == nil {
		return 0
	}
	return d.addr.Node()
}

// ReadDatagram reads frames until one is an LLAP DDP datagram, then returns the
// decoded ddp.Datagram. Control frames (ENQ/ACK) and malformed frames are skipped
// — surfaced to the caller only as the underlying link's ErrTimeout/ErrClosed.
func (d *ltDatagramLink) ReadDatagram() (ddp.Datagram, error) {
	for {
		frame, err := d.fl.Read()
		if err != nil {
			return ddp.Datagram{}, err
		}
		dg, err := d.decode(frame)
		if err != nil {
			// Control frame, non-DDP, or malformed: skip and keep reading.
			continue
		}
		return dg, nil
	}
}

// WriteDatagram encodes dg as an LLAP DDP frame and writes it. Per spec
// §"Outbound Frame Sending": an intra-network datagram (same src/dst network, and
// that network is 0 or this port's) uses the short header; otherwise the long
// header. The LLAP source node is this port's claimed node.
func (d *ltDatagramLink) WriteDatagram(dg ddp.Datagram) error {
	frame, err := d.encode(dg)
	if err != nil {
		return err
	}
	return d.fl.Write(frame)
}

func (d *ltDatagramLink) Close() error { return d.fl.Close() }

// encode builds an LLAP frame carrying dg. It chooses short vs long per the
// intra-network test, stamps the LLAP dst node from dg.DestNode (0xFF broadcast)
// and the src node from the claimed address.
func (d *ltDatagramLink) encode(dg ddp.Datagram) ([]byte, error) {
	srcNode := d.node()
	dstNode := dg.DestNode
	if dstNode == 0 {
		dstNode = llapBroadcastNode
	}

	if d.useShortHeader(dg) {
		payload, err := encodeShortDDP(dg)
		if err != nil {
			return nil, err
		}
		return appendLLAP(dstNode, srcNode, llapShortDDP, payload), nil
	}

	payload, err := dg.Encode(nil)
	if err != nil {
		return nil, err
	}
	if d.calcChecksum {
		stampChecksum(payload)
	}
	return appendLLAP(dstNode, srcNode, llapLongDDP, payload), nil
}

// useShortHeader reports whether dg should use the short LLAP header. The choice
// follows entirely from the datagram the ROUTER produced (spec §"Outbound Frame
// Sending"): short header iff the traffic is intra-network — source and
// destination on the same network, with that network unspecified (0, segment-local
// — including a broadcast the router emits with no network) or a concrete shared
// number. The router set Dest/SrcNetwork when it routed to this port, so this is
// reading its decision, NOT re-deriving it against the port's own network.
func (d *ltDatagramLink) useShortHeader(dg ddp.Datagram) bool {
	return dg.DestNetwork == dg.SrcNetwork
}

// decode parses an LLAP frame into a ddp.Datagram, returning ErrLLAPControl for a
// control frame and an error for anything malformed/non-DDP.
func (d *ltDatagramLink) decode(frame []byte) (ddp.Datagram, error) {
	if len(frame) < llapHdrLen {
		return ddp.Datagram{}, ErrShortLLAP
	}
	dstNode := frame[0]
	srcNode := frame[1]
	typ := frame[2]
	payload := frame[llapHdrLen:]

	switch typ {
	case llapLongDDP:
		// Long header carries the full DDP datagram; the core codec validates it
		// (length + optional checksum).
		return ddp.Decode(payload)
	case llapShortDDP:
		// Short header omits net numbers + node addresses; reconstruct them from the
		// LLAP frame (nodes) and this port's claimed network.
		return decodeShortDDP(d.network(), dstNode, srcNode, payload)
	case llapENQ, llapACK:
		return ddp.Datagram{}, ErrLLAPControl
	default:
		return ddp.Datagram{}, errors.New("framing: unknown LLAP type")
	}
}

// appendLLAP prepends the 3-byte LLAP header to payload, returning a fresh frame.
func appendLLAP(dstNode, srcNode, typ uint8, payload []byte) []byte {
	out := make([]byte, 0, llapHdrLen+len(payload))
	out = append(out, dstNode, srcNode, typ)
	out = append(out, payload...)
	return out
}

// encodeShortDDP renders dg's short-header form: length(2) + destSocket + srcSocket
// + ddpType + data. Net numbers and node addresses are NOT included (they ride in
// the LLAP header). Mirrors the legacy AsShortHeaderBytes.
func encodeShortDDP(dg ddp.Datagram) ([]byte, error) {
	if len(dg.Data) > ddp.MaxDataLength {
		return nil, ddp.ErrTooLong
	}
	length := ddpShortHdrLen + len(dg.Data)
	out := make([]byte, 0, length)
	out = append(out,
		uint8((length&0x300)>>8),
		uint8(length&0xFF),
		dg.DestSocket,
		dg.SrcSocket,
		dg.DDPType,
	)
	out = append(out, dg.Data...)
	return out, nil
}

// decodeShortDDP reconstructs a ddp.Datagram from a short-header payload, taking
// the network from the port (intra-network) and the node addresses from the LLAP
// frame. Mirrors the legacy DatagramFromShortHeaderBytes.
func decodeShortDDP(network uint16, dstNode, srcNode uint8, payload []byte) (ddp.Datagram, error) {
	if len(payload) < ddpShortHdrLen {
		return ddp.Datagram{}, ErrShortDDPHeader
	}
	if payload[0]&0xFC != 0 {
		return ddp.Datagram{}, ErrShortDDPHeader
	}
	length := int(payload[0]&0x03)<<8 | int(payload[1])
	if length != len(payload) || length > ddpShortHdrLen+ddp.MaxDataLength {
		return ddp.Datagram{}, ErrShortDDPHeader
	}
	return ddp.Datagram{
		DestNetwork: network,
		SrcNetwork:  network,
		DestNode:    dstNode,
		SrcNode:     srcNode,
		DestSocket:  payload[2],
		SrcSocket:   payload[3],
		DDPType:     payload[4],
		Data:        payload[ddpShortHdrLen:],
	}, nil
}

// stampChecksum writes the AppleTalk DDP checksum over the long-header body
// (everything after the 4-byte length+checksum prefix) into bytes 2..4. ddp.Encode
// leaves a zero ("disabled") checksum; this overwrites it when CalcChecksum is set.
func stampChecksum(longHeader []byte) {
	if len(longHeader) <= 4 {
		return
	}
	sum := ddpChecksum(longHeader[4:])
	longHeader[2] = byte(sum >> 8)
	longHeader[3] = byte(sum)
}

// ddpChecksum mirrors the AppleTalk DDP checksum (core ddp keeps its copy
// unexported); it is the rotate-add over the post-checksum bytes.
func ddpChecksum(data []byte) uint16 {
	var v uint16
	for _, b := range data {
		v += uint16(b)
		v = (v&0x7FFF)<<1 | (v>>15)&1
	}
	if v == 0 {
		return 0xFFFF
	}
	return v
}
