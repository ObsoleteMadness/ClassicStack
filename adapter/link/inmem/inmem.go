package inmem

import (
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/core/link"
)

// Link is an in-memory loopback FrameLink: frames written to one end are readable
// from the paired end. It lets the harness run an end-to-end stack with no real
// hardware (Phase 1 D4). It satisfies core/link.FrameLink and is safe for one
// reader + one writer goroutine per end, matching the FrameLink contract.
type Link struct {
	in   chan link.Frame // frames arriving for this end (peer writes here)
	out  chan link.Frame // frames this end writes (peer reads here)
	once sync.Once
	done chan struct{}
}

// Pair returns two Links wired back-to-back: a Write on one is a Read on the
// other. buffer is the per-direction channel depth (0 → unbuffered).
func Pair(buffer int) (*Link, *Link) {
	if buffer < 0 {
		buffer = 0
	}
	a2b := make(chan link.Frame, buffer)
	b2a := make(chan link.Frame, buffer)
	done := make(chan struct{})
	a := &Link{in: b2a, out: a2b, done: done}
	b := &Link{in: a2b, out: b2a, done: done}
	return a, b
}

// Loopback returns a single Link whose writes loop straight back to its own
// reads — handy for a port that just needs a non-nil, inert link in Phase 1.
func Loopback(buffer int) *Link {
	if buffer < 0 {
		buffer = 0
	}
	ch := make(chan link.Frame, buffer)
	return &Link{in: ch, out: ch, done: make(chan struct{})}
}

// Read returns the next frame, ErrTimeout never (this link has no deadline), or
// ErrClosed after Close. The returned slice is owned by the caller.
func (l *Link) Read() (link.Frame, error) {
	select {
	case f, ok := <-l.in:
		if !ok {
			return nil, link.ErrClosed
		}
		// Hand the caller its own copy; we never retain it.
		cp := make(link.Frame, len(f))
		copy(cp, f)
		return cp, nil
	case <-l.done:
		return nil, link.ErrClosed
	}
}

// Write enqueues a copy of f for the peer. It does not retain f past the call
// (FrameLink contract). Returns ErrClosed after Close.
func (l *Link) Write(f link.Frame) error {
	cp := make(link.Frame, len(f))
	copy(cp, f)
	select {
	case <-l.done:
		return link.ErrClosed
	default:
	}
	select {
	case l.out <- cp:
		return nil
	case <-l.done:
		return link.ErrClosed
	}
}

// Close terminates both ends; subsequent Read/Write return ErrClosed. Idempotent.
func (l *Link) Close() error {
	l.once.Do(func() { close(l.done) })
	return nil
}

// compile-time assertion: *Link satisfies core/link.FrameLink.
var _ link.FrameLink = (*Link)(nil)
