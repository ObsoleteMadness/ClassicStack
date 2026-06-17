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
}
