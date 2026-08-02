package local

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/auth"
)

func tempStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "users.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return s, path
}

func mustAuth(t *testing.T, s *Store, user, pass string) bool {
	t.Helper()
	ok, err := s.Authenticate(user, pass)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

func TestStoreCRUDAndAuth(t *testing.T) {
	s, _ := tempStore(t)

	if err := s.SetUser("alice", "wonderland"); err != nil {
		t.Fatal(err)
	}
	if !mustAuth(t, s, "alice", "wonderland") {
		t.Fatal("correct password rejected")
	}
	if mustAuth(t, s, "alice", "bad") {
		t.Fatal("wrong password accepted")
	}
	// Case-insensitive username match.
	if !mustAuth(t, s, "ALICE", "wonderland") {
		t.Fatal("username match should be case-insensitive")
	}
	if mustAuth(t, s, "nobody", "x") {
		t.Fatal("unknown user authenticated")
	}

	// Password reset.
	if err := s.SetUser("alice", "new-pass"); err != nil {
		t.Fatal(err)
	}
	if mustAuth(t, s, "alice", "wonderland") {
		t.Fatal("old password still works after reset")
	}
	if !mustAuth(t, s, "alice", "new-pass") {
		t.Fatal("new password rejected after reset")
	}

	// Disable / enable.
	if err := s.SetDisabled("alice", true); err != nil {
		t.Fatal(err)
	}
	if mustAuth(t, s, "alice", "new-pass") {
		t.Fatal("disabled user authenticated")
	}
	if err := s.SetDisabled("alice", false); err != nil {
		t.Fatal(err)
	}
	if !mustAuth(t, s, "alice", "new-pass") {
		t.Fatal("re-enabled user rejected")
	}

	// Remove.
	if err := s.RemoveUser("alice"); err != nil {
		t.Fatal(err)
	}
	if mustAuth(t, s, "alice", "new-pass") {
		t.Fatal("removed user authenticated")
	}
}

func TestStoreGuest(t *testing.T) {
	s, path := tempStore(t)

	users, err := s.Users()
	if err != nil || len(users) != 1 || users[0].Name != auth.GuestName || users[0].Disabled {
		t.Fatalf("Users() = %v, want enabled Guest only", users)
	}
	if !s.GuestEnabled() {
		t.Fatal("GuestEnabled = false on fresh store")
	}
	if s.HasUsers() {
		t.Fatal("HasUsers must ignore Guest")
	}
	if err := s.SetUser(auth.GuestName, "pw"); err != auth.ErrGuestImmutable {
		t.Fatalf("SetUser(Guest) = %v, want ErrGuestImmutable", err)
	}
	if err := s.RemoveUser(auth.GuestName); err != auth.ErrGuestImmutable {
		t.Fatalf("RemoveUser(Guest) = %v, want ErrGuestImmutable", err)
	}
	if mustAuth(t, s, auth.GuestName, "pw") {
		t.Fatal("Guest must never authenticate via password")
	}

	if err := s.SetDisabled(auth.GuestName, true); err != nil {
		t.Fatal(err)
	}
	if s.GuestEnabled() {
		t.Fatal("GuestEnabled after disable")
	}
	users, _ = s.Users()
	if !users[0].Disabled {
		t.Fatal("Guest row not marked disabled")
	}

	// Persist + reload.
	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if s2.GuestEnabled() {
		t.Fatal("Guest disabled state did not survive reload")
	}
	if err := s2.SetDisabled(auth.GuestName, false); err != nil {
		t.Fatal(err)
	}
	if !s2.GuestEnabled() {
		t.Fatal("Guest re-enable failed")
	}
}

func TestStoreErrors(t *testing.T) {
	s, _ := tempStore(t)
	if err := s.SetUser("", "pw"); err != auth.ErrEmptyUsername {
		t.Fatalf("empty username err=%v", err)
	}
	if err := s.SetUser("bob", ""); err != auth.ErrEmptyPassword {
		t.Fatalf("empty password err=%v", err)
	}
	if err := s.SetDisabled("ghost", true); err != auth.ErrNoSuchUser {
		t.Fatalf("disable-unknown err=%v", err)
	}
	if err := s.RemoveUser("ghost"); err != auth.ErrNoSuchUser {
		t.Fatalf("remove-unknown err=%v", err)
	}
}

func TestStorePersistenceReload(t *testing.T) {
	s, path := tempStore(t)
	if err := s.SetUser("alice", "pw1"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetUser("BOB", "pw2"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDisabled("bob", true); err != nil {
		t.Fatal(err)
	}

	// Reopen from disk: state must survive.
	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if !mustAuth(t, s2, "alice", "pw1") {
		t.Fatal("alice did not survive reload")
	}
	if mustAuth(t, s2, "bob", "pw2") {
		t.Fatal("disabled bob authenticated after reload")
	}
	users, err := s2.Users()
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 3 {
		t.Fatalf("reloaded %d users, want 3 (Guest + alice + BOB)", len(users))
	}
	// Guest first, then named accounts sorted (alice, BOB) with original-case preserved.
	if users[0].Name != auth.GuestName || users[1].Name != "alice" || users[2].Name != "BOB" {
		t.Fatalf("users = %+v, want [Guest alice BOB]", users)
	}
	if !users[2].Disabled {
		t.Fatal("bob disabled flag lost on reload")
	}
}

func TestStoreMalformedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.db")
	if err := os.WriteFile(path, []byte("# comment\n\nalice:onlytwofields\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err != ErrMalformedFile {
		t.Fatalf("Open malformed err=%v, want ErrMalformedFile", err)
	}
}
