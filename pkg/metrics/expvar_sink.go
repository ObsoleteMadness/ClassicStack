package metrics

import (
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/pkg/telemetry"
)

// ExpvarSink forwards samples to the telemetry package's expvar-backed
// counters and gauges so they remain visible at /debug/vars and to any
// telemetry backend. Counters are set to the latest absolute value (the
// sample carries the running total, not a delta).
type ExpvarSink struct {
	mu       sync.Mutex
	counters map[string]telemetry.Gauge // counters tracked as set-able gauges
	gauges   map[string]telemetry.Gauge
}

// NewExpvarSink returns a ready sink.
func NewExpvarSink() *ExpvarSink {
	return &ExpvarSink{
		counters: make(map[string]telemetry.Gauge),
		gauges:   make(map[string]telemetry.Gauge),
	}
}

// Write publishes the sample's current value under its name. Both counter
// and gauge samples carry an absolute value, so each maps onto a
// set-able expvar gauge; the Prometheus-style _total naming on counter
// names preserves the semantic distinction for scrapers.
func (s *ExpvarSink) Write(sample Sample) {
	s.mu.Lock()
	defer s.mu.Unlock()
	table := s.gauges
	if sample.Kind == KindCounter {
		table = s.counters
	}
	g, ok := table[sample.Name]
	if !ok {
		g = telemetry.NewGauge(sample.Name)
		table[sample.Name] = g
	}
	g.Set(sample.Value)
}
