//go:build tinygo

// TinyGo's baremetal targets have no OS serial port to open via termios (see
// serial.go): a board wires its UART directly (see hardware/*/cts.go) instead of
// going through this package's Open.
package serial

import (
	"errors"
	"io"
)

// ErrUnsupported is returned by Open on builds with no OS serial port.
var ErrUnsupported = errors.New("serial: Open is not supported on this build")

// Open is a stub on TinyGo builds: see ErrUnsupported.
func Open(_ Config) (io.ReadWriteCloser, error) {
	return nil, ErrUnsupported
}
