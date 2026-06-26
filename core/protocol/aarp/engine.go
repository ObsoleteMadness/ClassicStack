package aarp

// engine.go is the pure AARP decision core: node-address claim, address resolution, and
// inbound packet handling over the AMT. It owns NO I/O, goroutines, or timers — the
// adapter (the EtherTalk AARP framer) supplies the wire and drives the probe/retransmit
// timing, feeding inbound packets to Inbound and sending the packets the engine returns.
// This keeps the protocol logic deterministic and table-testable, and TinyGo-clean
// (matching the core/protocol/ddp + core/service/rtmp Age(now) discipline).
//
// Lifecycle the adapter drives:
//   - Claim: BeginProbe(tentative); repeatedly NextProbe() to get a probe packet to send,
//     waiting the probe interval between sends; feed every inbound packet to Inbound,
//     which sets claimConflict when the tentative address is in use; after the configured
//     probe count with no conflict, AcceptTentative() promotes it to the claimed address.
//   - Resolve: Resolve(addr) → (hw, ok) from the AMT; on a miss, StartResolve(addr)
//     returns the Request packet(s) to broadcast and queues the resolve; Tick(now)
//     returns retransmits and ages out the AMT + stale resolves.
//   - Inbound: Inbound(packet, now) → (replies, claimConflict): gleans (non-Probe),
//     answers Requests for our claimed address, resolves pending entries from Replies,
//     deletes AMT entries on a Probe, and reports a claim conflict.

// Config tunes the engine. Zero fields take the defaults (Linux net/appletalk: 10
// probes/requests at ~100ms; we keep the count here and the interval in the adapter).
type Config struct {
	// HardwareAddr is this station's 6-byte Ethernet MAC, stamped as the sender on
	// every probe/request/reply.
	HardwareAddr [6]byte
	// ProbeCount is how many probes a claim sends before accepting (0 → DefaultProbeCount).
	ProbeCount int
	// ResolveRetransmits is how many Requests a resolve sends before giving up (0 →
	// DefaultResolveRetransmits).
	ResolveRetransmits int
	// AMTTTL / AMTMaxEntries tune the table (0 → AMT defaults).
	AMTTTL        int64
	AMTMaxEntries int
}

// DefaultProbeCount / DefaultResolveRetransmits mirror Linux AARP_RETRANSMIT_LIMIT.
const (
	DefaultProbeCount         = 10
	DefaultResolveRetransmits = 10
	defaultResolveInterval    = 1_000_000_000 // 1s between resolve retransmits (ns)
)

// claimState tracks the probe/claim progress.
type claimState uint8

const (
	claimIdle claimState = iota
	claimProbing
	claimDone
)

// pendingResolve tracks one in-flight address resolution awaiting a Reply.
type pendingResolve struct {
	addr     ProtoAddr
	sent     int   // requests sent so far
	lastSent int64 // UnixNano of the last request
}

// Engine is the pure AARP state machine. Build with NewEngine; it is single-goroutine
// (the adapter calls it from its read/claim/tick paths under the adapter's own lock).
type Engine struct {
	cfg Config
	amt *AMT

	// claim
	state      claimState
	tentative  ProtoAddr
	claimed    ProtoAddr
	probesLeft int
	conflict   bool

	// resolve
	pending map[ProtoAddr]*pendingResolve
}

// NewEngine builds an engine for a station with the given config.
func NewEngine(cfg Config) *Engine {
	if cfg.ProbeCount <= 0 {
		cfg.ProbeCount = DefaultProbeCount
	}
	if cfg.ResolveRetransmits <= 0 {
		cfg.ResolveRetransmits = DefaultResolveRetransmits
	}
	return &Engine{
		cfg:     cfg,
		amt:     NewAMT(cfg.AMTTTL, cfg.AMTMaxEntries),
		pending: make(map[ProtoAddr]*pendingResolve),
	}
}

// AMT exposes the table (diagnostics/tests).
func (e *Engine) AMT() *AMT { return e.amt }

// --- claim ---

// BeginProbe starts (or restarts) a node-claim for a tentative address: it clears any
// prior conflict and arms the probe counter. Call it to begin and again after a conflict
// with a freshly-picked tentative address.
func (e *Engine) BeginProbe(tentative ProtoAddr) {
	e.state = claimProbing
	e.tentative = tentative
	e.probesLeft = e.cfg.ProbeCount
	e.conflict = false
}

// NextProbe returns the next probe packet to send and whether one was produced. It
// decrements the remaining-probe counter; when none remain it returns ok=false and the
// adapter calls AcceptTentative (if no conflict was seen). A conflicted claim returns
// ok=false too (the adapter picks a new tentative and BeginProbe again).
func (e *Engine) NextProbe() (pkt []byte, ok bool) {
	if e.state != claimProbing || e.conflict || e.probesLeft <= 0 {
		return nil, false
	}
	e.probesLeft--
	return Probe(e.cfg.HardwareAddr, e.tentative).Encode(nil), true
}

// Conflicted reports whether the in-progress claim saw a conflict (an inbound packet
// using or probing our tentative address). The adapter checks this to decide between
// AcceptTentative and picking a new address.
func (e *Engine) Conflicted() bool { return e.conflict }

// AcceptTentative promotes the tentative address to the claimed address. The adapter
// calls it after the probes complete with no conflict. A conflicted or non-probing state
// is a no-op returning ok=false.
func (e *Engine) AcceptTentative() (ProtoAddr, bool) {
	if e.state != claimProbing || e.conflict {
		return ProtoAddr{}, false
	}
	e.claimed = e.tentative
	e.state = claimDone
	return e.claimed, true
}

// Claimed returns the accepted address and whether the claim has completed.
func (e *Engine) Claimed() (ProtoAddr, bool) { return e.claimed, e.state == claimDone }

// --- resolve ---

// Resolve returns the hardware address for addr from the AMT, or ok=false on a miss (the
// adapter then calls StartResolve).
func (e *Engine) Resolve(addr ProtoAddr) (hw [6]byte, ok bool) { return e.amt.Lookup(addr) }

// StartResolve begins resolving addr: it returns the Request packet to broadcast and
// queues the resolve for retransmit/aging. A resolve already in flight returns the next
// request without re-queuing. The source proto address is the claimed address (0/0 until
// claimed — still valid on the wire for a query).
func (e *Engine) StartResolve(addr ProtoAddr, now int64) []byte {
	pr, ok := e.pending[addr]
	if !ok {
		pr = &pendingResolve{addr: addr}
		e.pending[addr] = pr
	}
	pr.sent++
	pr.lastSent = now
	return Request(e.cfg.HardwareAddr, e.claimed, addr).Encode(nil)
}

// Tick advances time: it ages the AMT, retransmits any resolve whose interval has elapsed
// (giving up — dropping the pending resolve — after ResolveRetransmits), and returns the
// request packets to send now.
func (e *Engine) Tick(now int64) [][]byte {
	e.amt.Age(now)
	var out [][]byte
	for addr, pr := range e.pending {
		if now-pr.lastSent < defaultResolveInterval {
			continue
		}
		if pr.sent >= e.cfg.ResolveRetransmits {
			delete(e.pending, addr) // give up; the client retries later
			continue
		}
		pr.sent++
		pr.lastSent = now
		out = append(out, Request(e.cfg.HardwareAddr, e.claimed, addr).Encode(nil))
	}
	return out
}

// --- inbound ---

// Inbound processes one received AARP packet (the bytes AFTER the SNAP header) at time
// now. It returns the reply packets to send (a Reply when a Request/Probe targets our
// claimed address) and claimConflict=true when the packet collides with our in-progress
// tentative address. It also gleans mappings (from Request/Reply, never Probe), resolves
// pending entries from Replies, and deletes an AMT entry when a Probe is seen for it. A
// packet that does not decode is ignored (returns nil, false).
func (e *Engine) Inbound(payload []byte, now int64) (replies [][]byte, claimConflict bool) {
	p, err := Decode(payload)
	if err != nil {
		return nil, false
	}

	// Claim conflict: another node uses or is probing our tentative address. (A Probe
	// or any packet whose SOURCE is our tentative, or a Reply/Request targeting it.)
	if e.state == claimProbing {
		if p.SrcProto == e.tentative ||
			(p.Function == FuncProbe && p.TargetProto == e.tentative) {
			e.conflict = true
			claimConflict = true
		}
	}

	switch p.Function {
	case FuncProbe:
		// A probe for an address we have cached means that address may be changing
		// owners — drop the stale mapping (spec's probe-triggered aging). Do NOT glean
		// (the source is tentative). Defend our claimed address with a Reply.
		e.amt.Delete(p.SrcProto)
		if claimed, ok := e.Claimed(); ok && p.TargetProto == claimed {
			replies = append(replies, e.reply(p).Encode(nil))
		}
	case FuncRequest:
		// Glean the sender (reliable) and, if the request targets our claimed address,
		// answer it.
		e.amt.Glean(p.SrcProto, p.SrcHw, now)
		if claimed, ok := e.Claimed(); ok && p.TargetProto == claimed {
			replies = append(replies, e.reply(p).Encode(nil))
		}
	case FuncReply:
		// Glean the sender and complete any pending resolve for it.
		e.amt.Glean(p.SrcProto, p.SrcHw, now)
		delete(e.pending, p.SrcProto)
	}
	return replies, claimConflict
}

// reply builds the Reply to a Request/Probe that targeted our claimed address: our
// hw/proto as the source, the asker's hw/proto as the target.
func (e *Engine) reply(req Packet) Packet {
	return Reply(e.cfg.HardwareAddr, e.claimed, req.SrcHw, req.SrcProto)
}
