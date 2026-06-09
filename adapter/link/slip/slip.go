// Package slip is the serial SLIP FrameLink adapter (§2, M1).
//
// STUB: not yet implemented. This package exists so the M1 link-adapter surface
// is complete and importable; the real SLIP framing (END/ESC byte-stuffing) over
// a serial port lands in a later M1/M3 increment. Open returns ErrNotImplemented
// today.
//
// Ring: adapter.
package slip

import (
	"errors"

	"github.com/ObsoleteMadness/ClassicStack/core/link"
)

// ErrNotImplemented is returned by every entry point until the SLIP backend is
// ported.
var ErrNotImplemented = errors.New("slip: serial SLIP link not implemented yet (M1 stub)")

// Config holds serial-port parameters for SLIP. Provisional.
type Config struct {
	Port string // serial device, e.g. "/dev/ttyUSB0" or "COM3"
	Baud int    // line speed
}

// Open is a stub: it always returns ErrNotImplemented.
func Open(cfg Config) (link.FrameLink, error) { return nil, ErrNotImplemented }
