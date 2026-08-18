package ethertalk

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
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
	"github.com/ObsoleteMadness/ClassicStack/core/router"
)

// fakeDatagramLink is an in-test link.DatagramLink: queued inbound datagrams are
// returned by ReadDatagram (then ErrTimeout to idle the loop), and outbound
// datagrams are captured. A fresh one is handed out per Start to model the
// real per-Start LinkFactory (a closed link cannot be reopened).
type fakeDatagramLink struct {
	mu     sync.Mutex
	inbox  []ddp.Datagram
	sent   []ddp.Datagram
	closed bool
	idleCh chan struct{} // closed when the inbox drains, so tests can sync
}

func newFakeLink(inbound ...ddp.Datagram) *fakeDatagramLink {
	return &fakeDatagramLink{inbox: inbound, idleCh: make(chan struct{})}
}

func (f *fakeDatagramLink) ReadDatagram() (ddp.Datagram, error) {
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return ddp.Datagram{}, link.ErrClosed
	}
	if len(f.inbox) > 0 {
		dg := f.inbox[0]
		f.inbox = f.inbox[1:]
		drained := len(f.inbox) == 0
		f.mu.Unlock()
		if drained {
			close(f.idleCh)
		}
		return dg, nil
	}
	f.mu.Unlock()
	// Inbox empty: report a timeout so the read loop keeps spinning without
	// busy-erroring. The loop's select on stopCh lets Stop unwind it.
	return ddp.Datagram{}, link.ErrTimeout
}

func (f *fakeDatagramLink) WriteDatagram(d ddp.Datagram) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return link.ErrClosed
	}
	f.sent = append(f.sent, d)
	return nil
}

func (f *fakeDatagramLink) Close() error {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
	return nil
}

func (f *fakeDatagramLink) sentCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

// fakeFramer hands out the same fake DatagramLink regardless of the FrameLink,
// modelling adapter/link/framing without importing it into a core test.
type fakeFramer struct{ dl link.DatagramLink }

func (f fakeFramer) Framing(link.FrameLink) (link.DatagramLink, error) { return f.dl, nil }

// nilFrameLink is a non-nil FrameLink so New takes the framed path; its methods
// are never called because the fakeFramer ignores it.
type nilFrameLink struct{}

func (nilFrameLink) Read() (link.Frame, error) { return nil, link.ErrClosed }
func (nilFrameLink) Write(link.Frame) error    { return nil }
func (nilFrameLink) Close() error              { return nil }

// recordingRouter is an in-test router.Router that records inbound datagrams.
type recordingRouter struct {
	mu       sync.Mutex
	inbound  []ddp.Datagram
	fromPort []router.RoutedPort
}

func (r *recordingRouter) Name() string                   { return "test-router" }
func (r *recordingRouter) Start(context.Context) error    { return nil }
func (r *recordingRouter) Stop(context.Context) error     { return nil }
func (r *recordingRouter) Attach(router.RoutedPort) error { return nil }
func (r *recordingRouter) Detach(router.RoutedPort) error { return nil }
func (r *recordingRouter) Inbound(d ddp.Datagram, from router.RoutedPort) {
	r.mu.Lock()
	r.inbound = append(r.inbound, d)
	r.fromPort = append(r.fromPort, from)
	r.mu.Unlock()
}
func (r *recordingRouter) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.inbound)
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

func TestDisabledReturnsNil(t *testing.T) {
	m := config.NewModel()
	m.Set(&port.Section{SKey: Name, Iface: "eth0", IsEnabled: false})
	c, err := New(m, nil, nil, nil, newTestLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c != nil {
		t.Fatalf("disabled section must yield nil component, got %T", c)
	}
}

func TestFrameWithoutFramerErrors(t *testing.T) {
	_, err := New(enabledModel(t), nilFrameLink{}, nil, nil, newTestLogger())
	if err == nil {
		t.Fatal("expected error: frame link without framer")
	}
}

func TestInboundDeliveredToRouter(t *testing.T) {
	dl := newFakeLink(
		ddp.Datagram{DestSocket: 4, SrcSocket: 5, DDPType: 1, Data: []byte{0xDE, 0xAD}},
	)
	rtr := &recordingRouter{}
	c, err := New(enabledModel(t), nilFrameLink{}, fakeFramer{dl: dl}, rtr, newTestLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer c.Stop(context.Background())

	<-dl.idleCh // wait until the single inbound datagram has been read
	// The read loop calls router.Inbound synchronously after the read returns;
	// idleCh closes inside that same ReadDatagram, so a tiny settle is needed.
	waitFor(t, func() bool { return rtr.count() == 1 })

	if rtr.count() != 1 {
		t.Fatalf("router received %d datagrams, want 1", rtr.count())
	}
	rtr.mu.Lock()
	from := rtr.fromPort[0]
	rtr.mu.Unlock()
	if from != c {
		t.Errorf("router.Inbound from = %v, want the EtherTalk port itself", from)
	}
}

func TestOutboundMetersAndWrites(t *testing.T) {
	dl := newFakeLink()
	c, err := New(enabledModel(t), nilFrameLink{}, fakeFramer{dl: dl}, &recordingRouter{}, newTestLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer c.Stop(context.Background())

	rp := c.(router.RoutedPort)
	rp.Broadcast(ddp.Datagram{DDPType: 1, Data: []byte{1, 2, 3}})
	rp.Unicast(0x1234, 0x42, ddp.Datagram{DDPType: 2})

	if dl.sentCount() != 2 {
		t.Fatalf("sent %d datagrams, want 2", dl.sentCount())
	}
	stats := c.(component.Statful).Stats()
	if stats.Counters["frames_tx"] != 2 {
		t.Errorf("frames_tx = %d, want 2", stats.Counters["frames_tx"])
	}
	if stats.Counters["bytes_tx"] == 0 {
		t.Error("bytes_tx = 0, want >0 (throughput metered)")
	}
}

func TestStopStartReopensLink(t *testing.T) {
	// Two links: the first is closed on Stop, the second opened on the next
	// Start — proving the port survives Stop→Start by reopening (a closed link
	// cannot be reused).
	links := []*fakeDatagramLink{newFakeLink(), newFakeLink()}
	var n int
	var mu sync.Mutex
	open := func() (link.DatagramLink, error) {
		mu.Lock()
		defer mu.Unlock()
		dl := links[n]
		n++
		return dl, nil
	}
	// Build the port directly to inject a multi-link factory via the framer:
	// fakeFramer can only hold one link, so use a framer that pulls from open().
	c, err := New(enabledModel(t), nilFrameLink{}, framerFunc(open), &recordingRouter{}, newTestLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start #1: %v", err)
	}
	if err := c.Stop(ctx); err != nil {
		t.Fatalf("Stop #1: %v", err)
	}
	if !links[0].closed {
		t.Error("first link not closed on Stop")
	}
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start #2: %v", err)
	}
	defer c.Stop(ctx)

	// After the second Start the port writes to the second link.
	c.(router.RoutedPort).Broadcast(ddp.Datagram{DDPType: 1})
	if links[1].sentCount() != 1 {
		t.Fatalf("second link sent %d, want 1 (port did not reopen)", links[1].sentCount())
	}
}

func TestReconfigureIfaceChangeNeedsRestart(t *testing.T) {
	c, err := New(enabledModel(t), nilFrameLink{}, fakeFramer{dl: newFakeLink()}, &recordingRouter{}, newTestLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cfg := c.(component.Configurable)

	// Same iface, enabled flag flip → applies live (nil error).
	if err := cfg.ApplyConfig(&port.Section{SKey: Name, Iface: "eth0", IsEnabled: false}); err != nil {
		t.Errorf("same-iface reconfigure should apply live, got %v", err)
	}
	// Different iface → structural, must request restart.
	if err := cfg.ApplyConfig(&port.Section{SKey: Name, Iface: "eth1", IsEnabled: true}); err != component.ErrNeedsRestart {
		t.Errorf("iface change err = %v, want ErrNeedsRestart", err)
	}
}

func TestOpenerNilFrameStartsInert(t *testing.T) {
	sec := port.SectionFromModel(enabledModel(t), Name)
	open := func() (link.FrameLink, error) { return nil, nil }
	c, err := NewInstanceFromOpener(sec, open, fakeFramer{}, &recordingRouter{}, newTestLogger())
	if err != nil {
		t.Fatalf("NewInstanceFromOpener: %v", err)
	}
	if c == nil {
		t.Fatal("enabled section must still build when the opener returns a nil FrameLink")
	}
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v (nil FrameLink should be inert, not a framing error)", err)
	}
	if err := c.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

// framerFunc adapts a per-call link opener to a link.Framer so a test can hand
// out a different DatagramLink on each Start (modelling the LinkFactory).
type framerFunc func() (link.DatagramLink, error)

func (f framerFunc) Framing(link.FrameLink) (link.DatagramLink, error) { return f() }

// waitFor spins until cond is true or a short deadline elapses, yielding to let
// the read-loop goroutine run.
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
