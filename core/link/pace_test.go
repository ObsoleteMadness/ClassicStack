package link

import (
	"sync"
	"testing"
	"time"
)

// recordLink records the times and destination nodes of every Write, and returns a
// preset error. It is the innermost FrameLink under the pace decorator in tests.
type recordLink struct {
	mu    sync.Mutex
	times []time.Time
	nodes []uint8
}

func (r *recordLink) Read() (Frame, error) { return nil, ErrClosed }
func (r *recordLink) Close() error         { return nil }
func (r *recordLink) Write(f Frame) error {
	r.mu.Lock()
	r.times = append(r.times, time.Now())
	r.nodes = append(r.nodes, destNode(f))
	r.mu.Unlock()
	return nil
}

// frameTo builds a minimal LLAP frame (dst, src, type) addressed to node dst.
func frameTo(dst uint8) Frame { return Frame{dst, 0x01, TypeShortDDPByte} }

// TypeShortDDPByte is the LLAP short-DDP type; duplicated as a literal here so the
// test does not import core/protocol/llap (which would couple the core/link test to
// a sibling package). The value only has to be a valid non-control type for framing;
// pacing ignores it entirely.
const TypeShortDDPByte = 0x01

// Pace with a non-positive gap must return the inner link unchanged (no-op).
func TestPace_NonPositiveIsNoOp(t *testing.T) {
	inner := &recordLink{}
	if got := Pace(inner, 0); got != FrameLink(inner) {
		t.Fatalf("Pace(_, 0) = %T, want the inner link unchanged", got)
	}
	if got := Pace(inner, -1); got != FrameLink(inner) {
		t.Fatalf("Pace(_, -1) = %T, want the inner link unchanged", got)
	}
}

// Successive writes to the SAME node must be separated by at least the gap.
func TestPace_SameNodeSpaced(t *testing.T) {
	inner := &recordLink{}
	const gap = 20 * time.Millisecond
	p := Pace(inner, int64(gap))

	const n = 4
	start := time.Now()
	for i := range n {
		if err := p.Write(frameTo(16)); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	elapsed := time.Since(start)

	// pace.go schedules each node's next send against an absolute target time (see
	// paceLink.Write), so ordinary scheduler jitter can only push a gap LATER, never
	// earlier, under time.Sleep's "at least the duration" guarantee — but on a loaded
	// or virtualized runner the jitter itself can still be a few ms, so both checks
	// below allow a proportional tolerance rather than asserting the gap exactly.
	const tolerance = gap / 4

	// n writes to one node ⇒ (n-1) gaps of enforced spacing minimum.
	if want := time.Duration(n-1) * (gap - tolerance); elapsed < want {
		t.Fatalf("elapsed %v for %d paced writes, want ≥ %v", elapsed, n, want)
	}
	inner.mu.Lock()
	defer inner.mu.Unlock()
	for i := 1; i < len(inner.times); i++ {
		if d := inner.times[i].Sub(inner.times[i-1]); d < gap-tolerance {
			t.Fatalf("gap between write %d and %d = %v, want ≥ ~%v", i-1, i, d, gap)
		}
	}
}

// Writes to DIFFERENT nodes must not pace against each other: two frames to two
// distinct nodes should both go out promptly even though each node has its own gap.
func TestPace_DifferentNodesIndependent(t *testing.T) {
	inner := &recordLink{}
	const gap = 50 * time.Millisecond
	p := Pace(inner, int64(gap))

	start := time.Now()
	// One frame each to three different nodes: no same-node pair, so no sleeps.
	for _, node := range []uint8{16, 17, 18} {
		if err := p.Write(frameTo(node)); err != nil {
			t.Fatalf("write to node %d: %v", node, err)
		}
	}
	if elapsed := time.Since(start); elapsed >= gap {
		t.Fatalf("three writes to distinct nodes took %v, want < one gap (%v) — nodes paced against each other", elapsed, gap)
	}
}

// A runt frame too short for an LLAP header is paced against the broadcast bucket
// and must not panic.
func TestPace_RuntFrame(t *testing.T) {
	inner := &recordLink{}
	p := Pace(inner, int64(5*time.Millisecond))
	if err := p.Write(Frame{}); err != nil {
		t.Fatalf("empty-frame write: %v", err)
	}
	if got := destNode(Frame{}); got != 0xFF {
		t.Fatalf("destNode(empty) = %#x, want 0xFF (broadcast bucket)", got)
	}
}
