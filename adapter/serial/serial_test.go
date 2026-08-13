package serial

import "testing"

// TestOpenRejectsEmptyDevice proves Open guards against an empty device path before
// touching the serial library (a misconfigured serial interface is a clear error,
// not a library-level open failure).
func TestOpenRejectsEmptyDevice(t *testing.T) {
	if _, err := Open(Config{Device: ""}); err == nil {
		t.Fatal("Open with empty device = nil error, want rejection")
	}
}

// TestDefaultConfig pins the default: the given device at DefaultBaud.
func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig("/dev/ttyUSB0")
	if cfg.Device != "/dev/ttyUSB0" {
		t.Fatalf("Device = %q, want /dev/ttyUSB0", cfg.Device)
	}
	if cfg.Baud != DefaultBaud {
		t.Fatalf("Baud = %d, want DefaultBaud %d", cfg.Baud, DefaultBaud)
	}
	if cfg.NoFlowControl {
		t.Fatal("NoFlowControl = true, want false: RTS/CTS is on by default")
	}
}

// TestDefaultRTSCTS pins hardware flow control ON by default. TashTalk accepts host
// bytes at 1 Mbit/s but clocks them onto LocalTalk at 230.4 kbaud, so without CTS
// back-pressure its receive buffer overruns mid-frame and the truncated LLAP frame
// fails FCS and vanishes. tashrouter, the reference implementation, opens its port
// with rtscts=True for the same reason. Flipping this to false is a regression.
func TestDefaultRTSCTS(t *testing.T) {
	if !DefaultRTSCTS {
		t.Fatal("DefaultRTSCTS = false, want true (TashTalk needs RTS/CTS; see tashrouter)")
	}
	// The effective flag Open passes to the serial library: on unless opted out.
	rtscts := func(cfg Config) bool { return DefaultRTSCTS && !cfg.NoFlowControl }
	if !rtscts(Config{}) {
		t.Fatal("a zero Config resolves to RTS/CTS off, want on")
	}
	if rtscts(Config{NoFlowControl: true}) {
		t.Fatal("NoFlowControl=true still resolves to RTS/CTS on, want off")
	}
}
