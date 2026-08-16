package tashtalk

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
)

// fakeSerial is an in-memory io.ReadWriteCloser standing in for the serial port:
// bytes Written by the test are readable here (rx), and what the adapter Writes
// is captured in tx. Read blocks until rx has bytes or Close.
type fakeSerial struct {
	mu     sync.Mutex
	rx     []byte // bytes the device "sends" to the host (adapter reads these)
	tx     bytes.Buffer
	closed bool
	signal chan struct{}
}

func newFakeSerial() *fakeSerial { return &fakeSerial{signal: make(chan struct{}, 1)} }

// push enqueues device→host bytes and wakes a blocked Read.
func (f *fakeSerial) push(b []byte) {
	f.mu.Lock()
	f.rx = append(f.rx, b...)
	f.mu.Unlock()
	select {
	case f.signal <- struct{}{}:
	default:
	}
}

func (f *fakeSerial) Read(p []byte) (int, error) {
	for {
		f.mu.Lock()
		if f.closed {
			f.mu.Unlock()
			return 0, io.EOF
		}
		if len(f.rx) > 0 {
			n := copy(p, f.rx)
			f.rx = f.rx[n:]
			f.mu.Unlock()
			return n, nil
		}
		f.mu.Unlock()
		<-f.signal // wait for push or Close
	}
}

func (f *fakeSerial) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return 0, io.ErrClosedPipe
	}
	return f.tx.Write(p)
}

func (f *fakeSerial) Close() error {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
	select {
	case f.signal <- struct{}{}:
	default:
	}
	return nil
}

// newTestLink wraps a fakeSerial in a *frameLink without going through Open (no
// real serial port, no init write).
func newTestLink(s io.ReadWriteCloser) *frameLink {
	return &frameLink{s: s, rdBuf: make([]byte, 0, 64)}
}

// encodeInbound builds a device→host wire frame for an LLAP payload: the payload +
// FCS with 0x00 escaped as 0x00 0xFF, then the 0x00 0xFD end marker. This is the
// inverse of the adapter's Read path.
//
// NOTE: NO 0x01 start marker. That marker is HOST→DEVICE ONLY — real TashTalk
// hardware does not prefix its frames with it. This fixture used to prepend one,
// which is why the suite stayed green while the adapter waited forever for a start
// marker that never arrives and received nothing on real hardware.
func encodeInbound(llap []byte) []byte {
	b1, b2 := fcsBytes(llap)
	body := append(append([]byte{}, llap...), b1, b2)
	out := []byte{}
	for _, b := range body {
		if b == 0x00 {
			out = append(out, escapePfx, escDataNull)
		} else {
			out = append(out, b)
		}
	}
	out = append(out, escapePfx, escEndFrame)
	return out
}

// TestNewStreamSendsInit proves NewStream wraps a provided byte stream (the §3b/D7
// shape: the device-open lives in adapter/serial, this is a framer over the stream)
// and sends the reset/init sequence (1024 nulls + 0x02) on it. A nil stream is
// rejected.
func TestNewStreamSendsInit(t *testing.T) {
	if _, err := NewStream(nil); err == nil {
		t.Fatal("NewStream(nil) = nil error, want rejection")
	}
	fs := newFakeSerial()
	fl, err := NewStream(fs)
	if err != nil {
		t.Fatalf("NewStream: %v", err)
	}
	if fl == nil {
		t.Fatal("NewStream returned a nil FrameLink")
	}
	if got := fs.tx.Bytes(); !bytes.Equal(got, buildInitSequence()) {
		t.Fatalf("init sequence on wrap = % X (len %d), want the 1024-null + 0x02 reset (len %d)", got, len(got), len(buildInitSequence()))
	}
}

// TestWriteFraming proves Write emits 0x01 + frame + FCS (no escaping outbound,
// per spec/08).
func TestWriteFraming(t *testing.T) {
	fs := newFakeSerial()
	l := newTestLink(fs)

	llap := []byte{0xFF, 0x42, 0x01, 0xDE, 0xAD}
	if err := l.Write(llap); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got := fs.tx.Bytes()
	b1, b2 := fcsBytes(llap)
	want := append(append([]byte{startMarker}, llap...), b1, b2)
	if !bytes.Equal(got, want) {
		t.Fatalf("Write wire = % X, want % X", got, want)
	}
}

// TestReadDecodesFrame proves Read runs the escape state machine + FCS check and
// returns the clean LLAP payload, including an escaped null in the data.
func TestReadDecodesFrame(t *testing.T) {
	fs := newFakeSerial()
	l := newTestLink(fs)

	llap := []byte{0x01, 0x02, 0x02, 0x00, 0x99} // contains a 0x00 to exercise escaping
	fs.push(encodeInbound(llap))

	got, err := l.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !bytes.Equal(got, llap) {
		t.Fatalf("Read = % X, want % X (FCS+framing should be stripped)", got, llap)
	}
}

// TestReadReassemblesAcrossChunks proves a frame split across two serial reads
// still reassembles (the state machine is fed across Read calls), including a
// split landing right after an escape prefix at a chunk boundary.
func TestReadReassemblesAcrossChunks(t *testing.T) {
	fs := newFakeSerial()
	l := newTestLink(fs)

	llap := []byte{0xFF, 0x10, 0x02, 0x00, 0x77}
	wire := encodeInbound(llap)
	// Split so the first chunk ends on an escape prefix (0x00) whose code is in
	// the next chunk — the tail-escape stash path.
	split := bytes.IndexByte(wire, escapePfx)
	if split < 0 || split+1 >= len(wire) {
		t.Fatalf("test frame has no mid escape to split on: % X", wire)
	}
	fs.push(wire[:split+1]) // up to and including the escape prefix
	fs.push(wire[split+1:]) // the escape code and the rest

	got, err := l.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !bytes.Equal(got, llap) {
		t.Fatalf("reassembled Read = % X, want % X", got, llap)
	}
}

// TestReadRejectsBadFCS proves a frame with a corrupt FCS is discarded (Read does
// not return it); with only the bad frame on the wire, Read sees EOF after Close.
func TestReadRejectsBadFCS(t *testing.T) {
	fs := newFakeSerial()
	l := newTestLink(fs)

	llap := []byte{0xFF, 0x42, 0x01, 0x05, 0x06}
	wire := encodeInbound(llap)
	wire[len(wire)-3] ^= 0xFF // corrupt the last FCS byte (before the 0x00 0xFD end)
	fs.push(wire)
	fs.Close() // so Read returns rather than blocking once the bad frame is drained

	if _, err := l.Read(); !errors.Is(err, link.ErrClosed) {
		t.Fatalf("Read after only-bad-FCS frame = %v, want ErrClosed (frame discarded)", err)
	}
}

// TestReadShortFrameDiscarded proves a frame shorter than an LLAP header + FCS is
// dropped.
func TestReadShortFrameDiscarded(t *testing.T) {
	fs := newFakeSerial()
	l := newTestLink(fs)
	fs.push(encodeInbound([]byte{0x01, 0x02})) // 2 bytes < minLLAPFrame
	fs.Close()
	if _, err := l.Read(); !errors.Is(err, link.ErrClosed) {
		t.Fatalf("Read of short frame = %v, want ErrClosed (discarded)", err)
	}
}

// TestClosedTerminal: after Close, Read and Write are terminal.
func TestClosedTerminal(t *testing.T) {
	fs := newFakeSerial()
	l := newTestLink(fs)
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("second Close = %v, want nil (idempotent)", err)
	}
	if _, err := l.Read(); !errors.Is(err, link.ErrClosed) {
		t.Errorf("Read after Close = %v, want ErrClosed", err)
	}
	if err := l.Write([]byte{1, 2, 3}); !errors.Is(err, link.ErrClosed) {
		t.Errorf("Write after Close = %v, want ErrClosed", err)
	}
}

// TestBuildInitSequence pins the FULL init sequence: 1024 nulls, then a COMPLETE
// 33-byte set-node-address command (0x02 + 32-byte empty bitmap), then 0x03 0x00.
// Regression: this used to emit 0x02 alone, leaving the device consuming the next
// 32 wire bytes as bitmap data — the port transmitted but received nothing.
func TestBuildInitSequence(t *testing.T) {
	got := buildInitSequence()
	want := 1024 + nodeAddrCmdLen + 2
	if len(got) != want {
		t.Fatalf("init len = %d, want %d", len(got), want)
	}
	for i, b := range got[:1024] {
		if b != 0x00 {
			t.Fatalf("init byte %d = 0x%02X, want 0x00", i, b)
		}
	}
	if got[1024] != setNodeAddrCmd {
		t.Fatalf("init[1024] = 0x%02X, want set-node-address 0x%02X", got[1024], setNodeAddrCmd)
	}
	// The 32-byte bitmap must be present and empty (receive nothing until claimed).
	for i, b := range got[1025 : 1024+nodeAddrCmdLen] {
		if b != 0x00 {
			t.Fatalf("init bitmap byte %d = 0x%02X, want 0x00", i, b)
		}
	}
	if tail := got[1024+nodeAddrCmdLen:]; tail[0] != 0x03 || tail[1] != 0x00 {
		t.Fatalf("init tail = % X, want 03 00", tail)
	}
}

// TestBuildSetNodeAddressCmd pins the hardware receive-filter command. Without it
// TashTalk drops every inbound frame in hardware, which is why the port could
// transmit RTMP happily while receiving absolutely nothing.
func TestBuildSetNodeAddressCmd(t *testing.T) {
	// The default LocalTalk node 0xFE (254) → bit 254: byte 254/8=31, bit 254%8=6.
	cmd, err := buildSetNodeAddressCmd(0xFE)
	if err != nil {
		t.Fatalf("buildSetNodeAddressCmd(0xFE): %v", err)
	}
	if len(cmd) != nodeAddrCmdLen {
		t.Fatalf("cmd len = %d, want %d", len(cmd), nodeAddrCmdLen)
	}
	if cmd[0] != setNodeAddrCmd {
		t.Fatalf("cmd[0] = 0x%02X, want 0x%02X", cmd[0], setNodeAddrCmd)
	}
	if cmd[1+31] != 1<<6 {
		t.Fatalf("bitmap byte 31 = 0x%02X, want 0x%02X (bit for node 254)", cmd[1+31], 1<<6)
	}
	for i, b := range cmd[1:] {
		if i != 31 && b != 0x00 {
			t.Fatalf("bitmap byte %d = 0x%02X, want 0x00 (only node 254 set)", i, b)
		}
	}

	// node 0 clears the filter: a valid command with an all-zero bitmap.
	zero, err := buildSetNodeAddressCmd(0)
	if err != nil {
		t.Fatalf("buildSetNodeAddressCmd(0): %v", err)
	}
	for i, b := range zero[1:] {
		if b != 0x00 {
			t.Fatalf("cleared bitmap byte %d = 0x%02X, want 0x00", i, b)
		}
	}

	// 255 is broadcast, not assignable.
	if _, err := buildSetNodeAddressCmd(255); err == nil {
		t.Fatal("buildSetNodeAddressCmd(255) = nil error, want rejection")
	}
}

// TestSetNodeAddressWritesCommand proves SetNodeAddress reaches the serial stream,
// so the claim hook actually arms the hardware filter.
func TestSetNodeAddressWritesCommand(t *testing.T) {
	s := newFakeSerial()
	fl, err := NewStream(s)
	if err != nil {
		t.Fatalf("NewStream: %v", err)
	}
	na, ok := fl.(interface{ SetNodeAddress(uint8) error })
	if !ok {
		t.Fatal("frameLink does not expose SetNodeAddress")
	}
	s.mu.Lock()
	initLen := s.tx.Len()
	s.mu.Unlock()

	if err := na.SetNodeAddress(0xFE); err != nil {
		t.Fatalf("SetNodeAddress: %v", err)
	}

	s.mu.Lock()
	got := append([]byte(nil), s.tx.Bytes()[initLen:]...)
	s.mu.Unlock()
	want, _ := buildSetNodeAddressCmd(0xFE)
	if len(got) != len(want) {
		t.Fatalf("wrote %d bytes, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("byte %d = 0x%02X, want 0x%02X", i, got[i], want[i])
		}
	}
}

// shortSerial writes only the first `limit` bytes of any packet and reports that
// count with a nil error — the documented device-overrun mode (spec/08: the host
// feeds at 1 Mbit/s while the device clocks LocalTalk at 230.4 kbaud, so its buffer
// overruns mid-frame). A truncated LLAP frame fails FCS at the receiver and simply
// DISAPPEARS, so before this was detected the loss was invisible on both sides.
type shortSerial struct {
	limit int
	tx    bytes.Buffer
}

func (s *shortSerial) Read(p []byte) (int, error) { return 0, io.EOF }
func (s *shortSerial) Write(p []byte) (int, error) {
	n := len(p)
	if n > s.limit {
		n = s.limit
	}
	s.tx.Write(p[:n])
	return n, nil
}
func (s *shortSerial) Close() error { return nil }

// TestShortInitWriteIsRejected pins that a truncated init leaves the device in an
// indeterminate state and must fail construction rather than run on. The 0x02
// set-node-address command is 33 bytes; a partial one desynchronises the firmware's
// command stream, which then eats subsequent LLAP bytes as bitmap data.
func TestShortInitWriteIsRejected(t *testing.T) {
	s := &shortSerial{limit: 10} // init is >1024 bytes; only 10 land
	if _, err := NewStreamLogged(s, nil); err == nil {
		t.Fatal("NewStreamLogged with a short init write = nil error, want rejection")
	}
}

// TestShortFrameWriteIsReported pins that a short write of a DATA frame surfaces.
// It must not be silently discarded: this is the exact path by which a frame the
// server believes it sent never reaches the wire.
func TestShortFrameWriteIsReported(t *testing.T) {
	// Let the full init through, then truncate everything after it.
	init := len(buildInitSequence())
	s := &shortSerial{limit: init}
	fl, err := NewStreamLogged(s, nil)
	if err != nil {
		t.Fatalf("NewStreamLogged: %v", err)
	}
	s.limit = 3 // any subsequent frame is truncated to 3 bytes

	rec := &recordLogger{}
	fl.(*frameLink).logger = rec

	if err := fl.Write([]byte{0x01, 0xFE, 0x01, 0x00, 0x00}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if !rec.sawShortWrite() {
		t.Fatal("a short frame write produced no error record; truncated frames would vanish silently")
	}
}

// recordLogger captures emitted records so a test can assert an error was actually
// reported rather than swallowed.
type recordLogger struct {
	mu   sync.Mutex
	msgs []string
}

func (r *recordLogger) With(...log.Field) log.Logger { return r }
func (r *recordLogger) Enabled(log.Level) bool       { return true }
func (r *recordLogger) Log(_ log.Level, msg string, _ ...log.Field) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs = append(r.msgs, msg)
}
func (r *recordLogger) Log0(l log.Level, msg string)                 { r.Log(l, msg) }
func (r *recordLogger) Log1(l log.Level, msg string, _ log.Field)    { r.Log(l, msg) }
func (r *recordLogger) Log2(l log.Level, msg string, _, _ log.Field) { r.Log(l, msg) }

func (r *recordLogger) sawShortWrite() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, m := range r.msgs {
		if strings.Contains(m, "SHORT serial write") {
			return true
		}
	}
	return false
}
