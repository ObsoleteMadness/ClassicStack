package supervisor

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/adapter/auth/local"
	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/control"
)

func TestSupervisorUserAdmin_NoStoreUnavailable(t *testing.T) {
	s := New(config.NewModel(), bus.New(4))
	if _, err := s.Users(); !errors.Is(err, control.ErrUnavailable) {
		t.Fatalf("Users() without a store = %v, want ErrUnavailable", err)
	}
	if err := s.SetUser("a", "p"); !errors.Is(err, control.ErrUnavailable) {
		t.Fatalf("SetUser() without a store = %v, want ErrUnavailable", err)
	}
}

func TestSupervisorUserAdmin_DelegatesToStore(t *testing.T) {
	store, err := local.Open(filepath.Join(t.TempDir(), "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(config.NewModel(), bus.New(4))
	s.SetUserStore(store)

	if err := s.SetUser("alice", "secret"); err != nil {
		t.Fatal(err)
	}
	users, err := s.Users()
	if err != nil || len(users) != 2 || users[0].Name != "Guest" || users[1].Name != "alice" {
		t.Fatalf("Users() = %v err %v (want Guest, alice)", users, err)
	}
	if err := s.SetUserDisabled("alice", true); err != nil {
		t.Fatal(err)
	}
	if users, _ := s.Users(); !users[1].Disabled {
		t.Fatal("SetUserDisabled did not propagate to the store")
	}
	// The store actually validates — confirm the supervisor wired the real thing.
	if ok, _ := store.Authenticate("alice", "secret"); ok {
		t.Fatal("disabled user authenticated through the wired store")
	}
	if err := s.RemoveUser("alice"); err != nil {
		t.Fatal(err)
	}
	if users, _ := s.Users(); len(users) != 1 || users[0].Name != "Guest" {
		t.Fatalf("RemoveUser left %v, want only Guest", users)
	}
}
