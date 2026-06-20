package supervisor

import (
	"context"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/config"
)

// namedSection is a config.NamedSection (a repeated-section instance) for testing the
// supervisor's AddInstance/RemoveInstance path: it carries a schema key + an instance
// name, mirroring an AFP volume / SMB share.
type namedSection struct {
	key  string
	name string
}

func (n namedSection) Key() string           { return n.key }
func (n namedSection) InstanceName() string  { return n.name }
func (n namedSection) Clone() config.Section { return n }
func (n namedSection) Validate() error       { return nil }

var _ config.NamedSection = namedSection{}

// TestAddInstanceStagesAndReconfiguresOwner asserts AddInstance writes the named
// instance into Model.Lists (not Sections) and reconfigures the OWNER component so it
// reconciles the new volume/share live. The owner is Configurable and hot-applies, so
// no restart occurs.
func TestAddInstanceStagesAndReconfiguresOwner(t *testing.T) {
	log := &orderLog{}
	m := config.NewModel()
	s := New(m, nil)

	owner := &configurableComp{name: "AFP", log: log, applyErr: nil}
	s.Add(owner, nil)
	if err := s.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	owner.applied = 0 // ignore any apply during start path

	sec := namedSection{key: "AFPVolumes", name: "Public"}
	if err := s.AddInstance(context.Background(), "AFP", sec); err != nil {
		t.Fatalf("AddInstance: %v", err)
	}

	// The instance must land in Lists under its schema key, not Sections.
	if _, ok := m.Instance("AFPVolumes", "Public"); !ok {
		t.Fatalf("instance not staged into Model.Lists[AFPVolumes]")
	}
	if _, ok := m.Get("AFPVolumes"); ok {
		t.Fatalf("named instance wrongly written to Model.Sections")
	}
	// The owner must have been reconfigured (re-resolved from the model).
	if owner.applied != 1 {
		t.Fatalf("owner ApplyConfig called %d times, want 1", owner.applied)
	}
}

// TestRemoveInstanceDropsAndReconfiguresOwner asserts RemoveInstance drops the named
// instance from the model and reconfigures the owner; a missing instance is a no-op.
func TestRemoveInstanceDropsAndReconfiguresOwner(t *testing.T) {
	log := &orderLog{}
	m := config.NewModel()
	m.AddInstance(namedSection{key: "SMBShares", name: "Docs"})
	s := New(m, nil)

	owner := &configurableComp{name: "SMB", log: log, applyErr: nil}
	s.Add(owner, nil)
	if err := s.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	owner.applied = 0

	if err := s.RemoveInstance(context.Background(), "SMB", "SMBShares", "Docs"); err != nil {
		t.Fatalf("RemoveInstance: %v", err)
	}
	if _, ok := m.Instance("SMBShares", "Docs"); ok {
		t.Fatalf("instance still present after RemoveInstance")
	}
	if owner.applied != 1 {
		t.Fatalf("owner ApplyConfig called %d times, want 1", owner.applied)
	}

	// Removing an absent instance is a no-op: no further owner reconfigure.
	if err := s.RemoveInstance(context.Background(), "SMB", "SMBShares", "Nope"); err != nil {
		t.Fatalf("RemoveInstance(absent): %v", err)
	}
	if owner.applied != 1 {
		t.Fatalf("owner reconfigured for an absent removal: applied=%d", owner.applied)
	}
}

// TestReconfigureNamedSectionRoutesToLists asserts a Reconfigure carrying a NamedSection
// (an in-place edit of an existing instance) updates Model.Lists, not Sections.
func TestReconfigureNamedSectionRoutesToLists(t *testing.T) {
	log := &orderLog{}
	m := config.NewModel()
	s := New(m, nil)
	owner := &configurableComp{name: "AFP", log: log}
	s.Add(owner, nil)
	if err := s.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: %v", err)
	}

	if err := s.Reconfigure(context.Background(), "AFP", namedSection{key: "AFPVolumes", name: "Public"}); err != nil {
		t.Fatalf("Reconfigure: %v", err)
	}
	if _, ok := m.Instance("AFPVolumes", "Public"); !ok {
		t.Fatalf("Reconfigure of a NamedSection did not write to Model.Lists")
	}
	if _, ok := m.Get("AFPVolumes"); ok {
		t.Fatalf("Reconfigure of a NamedSection wrongly wrote to Model.Sections")
	}
}
