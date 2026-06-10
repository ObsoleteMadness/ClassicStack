package afp

import (
	"errors"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/share"
)

func memSpec(name string) fs.ShareSpec {
	return fs.ShareSpec{Name: name, FSType: "memfs", ForkBackend: "appledouble", FilenameCodec: "macroman-utf8"}
}

func TestService_Manager_AllocatesIDsAndRejectsDuplicates(t *testing.T) {
	s := New(nil)

	if err := s.AddShare(memSpec("Alpha")); err != nil {
		t.Fatalf("AddShare Alpha: %v", err)
	}
	if err := s.AddShare(memSpec("Beta")); err != nil {
		t.Fatalf("AddShare Beta: %v", err)
	}
	if err := s.AddShare(memSpec("Alpha")); !errors.Is(err, share.ErrDuplicateShare) {
		t.Fatalf("duplicate err = %v, want ErrDuplicateShare", err)
	}

	a, _ := s.VolumeByID(1)
	b, _ := s.VolumeByID(2)
	if a == nil || a.Name() != "Alpha" || b == nil || b.Name() != "Beta" {
		t.Fatalf("ids not allocated lowest-first: id1=%v id2=%v", a, b)
	}

	// Removing id 1 frees it; the next AddShare reuses the lowest free id.
	if err := s.RemoveShare("Alpha"); err != nil {
		t.Fatalf("RemoveShare: %v", err)
	}
	if err := s.AddShare(memSpec("Gamma")); err != nil {
		t.Fatalf("AddShare Gamma: %v", err)
	}
	if g, _ := s.VolumeByID(1); g == nil || g.Name() != "Gamma" {
		t.Fatalf("freed id 1 not reused: %v", g)
	}
}

func TestService_Manager_UpdatePreservesID(t *testing.T) {
	s := New(nil)
	if err := s.AddShare(memSpec("Vol")); err != nil {
		t.Fatalf("AddShare: %v", err)
	}
	v, _ := s.VolumeByID(1)
	if v == nil {
		t.Fatal("volume id 1 missing")
	}

	updated := memSpec("Vol")
	updated.ReadOnly = true
	if err := s.UpdateShare("Vol", updated); err != nil {
		t.Fatalf("UpdateShare: %v", err)
	}
	v2, ok := s.VolumeByID(1)
	if !ok || v2.Name() != "Vol" {
		t.Fatalf("update did not preserve id 1: ok=%v", ok)
	}
	if !v2.sh.ReadOnly() {
		t.Fatal("update did not apply ReadOnly")
	}

	if err := s.UpdateShare("Ghost", memSpec("Ghost")); !errors.Is(err, share.ErrNoSuchShare) {
		t.Fatalf("update unknown err = %v, want ErrNoSuchShare", err)
	}
}
