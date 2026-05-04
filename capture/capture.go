// Package capture writes copies of in-flight network frames to pcap
// files for offline analysis in Wireshark or similar tools.
//
// A Sink is the minimal contract a port needs: hand it a timestamp and
// a frame, and it persists the frame. A nil Sink is a no-op via the
// Write helper, so call sites can stay terse.
package capture

import "time"

// Sink consumes captured frames. Implementations must be safe for
// concurrent use; ports tap from multiple goroutines.
type Sink interface {
	WriteFrame(ts time.Time, frame []byte)
	Close() error
}

// Write writes frame to s if s is non-nil. The frame slice may be
// retained by the sink, so callers should not mutate it after the call.
func Write(s Sink, ts time.Time, frame []byte) {
	if s == nil {
		return
	}
	s.WriteFrame(ts, frame)
}
