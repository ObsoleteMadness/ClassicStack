package link

import (
	"errors"
	"time"

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
	// ErrUnsupported indicates the link does not implement an OPTIONAL capability
	// (e.g. SetNodeAddress on a transport with no hardware node filter). It is not a
	// failure: a caller probing for a capability treats it as "nothing to do".
	ErrUnsupported = errors.New("link: unsupported capability")
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

// NodeAddressSetter is implemented by links whose HARDWARE filters inbound frames
// by node address, so a node-claim must arm the filter before anything is received.
// TashTalk is the case in point: its device drops every frame not matching a
// 256-bit node bitmap that starts EMPTY, so an unarmed port transmits normally
// while receiving nothing at all.
//
// Decorators (Capture, Pace, Filter, Dedup) MUST forward this — they embed
// FrameLink as an interface, which does NOT promote extra methods, so a wrapped
// link would otherwise fail the caller's type assertion and silently never arm.
type NodeAddressSetter interface{ SetNodeAddress(node uint8) error }

// setNodeAddressOn forwards a SetNodeAddress call to inner when inner supports it,
// so a decorator can pass the capability through. Returns ErrUnsupported when the
// wrapped link has no hardware node filter.
func setNodeAddressOn(inner FrameLink, node uint8) error {
	if s, ok := inner.(NodeAddressSetter); ok {
		return s.SetNodeAddress(node)
	}
	return ErrUnsupported
}

// Framer adapts a FrameLink to a DatagramLink (DDP framing/deframing).
type Framer interface {
	Framing(FrameLink) (DatagramLink, error)
}

// FilterFunc reports whether a frame passes software filtering.
type FilterFunc func(Frame) bool

// Filter, Dedup, and Bridge decorator bodies live in decorators.go and
// bridge.go respectively (Capture stays here, next to CaptureSink).

type captureLink struct {
	FrameLink
	sink CaptureSink
}

func (c *captureLink) Read() (Frame, error) {
	f, err := c.FrameLink.Read()
	if err == nil && c.sink != nil && len(f) > 0 {
		c.sink.WriteFrame(time.Now().UnixNano(), f)
	}
	return f, err
}

func (c *captureLink) Write(f Frame) error {
	err := c.FrameLink.Write(f)
	if err == nil && c.sink != nil && len(f) > 0 {
		c.sink.WriteFrame(time.Now().UnixNano(), f)
	}
	return err
}

// SetNodeAddress forwards the hardware node-filter capability to the wrapped link.
// Without this passthrough the embedded-interface field would hide the method and a
// captured TashTalk port would never arm its filter (→ receives nothing).
func (c *captureLink) SetNodeAddress(node uint8) error {
	return setNodeAddressOn(c.FrameLink, node)
}

func (c *captureLink) Close() error {
	err := c.FrameLink.Close()
	if c.sink != nil {
		_ = c.sink.Close()
	}
	return err
}

// Capture wraps inner with frame teeing into sink.
func Capture(inner FrameLink, sink CaptureSink) FrameLink {
	if sink == nil {
		return inner
	}
	return &captureLink{FrameLink: inner, sink: sink}
}

// CaptureSink consumes tee'd frames.
type CaptureSink interface {
	WriteFrame(tsUnixNano int64, f Frame)
	Close() error
}
