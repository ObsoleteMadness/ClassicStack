package ipx

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
	ipxproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx"
)

// fakeFrameLink is an in-test link.FrameLink: queued frames are returned by
// Read (then ErrTimeout to idle the loop); written frames are captured.
type fakeFrameLink struct {
	mu     sync.Mutex
	inbox  [][]byte
	sent   [][]byte
	closed bool
}

func (f *fakeFrameLink) push(frame []byte) {
	f.mu.Lock()
	f.inbox = append(f.inbox, frame)
	f.mu.Unlock()
}

func (f *fakeFrameLink) Read() (link.Frame, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil, link.ErrClosed
	}
	if len(f.inbox) > 0 {
		frame := f.inbox[0]
		f.inbox = f.inbox[1:]
		return frame, nil
	}
	return nil, link.ErrTimeout
}

func (f *fakeFrameLink) Write(frame link.Frame) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return link.ErrClosed
	}
	cp := make([]byte, len(frame))
	copy(cp, frame)
	f.sent = append(f.sent, cp)
	return nil
}

func (f *fakeFrameLink) Close() error {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
	return nil
}

func (f *fakeFrameLink) sentCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

func enabledModel(t *testing.T) *config.Model {
	t.Helper()
	m := config.NewModel()
	m.Set(&port.Section{SKey: Name, Iface: "eth0", IsEnabled: true})
	return m
}

func newTestLogger() log.Logger {
	return log.New(Name, log.NewStderrSink(log.NewLevelVar(log.Warn)))
}

// ethIPXFrame wraps an IPX datagram in an Ethernet II (0x8137) frame.
func ethIPXFrame(dst, src [6]byte, ipxBytes []byte) []byte {
	frame := make([]byte, 0, ethHdrLen+len(ipxBytes))
	frame = append(frame, dst[:]...)
	frame = append(frame, src[:]...)
	frame = append(frame, 0x81, 0x37)
	frame = append(frame, ipxBytes...)
	return frame
}

func sampleDatagram() *ipxproto.Datagram {
	d := &ipxproto.Datagram{Type: 0x04}
	d.DstSock = [2]byte{0x04, 0x53}
	d.SrcSock = [2]byte{0x04, 0x53}
	d.Payload = []byte{0xAA, 0xBB, 0xCC}
	return d
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for range 1000 {
		if cond() {
			return
		}
		runtime.Gosched()
		time.Sleep(time.Millisecond)
	}
}

func TestDisabledReturnsNil(t *testing.T) {
	m := config.NewModel()
	m.Set(&port.Section{SKey: Name, Iface: "eth0", IsEnabled: false})
	c, err := New(m, nil, [6]byte{}, newTestLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c != nil {
		t.Fatalf("disabled section must yield nil, got %T", c)
	}
}

func TestInboundEthernetIIDelivered(t *testing.T) {
	fl := &fakeFrameLink{}
	ipxBytes, _ := sampleDatagram().Encode(nil)
	fl.push(ethIPXFrame([6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}, [6]byte{1, 2, 3, 4, 5, 6}, ipxBytes))

	c, err := New(enabledModel(t), fl, [6]byte{}, newTestLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var got *ipxproto.Datagram
	var mu sync.Mutex
	c.(*Port).SetDeliveryCallback(func(d *ipxproto.Datagram) {
		mu.Lock()
		got = d
		mu.Unlock()
	})
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer c.Stop(context.Background())

	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return got != nil })
	mu.Lock()
	defer mu.Unlock()
	if got == nil {
		t.Fatal("no datagram delivered")
	}
	if got.DstSock != [2]byte{0x04, 0x53} {
		t.Errorf("dst socket = % x, want 04 53", got.DstSock)
	}
}

func TestInbound8023RawAnd802LLC(t *testing.T) {
	fl := &fakeFrameLink{}
	ipxBytes, _ := sampleDatagram().Encode(nil) // starts with 0xFFFF checksum
	dst := [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	src := [6]byte{1, 2, 3, 4, 5, 6}

	// Raw 802.3: 802.3 length-typed, body begins with the 0xFFFF IPX magic.
	raw := make([]byte, 0)
	raw = append(raw, dst[:]...)
	raw = append(raw, src[:]...)
	raw = append(raw, byte(len(ipxBytes)>>8), byte(len(ipxBytes)))
	raw = append(raw, ipxBytes...)
	fl.push(raw)

	// 802.2 LLC: DSAP=SSAP=0xE0 control=0x03 then IPX body.
	llc := make([]byte, 0)
	llc = append(llc, dst[:]...)
	llc = append(llc, src[:]...)
	llcBody := append([]byte{0xE0, 0xE0, 0x03}, ipxBytes...)
	llc = append(llc, byte(len(llcBody)>>8), byte(len(llcBody)))
	llc = append(llc, llcBody...)
	fl.push(llc)

	c, _ := New(enabledModel(t), fl, [6]byte{}, newTestLogger())
	var n int
	var mu sync.Mutex
	c.(*Port).SetDeliveryCallback(func(*ipxproto.Datagram) { mu.Lock(); n++; mu.Unlock() })
	c.Start(context.Background())
	defer c.Stop(context.Background())

	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return n == 2 })
	mu.Lock()
	defer mu.Unlock()
	if n != 2 {
		t.Fatalf("delivered %d datagrams, want 2 (raw 802.3 + 802.2 LLC)", n)
	}
}

func TestSendEncapsulatesEthernetII(t *testing.T) {
	fl := &fakeFrameLink{}
	src := [6]byte{0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0x01}
	c, _ := New(enabledModel(t), fl, src, newTestLogger())
	c.Start(context.Background())
	defer c.Stop(context.Background())

	dst := [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	if err := c.(*Port).Send(dst, sampleDatagram()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if fl.sentCount() != 1 {
		t.Fatalf("sent %d frames, want 1", fl.sentCount())
	}
	fl.mu.Lock()
	frame := fl.sent[0]
	fl.mu.Unlock()
	if [6]byte(frame[0:6]) != dst {
		t.Errorf("dst MAC = % x, want % x", frame[0:6], dst)
	}
	if [6]byte(frame[6:12]) != src {
		t.Errorf("src MAC = % x, want % x", frame[6:12], src)
	}
	if frame[12] != 0x81 || frame[13] != 0x37 {
		t.Errorf("ethertype = % x, want 81 37", frame[12:14])
	}
	stats := c.(component.Statful).Stats()
	if stats.Counters["frames_tx"] != 1 {
		t.Errorf("frames_tx = %d, want 1", stats.Counters["frames_tx"])
	}
}

func TestInboundDedup(t *testing.T) {
	fl := &fakeFrameLink{}
	ipxBytes, _ := sampleDatagram().Encode(nil)
	frame := ethIPXFrame([6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}, [6]byte{1, 2, 3, 4, 5, 6}, ipxBytes)
	fl.push(frame)
	fl.push(frame) // immediate duplicate → must be deduped

	c, _ := New(enabledModel(t), fl, [6]byte{}, newTestLogger())
	var n int
	var mu sync.Mutex
	c.(*Port).SetDeliveryCallback(func(*ipxproto.Datagram) { mu.Lock(); n++; mu.Unlock() })
	c.Start(context.Background())
	defer c.Stop(context.Background())

	// Give the loop time to process both frames.
	waitFor(t, func() bool {
		return c.(component.Statful).Stats().Counters["frames_rx"] >= 2
	})
	mu.Lock()
	delivered := n
	mu.Unlock()
	if delivered != 1 {
		t.Fatalf("delivered %d, want 1 (second frame deduped)", delivered)
	}
	if dup := c.(component.Statful).Stats().Counters["frames_dup"]; dup != 1 {
		t.Errorf("frames_dup = %d, want 1", dup)
	}
}

func TestStopStartRestartable(t *testing.T) {
	fl := &fakeFrameLink{}
	c, _ := New(enabledModel(t), fl, [6]byte{}, newTestLogger())
	ctx := context.Background()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start #1: %v", err)
	}
	if err := c.Stop(ctx); err != nil {
		t.Fatalf("Stop #1: %v", err)
	}
	// The injected single link is closed; Start again must not panic. Because the
	// registry-style single-link factory hands back the same (now closed) link,
	// the read loop exits immediately — the lifecycle is still clean.
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start #2: %v", err)
	}
	if err := c.Stop(ctx); err != nil {
		t.Fatalf("Stop #2: %v", err)
	}
}

func TestReconfigureIfaceChangeNeedsRestart(t *testing.T) {
	c, _ := New(enabledModel(t), &fakeFrameLink{}, [6]byte{}, newTestLogger())
	cfg := c.(component.Configurable)
	if err := cfg.ApplyConfig(&port.Section{SKey: Name, Iface: "eth0", IsEnabled: false}); err != nil {
		t.Errorf("same-iface reconfigure should apply live, got %v", err)
	}
	if err := cfg.ApplyConfig(&port.Section{SKey: Name, Iface: "eth1", IsEnabled: true}); err != component.ErrNeedsRestart {
		t.Errorf("iface change err = %v, want ErrNeedsRestart", err)
	}
}
