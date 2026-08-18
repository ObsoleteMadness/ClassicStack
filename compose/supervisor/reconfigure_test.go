package supervisor

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
)

// fakeSection is a config.Section that records whether Clone was ever called. The §11a
// reconfigure is ADDRESSED, not diffed, so the supervisor must never Clone/compare it.
type fakeSection struct {
	key    string
	cloned *bool
}

func (f fakeSection) Key() string { return f.key }
func (f fakeSection) Clone() config.Section {
	if f.cloned != nil {
		*f.cloned = true
	}
	return f
}
func (f fakeSection) Validate() error { return nil }

// configurableComp records lifecycle events and answers ApplyConfig per a configurable policy.
type configurableComp struct {
	name        string
	log         *orderLog
	applyErr    error // returned by ApplyConfig (nil = hot-apply; ErrNeedsRestart = restart)
	applied     int
	lastSection any
}

func (c *configurableComp) Name() string { return c.name }
func (c *configurableComp) Start(context.Context) error {
	c.log.add("start:" + c.name)
	return nil
}
func (c *configurableComp) Stop(context.Context) error {
	c.log.add("stop:" + c.name)
	return nil
}
func (c *configurableComp) ApplyConfig(section any) error {
	c.applied++
	c.lastSection = section
	c.log.add("apply:" + c.name)
	return c.applyErr
}

var _ component.Configurable = (*configurableComp)(nil)

// TestReconfigureHotApply: a Configurable that hot-applies does NOT restart; it emits a
// running->reconfigured transition instead.
func TestReconfigureHotApply(t *testing.T) {
	telemetry := bus.New(16)
	ch, cancel := telemetry.Subscribe(bus.TopicState)
	defer cancel()

	log := &orderLog{}
	s := New(config.NewModel(), telemetry)
	c := &configurableComp{name: "port", log: log, applyErr: nil}
	s.Add(c, nil)
	if err := s.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	<-ch // drain the stopped->running from StartAll
	log.seq = nil

	if err := s.Reconfigure(context.Background(), "port", fakeSection{key: "port"}); err != nil {
		t.Fatalf("Reconfigure: %v", err)
	}
	if c.applied != 1 {
		t.Fatalf("ApplyConfig called %d times, want 1", c.applied)
	}
	// No Stop/Start should have happened (hot-applied live).
	if want := []string{"apply:port"}; !reflect.DeepEqual(log.seq, want) {
		t.Fatalf("lifecycle log = %v, want %v (no restart)", log.seq, want)
	}
	ev := (<-ch).(bus.StateChanged)
	if ev.To != stateReconfigured {
		t.Fatalf("transition To = %q, want %q", ev.To, stateReconfigured)
	}
}

// TestReconfigureNeedsRestart: ErrNeedsRestart forces Stop->Start on the addressed component.
func TestReconfigureNeedsRestart(t *testing.T) {
	log := &orderLog{}
	s := New(config.NewModel(), nil)
	c := &configurableComp{name: "port", log: log, applyErr: component.ErrNeedsRestart}
	s.Add(c, nil)
	if err := s.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	log.seq = nil

	if err := s.Reconfigure(context.Background(), "port", fakeSection{key: "port"}); err != nil {
		t.Fatalf("Reconfigure: %v", err)
	}
	want := []string{"apply:port", "stop:port", "start:port"}
	if !reflect.DeepEqual(log.seq, want) {
		t.Fatalf("lifecycle log = %v, want %v", log.seq, want)
	}
}

// TestReconfigureNotifyCascade: the §11a cascade. router needs restart; its dependent afp can
// hot-apply (cascade stops there); unrelated comp is untouched.
func TestReconfigureNotifyCascade(t *testing.T) {
	log := &orderLog{}
	m := config.NewModel()
	m.Set(fakeSection{key: "router"})
	m.Set(fakeSection{key: "afp"})
	m.Set(fakeSection{key: "smb"})
	s := New(m, nil)

	router := &configurableComp{name: "router", log: log, applyErr: component.ErrNeedsRestart}
	afp := &configurableComp{name: "afp", log: log, applyErr: nil} // hot-applies
	smb := &recordingComponent{name: "smb", log: log}              // unrelated, not a dependent

	s.Add(router, nil)
	s.Add(afp, []string{"router"})
	s.Add(smb, nil)
	if err := s.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	log.seq = nil

	if err := s.Reconfigure(context.Background(), "router", fakeSection{key: "router"}); err != nil {
		t.Fatalf("Reconfigure: %v", err)
	}

	// router restarts; afp is notified and hot-applies (no restart); smb untouched.
	want := []string{"apply:router", "stop:router", "start:router", "apply:afp"}
	if !reflect.DeepEqual(log.seq, want) {
		t.Fatalf("cascade log = %v, want %v", log.seq, want)
	}
}

// TestReconfigureNoDiff: the supervisor must never Clone the section (a diff pass would).
func TestReconfigureNoDiff(t *testing.T) {
	cloned := false
	log := &orderLog{}
	s := New(config.NewModel(), nil)
	s.Add(&configurableComp{name: "port", log: log, applyErr: nil}, nil)
	if err := s.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	sec := fakeSection{key: "port", cloned: &cloned}
	if err := s.Reconfigure(context.Background(), "port", sec); err != nil {
		t.Fatalf("Reconfigure: %v", err)
	}
	if cloned {
		t.Fatalf("section was Clone()d during Reconfigure — implies a model-diff pass (§11a forbids it)")
	}
}

// TestReconfigureRebuild: a restart-driven reconfigure with a Rebuilder swaps the instance.
func TestReconfigureRebuild(t *testing.T) {
	log := &orderLog{}
	s := New(config.NewModel(), nil)

	var rebuilt sync.Once
	built := &configurableComp{name: "port", log: log, applyErr: component.ErrNeedsRestart}
	replacement := &configurableComp{name: "port", log: log, applyErr: nil}
	s.AddBuildable(built, nil, func(*config.Model) (component.Component, error) {
		var out component.Component = built
		rebuilt.Do(func() { out = replacement })
		return out, nil
	})
	if err := s.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	if err := s.Reconfigure(context.Background(), "port", fakeSection{key: "port"}); err != nil {
		t.Fatalf("Reconfigure: %v", err)
	}
	// After rebuild, a second reconfigure hits the replacement (which hot-applies).
	log.seq = nil
	if err := s.Reconfigure(context.Background(), "port", fakeSection{key: "port"}); err != nil {
		t.Fatalf("second Reconfigure: %v", err)
	}
	if want := []string{"apply:port"}; !reflect.DeepEqual(log.seq, want) {
		t.Fatalf("after rebuild log = %v, want %v (replacement hot-applies)", log.seq, want)
	}
}
