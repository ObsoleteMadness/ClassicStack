package ipx

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/port/rawlink"
	protocol "github.com/ObsoleteMadness/ClassicStack/protocol/ipx"
)

// fakeRawLink is a channel-backed RawLink suitable for unit tests.
// Inbound frames are queued via Push; ReadFrame blocks (with a
// timeout) on the queue. Outbound frames written via WriteFrame
// accumulate in Sent for assertions.
type fakeRawLink struct {
	in  chan []byte
	mu  sync.Mutex
	out [][]byte
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

// buildEthernetIIIPX wraps the IPX bytes in a 14-byte Ethernet II
// header with EtherType 0x8137.
func buildEthernetIIIPX(payload []byte) []byte {
	frame := make([]byte, 14+len(payload))
	frame[12] = 0x81
	frame[13] = 0x37
	copy(frame[14:], payload)
	return frame
}

// buildRaw8023IPX wraps the IPX bytes in an 802.3 length-encoded
// frame (the Ethernet "type" slot carries a length ≤ 0x05DC).
func buildRaw8023IPX(payload []byte) []byte {
	frame := make([]byte, 14+len(payload))
	frame[12] = byte(len(payload) >> 8)
	frame[13] = byte(len(payload))
	copy(frame[14:], payload)
	return frame
}

// buildLLCIPX wraps the IPX bytes in 802.3 + LLC with DSAP=SSAP=0xE0
// and a UI control byte. The IPX body sits at offset 17.
func buildLLCIPX(payload []byte) []byte {
	const llcLen = 3
	frame := make([]byte, 14+llcLen+len(payload))
	total := llcLen + len(payload)
	frame[12] = byte(total >> 8)
	frame[13] = byte(total)
	frame[14] = 0xE0
	frame[15] = 0xE0
	frame[16] = 0x03
	copy(frame[17:], payload)
	return frame
}

// makeIPXBytes builds a minimal valid IPX datagram with checksum
// 0xFFFF. Payload is the bytes that follow the 30-byte IPX header.
func makeIPXBytes(t *testing.T, body []byte) []byte {
	t.Helper()
	d := &protocol.Datagram{
		Hops:    1,
		Type:    4,
		DstSock: [2]byte{0x04, 0x53},
		SrcSock: [2]byte{0x04, 0x52},
		Payload: body,
	}
	wire, err := d.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	return wire
}

func TestIPXEthernetIIRoundTrip(t *testing.T) {
	link := newFakeRawLink()
	p := NewPort(link)
	defer p.Stop()

	delivered := make(chan *protocol.Datagram, 1)
	p.SetDeliveryCallback(func(d *protocol.Datagram) { delivered <- d })

	if err := p.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	ipxBytes := makeIPXBytes(t, []byte("hi"))
	link.Push(buildEthernetIIIPX(ipxBytes))

	select {
	case got := <-delivered:
		if string(got.Payload) != "hi" {
			t.Fatalf("payload: got %q want %q", got.Payload, "hi")
		}
	case <-time.After(time.Second):
		t.Fatal("no delivery")
	}
}

func TestIPXRaw8023Decoded(t *testing.T) {
	link := newFakeRawLink()
	p := NewPort(link)
	defer p.Stop()

	delivered := make(chan *protocol.Datagram, 1)
	p.SetDeliveryCallback(func(d *protocol.Datagram) { delivered <- d })

	if err := p.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	link.Push(buildRaw8023IPX(makeIPXBytes(t, []byte("raw"))))

	select {
	case got := <-delivered:
		if string(got.Payload) != "raw" {
			t.Fatalf("payload: got %q want %q", got.Payload, "raw")
		}
	case <-time.After(time.Second):
		t.Fatal("no delivery for raw 802.3 framing")
	}
}

func TestIPXLLCDecoded(t *testing.T) {
	link := newFakeRawLink()
	p := NewPort(link)
	defer p.Stop()

	delivered := make(chan *protocol.Datagram, 1)
	p.SetDeliveryCallback(func(d *protocol.Datagram) { delivered <- d })

	if err := p.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	link.Push(buildLLCIPX(makeIPXBytes(t, []byte("llc"))))

	select {
	case got := <-delivered:
		if string(got.Payload) != "llc" {
			t.Fatalf("payload: got %q want %q", got.Payload, "llc")
		}
	case <-time.After(time.Second):
		t.Fatal("no delivery for LLC framing")
	}
}

func TestIPXDedupsImmediateDuplicateInboundFrame(t *testing.T) {
	link := newFakeRawLink()
	p := NewPort(link)
	defer p.Stop()

	delivered := make(chan *protocol.Datagram, 2)
	p.SetDeliveryCallback(func(d *protocol.Datagram) { delivered <- d })

	if err := p.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	frame := buildEthernetIIIPX(makeIPXBytes(t, []byte("dup")))
	link.Push(frame)
	link.Push(frame)

	select {
	case got := <-delivered:
		if string(got.Payload) != "dup" {
			t.Fatalf("payload: got %q want %q", got.Payload, "dup")
		}
	case <-time.After(time.Second):
		t.Fatal("no delivery for first frame")
	}

	select {
	case <-delivered:
		t.Fatal("unexpected second delivery for duplicate frame")
	case <-time.After(100 * time.Millisecond):
		// pass
	}
}

func TestIPXSendEthernetII(t *testing.T) {
	link := newFakeRawLink()
	p := NewPort(link)
	defer p.Stop()
	if err := p.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	d := &protocol.Datagram{
		DstNode: [6]byte{0xDE, 0xAD, 0xBE, 0xEF, 0xCA, 0xFE},
		SrcNode: [6]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
		DstSock: [2]byte{0x04, 0x53},
		SrcSock: [2]byte{0x04, 0x52},
		Payload: []byte("ping"),
	}
	if err := p.Send(d); err != nil {
		t.Fatalf("Send: %v", err)
	}
	link.mu.Lock()
	defer link.mu.Unlock()
	if len(link.out) != 1 {
		t.Fatalf("Sent count: got %d want 1", len(link.out))
	}
	out := link.out[0]
	if out[12] != 0x81 || out[13] != 0x37 {
		t.Fatalf("EtherType: got %02x%02x, want 8137", out[12], out[13])
	}
	// Dst MAC matches the DstNode field per Ethernet II IPX wrapping.
	for i := range 6 {
		if out[i] != d.DstNode[i] {
			t.Fatalf("dst MAC mismatch at byte %d", i)
		}
	}
}
