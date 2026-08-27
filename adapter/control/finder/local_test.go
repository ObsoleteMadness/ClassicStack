package finder

import (
	"context"
	"errors"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
	etherport "github.com/ObsoleteMadness/ClassicStack/core/port/etherdfs"
	etherdfssvc "github.com/ObsoleteMadness/ClassicStack/core/service/etherdfs"
)

type fakeComponent struct{ name string }

func (f fakeComponent) Name() string                { return f.name }
func (f fakeComponent) Start(context.Context) error { return nil }
func (f fakeComponent) Stop(context.Context) error  { return nil }

type fakeEnableableComponent struct {
	fakeComponent
	enabled bool
}

func (f fakeEnableableComponent) Enabled() bool { return f.enabled }

func TestComponentEnabledDefaultsTrueWithoutCapability(t *testing.T) {
	if !componentEnabled(fakeComponent{name: "x"}) {
		t.Fatal("a component with no Enableable capability should default to visible, matching Supervisor.Status()")
	}
}

func TestComponentEnabledReflectsEnableableCapability(t *testing.T) {
	if componentEnabled(fakeEnableableComponent{fakeComponent{"x"}, false}) {
		t.Fatal("Enabled()=false should report not enabled")
	}
	if !componentEnabled(fakeEnableableComponent{fakeComponent{"x"}, true}) {
		t.Fatal("Enabled()=true should report enabled")
	}
}

// fakeSource is a componentSource exposing exactly one named component, standing
// in for *runtime.Runtime.
type fakeSource struct {
	name string
	comp component.Component
}

func (f fakeSource) Component(name string) component.Component {
	if name == f.name {
		return f.comp
	}
	return nil
}

func (f fakeSource) Built() []string { return []string{f.name} }

// buildEtherDFS constructs a real *etherdfs.Service over a real (never-started)
// port so componentEnabled exercises the actual embedding chain
// (etherdfs.Service -> etherdfs.Port -> frameport.Port.Enabled(), which reads the
// port.Section.IsEnabled it was built with) rather than a fake stand-in — this is
// the one case (§ EtherDFS has no Enabled() of its own) where the fix depends on
// Go method promotion actually reaching through two layers of embedding.
func buildEtherDFS(t *testing.T, enabled bool) *etherdfssvc.Service {
	t.Helper()
	p, err := etherport.NewInstanceFromOpener(&port.Section{IsEnabled: enabled}, nil, [6]byte{}, nil)
	if err != nil {
		t.Fatalf("NewInstanceFromOpener: %v", err)
	}
	svc := etherdfssvc.New(p, nil)
	if svc == nil {
		t.Fatal("etherdfs.New returned nil")
	}
	return svc
}

func TestLocalVolumesOmitsDisabledEtherDFS(t *testing.T) {
	svc := buildEtherDFS(t, false)
	f := New(fakeSource{name: etherdfssvc.Name, comp: svc}, nil)
	if got := f.LocalVolumes(); len(got) != 0 {
		t.Fatalf("LocalVolumes with EtherDFS disabled = %+v, want empty", got)
	}
}

func TestResolveLocalFSRejectsDisabledEtherDFS(t *testing.T) {
	svc := buildEtherDFS(t, false)
	f := New(fakeSource{name: etherdfssvc.Name, comp: svc}, nil)
	_, err := f.resolveLocalFS(KindEtherDFS, "anything")
	if !errors.Is(err, ErrLocalServiceDisabled) {
		t.Fatalf("err = %v, want ErrLocalServiceDisabled", err)
	}
}

func TestResolveLocalFSEnabledEtherDFSPassesTheGate(t *testing.T) {
	svc := buildEtherDFS(t, true)
	f := New(fakeSource{name: etherdfssvc.Name, comp: svc}, nil)
	_, err := f.resolveLocalFS(KindEtherDFS, "NoSuchDrive")
	// Past the enabled gate, it fails on "drive not found" (no drives configured
	// here), not on being disabled — proves the gate does not misfire when enabled.
	if errors.Is(err, ErrLocalServiceDisabled) {
		t.Fatal("enabled EtherDFS incorrectly rejected as disabled")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound (no drives configured)", err)
	}
}
