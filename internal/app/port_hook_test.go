//go:build all

package app

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/port"
	"github.com/ObsoleteMadness/ClassicStack/protocol/ddp"
	"github.com/ObsoleteMadness/ClassicStack/router"
	"github.com/ObsoleteMadness/ClassicStack/service"
)

// fakePort is a minimal port.Port that records Start/Stop calls so the
// portHook/routerHook lifecycle can be exercised without a real transport.
type fakePort struct {
	starts  int32
	stops   int32
	running atomic.Bool
}

func (f *fakePort) ShortString() string { return "fake" }
func (f *fakePort) Start(port.RouterHooks) error {
	atomic.AddInt32(&f.starts, 1)
	f.running.Store(true)
	return nil
}
func (f *fakePort) Stop() error {
	atomic.AddInt32(&f.stops, 1)
	f.running.Store(false)
	return nil
}
func (f *fakePort) Unicast(uint16, uint8, ddp.Datagram)  {}
func (f *fakePort) Broadcast(ddp.Datagram)               {}
func (f *fakePort) Multicast([]byte, ddp.Datagram)       {}
func (f *fakePort) SetNetworkRange(uint16, uint16) error { return nil }
func (f *fakePort) Network() uint16                      { return 0 }
func (f *fakePort) Node() uint8                          { return 0 }
func (f *fakePort) NetworkMin() uint16                   { return 0 }
func (f *fakePort) NetworkMax() uint16                   { return 0 }
func (f *fakePort) ExtendedNetwork() bool                { return false }

// TestPortHook_StandaloneLifecycle verifies a standalone (non-routed) port hook
// starts and stops the port directly, independent of the router, and never adds
// it to the router's port set.
func TestPortHook_StandaloneLifecycle(t *testing.T) {
	r := router.New("test", nil, []service.Service{})
	p := &fakePort{}
	h := newPortHook(p, r, false, func() bool { return false })

	if err := h.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !p.running.Load() {
		t.Fatal("standalone port should be running after Start")
	}
	if r.HasPort(p) {
		t.Fatal("standalone port must not join the router set")
	}
	if err := h.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if p.running.Load() {
		t.Fatal("standalone port should be stopped after Stop")
	}
}

// TestPortHook_RoutedAttachesToRunningRouter verifies a routed port hook joins
// the router (via AddPort) when the router is already running, and is removed
// from it on Stop.
func TestPortHook_RoutedAttachesToRunningRouter(t *testing.T) {
	r := newTestRouter(t) // started
	p := &fakePort{}
	h := newPortHook(p, r, true, func() bool { return true })

	if err := h.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !r.HasPort(p) {
		t.Fatal("routed port should join the running router")
	}
	if !p.running.Load() {
		t.Fatal("routed port should be running")
	}
	if err := h.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if r.HasPort(p) {
		t.Fatal("routed port should leave the router on Stop")
	}
	if p.running.Load() {
		t.Fatal("routed port should be stopped after Stop")
	}
}

// TestRouterHook_StopLeavesPortsRunning verifies that stopping the router hook
// detaches the routed ports from the router but leaves them running — ports are
// independent of the router (their frames simply go nowhere).
func TestRouterHook_StopLeavesPortsRunning(t *testing.T) {
	r := router.New("test", nil, []service.Service{})
	p := &fakePort{}

	var rh *routerHook
	ph := newPortHook(p, r, true, func() bool { return rh.IsRunning() })
	rh = newRouterHook(r, func() []*portHook { return []*portHook{ph} })

	ctx := context.Background()
	// Bring the router up first, then the (routed) port — mirrors start order.
	if err := rh.Start(ctx); err != nil {
		t.Fatalf("router Start: %v", err)
	}
	if err := ph.Start(ctx); err != nil {
		t.Fatalf("port Start: %v", err)
	}
	if !r.HasPort(p) {
		t.Fatal("routed port should be attached while router runs")
	}

	// Stop the router: the port must stay up but leave the router set.
	if err := rh.Stop(); err != nil {
		t.Fatalf("router Stop: %v", err)
	}
	if r.HasPort(p) {
		t.Fatal("router stop should detach the port from the router set")
	}
	if !p.running.Load() {
		t.Fatal("router stop must NOT stop the port")
	}

	// Restart the router: it should re-adopt the still-running port.
	if err := rh.Start(ctx); err != nil {
		t.Fatalf("router restart: %v", err)
	}
	if !r.HasPort(p) {
		t.Fatal("router restart should re-attach the running routed port")
	}
	if !p.running.Load() {
		t.Fatal("port should still be running after router restart")
	}
}
