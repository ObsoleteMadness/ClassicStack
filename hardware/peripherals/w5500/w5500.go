//go:build tinygo

// KNOWN GAP: this driver does not work yet. It was written against a MACRAW
// raw-Ethernet-frame socket API (OpenMACRAW/GetRxSize/per-socket Read/Write/Send)
// that tinygo.org/x/drivers/w5500 (the actual pinned dependency, v0.35.0) does not
// have -- that driver only exposes the W5500's IP-socket offload (Configure/SetAddr/
// SetGateway, meant to back TinyGo's netdev framework), not a raw-frame passthrough
// mode, even though the chip's hardware does support MACRAW. Bridging ClassicStack's
// link.FrameLink (raw Ethernet frames, needed for AppleTalk/IPX which run under IP)
// onto that socket-oriented API needs either a different/lower-level W5500 driver or
// a genuine netdev-based redesign of this board's link layer -- tracked as follow-up,
// not attempted here. OpenW5500 fails cleanly instead of not compiling.
package w5500

import (
	"errors"
	"machine"

	"github.com/ObsoleteMadness/ClassicStack/core/link"
)

// ErrNotImplemented is returned by OpenW5500 until a MACRAW-capable driver lands.
var ErrNotImplemented = errors.New("w5500: raw-frame MACRAW support is not implemented for this driver (see hardware/peripherals/w5500/w5500.go)")

// OpenW5500 fails with ErrNotImplemented; see the package doc comment for why.
func OpenW5500(_ *machine.SPI, _, _, _ machine.Pin) (link.FrameLink, error) {
	return nil, ErrNotImplemented
}
