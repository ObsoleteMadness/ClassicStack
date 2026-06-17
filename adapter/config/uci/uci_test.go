package uci

import (
	"reflect"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
)

func TestUCICodec_RoundTrip(t *testing.T) {
	// Register a schema for EtherTalk port section so it can be unmarshalled
	config.Register(config.SectionSchema{
		Key: "EtherTalk",
		New: func() config.Section { return &port.Section{SKey: "EtherTalk"} },
	})

	m := config.NewModel()
	m.Identity = config.Identity{Hostname: "CLASSICSTACK", Workgroup: "ETHERGRP", Description: "uci test server"}
	m.Logging = config.LoggingSection{Level: "info"}
	m.Router = config.RouterSection{DefaultZone: "EtherZone", Members: []string{"et-lab", "et-dmz"}}
	m.Bridge = config.InterfaceSection{Name: "br-lan", Addr: "192.168.1.1"}
	m.Set(&port.Section{SKey: "EtherTalk", Iface: "eth0", IsEnabled: true})

	codec := New()
	data, err := codec.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	got := config.NewModel()
	if err := codec.Unmarshal(data, got); err != nil {
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

	wantSec, _ := m.Get("EtherTalk")
	gotSec, ok := got.Get("EtherTalk")
	if !ok {
		t.Fatal("EtherTalk section missing after round-trip")
	}

	if !reflect.DeepEqual(wantSec, gotSec) {
		t.Errorf("EtherTalk: got %+v want %+v", gotSec, wantSec)
	}
}

// TestUCICodec_InterfaceNamespaceRoundTrip proves the named interface namespace
// survives a UCI `config interface '<name>'` round-trip (nic, serial, bridge).
func TestUCICodec_InterfaceNamespaceRoundTrip(t *testing.T) {
	m := config.NewModel()
	m.SetInterface(config.InterfaceSection{Name: "eth0", Kind: config.IfaceKindNIC, Addr: "10.0.0.2"})
	m.SetInterface(config.InterfaceSection{Name: "ttyUSB-attic", Kind: config.IfaceKindSerial, Device: "/dev/ttyUSB0", Baud: 1000000})
	m.SetInterface(config.InterfaceSection{Name: "br-lan", Kind: config.IfaceKindBridge, Members: []string{"eth0", "eth1"}})

	codec := New()
	data, err := codec.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got config.Model
	if err := codec.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got.Interfaces, m.Interfaces) {
		t.Fatalf("Interfaces round-trip:\n got  %+v\n want %+v", got.Interfaces, m.Interfaces)
	}
}

// uciFakeVolume is a repeated (named-instance) section for the UCI repeated-block test.
type uciFakeVolume struct {
	VName   string   `toml:"name"`
	Path    string   `toml:"path"`
	RO      bool     `toml:"read_only"`
	Allowed []string `toml:"allowed_users"`
}

func (s *uciFakeVolume) Key() string          { return "UCIVolumes" }
func (s *uciFakeVolume) InstanceName() string { return s.VName }
func (s *uciFakeVolume) Clone() config.Section {
	cp := *s
	cp.Allowed = append([]string(nil), s.Allowed...)
	return &cp
}
func (s *uciFakeVolume) Validate() error { return nil }

func TestUCICodec_RepeatedSections(t *testing.T) {
	config.Register(config.SectionSchema{
		Key:      "UCIVolumes",
		Repeated: true,
		New:      func() config.Section { return &uciFakeVolume{} },
	})

	m := config.NewModel()
	m.AddInstance(&uciFakeVolume{VName: "public", Path: "/srv/public", Allowed: []string{"alice", "bob"}})
	m.AddInstance(&uciFakeVolume{VName: "private", Path: "/srv/private", RO: true})

	codec := New()
	data, err := codec.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	got := config.NewModel()
	if err := codec.Unmarshal(data, got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	list := got.List("UCIVolumes")
	if len(list) != 2 {
		t.Fatalf("got %d instances, want 2 (data:\n%s)", len(list), data)
	}
	pub := list[0].(*uciFakeVolume)
	priv := list[1].(*uciFakeVolume)
	if pub.VName != "public" || pub.Path != "/srv/public" || len(pub.Allowed) != 2 {
		t.Errorf("public round-trip: %+v", pub)
	}
	if priv.VName != "private" || priv.Path != "/srv/private" || !priv.RO {
		t.Errorf("private round-trip: %+v", priv)
	}
}

// TestUCICodec_BlockNameAuthoritative proves the UCI block name overrides a divergent
// name field on unmarshal (the block name is the authoritative instance key).
func TestUCICodec_BlockNameAuthoritative(t *testing.T) {
	config.Register(config.SectionSchema{
		Key:      "UCIVolumes",
		Repeated: true,
		New:      func() config.Section { return &uciFakeVolume{} },
	})
	// A block named 'renamed' whose inner option name says 'stale'.
	data := []byte("package classicstack\n\nconfig ucivolumes 'renamed'\n\toption name 'stale'\n\toption path '/p'\n\n")
	got := config.NewModel()
	if err := New().Unmarshal(data, got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	list := got.List("UCIVolumes")
	if len(list) != 1 {
		t.Fatalf("got %d instances, want 1", len(list))
	}
	if name := list[0].(*uciFakeVolume).VName; name != "renamed" {
		t.Errorf("block name should win: got %q, want renamed", name)
	}
}
