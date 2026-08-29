package describe_test

import (
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/adapter/config/describe"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
)

func TestFieldsOfIPXSection(t *testing.T) {
	fields := describe.FieldsOf(&port.IPXSection{Base: port.Base{SKey: "IPX"}})
	byKey := map[string]config.FieldInfo{}
	for _, f := range fields {
		byKey[f.Key] = f
	}
	for _, want := range []string{"Name", "Iface", "IsEnabled", "IPXFrameType", "IPXNetwork", "Capture"} {
		if _, ok := byKey[want]; !ok {
			t.Errorf("missing field %q in %#v", want, fields)
		}
	}
	if byKey["IPXNetwork"].Capability != config.CapIPXNetwork {
		t.Errorf("IPXNetwork capability = %q", byKey["IPXNetwork"].Capability)
	}
	if byKey["Capture"].DisplayName == "" {
		t.Error("Capture should carry display name from tag")
	}
	if byKey["IPXNetwork"].Example == "" {
		t.Error("IPXNetwork should carry example from tag")
	}
	if _, ok := byKey["SeedNetwork"]; ok {
		t.Error("IPX section must not expose AppleTalk seed fields")
	}
	if _, ok := byKey["Device"]; ok {
		t.Error("IPX section must not expose serial fields")
	}
}

func TestFieldsOfEtherTalkSection(t *testing.T) {
	fields := describe.FieldsOf(&port.EtherTalkSection{Base: port.Base{SKey: "EtherTalk"}})
	byKey := map[string]config.FieldInfo{}
	for _, f := range fields {
		byKey[f.Key] = f
	}
	if byKey["SeedZone"].Capability != config.CapSeed {
		t.Errorf("SeedZone capability = %q", byKey["SeedZone"].Capability)
	}
	if byKey["SeedZone"].Widget != "" {
		t.Errorf("SeedZone widget = %q, want empty (ports seed zones; they do not pick from the live list)", byKey["SeedZone"].Widget)
	}
	if _, ok := byKey["IPXNetwork"]; ok {
		t.Error("EtherTalk must not expose IPX network")
	}
}

func TestDescribeDetectsCapabilities(t *testing.T) {
	const key = "DescribeTestIPX"
	config.Register(config.SectionSchema{
		Key:         key,
		Repeated:    true,
		DisplayName: "Test IPX",
		Description: "fixture",
		New:         func() config.Section { return &port.IPXSection{Base: port.Base{SKey: key}} },
	})
	sc, ok := config.SchemaFor(key)
	if !ok {
		t.Fatal("schema not registered")
	}
	info := describe.Describe(sc)
	if info.DisplayName != "Test IPX" {
		t.Errorf("DisplayName = %q", info.DisplayName)
	}
	wantCaps := map[string]bool{
		config.CapWireBinding: true,
		config.CapCapture:     true,
		config.CapIPXNetwork:  true,
		config.CapIPXFraming:  true,
	}
	for _, c := range info.Capabilities {
		delete(wantCaps, c)
	}
	for missing := range wantCaps {
		t.Errorf("missing capability %q; got %v", missing, info.Capabilities)
	}
	if len(info.Fields) == 0 {
		t.Error("expected reflected fields")
	}
}

func TestDefaultValue(t *testing.T) {
	if v := describe.DefaultValue(config.FieldInfo{Type: "bool", Default: "true"}); v != true {
		t.Errorf("bool default = %#v", v)
	}
	if v := describe.DefaultValue(config.FieldInfo{Type: "int", Default: "30"}); v != 30 {
		t.Errorf("int default = %#v", v)
	}
	if v := describe.DefaultValue(config.FieldInfo{Type: "string", Default: "x"}); v != "x" {
		t.Errorf("string default = %#v", v)
	}
}

func TestFieldsOfShareLabels(t *testing.T) {
	// Volume/share fields must carry display tags so the SPA does not fall back to
	// acronym-splitting humanise ("D Name", "M A C", "F S Type").
	type tagged struct {
		DName  string `toml:"name" display:"Drive letter"`
		MAC    string `toml:"mac,omitempty" display:"Station MAC"`
		FSType string `toml:"fs_type,omitempty" display:"Filesystem type"`
	}
	fields := describe.FieldsOf(&tagged{})
	byKey := map[string]config.FieldInfo{}
	for _, f := range fields {
		byKey[f.Key] = f
	}
	if byKey["DName"].DisplayName != "Drive letter" {
		t.Errorf("DName display = %q", byKey["DName"].DisplayName)
	}
	if byKey["MAC"].DisplayName != "Station MAC" {
		t.Errorf("MAC display = %q", byKey["MAC"].DisplayName)
	}
	if byKey["FSType"].DisplayName != "Filesystem type" {
		t.Errorf("FSType display = %q", byKey["FSType"].DisplayName)
	}
}
