//go:build !tinygo

// LToUDP needs a real multicast UDP socket (golang.org/x/net/ipv4, via
// adapter/link/ltoudp), which TinyGo's baremetal targets don't implement (see
// localtalk_tinygo.go for the stub those targets get instead).

package link

import (
	"fmt"

	"github.com/ObsoleteMadness/ClassicStack/adapter/link/ltoudp"
	"github.com/ObsoleteMadness/ClassicStack/adapter/link/tashtalk"
	"github.com/ObsoleteMadness/ClassicStack/adapter/serial"
	"github.com/ObsoleteMadness/ClassicStack/client/trace"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
)

// openLToUDP opens the LToUDP multicast segment with LLAP framing (mirrors atlink). The
// Logger is wired to the shared client trace sink (client/trace) so `-v` surfaces peer
// activity and malformed-frame drops the same way the server side already logs them
// (adapter/link/ltoudp: "ltoudp: peer seen" / "ltoudp: dropping malformed frame from
// peer") — without it, a peer answering with a corrupt frame looks identical to a peer
// that never answered at all, from every client probe tool (csnbp, csecho, csclient).
func openLToUDP(iface string, network uint16, srcNode uint8) (link.DatagramLink, error) {
	cfg := ltoudp.DefaultConfig(iface)
	cfg.Logger = trace.Logger("ltoudp")
	fl, err := ltoudp.Open(cfg)
	if err != nil {
		return nil, fmt.Errorf("open LToUDP: %w", err)
	}
	return frameLocalTalk(fl, network, srcNode)
}

// openTashTalk opens a TashTalk serial adapter with LLAP framing (mirrors atlink).
func openTashTalk(device string, baud uint, network uint16, srcNode uint8) (link.DatagramLink, error) {
	if device == "" {
		return nil, fmt.Errorf("tashtalk transport needs a device (a serial port path)")
	}
	s, err := serial.Open(serial.Config{Device: device, Baud: baud})
	if err != nil {
		return nil, fmt.Errorf("open serial %s: %w", device, err)
	}
	fl, err := tashtalk.NewStream(s)
	if err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("frame TashTalk: %w", err)
	}
	return frameLocalTalk(fl, network, srcNode)
}
