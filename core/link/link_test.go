package link

import (
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
)

type loopbackFrameLink struct {
	ch     chan Frame
	closed bool
}

func newLoopbackFrameLink(depth int) *loopbackFrameLink {
	if depth <= 0 {
		depth = 1
	}
	return &loopbackFrameLink{ch: make(chan Frame, depth)}
}

func (l *loopbackFrameLink) Read() (Frame, error) {
	select {
	case f, ok := <-l.ch:
		if !ok {
			return nil, ErrClosed
		}
		return f, nil
	default:
		if l.closed {
			return nil, ErrClosed
		}
		return nil, ErrTimeout
	}
}

func (l *loopbackFrameLink) Write(f Frame) error {
	if l.closed {
		return ErrClosed
	}
	cpy := append(Frame(nil), f...)
	l.ch <- cpy
	return nil
}

func (l *loopbackFrameLink) Close() error {
	if l.closed {
		return ErrClosed
	}
	l.closed = true
	close(l.ch)
	return nil
}

type identityDatagramLink struct{ inner FrameLink }

func (l *identityDatagramLink) ReadDatagram() (ddp.Datagram, error) {
	f, err := l.inner.Read()
	if err != nil {
		return ddp.Datagram{}, err
	}
	return ddp.Decode(f)
}

func (l *identityDatagramLink) WriteDatagram(d ddp.Datagram) error {
	f, err := d.Encode(nil)
	if err != nil {
		return err
	}
	return l.inner.Write(f)
}

func (l *identityDatagramLink) Close() error { return l.inner.Close() }

type identityFramer struct{}

func (identityFramer) Framing(fl FrameLink) (DatagramLink, error) {
	return &identityDatagramLink{inner: fl}, nil
}

func TestLoopbackFrameLink_ReadWrite(t *testing.T) {
	l := newLoopbackFrameLink(1)
	in := Frame{1, 2, 3}
	if err := l.Write(in); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	out, err := l.Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(out) != len(in) || out[0] != in[0] || out[1] != in[1] || out[2] != in[2] {
		t.Fatalf("Read() = %v, want %v", out, in)
	}
}

// TestDecorators_NoOpCases covers the pass-through contracts: a nil/empty
// decorator argument returns inner unchanged so callers can wire decorators
// unconditionally without paying for an empty wrapper.
func TestDecorators_NoOpCases(t *testing.T) {
	inner := newLoopbackFrameLink(1)
	if got := Filter(inner, nil); got != inner {
		t.Fatalf("Filter(nil pass) should return inner unchanged")
	}
	if got := Dedup(inner, 0); got != inner {
		t.Fatalf("Dedup(0 window) should return inner unchanged")
	}
	if got := Capture(inner, nil); got != inner {
		t.Fatalf("Capture(nil sink) should return inner unchanged")
	}
	// Ethernet bridge mode is pure pass-through.
	if got := Bridge(inner, "ethernet"); got != inner {
		t.Fatalf("Bridge(ethernet) should return inner unchanged")
	}
	// Bridge with an unknown mode is lenient: no-op.
	if got := Bridge(inner, "nonsense"); got != inner {
		t.Fatalf("Bridge(invalid) should return inner unchanged")
	}
}

// TestFilter_DropsRejected verifies the Filter decorator drops frames failing
// the predicate and surfaces only those that pass.
func TestFilter_DropsRejected(t *testing.T) {
	inner := newLoopbackFrameLink(4)
	// Pass only frames whose first byte is even.
	fl := Filter(inner, func(f Frame) bool { return len(f) > 0 && f[0]%2 == 0 })

	for _, b := range []byte{1, 2, 3, 4} {
		if err := inner.Write(Frame{b}); err != nil {
			t.Fatalf("seed Write(%d): %v", b, err)
		}
	}

	got := drainFrames(t, fl)
	if len(got) != 2 || got[0][0] != 2 || got[1][0] != 4 {
		t.Fatalf("Filter passed %v, want [[2] [4]]", got)
	}
}

// TestDedup_SuppressesEcho verifies an identical frame seen within the window is
// dropped (kernel loopback echo) while a distinct frame passes.
func TestDedup_SuppressesEcho(t *testing.T) {
	inner := newLoopbackFrameLink(4)
	// 1s window: both identical frames fall inside it in this fast test.
	dl := Dedup(inner, int64(time.Second))

	_ = inner.Write(Frame{0xAA, 0xBB})
	_ = inner.Write(Frame{0xAA, 0xBB}) // duplicate -> dropped
	_ = inner.Write(Frame{0xCC})       // distinct -> passes

	got := drainFrames(t, dl)
	if len(got) != 2 {
		t.Fatalf("Dedup passed %d frames, want 2: %v", len(got), got)
	}
	if got[0][0] != 0xAA || got[1][0] != 0xCC {
		t.Fatalf("Dedup passed %v, want [[AA BB] [CC]]", got)
	}
}

// drainFrames reads until the underlying loopback link reports ErrTimeout
// (empty), collecting every surfaced frame.
func drainFrames(t *testing.T, l FrameLink) []Frame {
	t.Helper()
	var out []Frame
	for {
		f, err := l.Read()
		if err == ErrTimeout {
			return out
		}
		if err != nil {
			t.Fatalf("Read() error = %v", err)
		}
		out = append(out, append(Frame(nil), f...))
	}
}

type mockCaptureSink struct {
	frames [][]byte
	times  []int64
	closed bool
}

func (m *mockCaptureSink) WriteFrame(tsUnixNano int64, f Frame) {
	m.frames = append(m.frames, append([]byte(nil), f...))
	m.times = append(m.times, tsUnixNano)
}

func (m *mockCaptureSink) Close() error {
	m.closed = true
	return nil
}

func TestCaptureDecorator(t *testing.T) {
	inner := newLoopbackFrameLink(2)
	sink := &mockCaptureSink{}
	cl := Capture(inner, sink)

	frame1 := Frame{0x01, 0x02}
	if err := cl.Write(frame1); err != nil {
		t.Fatalf("Write error: %v", err)
	}

	frame2, err := cl.Read()
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}

	if len(sink.frames) != 2 {
		t.Fatalf("Expected 2 frames in sink, got %d", len(sink.frames))
	}

	if string(sink.frames[0]) != string(frame1) {
		t.Errorf("Captured frame 1 mismatch: got %v, want %v", sink.frames[0], frame1)
	}
	if string(sink.frames[1]) != string(frame2) {
		t.Errorf("Captured frame 2 mismatch: got %v, want %v", sink.frames[1], frame2)
	}

	for _, ts := range sink.times {
		if ts <= 0 {
			t.Errorf("Expected positive nanosecond timestamp, got %d", ts)
		}
	}

	if err := cl.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}
	if sink.closed {
		t.Errorf("capture link Close must not close a shared capture sink")
	}
}

func TestIdentityFramer_RoundTripDatagram(t *testing.T) {
	fl := newLoopbackFrameLink(1)
	dl, err := identityFramer{}.Framing(fl)
	if err != nil {
		t.Fatalf("Framing() error = %v", err)
	}
	in := ddp.Datagram{
		Hops:        1,
		DestNetwork: 1,
		SrcNetwork:  2,
		DestNode:    3,
		SrcNode:     4,
		DestSocket:  5,
		SrcSocket:   6,
		DDPType:     7,
		Data:        []byte{0xAA, 0xBB},
	}
	if err := dl.WriteDatagram(in); err != nil {
		t.Fatalf("WriteDatagram() error = %v", err)
	}
	out, err := dl.ReadDatagram()
	if err != nil {
		t.Fatalf("ReadDatagram() error = %v", err)
	}
	if out.Hops != in.Hops ||
		out.DestNetwork != in.DestNetwork ||
		out.SrcNetwork != in.SrcNetwork ||
		out.DestNode != in.DestNode ||
		out.SrcNode != in.SrcNode ||
		out.DestSocket != in.DestSocket ||
		out.SrcSocket != in.SrcSocket ||
		out.DDPType != in.DDPType ||
		len(out.Data) != len(in.Data) ||
		out.Data[0] != in.Data[0] ||
		out.Data[1] != in.Data[1] {
		t.Fatalf("ReadDatagram() = %#v, want %#v", out, in)
	}
}
