package netbios

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	protocol "github.com/ObsoleteMadness/ClassicStack/protocol/netbios"
)

type fakeTransport struct {
	started, stopped atomic.Bool
	failStart        bool
	handler          CommandHandler
	sendNameCalls    []protocol.Name
	sendNameErr      error
}

func (f *fakeTransport) Start(_ context.Context) error {
	if f.failStart {
		return errors.New("boom")
	}
	f.started.Store(true)
	return nil
}
func (f *fakeTransport) Stop() error { f.stopped.Store(true); return nil }
func (f *fakeTransport) SendName(n protocol.Name) error {
	f.sendNameCalls = append(f.sendNameCalls, n)
	return f.sendNameErr
}
func (f *fakeTransport) SendDatagram(_ *protocol.Datagram) error { return nil }
func (f *fakeTransport) SendSession(_ *protocol.SessionPacket) error {
	return nil
}
func (f *fakeTransport) SetCommandHandler(h CommandHandler) { f.handler = h }

// recordingHandler is a no-op CommandHandler used to assert handler wiring.
type recordingHandler struct{}

func (*recordingHandler) HandleSession(_ *protocol.SessionPacket) error { return nil }
func (*recordingHandler) HandleDatagram(_ *protocol.Datagram) error     { return nil }

func TestServiceStartStopAcrossTransports(t *testing.T) {
	a, b := &fakeTransport{}, &fakeTransport{}
	svc := NewService("CLASSICSTACK", "", []Transport{a, b})
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !a.started.Load() || !b.started.Load() {
		t.Fatal("transports not started")
	}
	if got := len(a.sendNameCalls); got != 2 {
		t.Fatalf("expected 2 SendName calls on transport A, got %d", got)
	}
	if got := len(b.sendNameCalls); got != 2 {
		t.Fatalf("expected 2 SendName calls on transport B, got %d", got)
	}
	if err := svc.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !a.stopped.Load() || !b.stopped.Load() {
		t.Fatal("transports not stopped")
	}
}

func TestServiceRollsBackOnFailedTransport(t *testing.T) {
	good := &fakeTransport{}
	bad := &fakeTransport{failStart: true}
	svc := NewService("X", "", []Transport{good, bad})
	if err := svc.Start(context.Background()); err == nil {
		t.Fatal("expected error from failing second transport")
	}
	if !good.stopped.Load() {
		t.Fatal("first transport should have been rolled back via Stop()")
	}
}

// TestRemoveTransportKeepsServiceRunning is the core of the "stopping NetBEUI
// should just remove the NetBEUI binding from NetBIOS" requirement: removing
// one transport stops only that transport and leaves the rest serving.
func TestRemoveTransportKeepsServiceRunning(t *testing.T) {
	ipx, nbf := &fakeTransport{}, &fakeTransport{}
	svc := NewService("CLASSICSTACK", "", nil)
	if err := svc.AddTransport("ipx", ipx); err != nil {
		t.Fatalf("AddTransport ipx: %v", err)
	}
	if err := svc.AddTransport("netbeui", nbf); err != nil {
		t.Fatalf("AddTransport netbeui: %v", err)
	}
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !ipx.started.Load() || !nbf.started.Load() {
		t.Fatal("both transports should be started")
	}

	if err := svc.RemoveTransport("netbeui"); err != nil {
		t.Fatalf("RemoveTransport: %v", err)
	}
	if !nbf.stopped.Load() {
		t.Fatal("removed transport should be stopped")
	}
	if ipx.stopped.Load() {
		t.Fatal("remaining transport must keep running")
	}
	if got := svc.Transports(); len(got) != 1 || got[0] != "ipx" {
		t.Fatalf("Transports() = %v, want [ipx]", got)
	}

	// Removing an unknown name is a no-op.
	if err := svc.RemoveTransport("does-not-exist"); err != nil {
		t.Fatalf("RemoveTransport unknown: %v", err)
	}
}

// TestAddTransportWhileStartedStartsIt verifies a transport added after the
// service is running is wired with the handler, started, and given the names —
// the path used when NetBEUI comes up after NetBIOS from the UI.
func TestAddTransportWhileStartedStartsIt(t *testing.T) {
	svc := NewService("CLASSICSTACK", "", nil)
	handler := &recordingHandler{}
	svc.SetCommandHandler(handler)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	late := &fakeTransport{}
	if err := svc.AddTransport("netbeui", late); err != nil {
		t.Fatalf("AddTransport: %v", err)
	}
	if !late.started.Load() {
		t.Fatal("late-added transport should be started")
	}
	if late.handler != handler {
		t.Fatal("late-added transport should receive the command handler")
	}
	if len(late.sendNameCalls) == 0 {
		t.Fatal("late-added transport should be given the registered names")
	}
}

// TestAddTransportReplacesAndStopsOld verifies re-adding the same name stops
// the previous transport so it does not leak.
func TestAddTransportReplacesAndStopsOld(t *testing.T) {
	svc := NewService("X", "", nil)
	old := &fakeTransport{}
	if err := svc.AddTransport("ipx", old); err != nil {
		t.Fatalf("AddTransport old: %v", err)
	}
	newer := &fakeTransport{}
	if err := svc.AddTransport("ipx", newer); err != nil {
		t.Fatalf("AddTransport newer: %v", err)
	}
	if !old.stopped.Load() {
		t.Fatal("replaced transport should be stopped")
	}
	if got := svc.Transports(); len(got) != 1 {
		t.Fatalf("Transports() = %v, want one entry", got)
	}
}

func TestServiceRegisterDuringRuntimeSendsName(t *testing.T) {
	f := &fakeTransport{}
	svc := NewService("CLASSICSTACK", "", []Transport{f})
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	before := len(f.sendNameCalls)
	if err := svc.Register("EXTRA"); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if got := len(f.sendNameCalls); got != before+1 {
		t.Fatalf("expected one additional SendName call, got %d -> %d", before, got)
	}
}
