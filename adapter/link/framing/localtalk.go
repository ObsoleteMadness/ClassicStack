package framing

import (
	"errors"
	"math/rand"
	"strconv"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/llap"
)

// LocalTalk LLAP wire constants. The authoritative definitions live in the pure
// core/protocol/llap package (header length, type codes, broadcast node); these
// package-local aliases keep the existing framer body + tests reading the same names.
const (
	llapHdrLen = llap.HeaderLen // dest(1) + src(1) + type(1)

	// LLAP type codes carried in the third header byte.
	llapShortDDP = llap.TypeShortDDP // short-header DDP (intra-network; net numbers implicit)
	llapLongDDP  = llap.TypeLongDDP  // long-header DDP (inter-network; full DDP header)
	llapENQ      = llap.TypeENQ      // node-claim probe (control; no payload)
	llapACK      = llap.TypeACK      // node-claim response (control; no payload)

	llapBroadcastNode = llap.BroadcastNode // LLAP destination selecting every node on the segment

	// ddpShortHdrLen is the DDP short header: length(2) + destSocket(1) +
	// srcSocket(1) + ddpType(1) = 5 bytes (net numbers + nodes are implicit, taken
	// from the LLAP frame). The long header is ddp.headerLen (13), handled by the
	// core ddp codec.
	ddpShortHdrLen = 5

	// llapProbeInterval is the gap between node-claim ENQ probes (spec §"Acquisition
	// Algorithm": a 250ms timer tick).
	llapProbeInterval = 250 * time.Millisecond
)

var (
	// ErrShortLLAP is returned (and surfaced as a skipped frame) for a frame too
	// small to hold the 3-byte LLAP header.
	ErrShortLLAP = errors.New("framing: LocalTalk frame too short for LLAP header")
	// ErrLLAPControl marks an LLAP control frame (ENQ/ACK) — not a DDP
	// datagram. The read loop services it (node-claim) then skips it; it never
	// surfaces as a datagram.
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
// NODE-CLAIM: when EnableClaim is set (with a *LiveAddr Addr to publish into), the
// framer runs the LLAP ENQ/ACK probe-and-claim dance — the LocalTalk analogue of
// EtherTalk AARP — in a background goroutine started by Framing: it probes a
// candidate node, rerolls on a collision, and on success publishes the claimed
// node via the LiveAddr (src stamping) + the OnClaimed callback (compose wires that
// to port.SetAddress). The read loop services inbound ENQ/ACK (defending our node
// with an ACK when RespondToEnq is set, detecting collisions otherwise). Until a
// node is claimed (Addr reports node 0) the runport drops outbound.
//
// Without EnableClaim the framer is the plain stateless LLAP DDP framer (a fixed
// Addr, no goroutine): ReadDatagram still skips control frames, but no claim runs —
// the form tests and the inert-but-routed path use.
type LocalTalk struct {
	// Addr is the live node/network source. nil → unclaimed (net 0, node 0). When
	// EnableClaim is set this must be a *LiveAddr the claim goroutine Set()s.
	Addr Addr
	// CalcChecksum stamps a DDP checksum on outbound long-header frames when true
	// (the spec allows either; false matches the core ddp.Encode default of a zero
	// "checksum disabled" field).
	CalcChecksum bool

	// EnableClaim turns on the LLAP node-claim goroutine. It requires Live to be a
	// *LiveAddr (so the claimed node can be published back to the framer + port).
	EnableClaim bool
	// Live is the *LiveAddr shared with the port; the claim goroutine Set()s it once
	// a node is accepted. (Addr is set to this same value by the factory; Live is
	// kept typed so the claim goroutine can publish.)
	Live *LiveAddr
	// RespondToEnq makes a claimed segment answer an ENQ for our node with a
	// defending ACK — true for LToUDP (shared simulated segment), false for TashTalk
	// (the physical medium defends in hardware). Spec §"respondToEnq Flag".
	RespondToEnq bool
	// OnClaimed is called once a node is accepted, so compose can drive
	// port.SetAddress. nil is allowed (the LiveAddr update alone suffices for framing).
	OnClaimed func(network uint16, node uint8, netMin, netMax uint16)
	// SeedNetwork is the network the claimed node lives on, passed through to
	// OnClaimed (LocalTalk is non-extended: netMin==netMax==SeedNetwork). 0 until a
	// router teaches it via RTMP.
	SeedNetwork uint16
	// DesiredNode is the first node candidate to probe (0 → the spec default 0xFE).
	DesiredNode uint8
	// ProbeCount / ProbeInterval override the claim burst (0 → the spec defaults:
	// llap.DefaultProbeCount ENQs at llapProbeInterval). Tests set a small count +
	// interval to claim quickly.
	ProbeCount    int
	ProbeInterval time.Duration
	// RandNode supplies the engine's reroll RNG (a pseudo-random uint8); nil → a
	// default source so simultaneous routers diverge.
	RandNode func() uint8
	// Logger, when non-nil, narrates the LLAP node-claim (ENQ/ACK sent and received)
	// at Debug level. The claim dance is otherwise invisible except in a packet
	// capture, so this makes an ENQ storm / stuck claim diagnosable from the log
	// (spec/09 §"Node Address Acquisition"). nil disables the narration entirely.
	Logger log.Logger
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

// LiveAddr is a late-bound, concurrency-safe Addr: the framer needs an Addr at
// construction, but the live source (the LocalTalk port) only exists after the
// port is built — and its claimed node/network change over the port's life as
// node-claim completes. The compose factory builds the framer with a LiveAddr,
// constructs the port, then Set()s the port as the source. A LiveAddr with no
// source reports the unclaimed state (network 0, node 0), so a framer is safe to
// use before Set. Reads (Network/Node) run on the port read/write goroutines
// while Set runs once at wiring time, so the source pointer is guarded.
type LiveAddr struct {
	mu  sync.RWMutex
	src Addr
}

// Set binds the live source. A nil src reverts to the unclaimed state.
func (a *LiveAddr) Set(src Addr) {
	a.mu.Lock()
	a.src = src
	a.mu.Unlock()
}

// Network reports the source's network, or 0 when unbound.
func (a *LiveAddr) Network() uint16 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.src == nil {
		return 0
	}
	return a.src.Network()
}

// Node reports the source's node, or 0 when unbound.
func (a *LiveAddr) Node() uint8 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.src == nil {
		return 0
	}
	return a.src.Node()
}

var _ Addr = (*LiveAddr)(nil)

// Framing wraps a FrameLink as a DatagramLink doing LLAP DDP framing. It
// satisfies link.Framer. When EnableClaim is set it also starts the node-claim
// goroutine (which Set()s the Live address + calls OnClaimed on success) and the
// read loop services inbound ENQ/ACK; the call returns immediately (async claim,
// like the EtherTalk AARP framer).
func (e *LocalTalk) Framing(fl link.FrameLink) (link.DatagramLink, error) {
	if fl == nil {
		return nil, errors.New("framing: nil FrameLink")
	}
	d := &ltDatagramLink{fl: fl, addr: e.Addr, calcChecksum: e.CalcChecksum, logger: e.Logger}

	if e.EnableClaim && e.Live != nil {
		d.live = e.Live
		d.onClaimed = e.OnClaimed
		d.seedNetwork = e.SeedNetwork
		d.probeInterval = e.ProbeInterval
		if d.probeInterval <= 0 {
			d.probeInterval = llapProbeInterval
		}
		randNode := e.RandNode
		if randNode == nil {
			randNode = defaultLLAPRand
		}
		d.engine = llap.NewEngine(llap.Config{
			DesiredNode:  e.DesiredNode,
			ProbeCount:   e.ProbeCount,
			RespondToEnq: e.RespondToEnq,
			Rand:         randNode,
		})
		d.done = make(chan struct{})
		d.wg.Add(1)
		go d.claimLoop()
	}
	return d, nil
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
	logger       log.Logger // nil → no node-claim narration

	// Node-claim state (nil/zero when EnableClaim is off — the plain framer path).
	// The engine is touched by both the claim goroutine and the read loop's
	// serviceControl, so engineMu guards it.
	engineMu      sync.Mutex
	engine        *llap.Engine
	live          *LiveAddr
	onClaimed     func(uint16, uint8, uint16, uint16)
	seedNetwork   uint16
	probeInterval time.Duration

	done chan struct{}
	wg   sync.WaitGroup
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
// decoded ddp.Datagram. Control frames (ENQ/ACK) are serviced by the node-claim
// engine (defending our node / detecting collisions) and then skipped; non-DDP and
// malformed frames are skipped — surfaced to the caller only as the underlying
// link's ErrTimeout/ErrClosed.
func (d *ltDatagramLink) ReadDatagram() (ddp.Datagram, error) {
	for {
		frame, err := d.fl.Read()
		if err != nil {
			return ddp.Datagram{}, err
		}
		if _, _, typ, ok := llap.Header(frame); ok && llap.IsControl(typ) {
			d.serviceControl(frame)
			continue
		}
		dg, err := d.decode(frame)
		if err != nil {
			// Non-DDP or malformed: skip and keep reading.
			continue
		}
		return dg, nil
	}
}

// serviceControl feeds one inbound LLAP control frame (ENQ/ACK) to the claim engine
// and writes back any defending ACK. With no claim engine (plain framer) it is a
// no-op — the frame is simply skipped, the historical behaviour.
func (d *ltDatagramLink) serviceControl(frame []byte) {
	if d.engine == nil {
		return
	}
	c, err := llap.DecodeControl(frame)
	if err != nil {
		return
	}
	d.logControl("LLAP rx", c)
	d.engineMu.Lock()
	reply, hasReply, _ := d.engine.Inbound(c)
	d.engineMu.Unlock()
	if hasReply {
		d.logControl("LLAP tx", reply)
		_ = d.fl.Write(llap.EncodeControl(reply))
	}
}

// logControl narrates one LLAP control (ENQ/ACK) frame at Debug: the direction
// (dir, e.g. "LLAP rx"/"LLAP tx"), the frame type name, and its dst/src nodes. It
// is the only visibility into the node-claim dance outside a packet capture, so a
// stuck claim or an ENQ storm shows up in the log. A nil logger (or a level no sink
// wants) costs nothing.
func (d *ltDatagramLink) logControl(dir string, c llap.ControlFrame) {
	if d.logger == nil || !d.logger.Enabled(log.Debug) {
		return
	}
	d.logger.Log(log.Debug, dir,
		log.Str("type", llapTypeName(c.Type)),
		log.Int("dst", int64(c.Dst)),
		log.Int("src", int64(c.Src)))
}

// llapTypeName renders an LLAP control type byte for the log; unknown types show
// the raw value so nothing is hidden.
func llapTypeName(typ uint8) string {
	switch typ {
	case llapENQ:
		return "ENQ"
	case llapACK:
		return "ACK"
	default:
		return "0x" + strconv.FormatUint(uint64(typ), 16)
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

// Close stops the claim goroutine (if any) and closes the link.
func (d *ltDatagramLink) Close() error {
	if d.done != nil {
		select {
		case <-d.done:
		default:
			close(d.done)
		}
	}
	err := d.fl.Close()
	d.wg.Wait()
	return err
}

// claimLoop runs the LLAP node-address acquisition: probe the candidate node with
// ENQs, reroll on a collision the read loop reported, and on success publish the
// claimed node via the LiveAddr + OnClaimed. It exits on success or Close. Mirrors
// the EtherTalk AARP claimLoop.
func (d *ltDatagramLink) claimLoop() {
	defer d.wg.Done()
	for {
		d.engineMu.Lock()
		d.engine.BeginProbe()
		d.engineMu.Unlock()

		if d.probeBurst() {
			return // claimed or closed
		}
		// conflict → loop; the engine already rerolled to a fresh candidate
	}
}

// probeBurst sends the ENQ burst for the current candidate. It returns true when the
// node is accepted (claim done, published) or the link closes; false on a collision
// (the caller re-arms with the rerolled candidate). The collision is detected by the
// read loop's serviceControl feeding the engine, so this just observes Conflicted().
func (d *ltDatagramLink) probeBurst() bool {
	for {
		d.engineMu.Lock()
		enq, ok := d.engine.NextProbe()
		conflicted := d.engine.Conflicted()
		d.engineMu.Unlock()

		if conflicted {
			return false
		}
		if !ok {
			// Burst complete with no collision → claim the candidate.
			d.engineMu.Lock()
			node, accepted := d.engine.AcceptTentative()
			d.engineMu.Unlock()
			if accepted {
				d.publishClaim(node)
			}
			return true
		}
		d.logControl("LLAP tx", enq)
		_ = d.fl.Write(llap.EncodeControl(enq))

		select {
		case <-d.done:
			return true
		case <-time.After(d.probeInterval):
		}
	}
}

// publishClaim records the claimed node into the LiveAddr (so the framer stamps it
// as the LLAP source) and notifies compose via OnClaimed (so the port's SetAddress
// runs). LocalTalk is non-extended, so the network range passed is SeedNetwork for
// both min and max (0 until a router teaches it via RTMP).
func (d *ltDatagramLink) publishClaim(node uint8) {
	if d.live != nil {
		d.live.Set(NewStaticAddr(d.seedNetwork, node))
	}
	if d.onClaimed != nil {
		d.onClaimed(d.seedNetwork, node, d.seedNetwork, d.seedNetwork)
	}
}

// defaultLLAPRand is the engine's reroll RNG when none is injected.
func defaultLLAPRand() uint8 { return uint8(rand.Intn(256)) }

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
