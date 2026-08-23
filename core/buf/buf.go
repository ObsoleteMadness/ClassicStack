//go:build !tinygo

// Package buf holds per-target buffer-size constants implementing the §1
// allocation discipline. This file carries the default (desktop/server) sizes;
// buf_tinygo.go overrides them with smaller values on embedded targets.
//
// The constants are deliberately a small, fixed set. Add a constant only when a
// real call site needs to size a buffer against the target, so the embedded
// build stays auditable. Code should reference these consts rather than
// hard-coding sizes (CLAUDE.md).
package buf

const (
	// FrameMax is the largest L2 frame a FrameLink read buffer must hold. On
	// desktop we size generously to cover jumbo-ish captures and headroom.
	FrameMax = 65536

	// ReadChunk is the default chunk size for streaming reads (e.g. DSI/TCP
	// transport reads, file copies).
	ReadChunk = 32768

	// LogFieldMax is the upper bound on a single rendered log field's bytes,
	// used to size scratch buffers in the log sinks without per-call alloc.
	LogFieldMax = 1024
)
