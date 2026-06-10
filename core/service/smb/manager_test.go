package smb

import (
	"errors"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/share"
)

func memSpec(name string) fs.ShareSpec {
	return fs.ShareSpec{Name: name, FSType: "memfs", ForkBackend: "appledouble"}
}

func TestService_Manager_AddUpdateRemove(t *testing.T) {
	s := New(nil)

	if err := s.AddShare(memSpec("Media")); err != nil {
		t.Fatalf("AddShare: %v", err)
	}
	if err := s.AddShare(memSpec("Media")); !errors.Is(err, share.ErrDuplicateShare) {
		t.Fatalf("duplicate AddShare err = %v, want ErrDuplicateShare", err)
	}
	if got := s.Shares(); len(got) != 1 || got[0].Name != "Media" {
		t.Fatalf("Shares = %+v, want one Media", got)
	}

	// A bad spec must fail without binding (and without disturbing the existing one).
	if err := s.AddShare(fs.ShareSpec{Name: "Bad", FSType: "no-such-fs"}); err == nil {
		t.Fatal("AddShare with unknown fs_type should fail")
	}
	if len(s.Shares()) != 1 {
		t.Fatal("failed AddShare must not bind a share")
	}

	if err := s.UpdateShare("Media", memSpec("Media")); err != nil {
		t.Fatalf("UpdateShare: %v", err)
	}
	if err := s.UpdateShare("Nope", memSpec("Nope")); !errors.Is(err, share.ErrNoSuchShare) {
		t.Fatalf("UpdateShare unknown err = %v, want ErrNoSuchShare", err)
	}

	if err := s.RemoveShare("Media"); err != nil {
		t.Fatalf("RemoveShare: %v", err)
	}
	if err := s.RemoveShare("Media"); !errors.Is(err, share.ErrNoSuchShare) {
		t.Fatalf("second RemoveShare err = %v, want ErrNoSuchShare", err)
	}
	if len(s.Shares()) != 0 {
		t.Fatal("share list should be empty after removal")
	}
}

// TestRemoveShare_KeepsInFlightHandle asserts a handle obtained before removal
// stays usable afterwards — RemoveShare unpublishes but does not tear down.
func TestRemoveShare_KeepsInFlightHandle(t *testing.T) {
	s := New(nil)
	if err := s.AddShare(memSpec("Media")); err != nil {
		t.Fatalf("AddShare: %v", err)
	}
	sh, ok := s.ShareByName("Media")
	if !ok {
		t.Fatal("ShareByName(Media) not found")
	}
	if _, err := sh.FS().CreateFile("note"); err != nil {
		t.Fatalf("CreateFile: %v", err)
	}

	if err := s.RemoveShare("Media"); err != nil {
		t.Fatalf("RemoveShare: %v", err)
	}
	// New binds fail…
	if _, ok := s.ShareByName("Media"); ok {
		t.Fatal("removed share still resolvable by name")
	}
	// …but the already-held handle still works.
	if _, err := sh.FS().Stat("note"); err != nil {
		t.Fatalf("in-flight handle broke after RemoveShare: %v", err)
	}
}
