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

func TestPlane_UserAdminUnavailableWithoutStore(t *testing.T) {
	// fakeSupervisor does NOT implement UserAdmin → every user op is unavailable.
	p := New(&fakeSupervisor{model: config.NewModel()}, fakeCodec{}, &fakeStore{}, bus.New(8))
	if _, err := p.Users(); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Users() err = %v, want ErrUnavailable", err)
	}
	if err := p.SetUser("a", "p"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("SetUser() err = %v, want ErrUnavailable", err)
	}
	if err := p.SetUserDisabled("a", true); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("SetUserDisabled() err = %v, want ErrUnavailable", err)
	}
	if err := p.RemoveUser("a"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("RemoveUser() err = %v, want ErrUnavailable", err)
	}
}

// userSupervisor is a fakeSupervisor that also implements UserAdmin, proving the
// plane delegates user ops when the surface is present.
type userSupervisor struct {
	fakeSupervisor
	users    []UserInfo
	lastSet  string
	disabled map[string]bool
}

func (s *userSupervisor) Users() ([]UserInfo, error) { return s.users, nil }
func (s *userSupervisor) SetUser(name, _ string) error {
	s.lastSet = name
	s.users = append(s.users, UserInfo{Name: name})
	return nil
}
func (s *userSupervisor) SetUserDisabled(name string, d bool) error {
	if s.disabled == nil {
		s.disabled = map[string]bool{}
	}
	s.disabled[name] = d
	return nil
}
func (s *userSupervisor) RemoveUser(name string) error {
	for i, u := range s.users {
		if u.Name == name {
			s.users = append(s.users[:i], s.users[i+1:]...)
		}
	}
	return nil
}

func TestPlane_UserAdminDelegates(t *testing.T) {
	sup := &userSupervisor{fakeSupervisor: fakeSupervisor{model: config.NewModel()}}
	p := New(sup, fakeCodec{}, &fakeStore{}, bus.New(8))

	if err := p.SetUser("alice", "pw"); err != nil {
		t.Fatal(err)
	}
	if sup.lastSet != "alice" {
		t.Fatalf("SetUser not delegated (lastSet=%q)", sup.lastSet)
	}
	users, err := p.Users()
	if err != nil || len(users) != 1 || users[0].Name != "alice" {
		t.Fatalf("Users() = %v, err %v", users, err)
	}
	if err := p.SetUserDisabled("alice", true); err != nil || !sup.disabled["alice"] {
		t.Fatalf("SetUserDisabled not delegated (err=%v disabled=%v)", err, sup.disabled)
	}
	if err := p.RemoveUser("alice"); err != nil {
		t.Fatal(err)
	}
	if users, _ := p.Users(); len(users) != 0 {
		t.Fatalf("RemoveUser left %d users", len(users))
	}
}

func TestPlane_SaveRejectsInvalidHostname(t *testing.T) {
	m := config.NewModel()
	m.Identity = config.Identity{Hostname: "bad/name"} // path separator → baseline fail
	sup := &fakeSupervisor{model: m}
	store := &fakeStore{}
	p := New(sup, fakeCodec{}, store, bus.New(2))

	if _, err := p.Save(context.Background()); err == nil {
		t.Fatal("Save should reject a hostname with a path separator")
	}
	if store.data != nil {
		t.Fatal("invalid model must not reach the store")
	}
}

func TestPlane_SaveNetBIOSHostnameRuleGated(t *testing.T) {
	const longName = "THIS-NAME-IS-WAY-TOO-LONG" // > 15 bytes, baseline-legal

	// NetBIOS NOT enabled (no such unit) → the ≤15-byte rule does not apply; Save OK.
	mOff := config.NewModel()
	mOff.Identity = config.Identity{Hostname: longName}
	pOff := New(&fakeSupervisor{model: mOff}, fakeCodec{}, &fakeStore{}, bus.New(2))
	if _, err := pOff.Save(context.Background()); err != nil {
		t.Fatalf("Save with NetBIOS off should accept a long hostname: %v", err)
	}

	// NetBIOS enabled → the consumer rule applies; Save rejected.
	mOn := config.NewModel()
	mOn.Identity = config.Identity{Hostname: longName}
	supOn := &fakeSupervisor{model: mOn, units: []Unit{{Name: "NetBIOS", Enabled: true}}}
	pOn := New(supOn, fakeCodec{}, &fakeStore{}, bus.New(2))
	if _, err := pOn.Save(context.Background()); err == nil {
		t.Fatal("Save with NetBIOS enabled should reject an over-length hostname")
	}

	// NetBIOS present but DISABLED → rule does not apply; Save OK.
	mDis := config.NewModel()
	mDis.Identity = config.Identity{Hostname: longName}
	supDis := &fakeSupervisor{model: mDis, units: []Unit{{Name: "NetBIOS", Enabled: false}}}
	pDis := New(supDis, fakeCodec{}, &fakeStore{}, bus.New(2))
	if _, err := pDis.Save(context.Background()); err != nil {
		t.Fatalf("Save with NetBIOS disabled should accept a long hostname: %v", err)
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
