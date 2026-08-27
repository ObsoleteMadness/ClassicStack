package supervisor

import (
	"context"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/config"
)

// ifaceSection is a config.Section that names an interface (config.InterfaceProvider),
// so the supervisor's interface-namespace reconcile can find the ports that reference a
// changed entry.
type ifaceSection struct {
	key   string
	iface config.InterfaceSection
}

func (s ifaceSection) Key() string                        { return s.key }
func (s ifaceSection) Clone() config.Section              { return s }
func (s ifaceSection) Validate() error                    { return nil }
func (s ifaceSection) Interface() config.InterfaceSection { return s.iface }

var _ config.InterfaceProvider = ifaceSection{}

// namedIfaceSection is a repeated-schema instance (config.NamedSection) that also names
// an interface — the shape NetBEUI/IPX/EtherTalk/LocalTalk sections take. An empty
// InstanceName models the unnamed default instance of a repeated schema (e.g. the sole
// [[netbeui]] entry in server.toml), whose node name falls back to the schema key.
type namedIfaceSection struct {
	key          string
	instanceName string
	iface        config.InterfaceSection
}

func (s namedIfaceSection) Key() string                        { return s.key }
func (s namedIfaceSection) InstanceName() string               { return s.instanceName }
func (s namedIfaceSection) Clone() config.Section              { return s }
func (s namedIfaceSection) Validate() error                    { return nil }
func (s namedIfaceSection) Interface() config.InterfaceSection { return s.iface }

var (
	_ config.NamedSection      = namedIfaceSection{}
	_ config.InterfaceProvider = namedIfaceSection{}
)

// TestSetInterfaceReconcilesRepeatedInstance: a component backing the unnamed default
// instance of a repeated schema (e.g. the sole [[netbeui]] entry, node-named "NetBEUI"
// after its schema key) must still be found and reconfigured when the interface it
// references changes. Regression for reconcileInterfaceRefsLocked using model.Get,
// which only resolves singleton Sections and silently skipped every repeated-schema
// port (NetBEUI, IPX, EtherTalk, LocalTalk) on a live interface edit.
func TestSetInterfaceReconcilesRepeatedInstance(t *testing.T) {
	m := config.NewModel()
	m.AddInstance(namedIfaceSection{key: "NetBEUI", iface: config.InterfaceSection{Name: "br-lan"}})

	log := &orderLog{}
	s := New(m, nil)
	c := &configurableComp{name: "NetBEUI", log: log, applyErr: nil}
	s.Add(c, nil)
	if err := s.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: %v", err)
	}

	if err := s.SetInterface(context.Background(), config.InterfaceSection{
		Name: "br-lan", Kind: config.IfaceKindNIC, Backend: config.IfaceBackendPcap, Device: "en1",
	}); err != nil {
		t.Fatalf("SetInterface: %v", err)
	}

	if c.applied != 1 {
		t.Errorf("repeated-instance port ApplyConfig called %d times, want 1", c.applied)
	}
}

// TestSetInterfaceReconcilesReferencingPort: editing a namespace interface a port
// references reconfigures that port (so the change goes live), and stages the entry.
func TestSetInterfaceReconcilesReferencingPort(t *testing.T) {
	m := config.NewModel()
	// A port section referencing the "eth0" namespace entry.
	m.Set(ifaceSection{key: "port", iface: config.InterfaceSection{Name: "eth0"}})

	log := &orderLog{}
	s := New(m, nil)
	c := &configurableComp{name: "port", log: log, applyErr: nil}
	s.Add(c, nil)
	if err := s.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: %v", err)
	}

	if err := s.SetInterface(context.Background(), config.InterfaceSection{
		Name: "eth0", Kind: config.IfaceKindNIC, Backend: config.IfaceBackendPcap, Addr: "10.0.0.5",
	}); err != nil {
		t.Fatalf("SetInterface: %v", err)
	}

	// The entry must be staged.
	got, ok := m.Interface("eth0")
	if !ok || got.Addr != "10.0.0.5" {
		t.Fatalf("interface not staged: %+v ok=%v", got, ok)
	}
	// The referencing port must have been reconfigured (hot-applied via ApplyConfig).
	if c.applied != 1 {
		t.Errorf("referencing port ApplyConfig called %d times, want 1", c.applied)
	}
}

// TestSetInterfaceSkipsUnrelatedPort: a port referencing a DIFFERENT interface is not
// reconfigured when an unrelated entry changes.
func TestSetInterfaceSkipsUnrelatedPort(t *testing.T) {
	m := config.NewModel()
	m.Set(ifaceSection{key: "port", iface: config.InterfaceSection{Name: "eth1"}})

	s := New(m, nil)
	c := &configurableComp{name: "port", log: &orderLog{}, applyErr: nil}
	s.Add(c, nil)
	if err := s.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: %v", err)
	}

	if err := s.SetInterface(context.Background(), config.InterfaceSection{Name: "eth0"}); err != nil {
		t.Fatalf("SetInterface: %v", err)
	}
	if c.applied != 0 {
		t.Errorf("unrelated port reconfigured %d times, want 0", c.applied)
	}
}

// TestRemoveInterface drops the entry and reconciles the referencing port.
func TestRemoveInterface(t *testing.T) {
	m := config.NewModel()
	m.SetInterface(config.InterfaceSection{Name: "br-lan", Kind: config.IfaceKindNIC})
	m.Set(ifaceSection{key: "port", iface: config.InterfaceSection{Name: "br-lan"}})

	s := New(m, nil)
	c := &configurableComp{name: "port", log: &orderLog{}, applyErr: nil}
	s.Add(c, nil)
	if err := s.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: %v", err)
	}

	if err := s.RemoveInterface(context.Background(), "br-lan"); err != nil {
		t.Fatalf("RemoveInterface: %v", err)
	}
	if _, ok := m.Interface("br-lan"); ok {
		t.Error("interface still present after RemoveInterface")
	}
	if c.applied != 1 {
		t.Errorf("referencing port ApplyConfig called %d times, want 1", c.applied)
	}
}
