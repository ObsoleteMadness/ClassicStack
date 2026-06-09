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
