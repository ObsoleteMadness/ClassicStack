package tashtalk

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/link"
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

// encodeInbound builds a device→host wire frame for an LLAP payload: 0x01 start,
// then the payload + FCS with 0x00 escaped as 0x00 0xFF, then the 0x00 0xFD end
// marker. This is the inverse of the adapter's Read path.
func encodeInbound(llap []byte) []byte {
	b1, b2 := fcsBytes(llap)
	body := append(append([]byte{}, llap...), b1, b2)
	out := []byte{startMarker}
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

// TestBuildInitSequence pins the reset/init bytes: 1024 nulls + 0x02.
func TestBuildInitSequence(t *testing.T) {
	got := buildInitSequence()
	if len(got) != 1025 {
		t.Fatalf("init len = %d, want 1025", len(got))
	}
	if got[1024] != resetCmd {
		t.Fatalf("init last byte = 0x%02X, want reset 0x%02X", got[1024], resetCmd)
	}
	for i, b := range got[:1024] {
		if b != 0x00 {
			t.Fatalf("init byte %d = 0x%02X, want 0x00", i, b)
		}
	}
}
