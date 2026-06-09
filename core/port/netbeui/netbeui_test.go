package netbeui

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
	nbf "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbeui"
)

// fakeFrameLink is an in-test link.FrameLink (queued reads, captured writes).
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

// uiFrame wraps an NBF body in an 802.3 + LLC UI (0xF0F003) Ethernet frame.
func uiFrame(dst, src [6]byte, nbfBody []byte) []byte {
	payloadLen := 3 + len(nbfBody)
	frame := make([]byte, 0, ethHdrLen+payloadLen)
	frame = append(frame, dst[:]...)
	frame = append(frame, src[:]...)
	frame = append(frame, byte(payloadLen>>8), byte(payloadLen))
	frame = append(frame, 0xF0, 0xF0, 0x03)
	frame = append(frame, nbfBody...)
	return frame
}

func sampleNBF() *nbf.Frame {
	f := &nbf.Frame{Command: nbf.CmdAddNameQuery, RspCorrelator: 0x0002}
	copy(f.SourceName[:], "CLASSICSTACK   \x00")
	return f
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

func TestInboundUIFrameDelivered(t *testing.T) {
	fl := &fakeFrameLink{}
	body, _ := sampleNBF().Encode()
	dst := nbf.NetBIOSMulticastMAC
	src := [6]byte{0x00, 0x50, 0x56, 0xc0, 0x00, 0x01}
	fl.push(uiFrame(dst, src, body))

	c, _ := New(enabledModel(t), fl, [6]byte{}, newTestLogger())
	var gotSrc, gotDst [6]byte
	var gotFrame *nbf.Frame
	var mu sync.Mutex
	c.(*Port).SetDeliveryCallback(func(s, d [6]byte, f *nbf.Frame) {
		mu.Lock()
		gotSrc, gotDst, gotFrame = s, d, f
		mu.Unlock()
	})
	c.Start(context.Background())
	defer c.Stop(context.Background())

	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return gotFrame != nil })
	mu.Lock()
	defer mu.Unlock()
	if gotFrame == nil {
		t.Fatal("no frame delivered")
	}
	if gotFrame.Command != nbf.CmdAddNameQuery {
		t.Errorf("command = %#x, want AddNameQuery", gotFrame.Command)
	}
	if gotSrc != src || gotDst != dst {
		t.Errorf("MACs = src % x dst % x, want src % x dst % x", gotSrc, gotDst, src, dst)
	}
}

func TestNonNetBIOSFrameSkipped(t *testing.T) {
	fl := &fakeFrameLink{}
	// LLC with the wrong DSAP (0xAA, i.e. SNAP, not NetBIOS 0xF0): must be skipped.
	dst := [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	src := [6]byte{1, 2, 3, 4, 5, 6}
	frame := make([]byte, 0)
	frame = append(frame, dst[:]...)
	frame = append(frame, src[:]...)
	frame = append(frame, 0x00, 0x10)
	frame = append(frame, 0xAA, 0xAA, 0x03) // SNAP, not NetBIOS
	frame = append(frame, make([]byte, 13)...)
	fl.push(frame)

	c, _ := New(enabledModel(t), fl, [6]byte{}, newTestLogger())
	var n int
	var mu sync.Mutex
	c.(*Port).SetDeliveryCallback(func([6]byte, [6]byte, *nbf.Frame) { mu.Lock(); n++; mu.Unlock() })
	c.Start(context.Background())
	defer c.Stop(context.Background())

	waitFor(t, func() bool { return c.(component.Statful).Stats().Counters["frames_rx"] >= 1 })
	mu.Lock()
	defer mu.Unlock()
	if n != 0 {
		t.Fatalf("delivered %d frames, want 0 (non-NetBIOS LLC skipped)", n)
	}
}

func TestSendUIEncapsulation(t *testing.T) {
	fl := &fakeFrameLink{}
	src := [6]byte{0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0x01}
	c, _ := New(enabledModel(t), fl, src, newTestLogger())
	c.Start(context.Background())
	defer c.Stop(context.Background())

	if err := c.(*Port).SendBroadcast(sampleNBF()); err != nil {
		t.Fatalf("SendBroadcast: %v", err)
	}
	if fl.sentCount() != 1 {
		t.Fatalf("sent %d frames, want 1", fl.sentCount())
	}
	fl.mu.Lock()
	frame := fl.sent[0]
	fl.mu.Unlock()
	if [6]byte(frame[0:6]) != nbf.NetBIOSMulticastMAC {
		t.Errorf("dst MAC = % x, want NetBIOS multicast", frame[0:6])
	}
	if frame[14] != 0xF0 || frame[15] != 0xF0 || frame[16] != 0x03 {
		t.Errorf("LLC header = % x, want f0 f0 03", frame[14:17])
	}
}

func TestStopStartRestartable(t *testing.T) {
	c, _ := New(enabledModel(t), &fakeFrameLink{}, [6]byte{}, newTestLogger())
	ctx := context.Background()
	for i := range 2 {
		if err := c.Start(ctx); err != nil {
			t.Fatalf("Start #%d: %v", i, err)
		}
		if err := c.Stop(ctx); err != nil {
			t.Fatalf("Stop #%d: %v", i, err)
		}
	}
}

func TestReconfigureIfaceChangeNeedsRestart(t *testing.T) {
	c, _ := New(enabledModel(t), &fakeFrameLink{}, [6]byte{}, newTestLogger())
	cfg := c.(component.Configurable)
	if err := cfg.ApplyConfig(&port.Section{SKey: Name, Iface: "eth0", IsEnabled: false}); err != nil {
		t.Errorf("same-iface reconfigure should apply live, got %v", err)
	}
	if err := cfg.ApplyConfig(&port.Section{SKey: Name, Iface: "wlan0", IsEnabled: true}); err != component.ErrNeedsRestart {
		t.Errorf("iface change err = %v, want ErrNeedsRestart", err)
	}
}
