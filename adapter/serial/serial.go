//go:build !tinygo

// Open needs github.com/jacobsa/go-serial, which shells out to termios ioctls TinyGo's
// baremetal targets don't implement -- see serial_tinygo.go for the stub those targets
// get instead. Config/DefaultConfig/DefaultRTSCTS are plain data and live in config.go,
// shared by both builds.

package serial

import (
	"errors"
	"fmt"
	"io"

	goserial "github.com/jacobsa/go-serial/serial"
)

// dataBits, stopBits, interCharTimeoutMs, minReadSize are the line parameters Open
// sends to go-serial; DefaultBaud/DefaultRTSCTS/Config/DefaultConfig live in config.go.
const (
	dataBits = 8
	stopBits = 1
	// interCharTimeoutMs is the read timeout (termios VTIME). go-serial requires it
	// to be at least 100 when minReadSize is 0.
	interCharTimeoutMs = 250
	// minReadSize is termios VMIN, and it MUST be 0.
	//
	// With VMIN > 0, VTIME is an INTER-character timer that does not start until the
	// first byte arrives (go-serial's OpenOptions doc says so explicitly), so a Read
	// on an idle line blocks forever. That is not a theoretical concern: closing the
	// fd does not unblock a POSIX read, and SetReadDeadline is a no-op on a serial
	// tty (it is not registered with the runtime poller), so the framer's read loop
	// had no way out at all. A TashTalk port sitting on a quiet wire wedged its own
	// Stop, burned the whole process shutdown budget, and left every component behind
	// it in the teardown order recorded as a deadline failure it had not caused.
	//
	// With VMIN = 0, Read returns after at most interCharTimeoutMs whether or not any
	// data arrived. The framer's read loop already expects this: a zero-byte read is
	// mapped to link.ErrTimeout so the loop can poll its stop channel.
	minReadSize = 0
)

// Open opens the named serial device and returns it as a raw byte stream. The
// caller wraps the result in the transport's framer (tashtalk/ppp/slip). 8N1, the
// configured baud (DefaultBaud when 0), RTS/CTS hardware flow control unless
// cfg.NoFlowControl, and a short inter-character read timeout so a blocked Read
// surfaces periodically (the framer's read loop polls Stop).
func Open(cfg Config) (io.ReadWriteCloser, error) {
	if cfg.Device == "" {
		return nil, errors.New("serial: empty device path")
	}
	baud := cfg.Baud
	if baud == 0 {
		baud = DefaultBaud
	}
	s, err := goserial.Open(goserial.OpenOptions{
		PortName:              normalizeDeviceName(cfg.Device),
		BaudRate:              baud,
		DataBits:              dataBits,
		StopBits:              stopBits,
		ParityMode:            goserial.PARITY_NONE,
		RTSCTSFlowControl:     DefaultRTSCTS && !cfg.NoFlowControl,
		InterCharacterTimeout: interCharTimeoutMs,
		MinimumReadSize:       minReadSize,
	})
	if err != nil {
		return nil, fmt.Errorf("serial: open %s: %w", cfg.Device, err)
	}
	return s, nil
}
