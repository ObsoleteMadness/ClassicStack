package link

import (
	"fmt"

	"github.com/ObsoleteMadness/ClassicStack/adapter/link/ltoudp"
	"github.com/ObsoleteMadness/ClassicStack/adapter/link/tashtalk"
	"github.com/ObsoleteMadness/ClassicStack/adapter/serial"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
)

// openLToUDP opens the LToUDP multicast segment with LLAP framing (mirrors atlink).
func openLToUDP(iface string, network uint16, srcNode uint8) (link.DatagramLink, error) {
	fl, err := ltoudp.Open(ltoudp.DefaultConfig(iface))
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
