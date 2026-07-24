package link

import (
	"fmt"

	"github.com/ObsoleteMadness/ClassicStack/adapter/link/tap"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
)

// tap.go opens a TUN/TAP device as a raw Ethernet FrameLink — the libpcap-free
// alternative to the pcap carrier for the raw-Ethernet client transports (IPX/NetBEUI/
// EtherDFS/EtherTalk and the NetBIOS datagram carrier) on a host without Npcap/libpcap.
// It reuses the SAME adapter/link/tap the servers do, so no device I/O is duplicated
// here. Unlike pcap it needs no build tag: the adapter is portable (the real TUN/TAP
// backend is Linux-only and currently a stub that returns a clear "not implemented yet",
// so `-ifacetype tap` is a first-class, honestly-reported seam until that backend lands).

// openTapFrame opens the TAP device as a raw FrameLink. A missing device name is a caller
// error; the adapter reports its own "not implemented yet" until the TUN/TAP backend is
// ported.
func openTapFrame(device string) (link.FrameLink, error) {
	if device == "" {
		return nil, fmt.Errorf("tap transport needs a TUN/TAP device name (e.g. tap0)")
	}
	fl, err := tap.Open(tap.Config{Name: device})
	if err != nil {
		return nil, fmt.Errorf("open tap %s: %w", device, err)
	}
	return fl, nil
}
