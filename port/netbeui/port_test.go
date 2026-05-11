package netbeui

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/port/rawlink"
	"github.com/ObsoleteMadness/ClassicStack/protocol/netbeui"
)

// fakeRawLink is a channel-backed RawLink for unit tests, identical
// in shape to the one in port/ipx but local to this package because
// it is not part of any exported API.
type fakeRawLink struct {
	in     chan []byte
	mu     sync.Mutex
	out    [][]byte
	closed chan struct{}
}

func newFakeRawLink() *fakeRawLink {
	return &fakeRawLink{
		in:     make(chan []byte, 16),
		closed: make(chan struct{}),
	}
}

func (f *fakeRawLink) Push(frame []byte) {
	select {
	case f.in <- frame:
	case <-f.closed:
	}
}

func (f *fakeRawLink) ReadFrame() ([]byte, error) {
	select {
	case <-f.closed:
		return nil, errors.New("closed")
	case frame := <-f.in:
		return frame, nil
	case <-time.After(50 * time.Millisecond):
		return nil, rawlink.ErrTimeout
	}
}

func (f *fakeRawLink) WriteFrame(frame []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]byte, len(frame))
	copy(cp, frame)
	f.out = append(f.out, cp)
	return nil
}

func (f *fakeRawLink) Close() error {
	select {
	case <-f.closed:
	default:
		close(f.closed)
	}
	return nil
}

// buildLLCNBF wraps an NBF body in 14 bytes of Ethernet and an LLC
// header. The control field can be either 1 byte (UI/U format) or 2
// bytes (I/S format). All MACs are zero.
func buildLLCNBF(body []byte, control ...byte) []byte {
	return buildLLCNBFAddressed([6]byte{}, [6]byte{}, body, control...)
}

// buildLLCNBFAddressed builds a frame with explicit dst/src MACs.
func buildLLCNBFAddressed(dst, src [6]byte, body []byte, control ...byte) []byte {
	if len(control) == 0 {
		control = []byte{0x03}
	}
	llcLen := 2 + len(control)
	frame := make([]byte, 14+llcLen+len(body))
	copy(frame[0:6], dst[:])
	copy(frame[6:12], src[:])
	total := llcLen + len(body)
	frame[12] = byte(total >> 8)
	frame[13] = byte(total)
	frame[14] = 0xF0
	frame[15] = 0xF0
	copy(frame[16:16+len(control)], control)
	copy(frame[14+llcLen:], body)
	return frame
}

func TestNetBEUIInboundDecodesNBFBody(t *testing.T) {
	link := newFakeRawLink()
	p := NewPort(link)
	defer p.Stop()

	delivered := make(chan *netbeui.Frame, 1)
	p.SetDeliveryCallback(func(_ [6]byte, _ [6]byte, f *netbeui.Frame) { delivered <- f })

	if err := p.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	want := &netbeui.Frame{
		Command:            0x08,
		ResponseCorrelator: 0x4242,
		Payload:            []byte("payload"),
	}
	copy(want.DestinationName[:], "WS01            ")
	copy(want.SourceName[:], "SERVER          ")
	body, err := want.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	link.Push(buildLLCNBF(body))

	select {
	case got := <-delivered:
		if got.Command != want.Command || got.ResponseCorrelator != want.ResponseCorrelator {
			t.Fatalf("header mismatch: got %+v want %+v", got, want)
		}
		if string(got.Payload) != "payload" {
			t.Fatalf("payload: got %q want %q", got.Payload, want.Payload)
		}
	case <-time.After(time.Second):
		t.Fatal("no delivery")
	}
}

func TestNetBEUIInboundDecodesNBFBodyWithTwoByteLLCControl(t *testing.T) {
	link := newFakeRawLink()
	p := NewPort(link)
	defer p.Stop()

	// Give the port a source MAC and configure a matching destination MAC for
	// inbound frames so the I-frame dstMAC check passes.
	ourMAC := [6]byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF}
	remotMAC := [6]byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66}
	p.SetSourceMAC(ourMAC)

	delivered := make(chan *netbeui.Frame, 1)
	p.SetDeliveryCallback(func(_ [6]byte, _ [6]byte, f *netbeui.Frame) { delivered <- f })

	if err := p.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Establish an LLC Type-2 connection by sending a SABME first.
	sabme := buildLLCNBFAddressed(ourMAC, remotMAC, nil, 0x7F) // SABME to ourMAC
	link.Push(sabme)
	time.Sleep(20 * time.Millisecond) // let the port process the SABME

	want := &netbeui.Frame{
		Command:        netbeui.CmdSessionInitialize,
		Data1:          0x81,
		Data2:          0x05B8,
		XmitCorrelator: 0x4242,
		DestNumber:     4,
		SourceNumber:   1,
	}
	body, err := want.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	// Send I-frame addressed to our MAC from the remote.
	link.Push(buildLLCNBFAddressed(ourMAC, remotMAC, body, 0x00, 0x00))

	select {
	case got := <-delivered:
		if got.Command != want.Command || got.Data2 != want.Data2 {
			t.Fatalf("header mismatch: got %+v want %+v", got, want)
		}
		if got.DestNumber != want.DestNumber || got.SourceNumber != want.SourceNumber {
			t.Fatalf("session numbers: got dest=%d src=%d want dest=%d src=%d", got.DestNumber, got.SourceNumber, want.DestNumber, want.SourceNumber)
		}
	case <-time.After(time.Second):
		t.Fatal("no delivery")
	}
}

func TestNetBEUISendBuildsLLCFrame(t *testing.T) {
	link := newFakeRawLink()
	p := NewPort(link)
	defer p.Stop()
	if err := p.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	src := [6]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06}
	dst := [6]byte{0xDE, 0xAD, 0xBE, 0xEF, 0xCA, 0xFE}
	p.SetSourceMAC(src)

	frame := &netbeui.Frame{Command: 0x08, Payload: []byte("hi")}
	copy(frame.DestinationName[:], "WS01            ")
	copy(frame.SourceName[:], "SERVER          ")

	if err := p.Send(dst, frame); err != nil {
		t.Fatalf("Send: %v", err)
	}

	link.mu.Lock()
	defer link.mu.Unlock()
	if len(link.out) != 1 {
		t.Fatalf("Sent count: got %d want 1", len(link.out))
	}
	out := link.out[0]
	for i := range 6 {
		if out[i] != dst[i] {
			t.Fatalf("dst MAC at byte %d: got %02x want %02x", i, out[i], dst[i])
		}
	}
	for i := range 6 {
		if out[6+i] != src[i] {
			t.Fatalf("src MAC at byte %d: got %02x want %02x", i, out[6+i], src[i])
		}
	}
	// 802.3 length-encoded; EtherType slot must be ≤ 0x05DC.
	length := uint16(out[12])<<8 | uint16(out[13])
	if length > 0x05DC {
		t.Fatalf("length field too large: got %#x", length)
	}
	if out[14] != 0xF0 || out[15] != 0xF0 || out[16] != 0x03 {
		t.Fatalf("LLC header: got %02x%02x%02x", out[14], out[15], out[16])
	}
}

func TestNetBEUISendRequiresSourceMAC(t *testing.T) {
	link := newFakeRawLink()
	p := NewPort(link)
	defer p.Stop()
	if err := p.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	dst := [6]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06}
	if err := p.Send(dst, &netbeui.Frame{Command: 0x08}); err != ErrNoSourceMAC {
		t.Fatalf("expected ErrNoSourceMAC, got %v", err)
	}
}
