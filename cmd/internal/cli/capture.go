package cli

import (
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/adapter/capture/pcapfile"
	"github.com/ObsoleteMadness/ClassicStack/compose/registry"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
)

// captureOpener wraps a base LinkOpener so that, when [Capture] names a pcap file for
// the interface being opened, the opened FrameLink is decorated with link.Capture
// teeing every frame to a pure-Go pcapfile.Sink. It is installed at the cmd edge (not
// in compose) so compose/runtime stays free of the capture adapter — the same injection
// discipline as the pcap opener itself.
//
// One Sink per interface path is created lazily on first open and reused across reopens
// (a port that restarts re-opens its link); a Sink is never closed for the process
// lifetime, which is fine for a capture file that should span the whole run. A capture
// section with no path for the interface returns the base link unchanged. The NIC
// opener path serves Ethernet-framed links, so the file is written as DLT_EN10MB.
func captureOpener(base registry.LinkOpener, cap *config.CaptureSection) registry.LinkOpener {
	if base == nil || cap == nil || !cap.Any() {
		return base
	}
	var (
		mu    sync.Mutex
		sinks = map[string]*pcapfile.Sink{}
	)
	snaplen := uint32(cap.Snaplen)
	return func(iface string) (link.FrameLink, error) {
		fl, err := base(iface)
		if err != nil {
			return nil, err
		}
		path := cap.PathFor(iface)
		if path == "" {
			return fl, nil
		}
		mu.Lock()
		sink, ok := sinks[path]
		if !ok {
			s, serr := pcapfile.New(path, pcapfile.LinkTypeEthernet, snaplen)
			if serr != nil {
				mu.Unlock()
				// Capture is best-effort: a bad path must not break the data path, so
				// return the undecorated link rather than failing the port's open.
				return fl, nil
			}
			sink = s
			sinks[path] = s
		}
		mu.Unlock()
		return link.Capture(fl, sink), nil
	}
}
