package supervisor

import (
	"context"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
)

// statfulComp is a Statful (and optionally StatsEmitter) component for the flush tests.
type statfulComp struct {
	name  string
	stats component.Stats
	sink  func(component.Stats) // set by SetStatsSink when emitter=true
	emit  bool
}

func (c *statfulComp) Name() string                { return c.name }
func (c *statfulComp) Start(context.Context) error { return nil }
func (c *statfulComp) Stop(context.Context) error  { return nil }
func (c *statfulComp) Stats() component.Stats      { return c.stats }
func (c *statfulComp) SetStatsSink(f func(component.Stats)) {
	if c.emit {
		c.sink = f
	}
}

// TestStatsFlushPublishesStatful asserts the periodic flush publishes a StatSample for
// each running Statful component on the telemetry bus.
func TestStatsFlushPublishesStatful(t *testing.T) {
	telemetry := bus.New(16)
	ch, cancel := telemetry.Subscribe(bus.TopicStats)
	defer cancel()

	s := New(config.NewModel(), telemetry)
	c := &statfulComp{name: "MacIP", stats: component.Stats{
		Counters: map[string]uint64{"assigns": 3},
		Gauges:   map[string]float64{"active_leases": 2},
	}}
	s.Add(c, nil)
	if err := s.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: %v", err)
	}

	s.StartStatsFlush(20 * time.Millisecond)
	defer s.StopStatsFlush()

	select {
	case ev := <-ch:
		ss, ok := ev.(bus.StatSample)
		if !ok {
			t.Fatalf("event %T, want bus.StatSample", ev)
		}
		if ss.Component != "MacIP" {
			t.Fatalf("Component = %q, want MacIP", ss.Component)
		}
		if ss.Stats.Counters["assigns"] != 3 || ss.Stats.Gauges["active_leases"] != 2 {
			t.Fatalf("stats not carried through: %+v", ss.Stats)
		}
	case <-time.After(time.Second):
		t.Fatal("no StatSample published within 1s")
	}
}

// TestStatsEmitterPushesOnDemand asserts a StatsEmitter component is handed a sink that
// publishes immediately when the component calls it — without waiting for the tick.
func TestStatsEmitterPushesOnDemand(t *testing.T) {
	telemetry := bus.New(16)
	ch, cancel := telemetry.Subscribe(bus.TopicStats)
	defer cancel()

	s := New(config.NewModel(), telemetry)
	c := &statfulComp{name: "AFP", emit: true, stats: component.Stats{Counters: map[string]uint64{}}}
	s.Add(c, nil)
	if err := s.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: %v", err)
	}

	// A long interval so any published sample must come from the push, not the tick.
	s.StartStatsFlush(time.Hour)
	defer s.StopStatsFlush()

	if c.sink == nil {
		t.Fatal("StatsEmitter was not handed a sink")
	}
	c.sink(component.Stats{Gauges: map[string]float64{"open_sessions": 5}})

	select {
	case ev := <-ch:
		ss := ev.(bus.StatSample)
		if ss.Component != "AFP" || ss.Stats.Gauges["open_sessions"] != 5 {
			t.Fatalf("pushed sample wrong: %+v", ss)
		}
	case <-time.After(time.Second):
		t.Fatal("push sink did not publish within 1s")
	}

	// After Stop the sink is detached.
	s.StopStatsFlush()
	if c.sink != nil {
		t.Fatal("sink not cleared on StopStatsFlush")
	}
}
