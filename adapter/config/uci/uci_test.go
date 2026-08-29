package uci

import (
	"reflect"
	"strings"
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
	m.SetInterface(config.InterfaceSection{Name: "br-lan", Kind: config.IfaceKindBridge, Addr: "192.168.1.1", Default: true})
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
	if got.HTTP != m.HTTP {
		t.Errorf("HTTP: got %+v want %+v", got.HTTP, m.HTTP)
	}
	if !reflect.DeepEqual(got.Client, m.Client) {
		t.Errorf("Client: got %+v want %+v", got.Client, m.Client)
	}
	if got.FUSE != m.FUSE {
		t.Errorf("FUSE: got %+v want %+v", got.FUSE, m.FUSE)
	}
	if !reflect.DeepEqual(got.Router, m.Router) {
		t.Errorf("Router: got %+v want %+v", got.Router, m.Router)
	}
	if !reflect.DeepEqual(got.Interfaces, m.Interfaces) {
		t.Errorf("Interfaces: got %+v want %+v", got.Interfaces, m.Interfaces)
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
// survives a UCI `config interface '<name>'` round-trip (nic, serial, wifi).
func TestUCICodec_InterfaceNamespaceRoundTrip(t *testing.T) {
	m := config.NewModel()
	m.SetInterface(config.InterfaceSection{Name: "eth0", Kind: config.IfaceKindNIC, Addr: "10.0.0.2"})
	m.SetInterface(config.InterfaceSection{Name: "ttyUSB-attic", Kind: config.IfaceKindSerial, Device: "/dev/ttyUSB0", Baud: 1000000})
	m.SetInterface(config.InterfaceSection{Name: "wlan0", Kind: config.IfaceKindWifi, SSID: "AppleNet", Key: "secret"})

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

func TestUCIHTTPOmittedDefaults(t *testing.T) {
	var got config.Model
	if err := New().Unmarshal([]byte("package classicstack\n\nconfig identity\n\toption hostname 'x'\n\n"), &got); err != nil {
		t.Fatal(err)
	}
	if !got.HTTP.Enabled || got.HTTP.Addr != config.DefaultHTTPAddr {
		t.Fatalf("omitted config http: %+v, want enabled on %s", got.HTTP, config.DefaultHTTPAddr)
	}
}

func TestUCIHTTPDisabledSticks(t *testing.T) {
	var got config.Model
	data := []byte("package classicstack\n\nconfig http\n\toption enabled '0'\n\n")
	if err := New().Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.HTTP.Enabled {
		t.Fatal("option enabled '0' did not stick")
	}
	if got.HTTP.Addr != config.DefaultHTTPAddr {
		t.Fatalf("blank addr should default to %s, got %q", config.DefaultHTTPAddr, got.HTTP.Addr)
	}
}

func TestUCIClientOmittedDefaultsDisabled(t *testing.T) {
	var got config.Model
	if err := New().Unmarshal([]byte("package classicstack\n\nconfig identity\n\toption hostname 'x'\n\n"), &got); err != nil {
		t.Fatal(err)
	}
	if got.Client.Enabled {
		t.Fatalf("omitted config client should be disabled, got %+v", got.Client)
	}
}

func TestUCIClientRoundTrip(t *testing.T) {
	m := config.NewModel()
	m.Client = config.ClientSection{
		Enabled:        true,
		Iface:          "br-lan",
		Services:       []string{"afp", "smb", "ncp", "etherdfs"},
		MaxIdleMinutes: 10,
		Mount:          true,
		LogFile:        "client.log",
	}
	codec := New()
	data, err := codec.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "config client") {
		t.Fatalf("Marshal should emit config client; got:\n%s", data)
	}
	got := config.NewModel()
	if err := codec.Unmarshal(data, got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Client, m.Client) {
		t.Fatalf("Client round-trip: got %+v want %+v", got.Client, m.Client)
	}
}

func TestUCIFUSEOmittedDefaults(t *testing.T) {
	var got config.Model
	if err := New().Unmarshal([]byte("package classicstack\n\nconfig identity\n\toption hostname 'x'\n\n"), &got); err != nil {
		t.Fatal(err)
	}
	if got.FUSE.MountTimeoutSeconds != config.DefaultFUSEMountTimeoutSeconds {
		t.Fatalf("omitted config fuse timeout = %d, want %d", got.FUSE.MountTimeoutSeconds, config.DefaultFUSEMountTimeoutSeconds)
	}
}

func TestUCIFUSERoundTrip(t *testing.T) {
	m := config.NewModel()
	m.FUSE = config.FUSESection{MountTimeoutSeconds: 45}
	codec := New()
	data, err := codec.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "config fuse") {
		t.Fatalf("Marshal should emit config fuse; got:\n%s", data)
	}
	got := config.NewModel()
	if err := codec.Unmarshal(data, got); err != nil {
		t.Fatal(err)
	}
	if got.FUSE != m.FUSE {
		t.Fatalf("FUSE round-trip: got %+v want %+v", got.FUSE, m.FUSE)
	}
}

func TestUCIFUSEVolumesRoundTrip(t *testing.T) {
	config.RegisterFUSEVolumes()
	m := config.NewModel()
	want := &config.FUSEVolumeSection{
		Remote:     "smb://foo:pass@foohost,smb/share",
		Mountpoint: "/Volumes/share",
		ReadOnly:   true,
	}
	m.AddInstance(want)
	codec := New()
	data, err := codec.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "config fusevolumes") {
		t.Fatalf("Marshal should emit config fusevolumes; got:\n%s", data)
	}
	got := config.NewModel()
	if err := codec.Unmarshal(data, got); err != nil {
		t.Fatal(err)
	}
	sec, ok := got.Instance(config.FUSEVolumesKey, "/Volumes/share")
	if !ok {
		t.Fatal("FUSE volume missing after UCI round-trip")
	}
	if !reflect.DeepEqual(sec, want) {
		t.Fatalf("FUSE volume UCI round-trip: got %+v want %+v", sec, want)
	}
}
