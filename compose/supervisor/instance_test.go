package supervisor

import (
	"context"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
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

// TestAddInstanceBuildsFirstPortNode asserts that adding the FIRST instance of a repeated
// PORT (owner == section key, no live node yet) BUILDS a new supervised node via the
// injected InstanceBuilder and starts it — the config-builder path for a transport that had
// zero instances at startup (the "unknown component: NetBEUI" fix). It must NOT go through
// the owner-reconcile path (there is no owner node to reconcile).
func TestAddInstanceBuildsFirstPortNode(t *testing.T) {
	log := &orderLog{}
	m := config.NewModel()
	s := New(m, nil)

	built := &configurableComp{name: "NetBEUI", log: log}
	var buildCalls int
	s.SetInstanceBuilder(func(_ *config.Model, ownerKey, instanceName string) (component.Component, []string, error) {
		buildCalls++
		if ownerKey != "NetBEUI" || instanceName != "NetBEUI" {
			t.Errorf("builder got owner=%q instance=%q, want NetBEUI/NetBEUI", ownerKey, instanceName)
		}
		return built, nil, nil
	})

	// A port instance whose owner == its schema key, with an empty name → node name defaults
	// to the key (mirrors registry.Instances: an unnamed instance is addressed by the key).
	sec := namedSection{key: "NetBEUI", name: ""}
	if err := s.AddInstance(context.Background(), "NetBEUI", sec); err != nil {
		t.Fatalf("AddInstance: %v", err)
	}
	if buildCalls != 1 {
		t.Fatalf("InstanceBuilder called %d times, want 1", buildCalls)
	}
	// The new node must be supervised, running, and started exactly once.
	if _, ok := s.nodes["NetBEUI"]; !ok {
		t.Fatalf("new port node not registered with the supervisor")
	}
	if !s.nodes["NetBEUI"].running {
		t.Fatalf("new port node was not started")
	}
	starts := 0
	for _, e := range log.seq {
		if e == "start:NetBEUI" {
			starts++
		}
	}
	if starts != 1 {
		t.Fatalf("start:NetBEUI logged %d times, want 1", starts)
	}
	if built.applied != 0 {
		t.Fatalf("new port node was reconfigured (applied=%d), want a fresh build", built.applied)
	}
}

// TestAddInstanceReconfiguresExistingPortWithSection asserts that editing an
// already-supervised port (owner == schema key, node exists) passes the section
// into ApplyConfig so iface/device changes take effect instead of a nil notify.
func TestAddInstanceReconfiguresExistingPortWithSection(t *testing.T) {
	log := &orderLog{}
	m := config.NewModel()
	s := New(m, nil)
	port := &configurableComp{name: "EtherTalk", log: log}
	s.Add(port, nil)
	if err := s.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	port.applied = 0

	sec := namedSection{key: "EtherTalk", name: "EtherTalk"}
	if err := s.AddInstance(context.Background(), "EtherTalk", sec); err != nil {
		t.Fatalf("AddInstance: %v", err)
	}
	if port.applied != 1 {
		t.Fatalf("ApplyConfig called %d times, want 1", port.applied)
	}
	if port.lastSection != sec {
		t.Fatalf("ApplyConfig got %#v, want the port section (not nil)", port.lastSection)
	}
}

// TestAddInstanceAttachesBuiltPortToTransport asserts that after AddInstance builds and
// starts a repeated-port node, the supervisor invokes the injected TransportAttacher on
// that exact component — the seam that joins a runtime-added IPX/NetBEUI port to its
// mini-router so it carries traffic immediately (not on the next Save+restart).
func TestAddInstanceAttachesBuiltPortToTransport(t *testing.T) {
	log := &orderLog{}
	m := config.NewModel()
	s := New(m, nil)

	built := &configurableComp{name: "IPX", log: log}
	s.SetInstanceBuilder(func(_ *config.Model, _, _ string) (component.Component, []string, error) {
		return built, nil, nil
	})
	var attached []component.Component
	s.SetTransportAttacher(func(_, c component.Component) { attached = append(attached, c) })

	sec := namedSection{key: "IPX", name: ""}
	if err := s.AddInstance(context.Background(), "IPX", sec); err != nil {
		t.Fatalf("AddInstance: %v", err)
	}
	if len(attached) != 1 || attached[0] != built {
		t.Fatalf("TransportAttacher called with %v, want exactly the built node once", attached)
	}
	// It must run AFTER the node started (a dark port would be attached before it can carry).
	if !s.nodes["IPX"].running {
		t.Fatalf("port node not running when attached")
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
