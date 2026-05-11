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
func (f *fakeTransport) Stop() error                              { f.stopped.Store(true); return nil }
func (f *fakeTransport) SendName(n protocol.Name) error {
	f.sendNameCalls = append(f.sendNameCalls, n)
	return f.sendNameErr
}
func (f *fakeTransport) SendDatagram(_ *protocol.Datagram) error  { return nil }
func (f *fakeTransport) SendSession(_ *protocol.SessionPacket) error {
	return nil
}
func (f *fakeTransport) SetCommandHandler(h CommandHandler) { f.handler = h }

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
