package registry

import (
	"context"
	"reflect"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
)

// TestSectionMACForInheritsInterfaceHWAddress pins the fix for the zero-source-MAC
// regression: a NetBEUI/IPX/EtherDFS port that pins no mac of its own must inherit the
// bound interface's hw_address, so its NBF/IPX frames carry a real Ethernet source
// instead of 00:00:00:00:00:00 (which broke NetBIOS registration on the wire).
func TestSectionMACForInheritsInterfaceHWAddress(t *testing.T) {
	bridge := config.InterfaceSection{Name: "br-lan", HWAddress: "DE:AD:BE:EF:CA:FE"}
	want := [6]byte{0xDE, 0xAD, 0xBE, 0xEF, 0xCA, 0xFE}

	// Empty section mac → inherit the interface hw_address.
	if got := sectionMACFor(&port.Section{}, bridge); got != want {
		t.Fatalf("empty section mac: got %v, want interface hw_address %v", got, want)
	}

	// A pinned section mac wins over the interface hw_address.
	own := [6]byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55}
	if got := sectionMACFor(&port.Section{MAC: "00:11:22:33:44:55"}, bridge); got != own {
		t.Fatalf("pinned section mac: got %v, want %v", got, own)
	}

	// Both empty → the zero MAC (the caller decides whether that is fatal).
	if got := sectionMACFor(&port.Section{}, config.InterfaceSection{}); got != ([6]byte{}) {
		t.Fatalf("no mac anywhere: got %v, want zero MAC", got)
	}

	// A malformed interface hw_address is ignored (falls through to zero), not panicked on.
	if got := sectionMACFor(&port.Section{}, config.InterfaceSection{HWAddress: "not-a-mac"}); got != ([6]byte{}) {
		t.Fatalf("malformed hw_address: got %v, want zero MAC", got)
	}
}

// stubComponent is a do-nothing component used to prove registration/build.
type stubComponent struct{ name string }

func (c *stubComponent) Name() string                { return c.name }
func (c *stubComponent) Start(context.Context) error { return nil }
func (c *stubComponent) Stop(context.Context) error  { return nil }

func TestBuildUnregisteredReturnsNotFound(t *testing.T) {
	c, ok, err := Build("definitely-not-registered", &BuildContext{Model: config.NewModel()})
	if err != nil {
		t.Fatalf("unexpected error for unregistered name: %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false for unregistered name, got ok=true")
	}
	if c != nil {
		t.Fatalf("expected nil component for unregistered name, got %v", c)
	}
}

func TestRegisterBuildNames(t *testing.T) {
	Register("stub-a", func(*BuildContext) (component.Component, error) {
		return &stubComponent{name: "stub-a"}, nil
	})
	// A disabled section: factory returns (nil, nil) but ok must still be true.
	Register("stub-disabled", func(*BuildContext) (component.Component, error) {
		return nil, nil
	})

	c, ok, err := Build("stub-a", &BuildContext{Model: config.NewModel()})
	if err != nil || !ok {
		t.Fatalf("Build(stub-a) = (_, %v, %v), want (_, true, nil)", ok, err)
	}
	if c == nil || c.Name() != "stub-a" {
		t.Fatalf("Build(stub-a) returned %v, want named stub-a", c)
	}

	dc, ok, err := Build("stub-disabled", &BuildContext{Model: config.NewModel()})
	if err != nil || !ok {
		t.Fatalf("Build(stub-disabled) = (_, %v, %v), want (_, true, nil)", ok, err)
	}
	if dc != nil {
		t.Fatalf("disabled factory should yield nil component, got %v", dc)
	}

	got := Names()
	want := []string{"stub-a", "stub-disabled"}
	// Names() may contain build-tag-gated entries too; assert ours are present and sorted.
	if !containsAllSorted(got, want) {
		t.Fatalf("Names() = %v, want to contain %v in sorted order", got, want)
	}
}

// TestBuildTagGatedRegistration proves the build-tag mechanism: stub_tagged.go registers
// "stub-tagged" only under the `registrytag` build tag. Without the tag it must be absent.
func TestBuildTagGatedRegistration(t *testing.T) {
	_, ok, _ := Build("stub-tagged", &BuildContext{Model: config.NewModel()})
	if ok != taggedRegistered {
		t.Fatalf("Build(stub-tagged) ok=%v, want %v (taggedRegistered)", ok, taggedRegistered)
	}
}

func containsAllSorted(got, want []string) bool {
	set := map[string]bool{}
	for _, g := range got {
		set[g] = true
	}
	for _, w := range want {
		if !set[w] {
			return false
		}
	}
	// verify sorted
	return reflect.DeepEqual(got, sortedCopy(got))
}

func sortedCopy(s []string) []string {
	out := append([]string(nil), s...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}
