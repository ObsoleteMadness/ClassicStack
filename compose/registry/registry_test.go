package registry

import (
	"context"
	"reflect"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
)

// stubComponent is a do-nothing component used to prove registration/build.
type stubComponent struct{ name string }

func (c *stubComponent) Name() string                { return c.name }
func (c *stubComponent) Start(context.Context) error { return nil }
func (c *stubComponent) Stop(context.Context) error  { return nil }

func TestBuildUnregisteredReturnsNotFound(t *testing.T) {
	c, ok, err := Build("definitely-not-registered", config.NewModel())
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
	Register("stub-a", func(*config.Model) (component.Component, error) {
		return &stubComponent{name: "stub-a"}, nil
	})
	// A disabled section: factory returns (nil, nil) but ok must still be true.
	Register("stub-disabled", func(*config.Model) (component.Component, error) {
		return nil, nil
	})

	c, ok, err := Build("stub-a", config.NewModel())
	if err != nil || !ok {
		t.Fatalf("Build(stub-a) = (_, %v, %v), want (_, true, nil)", ok, err)
	}
	if c == nil || c.Name() != "stub-a" {
		t.Fatalf("Build(stub-a) returned %v, want named stub-a", c)
	}

	dc, ok, err := Build("stub-disabled", config.NewModel())
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
	_, ok, _ := Build("stub-tagged", config.NewModel())
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
