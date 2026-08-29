package netbios

import (
	"context"
	"sync"
	"testing"

	protocol "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbios"
)

// fakeTransport records its open/close/announce activity so the binding's soft
// lifecycle can be asserted.
type fakeTransport struct {
	mu        sync.Mutex
	opens     int
	closes    int
	announced []protocol.Name
}

func (f *fakeTransport) Open(ctx context.Context) error {
	_ = ctx
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opens++
	return nil
}

func (f *fakeTransport) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closes++
	return nil
}

func (f *fakeTransport) Announce(n protocol.Name) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.announced = append(f.announced, n)
	return nil
}

func (f *fakeTransport) state() (int, int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.opens, f.closes, len(f.announced)
}

func TestNetBIOS_TransportAttachesOnStart(t *testing.T) {
	s := NewService(nil, "CLASSICSTACK")
	ft := &fakeTransport{}
	if err := s.AddTransport("netbeui", ft); err != nil {
		t.Fatalf("AddTransport: %v", err)
	}

	// Not started yet → not opened.
	if opens, _, _ := ft.state(); opens != 0 {
		t.Fatalf("opened before Start: opens=%d", opens)
	}

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	opens, _, announced := ft.state()
	if opens != 1 {
		t.Errorf("opens after Start = %d, want 1", opens)
	}
	if announced != 2 { // file-server + workstation names
		t.Errorf("announced names = %d, want 2", announced)
	}

	if err := s.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if _, closes, _ := ft.state(); closes != 1 {
		t.Errorf("closes after Stop = %d, want 1", closes)
	}
}

func TestNetBIOS_LateTransportAttachesToRunningService(t *testing.T) {
	s := NewService(nil, "CLASSICSTACK")
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop(context.Background())

	// A transport whose protocol comes up after NetBIOS (e.g. NetBEUI enabled
	// from the UI) must attach immediately to the live service (§11d).
	ft := &fakeTransport{}
	if err := s.AddTransport("netbeui", ft); err != nil {
		t.Fatalf("AddTransport late: %v", err)
	}
	if opens, _, announced := ft.state(); opens != 1 || announced != 2 {
		t.Errorf("late attach: opens=%d announced=%d, want 1,2", opens, announced)
	}
}

func TestNetBIOS_RemoveTransportDetachesOnlyThatBinding(t *testing.T) {
	s := NewService(nil, "CLASSICSTACK")
	a := &fakeTransport{}
	b := &fakeTransport{}
	_ = s.AddTransport("netbeui", a)
	_ = s.AddTransport("ipx", b)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop(context.Background())

	if err := s.RemoveTransport("netbeui"); err != nil {
		t.Fatalf("RemoveTransport: %v", err)
	}
	if _, closes, _ := a.state(); closes != 1 {
		t.Errorf("removed binding closes = %d, want 1", closes)
	}
	if _, closes, _ := b.state(); closes != 0 {
		t.Errorf("sibling binding closed: closes = %d, want 0", closes)
	}
	if got := s.Transports(); len(got) != 1 || got[0] != "ipx" {
		t.Errorf("Transports after remove = %v, want [ipx]", got)
	}
}

func TestNetBIOS_StartIdempotent(t *testing.T) {
	s := NewService(nil, "CLASSICSTACK")
	ft := &fakeTransport{}
	_ = s.AddTransport("netbeui", ft)
	ctx := context.Background()
	_ = s.Start(ctx)
	_ = s.Start(ctx) // second Start must not re-open.
	if opens, _, _ := ft.state(); opens != 1 {
		t.Errorf("opens after double Start = %d, want 1", opens)
	}
	_ = s.Stop(ctx)
}

func TestNetBIOS_RegisterNameAnnouncesOnAttached(t *testing.T) {
	s := NewService(nil, "CLASSICSTACK")
	ft := &fakeTransport{}
	_ = s.AddTransport("netbeui", ft)
	_ = s.Start(context.Background())
	defer s.Stop(context.Background())

	_, _, before := ft.state()
	if err := s.RegisterName("EXTRA"); err != nil {
		t.Fatalf("RegisterName: %v", err)
	}
	_, _, after := ft.state()
	if after <= before {
		t.Errorf("RegisterName did not announce on attached transport: before=%d after=%d", before, after)
	}
}
