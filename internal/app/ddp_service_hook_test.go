//go:build all

package app

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/pkg/status"
	"github.com/ObsoleteMadness/ClassicStack/port"
	"github.com/ObsoleteMadness/ClassicStack/protocol/ddp"
	"github.com/ObsoleteMadness/ClassicStack/router"
	"github.com/ObsoleteMadness/ClassicStack/service"
)

// fakeDDPService is a minimal router service that records Start/Stop calls and
// the socket it binds, so ddpServiceHook lifecycle can be observed without a
// real subsystem.
type fakeDDPService struct {
	socket  uint8
	starts  int32
	stops   int32
	failNth int32 // 1-based call index whose Start should fail; 0 = never
}

func (f *fakeDDPService) Socket() uint8 { return f.socket }

func (f *fakeDDPService) Start(_ context.Context, _ service.Router) error {
	n := atomic.AddInt32(&f.starts, 1)
	if f.failNth != 0 && n == f.failNth {
		return errFakeStart
	}
	return nil
}

func (f *fakeDDPService) Stop() error {
	atomic.AddInt32(&f.stops, 1)
	return nil
}

func (f *fakeDDPService) Inbound(_ ddp.Datagram, _ port.Port) {}

// errFakeStart is returned by fakeDDPService.Start on its configured failure.
var errFakeStart = fakeErr("forced start failure")

type fakeErr string

func (e fakeErr) Error() string { return string(e) }

// newTestRouter returns a router with no ports and only the default core
// services, started so AddService/RemoveService operate on a live router.
func newTestRouter(t *testing.T) *router.Router {
	t.Helper()
	r := router.New("test", nil, []service.Service{})
	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("router start: %v", err)
	}
	t.Cleanup(func() { _ = r.Stop() })
	return r
}

// TestDDPServiceHookStartStop verifies the hook adds its services to the live
// router on Start and removes them on Stop, and that the router's dispatch map
// reflects the change.
func TestDDPServiceHookStartStop(t *testing.T) {
	r := newTestRouter(t)
	svc := &fakeDDPService{socket: 200}
	h := newDDPServiceHook(r, []service.Service{svc})
	if h == nil {
		t.Fatal("newDDPServiceHook returned nil for a non-empty group")
	}

	if err := h.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := atomic.LoadInt32(&svc.starts); got != 1 {
		t.Fatalf("service Start calls = %d, want 1", got)
	}
	if !routerHasService(r, svc) {
		t.Fatal("service not present in router after hook Start")
	}

	// Start again is idempotent (no second AddService).
	if err := h.Start(context.Background()); err != nil {
		t.Fatalf("second Start: %v", err)
	}
	if got := atomic.LoadInt32(&svc.starts); got != 1 {
		t.Fatalf("service Start calls after re-Start = %d, want 1", got)
	}

	if err := h.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got := atomic.LoadInt32(&svc.stops); got != 1 {
		t.Fatalf("service Stop calls = %d, want 1", got)
	}
	if routerHasService(r, svc) {
		t.Fatal("service still present in router after hook Stop")
	}
}

// TestDDPServiceHookStartRollback verifies that if one service in the group
// fails to start, the services already added are rolled back so the subsystem
// is not left half-up.
func TestDDPServiceHookStartRollback(t *testing.T) {
	r := newTestRouter(t)
	ok := &fakeDDPService{socket: 201}
	bad := &fakeDDPService{socket: 202, failNth: 1}
	h := newDDPServiceHook(r, []service.Service{ok, bad})

	if err := h.Start(context.Background()); err == nil {
		t.Fatal("Start: expected error from failing service")
	}
	if routerHasService(r, ok) {
		t.Fatal("first service must be rolled back when a later one fails")
	}
	if got := atomic.LoadInt32(&ok.stops); got != 1 {
		t.Fatalf("rolled-back service Stop calls = %d, want 1", got)
	}
}

// TestNewDDPServiceHookEmpty verifies an empty group yields a nil hook so the
// supervisor registers no unit for a subsystem with no services.
func TestNewDDPServiceHookEmpty(t *testing.T) {
	if h := newDDPServiceHook(nil, nil); h != nil {
		t.Fatalf("newDDPServiceHook(nil, nil) = %v, want nil", h)
	}
}

// TestPromoteUnitToHook verifies a KindService unit is re-published as a
// KindHook (so the dashboard shows lifecycle controls) while preserving its
// binding and properties.
func TestPromoteUnitToHook(t *testing.T) {
	reg := status.NewRegistry()
	reg.Set(status.Unit{
		Name:       "AFP",
		Kind:       status.KindService,
		Enabled:    true,
		Running:    true,
		Binding:    ":548",
		Properties: map[string]string{"zone": "MyZone"},
	})
	s := &Supervisor{reg: reg}
	s.promoteUnitToHook("AFP", true)

	u := unitByName(reg, "AFP")
	if u.Kind != status.KindHook {
		t.Fatalf("Kind = %q, want %q", u.Kind, status.KindHook)
	}
	if u.Running {
		t.Fatal("promoted unit should start not-running")
	}
	if u.Binding != ":548" || u.Properties["zone"] != "MyZone" {
		t.Fatalf("promotion lost detail: binding=%q props=%v", u.Binding, u.Properties)
	}
}

func routerHasService(r *router.Router, target service.Service) bool {
	for _, s := range r.Services {
		if s == target {
			return true
		}
	}
	return false
}
