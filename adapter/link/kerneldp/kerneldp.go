// Package kerneldp is the kernel datagram (AF_APPLETALK) DatagramLink adapter
// (§2, M1). Unlike the frame-altitude link adapters, this one yields a
// pre-framed core/link.DatagramLink directly from a kernel socket — the router
// cannot tell it apart from a Framing(FrameLink) source.
//
// STUB: not yet implemented. This package exists so the M1 link-adapter surface
// is complete and importable; the real AF_APPLETALK socket I/O lands in a later
// M1/M3 increment. Open returns ErrNotImplemented today.
//
// Ring: adapter.
package kerneldp

import (
	"errors"

	"github.com/ObsoleteMadness/ClassicStack/core/link"
)

// ErrNotImplemented is returned by every entry point until the AF_APPLETALK
// backend is ported.
var ErrNotImplemented = errors.New("kerneldp: AF_APPLETALK datagram link not implemented yet (M1 stub)")

// Config holds kernel datagram socket parameters. Provisional.
type Config struct {
	Interface string // bound interface name
}

// Open is a stub: it always returns ErrNotImplemented. Note the return type is
// DatagramLink (pre-framed), not FrameLink.
func Open(cfg Config) (link.DatagramLink, error) { return nil, ErrNotImplemented }
