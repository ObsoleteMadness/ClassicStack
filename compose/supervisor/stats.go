package supervisor

import (
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/component"
)

// DefaultStatsInterval is the periodic stats-flush cadence (§5). It is the heartbeat
// that keeps gauges (active leases, open sessions, route counts) and idle counters
// fresh on the dashboard even with no traffic; push-on-change (StatsEmitter) layers
// low-latency updates on top of it. 2s matches the legacy supervisor refresh tick.
const DefaultStatsInterval = 2 * time.Second

// StartStatsFlush wires the stats producers and begins the periodic flush. It does two
// things, mirroring the two halves of the §5 stats contract:
//
//   - PUSH: every component that implements component.StatsEmitter is handed a sink so
//     it can publish a StatSample the moment something changes (a session opens, a
//     lease is assigned), without waiting for the next tick.
//   - POLL: a ticker walks every component.Statful node each interval and publishes its
//     current snapshot. This covers components that only implement Statful, and keeps
//     gauges fresh while idle. The compose/stats Collector derives rates from the
//     successive samples, so a component never reports a rate or knows the interval.
//
// Idempotent: a second call while running is a no-op. A nil telemetry bus disables the
// flush (publish is a no-op). interval<=0 uses DefaultStatsInterval.
func (s *Supervisor) StartStatsFlush(interval time.Duration) {
	if s.telemetry == nil {
		return
	}
	if interval <= 0 {
		interval = DefaultStatsInterval
	}

	s.statsMu.Lock()
	if s.statsStop != nil {
		s.statsMu.Unlock()
		return // already running
	}
	stop := make(chan struct{})
	s.statsStop = stop
	s.statsMu.Unlock()

	// Wire push sinks for any StatsEmitter component. The sink publishes that
	// component's snapshot immediately; capturing name binds each closure to its owner.
	s.mu.Lock()
	for name, n := range s.nodes {
		if em, ok := n.c.(component.StatsEmitter); ok {
			name := name
			em.SetStatsSink(func(st component.Stats) { s.publishStats(name, st) })
		}
	}
	s.mu.Unlock()

	s.statsWG.Add(1)
	go s.runStatsFlush(stop, interval)
}

// StopStatsFlush halts the periodic flush and unwires the push sinks. Safe to call when
// not running. It does NOT publish a final sample.
func (s *Supervisor) StopStatsFlush() {
	s.statsMu.Lock()
	stop := s.statsStop
	s.statsStop = nil
	s.statsMu.Unlock()
	if stop == nil {
		return
	}
	close(stop)
	s.statsWG.Wait()

	// Detach push sinks so a stopped supervisor publishes nothing further.
	s.mu.Lock()
	for _, n := range s.nodes {
		if em, ok := n.c.(component.StatsEmitter); ok {
			em.SetStatsSink(nil)
		}
	}
	s.mu.Unlock()
}

// runStatsFlush ticks the poll loop until stopped, publishing one sample per Statful
// node each interval.
func (s *Supervisor) runStatsFlush(stop chan struct{}, interval time.Duration) {
	defer s.statsWG.Done()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			s.flushStats()
		}
	}
}

// flushStats publishes a StatSample for every Statful component. It snapshots the node
// set under the lock, then publishes outside it so a slow subscriber never stalls the
// supervisor (Publish is itself non-blocking, but Stats() should not run under s.mu).
func (s *Supervisor) flushStats() {
	type sample struct {
		name string
		c    component.Statful
	}
	s.mu.Lock()
	samples := make([]sample, 0, len(s.nodes))
	for name, n := range s.nodes {
		if sf, ok := n.c.(component.Statful); ok && n.running {
			samples = append(samples, sample{name: name, c: sf})
		}
	}
	s.mu.Unlock()

	for _, sm := range samples {
		s.publishStats(sm.name, sm.c.Stats())
	}
}

// publishStats wraps one component's snapshot in a bus.StatSample and publishes it on
// the telemetry bus. The shared body behind both the poll flush and the push sink.
func (s *Supervisor) publishStats(name string, st component.Stats) {
	if s.telemetry == nil {
		return
	}
	s.telemetry.Publish(bus.StatSample{Component: name, Stats: st})
}
