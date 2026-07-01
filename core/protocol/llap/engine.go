package llap

// engine.go is the pure LLAP node-claim decision core: the ENQ/ACK probe-and-claim
// state machine (spec/09-port-localtalk-base.md §"Node Address Acquisition"). It owns
// NO I/O, goroutines, or timers — the adapter (the LocalTalk framer) drives the probe
// tick, calls NextProbe to get an ENQ to send, feeds every inbound control frame to
// Inbound, and after the probe burst completes with no conflict calls AcceptTentative
// to claim. This mirrors the core/protocol/aarp.Engine split.
//
// Lifecycle the adapter drives:
//   - Claim: BeginProbe() arms the probe counter for the current candidate node;
//     repeatedly NextProbe() yields an ENQ to send, waiting the probe interval between
//     sends; feed every inbound ENQ/ACK to Inbound, which sets claimConflict (and
//     rerolls to a new candidate) when a peer is using/probing our candidate; after the
//     configured probe count with no conflict, AcceptTentative() promotes the candidate
//     to the claimed node.
//   - Defend: once claimed, Inbound returns an ACK reply to any ENQ probing our node
//     (when RespondToEnq is set — true for the shared LToUDP segment, false for the
//     physical TashTalk medium that defends in hardware).

// DefaultProbeCount is the number of consecutive collision-free ENQs after which a
// candidate node is claimed (~2s at the spec's 250ms tick).
const DefaultProbeCount = 8

// claimState tracks probe/claim progress (mirrors aarp.claimState).
type claimState uint8

const (
	claimIdle claimState = iota
	claimProbing
	claimDone
)

// Config tunes the engine. Zero fields take the defaults.
type Config struct {
	// DesiredNode is the first candidate to probe (0 → DefaultDesiredNode).
	DesiredNode uint8
	// ProbeCount is how many collision-free ENQs claim the candidate (0 →
	// DefaultProbeCount).
	ProbeCount int
	// RespondToEnq makes a claimed engine answer an ENQ for its node with a defending
	// ACK. True for LToUDP (shared simulated segment — participants must announce a
	// taken address); false for TashTalk (the physical medium defends in hardware).
	RespondToEnq bool
	// Rand returns a pseudo-random uint8, used to shuffle the fallback candidate pool
	// on a collision. nil → a deterministic order (1..MaxNode descending minus the
	// desired node); the adapter injects a real RNG so simultaneous routers diverge.
	Rand func() uint8
}

// Engine is the pure LLAP node-claim state machine. Build with NewEngine; it is
// single-goroutine (the adapter calls it from its read/claim paths under the adapter's
// own lock).
type Engine struct {
	cfg Config

	state       claimState
	desiredNode uint8 // the candidate currently being probed
	claimed     uint8 // the accepted node (0 until claimDone)
	probesLeft  int
	conflict    bool

	// fallbacks is the shuffled pool of remaining candidate nodes, popped on reroll.
	fallbacks []uint8
}

// NewEngine builds a claim engine for the given config.
func NewEngine(cfg Config) *Engine {
	if cfg.DesiredNode == 0 {
		cfg.DesiredNode = DefaultDesiredNode
	}
	if cfg.ProbeCount <= 0 {
		cfg.ProbeCount = DefaultProbeCount
	}
	e := &Engine{cfg: cfg, desiredNode: cfg.DesiredNode}
	e.fillFallbacks()
	return e
}

// --- claim ---

// BeginProbe arms (or re-arms) the probe burst for the current candidate node: it
// clears any prior conflict and resets the probe counter. The adapter calls it to start
// claiming and again after a reroll. The candidate is e.desiredNode (set initially from
// Config and advanced by rerollDesiredNode on a conflict).
func (e *Engine) BeginProbe() {
	e.state = claimProbing
	e.probesLeft = e.cfg.ProbeCount
	e.conflict = false
}

// NextProbe returns the next ENQ control frame to send and whether one was produced. It
// decrements the remaining-probe counter; when none remain it returns ok=false and the
// adapter calls AcceptTentative (if no conflict was seen). A conflicted claim returns
// ok=false too (the adapter rerolls and BeginProbe again).
func (e *Engine) NextProbe() (enq ControlFrame, ok bool) {
	if e.state != claimProbing || e.conflict || e.probesLeft <= 0 {
		return ControlFrame{}, false
	}
	e.probesLeft--
	return Enq(e.desiredNode), true
}

// Conflicted reports whether the in-progress claim saw a collision on its candidate.
func (e *Engine) Conflicted() bool { return e.conflict }

// Candidate returns the node currently being probed (for logging).
func (e *Engine) Candidate() uint8 { return e.desiredNode }

// AcceptTentative promotes the candidate node to the claimed node. The adapter calls it
// after the probes complete with no conflict. A conflicted or non-probing state is a
// no-op returning ok=false.
func (e *Engine) AcceptTentative() (node uint8, ok bool) {
	if e.state != claimProbing || e.conflict {
		return 0, false
	}
	e.claimed = e.desiredNode
	e.state = claimDone
	return e.claimed, true
}

// Claimed returns the accepted node and whether the claim has completed.
func (e *Engine) Claimed() (node uint8, ok bool) { return e.claimed, e.state == claimDone }

// --- inbound ---

// Inbound processes one received LLAP control frame (ENQ or ACK). It returns an optional
// ACK reply to send (defending our claimed node against an ENQ probing it, when
// RespondToEnq is set) and claimConflict=true when the frame collides with our
// in-progress candidate — in which case it also rerolls to a fresh candidate, so the
// adapter just calls BeginProbe again. A non-control frame is ignored.
//
// Spec §"Collision Detection":
//   - ENQ: if claimed and (RespondToEnq) and dst==claimed → defend with an ACK.
//     Else if unclaimed and dst==candidate → collision, reroll.
//   - ACK: if unclaimed and dst==candidate → a node answered our ENQ — collision, reroll.
func (e *Engine) Inbound(c ControlFrame) (reply ControlFrame, hasReply bool, claimConflict bool) {
	switch c.Type {
	case TypeENQ:
		if node, ok := e.Claimed(); ok {
			if e.cfg.RespondToEnq && c.Dst == node {
				return Ack(node), true, false
			}
			return ControlFrame{}, false, false
		}
		if e.state == claimProbing && c.Dst == e.desiredNode {
			e.rerollDesiredNode()
			return ControlFrame{}, false, true
		}
	case TypeACK:
		if _, ok := e.Claimed(); !ok && e.state == claimProbing && c.Dst == e.desiredNode {
			e.rerollDesiredNode()
			return ControlFrame{}, false, true
		}
	}
	return ControlFrame{}, false, false
}

// rerollDesiredNode picks a fresh candidate from the fallback pool after a collision and
// flags the conflict so the in-progress NextProbe burst stops. When the pool empties it
// is refilled (shuffled), so claiming never gets stuck. The probe counter is re-armed by
// the adapter's next BeginProbe.
func (e *Engine) rerollDesiredNode() {
	e.conflict = true
	if len(e.fallbacks) == 0 {
		e.fillFallbacks()
	}
	if len(e.fallbacks) == 0 {
		return // no candidates at all (range degenerate) — keep the current one
	}
	last := len(e.fallbacks) - 1
	e.desiredNode = e.fallbacks[last]
	e.fallbacks = e.fallbacks[:last]
}

// fillFallbacks (re)builds the candidate pool: every valid unicast node (MinNode..MaxNode)
// except the current candidate, optionally shuffled via cfg.Rand. The shuffle matters on a
// shared segment so multiple routers booting at once diverge instead of colliding in
// lock-step (spec §"Reroll Algorithm").
func (e *Engine) fillFallbacks() {
	e.fallbacks = e.fallbacks[:0]
	for n := int(MinNode); n <= int(MaxNode); n++ {
		if uint8(n) == e.desiredNode {
			continue
		}
		e.fallbacks = append(e.fallbacks, uint8(n))
	}
	if e.cfg.Rand == nil {
		return
	}
	// Fisher–Yates over the pool using the injected RNG.
	for i := len(e.fallbacks) - 1; i > 0; i-- {
		j := int(e.cfg.Rand()) % (i + 1)
		e.fallbacks[i], e.fallbacks[j] = e.fallbacks[j], e.fallbacks[i]
	}
}
