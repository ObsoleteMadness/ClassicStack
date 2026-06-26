package smb

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/metastore"
	"github.com/ObsoleteMadness/ClassicStack/core/share"
)

func memSpec(name string) fs.ShareSpec {
	return fs.ShareSpec{Name: name, FSType: "memfs", ForkBackend: "appledouble"}
}

// closeCounter is shared by the closing-backend factory below so a test can observe how
// many times a share's FS was torn down via the fs.FSCloser seam.
var closeCounter atomic.Int32

type closingBackend struct {
	fs.FileSystem
}

func (c *closingBackend) Close() error {
	closeCounter.Add(1)
	return nil
}

func init() {
	// A backend that also implements fs.FSCloser, so service Stop teardown is
	// observable. It embeds a freshly-built memfs share (the simplest real FileSystem)
	// and adds only the Close hook; BuildShare wraps THIS in a shareFS, whose Close
	// (via fs.CloseFS) reaches closingBackend.Close.
	fs.RegisterFS("smb-closing-test-fs", func(spec fs.ShareSpec, b bus.Bus, _ metastore.Store) (fs.FileSystem, error) {
		inner, err := fs.BuildShare(fs.ShareSpec{FSType: "memfs", Name: spec.Name, ForkBackend: "appledouble"}, b)
		if err != nil {
			return nil, err
		}
		return &closingBackend{FileSystem: inner}, nil
	})
}

func closingSpec(name string) fs.ShareSpec {
	return fs.ShareSpec{Name: name, FSType: "smb-closing-test-fs", ForkBackend: "appledouble"}
}

// TestStopClosesShares proves the service closes each live share's FS at Stop (the
// fs.FSCloser teardown that releases a backend's GC-invisible resources), and that a
// hot RemoveShare does NOT close — preserving the in-flight contract.
func TestStopClosesShares(t *testing.T) {
	closeCounter.Store(0)
	s := New(nil)
	if err := s.AddShare(closingSpec("Vault")); err != nil {
		t.Fatalf("AddShare: %v", err)
	}
	if err := s.AddShare(closingSpec("Archive")); err != nil {
		t.Fatalf("AddShare: %v", err)
	}

	// RemoveShare unpublishes but must not tear the FS down (in-flight handles ride out).
	if err := s.RemoveShare("Archive"); err != nil {
		t.Fatalf("RemoveShare: %v", err)
	}
	if got := closeCounter.Load(); got != 0 {
		t.Fatalf("RemoveShare closed the FS (count=%d); it must defer to GC", got)
	}

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := s.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	// Only the still-live share (Vault) is closed; the removed one was already dropped.
	if got := closeCounter.Load(); got != 1 {
		t.Fatalf("Stop close count = %d, want 1 (only the live share)", got)
	}
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

// TestDependencies_VariesByTransportBinding proves SMB's start-order edge to NetBEUI is
// config-varying — present only when the NetBEUI transport is bound — which the old
// static composition-root map could not express.
func TestDependencies_VariesByTransportBinding(t *testing.T) {
	// No bindings set → bind-all default → NetBEUI is bound → edge present.
	s := New(nil)
	if deps := s.Dependencies(); len(deps) != 1 || deps[0] != "NetBEUI" {
		t.Fatalf("default Dependencies = %v, want [NetBEUI]", deps)
	}

	// Bind only TCP → NetBEUI not bound → no NetBEUI edge.
	s.SetBoundTransports([]string{TransportTCP})
	if deps := s.Dependencies(); len(deps) != 0 {
		t.Fatalf("tcp-only Dependencies = %v, want none", deps)
	}

	// Explicitly bind NetBEUI → edge present.
	s.SetBoundTransports([]string{TransportNetBEUI})
	if deps := s.Dependencies(); len(deps) != 1 || deps[0] != "NetBEUI" {
		t.Fatalf("netbeui-bound Dependencies = %v, want [NetBEUI]", deps)
	}

	// BoundTransports reflects the binding for the compose root (TransportBinder).
	if bt := s.BoundTransports(); len(bt) != 1 || bt[0] != TransportNetBEUI {
		t.Fatalf("BoundTransports = %v, want [netbeui]", bt)
	}
}
