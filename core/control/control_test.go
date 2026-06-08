package control

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
)

type fakeSection struct{ key string }

func (s fakeSection) Key() string           { return s.key }
func (s fakeSection) Clone() config.Section { return fakeSection{key: s.key} }
func (s fakeSection) Validate() error       { return nil }

type fakeSupervisor struct {
	model      *config.Model
	units      []Unit
	lastName   string
	lastSetKey string
}

func (s *fakeSupervisor) Model() *config.Model { return s.model }

func (s *fakeSupervisor) Reconfigure(_ context.Context, name string, section config.Section) error {
	s.lastName = name
	s.lastSetKey = section.Key()
	return nil
}

func (s *fakeSupervisor) Start(context.Context, string) error   { return nil }
func (s *fakeSupervisor) Stop(context.Context, string) error    { return nil }
func (s *fakeSupervisor) Restart(context.Context, string) error { return nil }
func (s *fakeSupervisor) Status() []Unit                        { return s.units }
func (s *fakeSupervisor) ListInterfaces() ([]InterfaceInfo, error) {
	return []InterfaceInfo{{Name: "eth0", Addr: "10.0.0.1"}}, nil
}
func (s *fakeSupervisor) ListFSTypes() []string { return []string{"memfs"} }

type fakeCodec struct{ marshalErr error }

func (c fakeCodec) Marshal(*config.Model) ([]byte, error) {
	if c.marshalErr != nil {
		return nil, c.marshalErr
	}
	return []byte("cfg"), nil
}
func (fakeCodec) Unmarshal([]byte, *config.Model) error { return nil }

type fakeStore struct {
	data []byte
	err  error
}

func (s *fakeStore) Load() ([]byte, error) { return s.data, nil }
func (s *fakeStore) Save(data []byte) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	s.data = append([]byte(nil), data...)
	return "rev-1", nil
}

func TestPlane_StatusAndReconfigure(t *testing.T) {
	m := config.NewModel()
	sup := &fakeSupervisor{model: m, units: []Unit{{Name: "placeholder", Running: true}}}
	p := New(sup, fakeCodec{}, &fakeStore{}, bus.New(8))

	st := p.Status()
	if len(st) != 1 || st[0].Name != "placeholder" || !st[0].Running {
		t.Fatalf("Status() = %#v", st)
	}

	if err := p.Reconfigure(context.Background(), "placeholder", fakeSection{key: "AFP"}); err != nil {
		t.Fatalf("Reconfigure() error = %v", err)
	}
	if sup.lastName != "placeholder" || sup.lastSetKey != "AFP" {
		t.Fatalf("Reconfigure() did not delegate to supervisor: name=%q key=%q", sup.lastName, sup.lastSetKey)
	}
}

func TestPlane_SubscribeStateTopic(t *testing.T) {
	tb := bus.New(4)
	sup := &fakeSupervisor{model: config.NewModel()}
	p := New(sup, fakeCodec{}, &fakeStore{}, tb)

	ch, unsub := p.Subscribe(bus.TopicState)
	defer unsub()

	tb.Publish(bus.StateChanged{Component: "x", From: "stopped", To: "running"})

	select {
	case ev := <-ch:
		if ev.Topic() != bus.TopicState {
			t.Fatalf("topic = %q, want %q", ev.Topic(), bus.TopicState)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for state event")
	}
}

func TestPlane_SaveAndDiagnostics(t *testing.T) {
	sup := &fakeSupervisor{model: config.NewModel()}
	store := &fakeStore{}
	p := New(sup, fakeCodec{}, store, bus.New(2))

	rev, err := p.Save(context.Background())
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if rev != "rev-1" || string(store.data) != "cfg" {
		t.Fatalf("Save() = (%q, %q), want (rev-1, cfg)", rev, string(store.data))
	}

	_, derr := p.Diagnostics().ListZones(context.Background())
	if !errors.Is(derr, ErrUnavailable) {
		t.Fatalf("Diagnostics().ListZones() error = %v, want ErrUnavailable", derr)
	}
}
