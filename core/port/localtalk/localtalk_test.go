package localtalk

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
	"github.com/ObsoleteMadness/ClassicStack/core/router"
)

// LocalTalk shares the runport base with EtherTalk; these tests cover this
// package's wiring (disabled gate, framer-required guard, inbound delivery).

type fakeDatagramLink struct {
	mu     sync.Mutex
	inbox  []ddp.Datagram
	closed bool
}

func (f *fakeDatagramLink) ReadDatagram() (ddp.Datagram, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return ddp.Datagram{}, link.ErrClosed
	}
	if len(f.inbox) > 0 {
		d := f.inbox[0]
		f.inbox = f.inbox[1:]
		return d, nil
	}
	return ddp.Datagram{}, link.ErrTimeout
}
func (f *fakeDatagramLink) WriteDatagram(ddp.Datagram) error { return nil }
func (f *fakeDatagramLink) Close() error {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
	return nil
}

type fakeFramer struct{ dl link.DatagramLink }

func (f fakeFramer) Framing(link.FrameLink) (link.DatagramLink, error) { return f.dl, nil }

type nilFrameLink struct{}

func (nilFrameLink) Read() (link.Frame, error) { return nil, link.ErrClosed }
func (nilFrameLink) Write(link.Frame) error    { return nil }
func (nilFrameLink) Close() error              { return nil }

type recordingRouter struct {
	mu sync.Mutex
	n  int
}

func (r *recordingRouter) Name() string                   { return "test-router" }
func (r *recordingRouter) Start(context.Context) error    { return nil }
func (r *recordingRouter) Stop(context.Context) error     { return nil }
func (r *recordingRouter) Attach(router.RoutedPort) error { return nil }
func (r *recordingRouter) Detach(router.RoutedPort) error { return nil }
func (r *recordingRouter) Inbound(ddp.Datagram, router.RoutedPort) {
	r.mu.Lock()
	r.n++
	r.mu.Unlock()
}
func (r *recordingRouter) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.n
}

func enabledModel() *config.Model {
	m := config.NewModel()
	m.Set(&port.Section{SKey: Name, Iface: "lt0", IsEnabled: true})
	return m
}

func newTestLogger() log.Logger {
	return log.New(Name, log.NewStderrSink(log.NewLevelVar(log.Warn)))
}

func TestDisabledReturnsNil(t *testing.T) {
	m := config.NewModel()
	m.Set(&port.Section{SKey: Name, Iface: "lt0", IsEnabled: false})
	c, err := New(m, nil, nil, nil, newTestLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c != nil {
		t.Fatalf("disabled section must yield nil, got %T", c)
	}
}

func TestFrameWithoutFramerErrors(t *testing.T) {
	if _, err := New(enabledModel(), nilFrameLink{}, nil, nil, newTestLogger()); err == nil {
		t.Fatal("expected error: frame link without framer")
	}
}

func TestInboundDeliveredToRouter(t *testing.T) {
	dl := &fakeDatagramLink{inbox: []ddp.Datagram{{DDPType: 1, Data: []byte{1}}}}
	rtr := &recordingRouter{}
	c, err := New(enabledModel(), nilFrameLink{}, fakeFramer{dl: dl}, rtr, newTestLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer c.Stop(context.Background())

	for range 1000 {
		if rtr.count() == 1 {
			break
		}
		runtime.Gosched()
		time.Sleep(time.Millisecond)
	}
	if rtr.count() != 1 {
		t.Fatalf("router received %d datagrams, want 1", rtr.count())
	}
}
