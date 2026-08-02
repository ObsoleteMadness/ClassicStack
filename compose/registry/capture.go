package registry

import (
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/adapter/capture/pcapfile"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
)

// captureSinks memoises one pcap Sink per output path for the whole process: a port
// that restarts re-opens its link and must tee into the SAME file, not truncate a new
// one, and two ports must never share a Sink even if mis-configured to the same path
// (they can't — the map is keyed by path, so the second reuses the first's Sink, which
// is the documented "one file per path, spanning the run" behaviour the cmd-edge
// capture had). Sinks are never closed for the process lifetime — a capture file should
// cover the whole session.
var captureSinks = struct {
	mu    sync.Mutex
	byKey map[string]*pcapfile.Sink
}{byKey: map[string]*pcapfile.Sink{}}

// captureSink returns the shared Sink for path (creating it on first use with the given
// link type + snaplen), or nil when the file cannot be opened. Capture is best-effort:
// a bad path yields a nil Sink so the caller leaves the data path undecorated rather
// than failing the port's Start.
func captureSink(path string, lt pcapfile.LinkType, snaplen uint32) *pcapfile.Sink {
	captureSinks.mu.Lock()
	defer captureSinks.mu.Unlock()
	if s, ok := captureSinks.byKey[path]; ok {
		return s
	}
	s, err := pcapfile.New(path, lt, snaplen)
	if err != nil {
		return nil
	}
	captureSinks.byKey[path] = s
	return s
}

// FlushCaptureSinks flushes every open capture sink's buffered records to disk without
// closing them, so a capture survives a hard process kill with at most one flush-interval
// of records lost. Called periodically by the background flusher and once more on a clean
// shutdown (before CloseCaptureSinks). Best-effort: per-sink flush errors are ignored so a
// broken capture file never blocks shutdown.
func FlushCaptureSinks() {
	captureSinks.mu.Lock()
	sinks := make([]*pcapfile.Sink, 0, len(captureSinks.byKey))
	for _, s := range captureSinks.byKey {
		sinks = append(sinks, s)
	}
	captureSinks.mu.Unlock()
	// Flush outside the map lock: a WriteFrame in flight takes the sink's own lock, and we
	// must not hold the registry lock across a blocking file write.
	for _, s := range sinks {
		_ = s.Flush()
	}
}

// CloseCaptureSinks flushes and closes every capture sink, then forgets them, so a
// subsequent capture to the same path opens fresh. Called once on a clean shutdown after
// the ports have stopped writing. Best-effort.
func CloseCaptureSinks() {
	captureSinks.mu.Lock()
	sinks := make([]*pcapfile.Sink, 0, len(captureSinks.byKey))
	for _, s := range captureSinks.byKey {
		sinks = append(sinks, s)
	}
	captureSinks.byKey = map[string]*pcapfile.Sink{}
	captureSinks.mu.Unlock()
	for _, s := range sinks {
		_ = s.Close()
	}
}

// StartCaptureFlusher launches a background goroutine that flushes all capture sinks every
// interval until stop is closed, so an ungraceful kill (SIGKILL / double-Ctrl-C, which skips
// the clean-shutdown flush) loses at most one interval of buffered records rather than the
// whole in-flight buffer. A non-positive interval disables the flusher (returns immediately).
// The returned function stops the goroutine; it is safe to call more than once.
func StartCaptureFlusher(interval time.Duration) (stop func()) {
	if interval <= 0 {
		return func() {}
	}
	done := make(chan struct{})
	var once sync.Once
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				FlushCaptureSinks()
			}
		}
	}()
	return func() { once.Do(func() { close(done) }) }
}

// captureOpener decorates a per-Start FrameLink opener so that, when the section
// implements port.CaptureProvider with a non-empty path, every frame the link
// reads/writes is tee'd to that pcap file. Capture is a property of the port that
// owns the segment, so it works for any transport uniformly — including LToUDP,
// which opens its own multicast link and never touches the NIC opener.
//
// lt is the data-link type of THIS transport's frames: Ethernet for a NIC port
// (EtherTalk), raw LLAP (DLT_LTALK) for LToUDP/TashTalk — so the .pcap opens with the
// DLT Wireshark needs to dissect it. A nil base opener, an empty Capture path, or an
// unopenable file all fall through to the base opener unchanged (capture is never fatal).
func captureOpener(cap port.CaptureProvider, lt pcapfile.LinkType, base func() (link.FrameLink, error)) func() (link.FrameLink, error) {
	if base == nil || cap == nil || cap.CapturePath() == "" {
		return base
	}
	path := cap.CapturePath()
	snaplen := uint32(cap.CaptureSnapLen())
	return func() (link.FrameLink, error) {
		fl, err := base()
		if err != nil || fl == nil {
			return fl, err
		}
		sink := captureSink(path, lt, snaplen)
		if sink == nil {
			return fl, nil // best-effort: undecorated on a bad capture file
		}
		return link.Capture(fl, sink), nil
	}
}
