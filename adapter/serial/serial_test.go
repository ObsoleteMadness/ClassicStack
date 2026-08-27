package serial

import (
	"errors"
	"io"
	"testing"
)

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

// fakeStream replays a scripted sequence of Read results.
type fakeStream struct {
	steps []struct {
		data []byte
		err  error
	}
	i int
}

func (f *fakeStream) Read(p []byte) (int, error) {
	if f.i >= len(f.steps) {
		return 0, io.EOF
	}
	s := f.steps[f.i]
	f.i++
	return copy(p, s.data), s.err
}
func (f *fakeStream) Write(p []byte) (int, error) { return len(p), nil }
func (f *fakeStream) Close() error                { return nil }

// TestIdleReaderSwallowsTimeoutEOF pins the VMIN=0 contract: a zero-byte read is the
// read timeout on an idle line, not end-of-stream, and must NOT reach a framer as
// io.EOF (TashTalk maps that to link.ErrClosed and would tear the port down on every
// quiet interval). A real device failure, and a read that actually returns data, both
// pass through unchanged.
func TestIdleReaderSwallowsTimeoutEOF(t *testing.T) {
	devGone := errors.New("input/output error")
	f := &fakeStream{steps: []struct {
		data []byte
		err  error
	}{
		{nil, io.EOF},             // idle-line timeout
		{[]byte{0x01, 0x02}, nil}, // real data
		{[]byte{0x03}, io.EOF},    // data AND EOF: keep the EOF, there are bytes
		{nil, devGone},            // device failure
	}}
	r := &idleReader{ReadWriteCloser: f}
	buf := make([]byte, 8)

	if n, err := r.Read(buf); n != 0 || err != nil {
		t.Fatalf("idle read = (%d, %v), want (0, nil)", n, err)
	}
	if n, err := r.Read(buf); n != 2 || err != nil {
		t.Fatalf("data read = (%d, %v), want (2, nil)", n, err)
	}
	if n, err := r.Read(buf); n != 1 || !errors.Is(err, io.EOF) {
		t.Fatalf("data+EOF read = (%d, %v), want (1, EOF)", n, err)
	}
	if n, err := r.Read(buf); n != 0 || !errors.Is(err, devGone) {
		t.Fatalf("device-failure read = (%d, %v), want (0, %v)", n, err, devGone)
	}
}
