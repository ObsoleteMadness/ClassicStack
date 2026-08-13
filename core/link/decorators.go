package link

import (
	"hash/fnv"
	"sync"
	"time"
)

// This file holds the real frame-altitude decorator bodies (§2). They wrap a
// FrameLink and return a FrameLink, so they compose: Capture(Dedup(Filter(raw))).
// Everything here is stdlib-only and reflection-free (archtest-gated): no pcap,
// gopacket, or capture backend may appear in core/link. The Capture decorator
// lives in link.go alongside the CaptureSink interface; Filter/Dedup/Bridge are
// here.

// --- Filter -----------------------------------------------------------------

// filterLink drops inbound frames that fail the pass predicate. Writes are not
// filtered (software egress filtering has no use case yet); only Read drops.
type filterLink struct {
	FrameLink
	pass FilterFunc
}

// SetNodeAddress forwards the hardware node-filter capability to the wrapped link
// (see NodeAddressSetter): the embedded interface would otherwise hide the method.
func (f *filterLink) SetNodeAddress(node uint8) error {
	return setNodeAddressOn(f.FrameLink, node)
}

// Read loops, discarding frames the predicate rejects, until one passes, the
// inner link returns ErrTimeout (surfaced so the caller can re-poll), or any
// other error occurs. A nil predicate passes everything (handled in Filter).
func (f *filterLink) Read() (Frame, error) {
	for {
		fr, err := f.FrameLink.Read()
		if err != nil {
			return fr, err
		}
		if f.pass(fr) {
			return fr, nil
		}
		// dropped: keep reading without bubbling a frame the caller didn't want
	}
}

// Filter wraps inner with software-side ingress filtering: frames for which
// pass returns false are dropped before reaching the caller. A nil pass is a
// no-op (returns inner unchanged) so callers can wire it unconditionally.
func Filter(inner FrameLink, pass FilterFunc) FrameLink {
	if pass == nil {
		return inner
	}
	return &filterLink{FrameLink: inner, pass: pass}
}

// --- Dedup ------------------------------------------------------------------

// dedupTTL is how long a frame hash is remembered for garbage-collection. The
// suppression window is the caller-supplied value; the TTL bounds the map size
// and is a small multiple of a typical window. Mirrors the legacy IPX port's
// 25ms window / 100ms TTL pairing.
const dedupTTL = 100 * time.Millisecond

// dedupLink suppresses kernel loopback duplicates: when a host both injects and
// captures on the same interface, a transmitted frame is read back. Reading the
// identical bytes within window of a prior sighting is treated as the echo and
// dropped. Keyed by a fast non-cryptographic hash of the whole frame.
type dedupLink struct {
	FrameLink
	window time.Duration

	mu     sync.Mutex
	recent map[uint64]time.Time
}

// SetNodeAddress forwards the hardware node-filter capability to the wrapped link
// (see NodeAddressSetter): the embedded interface would otherwise hide the method.
func (d *dedupLink) SetNodeAddress(node uint8) error {
	return setNodeAddressOn(d.FrameLink, node)
}

func (d *dedupLink) Read() (Frame, error) {
	for {
		fr, err := d.FrameLink.Read()
		if err != nil {
			return fr, err
		}
		if d.isDuplicate(fr) {
			continue // loopback echo: drop and keep reading
		}
		return fr, nil
	}
}

// isDuplicate reports whether frame was seen within window, recording it as seen
// either way, and opportunistically evicts entries older than dedupTTL.
func (d *dedupLink) isDuplicate(frame []byte) bool {
	key := frameHash(frame)
	now := time.Now()

	d.mu.Lock()
	defer d.mu.Unlock()

	dup := false
	if seenAt, ok := d.recent[key]; ok && now.Sub(seenAt) <= d.window {
		dup = true
	}
	d.recent[key] = now
	for k, ts := range d.recent {
		if now.Sub(ts) > dedupTTL {
			delete(d.recent, k)
		}
	}
	return dup
}

// frameHash is a fast non-cryptographic 64-bit hash of the full frame, used as
// the dedup key. FNV-1a is stdlib (hash/fnv) and reflection-free.
func frameHash(frame []byte) uint64 {
	h := fnv.New64a()
	_, _ = h.Write(frame)
	return h.Sum64()
}

// Dedup wraps inner with duplicate-suppression over a window expressed in
// nanoseconds (the signature uses int64 to stay free of a time import in the
// public surface). A non-positive window is a no-op (returns inner unchanged).
func Dedup(inner FrameLink, window int64) FrameLink {
	if window <= 0 {
		return inner
	}
	return &dedupLink{
		FrameLink: inner,
		window:    time.Duration(window),
		recent:    make(map[uint64]time.Time),
	}
}
