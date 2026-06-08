package link

import (
	"errors"

	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
)

type Frame = []byte

// FrameLink is a raw L2 frame transport.
type FrameLink interface {
	Read() (Frame, error)
	Write(Frame) error
	Close() error
}

// DatagramLink is a pre-framed DDP datagram transport.
type DatagramLink interface {
	ReadDatagram() (ddp.Datagram, error)
	WriteDatagram(ddp.Datagram) error
	Close() error
}

var (
	// ErrTimeout indicates a read deadline was hit; callers should keep looping.
	ErrTimeout = errors.New("link: read timeout")
	// ErrClosed indicates the link has been closed and is terminal.
	ErrClosed = errors.New("link: closed")
)

type PhysicalMedium uint8

const (
	MediumEthernet PhysicalMedium = iota
	MediumWiFi
)

// MediumReporter is implemented by links that can report their physical medium.
type MediumReporter interface{ Medium() PhysicalMedium }

// FilterableLink is implemented by links that can push a kernel-side filter.
type FilterableLink interface{ SetFilter(expr string) error }

// Framer adapts a FrameLink to a DatagramLink (DDP framing/deframing).
type Framer interface {
	Framing(FrameLink) (DatagramLink, error)
}

// FilterFunc reports whether a frame passes software filtering.
type FilterFunc func(Frame) bool

// Filter wraps inner with software-side filtering.
// Phase 1 placeholder: returns inner unchanged.
func Filter(inner FrameLink, pass FilterFunc) FrameLink {
	_ = pass
	return inner
}

// Dedup wraps inner with duplicate-suppression over a time window.
// Phase 1 placeholder: returns inner unchanged.
func Dedup(inner FrameLink, window int64) FrameLink {
	_ = window
	return inner
}

// Capture wraps inner with frame teeing into sink.
// Phase 1 placeholder: returns inner unchanged.
func Capture(inner FrameLink, sink CaptureSink) FrameLink {
	_ = sink
	return inner
}

// Bridge wraps inner with Wi-Fi/bridged MAC rewrite behavior.
// Phase 1 placeholder: returns inner unchanged.
func Bridge(inner FrameLink, mode string) FrameLink {
	_ = mode
	return inner
}

// CaptureSink consumes tee'd frames.
type CaptureSink interface {
	WriteFrame(tsUnixNano int64, f Frame)
	Close() error
}
