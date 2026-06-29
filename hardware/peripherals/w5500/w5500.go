//go:build tinygo

package w5500

import (
	"errors"
	"machine"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"tinygo.org/x/drivers/w5500"
)

type w5500Link struct {
	dev    *w5500.Device
	closed bool
}

// Compile-time assertion: *w5500Link satisfies link.FrameLink.
var _ link.FrameLink = (*w5500Link)(nil)

func OpenW5500(spi *machine.SPI, csPin, rstPin, intPin machine.Pin) (link.FrameLink, error) {
	// Configure control pins
	csPin.Configure(machine.PinConfig{Mode: machine.PinOutput})
	csPin.High()

	if rstPin != machine.NoPin {
		rstPin.Configure(machine.PinConfig{Mode: machine.PinOutput})
		// Hard reset W5500
		rstPin.Low()
		time.Sleep(10 * time.Millisecond)
		rstPin.High()
		time.Sleep(50 * time.Millisecond)
	}

	dev := w5500.New(spi, csPin, intPin)
	
	// Initialize W5500
	err := dev.Configure()
	if err != nil {
		return nil, errors.New("w5500: failed to configure device over SPI")
	}

	// Configure Socket 0 in MACRAW mode
	// Socket 0 is the only socket that supports MACRAW on W5500.
	err = dev.OpenMACRAW(0)
	if err != nil {
		return nil, errors.New("w5500: failed to open Socket 0 in MACRAW mode")
	}

	return &w5500Link{
		dev: dev,
	}, nil
}

func (l *w5500Link) Read() (link.Frame, error) {
	if l.closed {
		return nil, link.ErrClosed
	}

	// Read raw L2 Ethernet frame from Socket 0
	// We poll or block until a packet is available in the W5500 RX buffer.
	for {
		if l.closed {
			return nil, link.ErrClosed
		}
		
		n, err := l.dev.GetRxSize(0)
		if err != nil {
			return nil, err
		}
		if n > 0 {
			buf := make([]byte, n)
			readLen, err := l.dev.Read(0, buf)
			if err != nil {
				return nil, err
			}
			return buf[:readLen], nil
		}
		
		time.Sleep(2 * time.Millisecond)
	}
}

func (l *w5500Link) Write(frame link.Frame) error {
	if l.closed {
		return link.ErrClosed
	}

	// Send raw L2 Ethernet frame through Socket 0
	_, err := l.dev.Write(0, frame)
	if err != nil {
		return err
	}
	return l.dev.Send(0)
}

func (l *w5500Link) Close() error {
	if l.closed {
		return nil
	}
	l.closed = true
	return l.dev.Close(0)
}
