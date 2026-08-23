package serial

// Default line parameters. AppleTalk-over-serial (TashTalk, spec/08) runs at
// 1 Mbit/s 8N1; the values are exported defaults so a caller can rely on them when
// the interface leaves Baud unset.
const DefaultBaud = 1000000

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
