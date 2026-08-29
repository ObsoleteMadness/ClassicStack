package afp

import (
	"errors"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// memVolSpec is a minimal valid volume spec (the memfs/appledouble/macroman triple
// the other tests use) under a given name.
func memVolSpec(name string) VolumeSpec {
	return VolumeSpec{
		Name: name,
		Share: fs.ShareSpec{
			Name:          name,
			FSType:        "memfs",
			ForkBackend:   "appledouble",
			FilenameCodec: "macroman-utf8",
		},
	}
}

// TestReconcileVolumesAddUpdateRemove drives the three reconcile moves keyed by name
// and asserts an update preserves the volume's AFP id (so a client mid-session keeps
// addressing the same volume number).
func TestReconcileVolumesAddUpdateRemove(t *testing.T) {
	svc, err := NewWithVolumes(nil, VolumeSpec{ID: 1, Name: "A", Share: memVolSpec("A").Share}, VolumeSpec{ID: 2, Name: "B", Share: memVolSpec("B").Share})
	if err != nil {
		t.Fatalf("NewWithVolumes: %v", err)
	}
	bID, ok := svc.volIDOf("B")
	if !ok {
		t.Fatal("B not bound")
	}

	// Drop A, keep B (update, id preserved), add C.
	if err := svc.ReconcileVolumes([]VolumeSpec{memVolSpec("B"), memVolSpec("C")}); err != nil {
		t.Fatalf("ReconcileVolumes: %v", err)
	}
	names := svc.volNames()
	if len(names) != 2 || names[0] != "B" || names[1] != "C" {
		t.Fatalf("after reconcile names = %v, want [B C]", names)
	}
	if svc.volumeByName("A") != nil {
		t.Fatal("A should have been removed")
	}
	if got, _ := svc.volIDOf("B"); got != bID {
		t.Fatalf("B id changed across update: was %d, now %d", bID, got)
	}
	if id, _ := svc.volIDOf("C"); id == 0 || id == bID {
		t.Fatalf("C should have a fresh non-zero id distinct from B (%d), got %d", bID, id)
	}
}

// TestReconcileVolumesBadSpecAtomic: a bad spec in the desired set leaves the live
// volumes untouched (all-or-nothing).
func TestReconcileVolumesBadSpecAtomic(t *testing.T) {
	svc, err := NewWithVolumes(nil, VolumeSpec{ID: 1, Name: "Keep", Share: memVolSpec("Keep").Share})
	if err != nil {
		t.Fatalf("NewWithVolumes: %v", err)
	}
	bad := VolumeSpec{Name: "Bad", Share: fs.ShareSpec{Name: "Bad", FSType: "no-such-fs-type"}}
	if err := svc.ReconcileVolumes([]VolumeSpec{memVolSpec("New"), bad}); err == nil {
		t.Fatal("reconcile with a bad spec should fail")
	}
	if names := svc.volNames(); len(names) != 1 || names[0] != "Keep" {
		t.Fatalf("live volumes mutated by a failed reconcile: %v", names)
	}
}

// TestApplyConfigReconcilesFromResolver: ApplyConfig ignores the section payload and
// reconciles from the wired resolver (the supervisor's hot-apply path).
func TestApplyConfigReconcilesFromResolver(t *testing.T) {
	svc := New(nil)
	desired := []VolumeSpec{memVolSpec("One")}
	svc.SetVolumeResolver(func() ([]VolumeSpec, error) { return desired, nil })

	if err := svc.ApplyConfig(nil); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}
	if names := svc.volNames(); len(names) != 1 || names[0] != "One" {
		t.Fatalf("ApplyConfig did not reconcile from resolver: %v", names)
	}

	// A later resolver result drops One and adds Two — hot-applied with no restart.
	desired = []VolumeSpec{memVolSpec("Two")}
	if err := svc.ApplyConfig(nil); err != nil {
		t.Fatalf("second ApplyConfig: %v", err)
	}
	if names := svc.volNames(); len(names) != 1 || names[0] != "Two" {
		t.Fatalf("ApplyConfig did not pick up the new desired set: %v", names)
	}
}

// TestApplyConfigNoResolverNeedsRestart: with no resolver wired, ApplyConfig defers to
// the supervisor's rebuild path.
func TestApplyConfigNoResolverNeedsRestart(t *testing.T) {
	svc := New(nil)
	if err := svc.ApplyConfig(nil); err == nil {
		t.Fatal("ApplyConfig with no resolver should report a need-restart")
	} else if !errors.Is(err, component.ErrNeedsRestart) {
		t.Fatalf("ApplyConfig err = %v, want ErrNeedsRestart", err)
	}
}

// TestApplyConfigEndToEndFromModel: the registry-style wiring — resolver closes over a
// model whose AFP volume list changes, and ApplyConfig reflects it.
func TestApplyConfigEndToEndFromModel(t *testing.T) {
	m := config.NewModel()
	m.AddInstance(&VolumeSection{VName: "Vol1", FSType: "memfs", ForkBackend: "appledouble", FilenameCodec: "macroman-utf8"})

	svc := New(nil)
	svc.SetVolumeResolver(func() ([]VolumeSpec, error) {
		specs := SpecsFromModel(m)
		out := make([]VolumeSpec, 0, len(specs))
		for i, sp := range specs {
			out = append(out, VolumeSpec{ID: uint16(i + 1), Name: sp.Name, Share: sp})
		}
		return out, nil
	})
	if err := svc.ApplyConfig(nil); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}
	if names := svc.volNames(); len(names) != 1 || names[0] != "Vol1" {
		t.Fatalf("initial apply: %v", names)
	}

	m.AddInstance(&VolumeSection{VName: "Vol2", FSType: "memfs", ForkBackend: "appledouble", FilenameCodec: "macroman-utf8"})
	if err := svc.ApplyConfig(nil); err != nil {
		t.Fatalf("ApplyConfig after model change: %v", err)
	}
	if names := svc.volNames(); len(names) != 2 || names[1] != "Vol2" {
		t.Fatalf("after model change: %v", names)
	}
}

// volNames returns the bound volume display names in order (test helper).
func (s *Service) volNames() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.volumes))
	for _, v := range s.volumes {
		out = append(out, v.Name())
	}
	return out
}

// volIDOf returns the AFP id bound to the named volume (test helper).
func (s *Service) volIDOf(name string) (uint16, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, v := range s.volumes {
		if v.Name() == name {
			return v.ID(), true
		}
	}
	return 0, false
}
