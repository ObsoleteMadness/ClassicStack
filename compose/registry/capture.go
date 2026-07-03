package registry

import (
	"sync"

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

// captureOpener decorates a per-Start FrameLink opener so that, when the port SECTION
// sets Capture, every frame the link reads/writes is tee'd to that pcap file. Capture is
// a property of the port that owns the segment (Section.Capture), so it works for any
// transport uniformly — including LToUDP, which opens its own multicast link and never
// touches the NIC opener the cmd edge used to key capture on.
//
// lt is the data-link type of THIS transport's frames: Ethernet for a NIC port
// (EtherTalk), raw LLAP (DLT_LTALK) for LToUDP/TashTalk — so the .pcap opens with the
// DLT Wireshark needs to dissect it. A nil base opener, an empty Capture path, or an
// unopenable file all fall through to the base opener unchanged (capture is never fatal).
func captureOpener(sec *port.Section, lt pcapfile.LinkType, base func() (link.FrameLink, error)) func() (link.FrameLink, error) {
	if base == nil || sec == nil || sec.Capture == "" {
		return base
	}
	path := sec.Capture
	snaplen := uint32(sec.CaptureSnaplen)
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
