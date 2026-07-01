// Package llap is the pure LocalTalk Link Access Protocol control core: the LLAP
// frame header (dest node · src node · type) and the node-address CLAIM state
// machine (the ENQ/ACK probe-and-claim dance). It is the LocalTalk analogue of
// core/protocol/aarp — a peer codec to core/protocol/ddp, owning the protocol logic
// the adapter framer drives.
//
// SCOPE. This package handles the LLAP CONTROL plane only — the node-claim
// (ENQ/ACK) and the 3-byte frame header it rides on. The DDP DATA plane (short-
// vs long-header DDP carried in 0x01/0x02 frames) stays in the adapter framer
// (adapter/link/framing/localtalk.go), which already owns the ddp codec seam; this
// package deliberately does not duplicate it.
//
// It owns NO I/O, goroutines, or timers — the adapter (the LocalTalk framer) supplies
// the wire and drives the probe timing, feeding inbound control frames to the engine
// and sending the frames it returns. The engine takes an explicit RNG seam (no
// math/rand import) so it stays deterministic, table-testable, and TinyGo-clean,
// matching the core/protocol/aarp + core/service/rtmp discipline.
//
// Spec: spec/09-port-localtalk-base.md ("Node Address Acquisition"). Ring: CORE.
package llap
