package port

import (
	"errors"
	"reflect"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/config"
)

// TestSectionInterfaceOverride proves a port Section is a config.InterfaceProvider
// so it participates in default-interface inheritance: a set Iface overrides the
// shared default, an empty Iface falls through to it.
func TestSectionInterfaceOverride(t *testing.T) {
	m := config.NewModel()
	m.SetInterface(config.InterfaceSection{Name: "br0", Kind: config.IfaceKindBridge, Default: true})

	m.Set(&Section{SKey: "EtherTalk", Iface: ""})
	if got := m.EffectiveInterface("EtherTalk").Name; got != "br0" {
		t.Fatalf("empty iface should inherit the default interface, got %q want br0", got)
	}
	m.Set(&Section{SKey: "EtherTalk", Iface: "eth9"})
	if got := m.EffectiveInterface("EtherTalk").Name; got != "eth9" {
		t.Fatalf("set iface should override the default, got %q want eth9", got)
	}
}

func TestParseMAC(t *testing.T) {
	want := [6]byte{0x00, 0x11, 0x22, 0xAA, 0xBB, 0xCC}
	for _, in := range []string{
		"00:11:22:aa:bb:cc",
		"00:11:22:AA:BB:CC",
		"00-11-22-aa-bb-cc",
		"001122AABBCC", // bare hex, now accepted (matches net.ParseMAC)
	} {
		got, err := ParseMAC(in)
		if err != nil {
			t.Fatalf("ParseMAC(%q) = %v, want nil", in, err)
		}
		if got != want {
			t.Fatalf("ParseMAC(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestParseMAC_Rejects(t *testing.T) {
	for _, in := range []string{
		"",                     // empty
		"00:11:22:33:44",       // too few octets
		"00:11:22:33:44:55:66", // too many octets
		"00:11:22:33:44:gg",    // non-hex
		"001122:33:44:55",      // three-nibble octet
		"00::11:22:33:44",      // empty octet
		"00:11:22:33:44:55:",   // trailing separator
		"0:11:22:aa:bb:cc",     // single-nibble octet — net.ParseMAC rejects this;
		// the old hand-rolled parser used to accept it, but ParseMAC now delegates
		// to core/csnet.ParseMAC (net.ParseMAC) to match every other MAC parser in
		// the codebase, so this is now intentionally a rejection.
	} {
		if _, err := ParseMAC(in); !errors.Is(err, ErrBadMAC) {
			t.Fatalf("ParseMAC(%q) err = %v, want ErrBadMAC", in, err)
		}
	}
}

func TestSectionValidate(t *testing.T) {
	// A disabled placeholder with no extra fields validates clean.
	if err := (&Section{SKey: "EtherTalk"}).Validate(); err != nil {
		t.Fatalf("empty section Validate = %v, want nil", err)
	}
	// A good MAC + a well-ordered seed range validates clean.
	ok := &Section{SKey: "EtherTalk", IsEnabled: true, MAC: "00:11:22:aa:bb:cc", SeedNetwork: 10, SeedNetworkEnd: 20}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid section Validate = %v, want nil", err)
	}
	// A malformed MAC is rejected.
	if err := (&Section{MAC: "nope"}).Validate(); !errors.Is(err, ErrBadMAC) {
		t.Fatalf("bad MAC Validate = %v, want ErrBadMAC", err)
	}
	// An inverted seed range is rejected.
	if err := (&Section{SeedNetwork: 20, SeedNetworkEnd: 10}).Validate(); !errors.Is(err, ErrSeedRange) {
		t.Fatalf("inverted seed range Validate = %v, want ErrSeedRange", err)
	}
	// A single-number seed (end == 0) is accepted (open end).
	if err := (&Section{SeedNetwork: 42}).Validate(); err != nil {
		t.Fatalf("single seed Validate = %v, want nil", err)
	}
}

func TestSectionCloneCopiesNewFields(t *testing.T) {
	orig := &Section{SKey: "EtherTalk", Iface: "eth0", IsEnabled: true, MAC: "00:11:22:aa:bb:cc", SeedNetwork: 10, SeedNetworkEnd: 20, SeedZone: "Eng", IPXFrameTypes: []string{"802.3", "802.2"}}
	cp, ok := orig.Clone().(*Section)
	if !ok {
		t.Fatal("Clone did not return *Section")
	}
	if !reflect.DeepEqual(cp, orig) {
		t.Fatalf("Clone = %+v, want %+v", *cp, *orig)
	}
	// Mutating the clone must not touch the original.
	cp.MAC = "ff:ff:ff:ff:ff:ff"
	if orig.MAC == cp.MAC {
		t.Fatal("Clone shares MAC field with original")
	}
	// The frame-type slice must be deep-copied, not shared.
	cp.IPXFrameTypes[0] = "ethernet_ii"
	if orig.IPXFrameTypes[0] == cp.IPXFrameTypes[0] {
		t.Fatal("Clone shares IPXFrameTypes slice with original")
	}
}
