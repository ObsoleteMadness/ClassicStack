// Package tap is the TUN/TAP FrameLink adapter (§2, M1).
//
// STUB: not yet implemented. This package exists so the M1 link-adapter surface
// is complete and importable; the real TUN/TAP I/O (porting the legacy
// port/rawlink/tuntap_*.go) lands in a later M1/M3 increment. Open returns
// ErrNotImplemented today.
//
// Ring: adapter.
package tap

import (
	"errors"

	"github.com/ObsoleteMadness/ClassicStack/core/link"
)

// ErrNotImplemented is returned by every entry point until the TAP backend is
// ported.
var ErrNotImplemented = errors.New("tap: TUN/TAP link not implemented yet (M1 stub)")

// Config holds TAP device parameters. Fields are provisional and may change when
// the real adapter lands.
type Config struct {
	Name string // TAP device name, e.g. "tap0"
}

// Open is a stub: it always returns ErrNotImplemented.
func Open(cfg Config) (link.FrameLink, error) { return nil, ErrNotImplemented }
