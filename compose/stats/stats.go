// Package stats is the telemetry-bus stats subscriber: it consumes StatSample events and
// computes per-counter rates from successive deltas (§5). It replaces the old metrics hub —
// rates are derived here, not pushed by components, so a component only emits monotonic
// counters + point-in-time gauges and never has to know the sampling interval.
//
// Ring: COMPOSE.
package stats

import (
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
)

// Snapshot is one component's latest derived view: the most recent counter/gauge values plus
// the per-counter rate (units/second) computed against the previous sample.
type Snapshot struct {
	Component string
	Counters  map[string]uint64  // latest absolute counter values
	Gauges    map[string]float64 // latest gauge values
	Rates     map[string]float64 // per-counter rate since the previous sample (units/sec)
	At        time.Time          // when this snapshot's sample was observed
}

// Collector subscribes to the telemetry bus "stats" topic and maintains a rolling Snapshot per
// component. It is safe for concurrent Snapshot reads while the consume loop runs.
type Collector struct {
	now    func() time.Time // clock seam (overridable in tests)
	cancel func()

	mu     sync.RWMutex
	prev   map[string]bus.StatSample // last raw sample per component (for delta)
	prevAt map[string]time.Time      // observation time of the last sample
	cur    map[string]Snapshot       // latest derived snapshot per component
}

// New builds a Collector bound to telemetry. Call Start to begin consuming; Stop to detach.
func New(telemetry bus.Bus) *Collector {
	return newWithClock(telemetry, time.Now)
}

func newWithClock(telemetry bus.Bus, now func() time.Time) *Collector {
	c := &Collector{
		now:    now,
		prev:   make(map[string]bus.StatSample),
		prevAt: make(map[string]time.Time),
		cur:    make(map[string]Snapshot),
	}
	ch, cancel := telemetry.Subscribe(bus.TopicStats)
	c.cancel = cancel
	go c.consume(ch)
	return c
}

// consume drains the subscription channel until it closes (on unsubscribe).
func (c *Collector) consume(ch <-chan bus.Event) {
	for ev := range ch {
		s, ok := ev.(bus.StatSample)
		if !ok {
			continue
		}
		c.observe(s, c.now())
	}
}

// observe folds one raw sample into the derived snapshot, computing rates against the prior
// sample for the same component (the consume loop calls it with the bus clock; tests call it
// directly with a scripted clock).
func (c *Collector) observe(s bus.StatSample, at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	rates := make(map[string]float64, len(s.Stats.Counters))
	if prev, ok := c.prev[s.Component]; ok {
		dt := at.Sub(c.prevAt[s.Component]).Seconds()
		if dt > 0 {
			for k, v := range s.Stats.Counters {
				pv := prev.Stats.Counters[k]
				if v >= pv { // monotonic; ignore counter resets
					rates[k] = float64(v-pv) / dt
				}
			}
		}
	}

	c.prev[s.Component] = cloneSample(s)
	c.prevAt[s.Component] = at
	c.cur[s.Component] = Snapshot{
		Component: s.Component,
		Counters:  cloneU64(s.Stats.Counters),
		Gauges:    cloneF64(s.Stats.Gauges),
		Rates:     rates,
		At:        at,
	}
}

// Snapshot returns the latest derived snapshot for a component.
func (c *Collector) Snapshot(component string) (Snapshot, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s, ok := c.cur[component]
	return s, ok
}

// Snapshots returns the latest snapshot for every observed component.
func (c *Collector) Snapshots() []Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Snapshot, 0, len(c.cur))
	for _, s := range c.cur {
		out = append(out, s)
	}
	return out
}

// Stop unsubscribes from the bus, ending the consume goroutine.
func (c *Collector) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
}

func cloneSample(s bus.StatSample) bus.StatSample {
	s.Stats.Counters = cloneU64(s.Stats.Counters)
	s.Stats.Gauges = cloneF64(s.Stats.Gauges)
	return s
}

func cloneU64(m map[string]uint64) map[string]uint64 {
	if m == nil {
		return nil
	}
	out := make(map[string]uint64, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func cloneF64(m map[string]float64) map[string]float64 {
	if m == nil {
		return nil
	}
	out := make(map[string]float64, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
