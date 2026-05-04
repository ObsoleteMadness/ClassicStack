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
}

func (f *fakeTransport) Start(_ context.Context) error {
	if f.failStart {
		return errors.New("boom")
	}
	f.started.Store(true)
	return nil
}
func (f *fakeTransport) Stop() error                              { f.stopped.Store(true); return nil }
func (f *fakeTransport) SendName(_ protocol.Name) error           { return nil }
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
