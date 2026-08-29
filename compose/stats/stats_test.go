package stats

import (
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/component"
)

// TestRateFromTwoSamples feeds two StatSamples N seconds apart and asserts the derived rate.
func TestRateFromTwoSamples(t *testing.T) {
	b := bus.New(8)
	base := time.Unix(1000, 0)
	var step int
	times := []time.Time{base, base.Add(2 * time.Second)} // 2-second interval
	c := newWithClock(b, func() time.Time {
		i := step
		if i >= len(times) {
			i = len(times) - 1
		}
		return times[i]
	})
	defer c.Stop()

	sample := func(frames uint64) bus.StatSample {
		return bus.StatSample{
			Component: "port",
			Stats:     component.Stats{Counters: map[string]uint64{"frames_rx": frames}},
		}
	}

	// First sample: establishes the baseline (no rate yet).
	step = 0
	c.observe(sample(100), times[0])
	if snap, _ := c.Snapshot("port"); len(snap.Rates) != 0 {
		t.Fatalf("first sample should have no rates, got %v", snap.Rates)
	}

	// Second sample 2s later: 200 frames means delta 100 over 2s = 50/s.
	step = 1
	c.observe(sample(200), times[1])
	snap, ok := c.Snapshot("port")
	if !ok {
		t.Fatalf("no snapshot for port")
	}
	if got := snap.Rates["frames_rx"]; got != 50 {
		t.Fatalf("rate = %v, want 50/s", got)
	}
	if got := snap.Counters["frames_rx"]; got != 200 {
		t.Fatalf("latest counter = %d, want 200", got)
	}
}

// TestCounterResetIgnored: a non-monotonic drop (restart) must not produce a negative rate.
func TestCounterResetIgnored(t *testing.T) {
	b := bus.New(8)
	base := time.Unix(2000, 0)
	c := newWithClock(b, time.Now)
	defer c.Stop()

	c.observe(bus.StatSample{Component: "p", Stats: component.Stats{Counters: map[string]uint64{"n": 500}}}, base)
	c.observe(bus.StatSample{Component: "p", Stats: component.Stats{Counters: map[string]uint64{"n": 10}}}, base.Add(time.Second))
	snap, _ := c.Snapshot("p")
	if _, present := snap.Rates["n"]; present {
		t.Fatalf("counter reset should yield no rate, got %v", snap.Rates["n"])
	}
}

// TestConsumeViaBus proves the live path: publishing through the bus reaches the collector.
func TestConsumeViaBus(t *testing.T) {
	b := bus.New(8)
	c := New(b)
	defer c.Stop()

	b.Publish(bus.StatSample{Component: "afp", Stats: component.Stats{
		Gauges: map[string]float64{"sessions": 3},
	}})

	// The consume goroutine is async; poll briefly for delivery.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if snap, ok := c.Snapshot("afp"); ok {
			if snap.Gauges["sessions"] != 3 {
				t.Fatalf("gauge = %v, want 3", snap.Gauges["sessions"])
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for sample delivery via bus")
		}
		time.Sleep(time.Millisecond)
	}
}
