package link

import (
	"sync"
	"time"
)

// This file holds the per-destination-node write PACING decorator (§2). Like the
// other frame-altitude decorators (Filter/Dedup/Capture) it wraps a FrameLink and
// returns a FrameLink, so it composes. It is stdlib-only and reflection-free
// (archtest-gated).
//
// WHY pacing lives here, not in a service: the constraint is the TRANSPORT — a slow
// classic-Mac LLAP receiver on a LocalTalk segment (LToUDP/TashTalk) drops frames
// that arrive back-to-back with no inter-frame gap, regardless of which service
// produced them (MacIP data, AFP-over-DDP bulk replies, netboot block floods).
// LToUDP in particular has NO link backpressure to flow-control against: RTS/CTS is
// synthesised locally and never transmitted, LLAP is unacknowledged, and the write
// is a fire-and-forget UDP multicast send. So the only lever the port has is TIME —
// an open-loop minimum gap between successive frames aimed at the same node. This is
// the universal floor; a protocol that also has a real backpressure signal (e.g.
// MacIP reading the Mac's TCP receive window) layers closed-loop flow control on top.
//
// PER-NODE, not global: the gap must serialise only frames aimed at the SAME slow
// receiver. A global send rate would pointlessly delay unrelated conversations to
// other nodes behind each other; the actual constraint is one node's receive path.

// paceLink enforces a minimum interval between successive Writes to the same LLAP
// destination node. The destination node is the first byte of every LLAP frame
// (dst(1) src(1) type(1) header); broadcast (0xFF) is treated as its own bucket so a
// broadcast storm cannot starve unicast to a real node and vice versa. Reads are not
// paced (ingress has no such constraint).
type paceLink struct {
	FrameLink
	gap time.Duration

	mu       sync.Mutex
	nextFree map[uint8]time.Time // per-dest-node earliest next send time
}

// Write sleeps until at least gap has elapsed since the previous frame to this
// frame's destination node, then writes. The per-node schedule is advanced under a
// lock but the sleep happens OUTSIDE the lock, so writes to different nodes never
// block each other — only successive frames to the SAME node are serialised.
func (p *paceLink) Write(f Frame) error {
	node := destNode(f)

	p.mu.Lock()
	now := time.Now()
	earliest := p.nextFree[node]
	var wait time.Duration
	if earliest.After(now) {
		wait = earliest.Sub(now)
	}
	// The next frame to this node may go one gap after THIS one lands.
	p.nextFree[node] = now.Add(wait).Add(p.gap)
	p.mu.Unlock()

	if wait > 0 {
		time.Sleep(wait)
	}
	return p.FrameLink.Write(f)
}

// destNode returns the LLAP destination node of a frame (its first byte), or the
// broadcast node for a frame too short to carry an LLAP header — so a malformed
// runt is paced against the broadcast bucket rather than colliding with node 0.
func destNode(f Frame) uint8 {
	if len(f) < 1 {
		return 0xFF
	}
	return f[0]
}

// Pace wraps inner so that successive Writes to the same LLAP destination node are
// separated by at least gap nanoseconds (the signature uses int64 to keep a time
// import off the public surface, matching Dedup). A non-positive gap is a no-op
// (returns inner unchanged) so callers can wire it unconditionally. Intended for the
// LocalTalk transports (LToUDP/TashTalk), whose classic-Mac receivers drop
// zero-gap bursts.
func Pace(inner FrameLink, gap int64) FrameLink {
	if gap <= 0 {
		return inner
	}
	return &paceLink{
		FrameLink: inner,
		gap:       time.Duration(gap),
		nextFree:  make(map[uint8]time.Time),
	}
}
