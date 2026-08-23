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

// dataBits, stopBits, interCharTimeoutMs are the line parameters Open sends to
// go-serial; DefaultBaud/DefaultRTSCTS/Config/DefaultConfig live in config.go.
const (
	dataBits           = 8
	stopBits           = 1
	interCharTimeoutMs = 250
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
		MinimumReadSize:       1,
	})
	if err != nil {
		return nil, fmt.Errorf("serial: open %s: %w", cfg.Device, err)
	}
	return s, nil
}
