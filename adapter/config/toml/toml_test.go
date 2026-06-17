package toml

import (
	"reflect"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
)

// TestPortSectionRoundTrip proves the real *port.Section — with the slice-B
// per-transport fields (mac, seed network/zone) — survives a TOML marshal/unmarshal
// cycle, so an [EtherTalk] table in server.toml decodes back to the same section the
// port factory reads. It registers the port schema the way the compose registry does.
func TestPortSectionRoundTrip(t *testing.T) {
	config.Register(config.SectionSchema{
		Key: "EtherTalk",
		New: func() config.Section { return &port.Section{SKey: "EtherTalk"} },
	})

	m := config.NewModel()
	want := &port.Section{
		SKey: "EtherTalk", Iface: "eth0", IsEnabled: true,
		MAC: "00:11:22:aa:bb:cc", SeedNetwork: 10, SeedNetworkEnd: 20, SeedZone: "Engineering",
	}
	m.Set(want)

	c := New()
	data, err := c.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got config.Model
	if err := c.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	sec, ok := got.Get("EtherTalk")
	if !ok {
		t.Fatal("EtherTalk section missing after round-trip")
	}
	if !reflect.DeepEqual(sec, want) {
		t.Fatalf("port section round-trip: got %+v want %+v", sec, want)
	}
}

// fakeSection is a registered component section for the round-trip test.
type fakeSection struct {
	SKey  string `toml:"-"`
	Iface string `toml:"iface"`
	Port  int64  `toml:"port"`
	On    bool   `toml:"on"`
}

func (s *fakeSection) Key() string { return s.SKey }
func (s *fakeSection) Clone() config.Section {
	cp := *s
	return &cp
}
func (s *fakeSection) Validate() error { return nil }

func registerFake(key string) {
	config.Register(config.SectionSchema{
		Key: key,
		New: func() config.Section { return &fakeSection{SKey: key} },
	})
}

// fakeVolume is a repeated (named-instance) section for the array-of-tables test.
type fakeVolume struct {
	VName   string   `toml:"name"`
	Path    string   `toml:"path"`
	RO      bool     `toml:"read_only"`
	Allowed []string `toml:"allowed_users"`
}

func (s *fakeVolume) Key() string          { return "FakeVolumes" }
func (s *fakeVolume) InstanceName() string { return s.VName }
func (s *fakeVolume) Clone() config.Section {
	cp := *s
	cp.Allowed = append([]string(nil), s.Allowed...)
	return &cp
}
func (s *fakeVolume) Validate() error { return nil }

func registerFakeVolumes() {
	config.Register(config.SectionSchema{
		Key:      "FakeVolumes",
		Repeated: true,
		New:      func() config.Section { return &fakeVolume{} },
	})
}

func TestRoundTrip(t *testing.T) {
	registerFake("Alpha")
	registerFake("Beta")

	m := config.NewModel()
	m.Identity = config.Identity{Hostname: "CLASSICSTACK", Workgroup: "MYGROUP", Description: "test server"}
	m.Logging = config.LoggingSection{Level: "debug"}
	m.Router = config.RouterSection{DefaultZone: "MyZone", Members: []string{"et-lab", "tt-attic"}}
	m.Bridge = config.InterfaceSection{Name: "br-lan", Addr: "10.0.0.1"}
	m.Set(&fakeSection{SKey: "Alpha", Iface: "eth0", Port: 548, On: true})
	m.Set(&fakeSection{SKey: "Beta", Iface: "eth1", Port: 139})

	c := New()
	data, err := c.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got config.Model
	if err := c.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.Identity != m.Identity {
		t.Errorf("Identity: got %+v want %+v", got.Identity, m.Identity)
	}
	if got.Logging != m.Logging {
		t.Errorf("Logging: got %+v want %+v", got.Logging, m.Logging)
	}
	if !reflect.DeepEqual(got.Router, m.Router) {
		t.Errorf("Router: got %+v want %+v", got.Router, m.Router)
	}
	if !reflect.DeepEqual(got.Bridge, m.Bridge) {
		t.Errorf("Bridge: got %+v want %+v", got.Bridge, m.Bridge)
	}
	for _, key := range []string{"Alpha", "Beta"} {
		want, _ := m.Get(key)
		gotSec, ok := got.Get(key)
		if !ok {
			t.Errorf("section %s missing after round-trip", key)
			continue
		}
		if !reflect.DeepEqual(want, gotSec) {
			t.Errorf("section %s: got %+v want %+v", key, gotSec, want)
		}
	}
}

// TestInterfaceNamespaceRoundTrip proves the named interface namespace survives a
// TOML [[interface]] array-of-tables round-trip — a nic, a serial, and a bridge
// entry decode back to the same Interfaces map.
func TestInterfaceNamespaceRoundTrip(t *testing.T) {
	m := config.NewModel()
	m.SetInterface(config.InterfaceSection{Name: "eth0", Kind: config.IfaceKindNIC, Addr: "10.0.0.2"})
	m.SetInterface(config.InterfaceSection{Name: "ttyUSB-attic", Kind: config.IfaceKindSerial, Device: "/dev/ttyUSB0", Baud: 1000000})
	m.SetInterface(config.InterfaceSection{Name: "br-lan", Kind: config.IfaceKindBridge, Members: []string{"eth0", "eth1"}})

	c := New()
	data, err := c.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got config.Model
	if err := c.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got.Interfaces, m.Interfaces) {
		t.Fatalf("Interfaces round-trip:\n got  %+v\n want %+v", got.Interfaces, m.Interfaces)
	}
}

func TestRepeatedSectionRoundTrip(t *testing.T) {
	registerFakeVolumes()

	m := config.NewModel()
	m.AddInstance(&fakeVolume{VName: "public", Path: "/srv/public", Allowed: []string{"alice"}})
	m.AddInstance(&fakeVolume{VName: "private", Path: "/srv/private", RO: true})

	c := New()
	data, err := c.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got config.Model
	if err := c.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	list := got.List("FakeVolumes")
	if len(list) != 2 {
		t.Fatalf("got %d instances, want 2 (data:\n%s)", len(list), data)
	}
	// Order is preserved.
	pub := list[0].(*fakeVolume)
	priv := list[1].(*fakeVolume)
	if pub.VName != "public" || pub.Path != "/srv/public" || len(pub.Allowed) != 1 || pub.Allowed[0] != "alice" {
		t.Errorf("public round-trip: %+v", pub)
	}
	if priv.VName != "private" || priv.Path != "/srv/private" || !priv.RO {
		t.Errorf("private round-trip: %+v", priv)
	}
}
