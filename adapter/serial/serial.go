package serial

import (
	"errors"
	"fmt"
	"io"

	goserial "github.com/jacobsa/go-serial/serial"
)

// Default line parameters. AppleTalk-over-serial (TashTalk, spec/08) runs at
// 1 Mbit/s 8N1; the values are exported defaults so a caller can rely on them when
// the interface leaves Baud unset.
const (
	DefaultBaud        = 1000000
	dataBits           = 8
	stopBits           = 1
	interCharTimeoutMs = 250
)

// Config holds the parameters for opening a serial device. Device is the OS path
// (e.g. "COM3" or "/dev/ttyUSB0"); Baud is the line speed (0 → DefaultBaud).
type Config struct {
	Device string
	Baud   uint
	// NoFlowControl disables RTS/CTS hardware flow control, which is ON by default
	// (see DefaultRTSCTS). Only set it for an adapter whose CTS line is not wired.
	NoFlowControl bool
}

// DefaultRTSCTS reports whether RTS/CTS hardware flow control is enabled when a
// Config leaves NoFlowControl unset. It is true: TashTalk clocks LocalTalk frames
// at 230.4 kbaud into a host link running at 1 Mbit/s, so the adapter must be able
// to stop the host mid-frame or its receive buffer overruns and bytes are dropped
// silently (a truncated LLAP frame just fails FCS and disappears). The reference
// implementation, tashrouter, opens its port with rtscts=True for the same reason.
const DefaultRTSCTS = true

// DefaultConfig returns a Config for device at the default baud, with RTS/CTS on.
func DefaultConfig(device string) Config { return Config{Device: device, Baud: DefaultBaud} }

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
