package smb

import (
	"errors"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// memShareSpec is a minimal valid share spec (the memfs/appledouble/macroman triple)
// under a given name, with an optional description.
func memShareSpec(name, desc string) ShareSpec {
	return ShareSpec{
		Name:        name,
		Description: desc,
		Share: fs.ShareSpec{
			Name:          name,
			FSType:        "memfs",
			ForkBackend:   "appledouble",
			FilenameCodec: "macroman-utf8",
		},
	}
}

// TestReconcileSharesAddUpdateRemove drives the three reconcile moves keyed by name
// (case-insensitively, as tree-connect matches) and asserts an update re-applies the
// description.
func TestReconcileSharesAddUpdateRemove(t *testing.T) {
	svc, err := NewWithShares(nil, memShareSpec("A", "first"), memShareSpec("B", "second"))
	if err != nil {
		t.Fatalf("NewWithShares: %v", err)
	}

	// Drop A, update B's description, add C.
	if err := svc.ReconcileShares([]ShareSpec{memShareSpec("B", "second-v2"), memShareSpec("C", "third")}); err != nil {
		t.Fatalf("ReconcileShares: %v", err)
	}
	names := svc.shareNames()
	if len(names) != 2 || names[0] != "B" || names[1] != "C" {
		t.Fatalf("after reconcile names = %v, want [B C]", names)
	}
	if _, ok := svc.ShareByName("A"); ok {
		t.Fatal("A should have been removed")
	}
	b, ok := svc.ShareByName("b") // case-insensitive lookup
	if !ok {
		t.Fatal("B should still be bound")
	}
	if b.Description() != "second-v2" {
		t.Fatalf("B description not updated: %q", b.Description())
	}
}

// TestReconcileSharesBadSpecAtomic: a bad spec in the desired set leaves the live
// shares untouched (all-or-nothing).
func TestReconcileSharesBadSpecAtomic(t *testing.T) {
	svc, err := NewWithShares(nil, memShareSpec("Keep", ""))
	if err != nil {
		t.Fatalf("NewWithShares: %v", err)
	}
	bad := ShareSpec{Name: "Bad", Share: fs.ShareSpec{Name: "Bad", FSType: "no-such-fs-type"}}
	if err := svc.ReconcileShares([]ShareSpec{memShareSpec("New", ""), bad}); err == nil {
		t.Fatal("reconcile with a bad spec should fail")
	}
	if names := svc.shareNames(); len(names) != 1 || names[0] != "Keep" {
		t.Fatalf("live shares mutated by a failed reconcile: %v", names)
	}
}

// TestApplyConfigReconcilesFromResolver: ApplyConfig ignores the section payload and
// reconciles from the wired resolver (the supervisor's hot-apply path).
func TestApplyConfigReconcilesFromResolver(t *testing.T) {
	svc := New(nil)
	desired := []ShareSpec{memShareSpec("One", "")}
	svc.SetShareResolver(func() ([]ShareSpec, error) { return desired, nil })

	if err := svc.ApplyConfig(nil); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}
	if names := svc.shareNames(); len(names) != 1 || names[0] != "One" {
		t.Fatalf("ApplyConfig did not reconcile from resolver: %v", names)
	}

	desired = []ShareSpec{memShareSpec("Two", "")}
	if err := svc.ApplyConfig(nil); err != nil {
		t.Fatalf("second ApplyConfig: %v", err)
	}
	if names := svc.shareNames(); len(names) != 1 || names[0] != "Two" {
		t.Fatalf("ApplyConfig did not pick up the new desired set: %v", names)
	}
}

// TestApplyConfigNoResolverNeedsRestart: with no resolver wired, ApplyConfig defers to
// the supervisor's rebuild path.
func TestApplyConfigNoResolverNeedsRestart(t *testing.T) {
	svc := New(nil)
	if err := svc.ApplyConfig(nil); !errors.Is(err, component.ErrNeedsRestart) {
		t.Fatalf("ApplyConfig err = %v, want ErrNeedsRestart", err)
	}
}

// TestApplyConfigEndToEndFromModel: the registry-style wiring — resolver closes over a
// model whose SMB share list changes, and ApplyConfig reflects it.
func TestApplyConfigEndToEndFromModel(t *testing.T) {
	m := config.NewModel()
	m.AddInstance(&ShareSection{SName: "S1", FSType: "memfs", ForkBackend: "appledouble", FilenameCodec: "macroman-utf8"})

	svc := New(nil)
	svc.SetShareResolver(func() ([]ShareSpec, error) { return SpecsFromModel(m), nil })
	if err := svc.ApplyConfig(nil); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}
	if names := svc.shareNames(); len(names) != 1 || names[0] != "S1" {
		t.Fatalf("initial apply: %v", names)
	}

	m.AddInstance(&ShareSection{SName: "S2", FSType: "memfs", ForkBackend: "appledouble", FilenameCodec: "macroman-utf8"})
	if err := svc.ApplyConfig(nil); err != nil {
		t.Fatalf("ApplyConfig after model change: %v", err)
	}
	if names := svc.shareNames(); len(names) != 2 || names[1] != "S2" {
		t.Fatalf("after model change: %v", names)
	}
}

// shareNames returns the bound share names in order (test helper).
func (s *Service) shareNames() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.shares))
	for _, sh := range s.shares {
		out = append(out, sh.Name())
	}
	return out
}
