// Package driversnet is the TinyGo/embedded (drivers/net, ESP32-raw) FrameLink
// adapter (§2, M1). It is the raw-L2 backend for embedded targets that have no
// libpcap — frames come straight off a netdev driver.
//
// STUB: not yet implemented. This package exists so the M1 link-adapter surface
// is complete and importable; the real drivers/net I/O lands in a later M1/M3
// increment alongside the TinyGo target work. Open returns ErrNotImplemented
// today. Kept stdlib-only so it stays TinyGo-safe when implemented.
//
// Ring: adapter.
package driversnet

import (
	"errors"

	"github.com/ObsoleteMadness/ClassicStack/core/link"
)

// ErrNotImplemented is returned by every entry point until the drivers/net
// backend is ported.
var ErrNotImplemented = errors.New("driversnet: embedded drivers/net link not implemented yet (M1 stub)")

// Config holds embedded netdev parameters. Provisional.
type Config struct {
	Device string // driver/device identifier
}

// Open is a stub: it always returns ErrNotImplemented.
func Open(cfg Config) (link.FrameLink, error) { return nil, ErrNotImplemented }
