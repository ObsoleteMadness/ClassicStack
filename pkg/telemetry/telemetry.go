// Package telemetry is OmniTalk's metrics abstraction. It exposes
// Counter, Gauge, and Histogram types with a default expvar-backed
// implementation that ships as part of the stdlib and requires no
// extra dependencies. A build-tagged OpenTelemetry backend may be
// swapped in by adding //go:build otel files alongside this one.
//
// Telemetry is deliberately separate from structured logging
// (pkg/logging): counters and histograms are cheap and continuous,
// logs are discrete events. Use both.
//
// Usage:
//
//	var framesIn = telemetry.NewCounter("omnitalk_router_frames_in_total")
//	framesIn.Inc()
//	framesIn.Add(n)
//
// Metric names follow Prometheus-style lower_snake_case with a unit
// suffix (_total, _seconds, _bytes). Labels are encoded into the name
// for the expvar backend (e.g. "omnitalk_afp_commands_total_OpenFork")
// because expvar does not support label dimensions natively; the OTel
// backend splits them back out.
package telemetry

import (
	"expvar"
	"sync/atomic"
)

// Counter is a monotonically increasing integer metric.
type Counter interface {
	Inc()
	Add(delta int64)
	Value() int64
}

// Gauge is an integer metric that may go up and down.
type Gauge interface {
	Set(v int64)
	Add(delta int64)
	Value() int64
}

// Histogram records an observation distribution. The default expvar
// backend keeps a simple count + sum + min + max; richer backends
// (OTel) record full buckets.
type Histogram interface {
	Observe(v float64)
}

// NewCounter returns a Counter registered under name.
// Calling NewCounter twice with the same name returns the same instance.
func NewCounter(name string) Counter {
	if v := expvar.Get(name); v != nil {
		if c, ok := v.(*expvarCounter); ok {
			return c
		}
	}
	c := &expvarCounter{}
	expvar.Publish(name, c)
	return c
}

// NewGauge returns a Gauge registered under name.
func NewGauge(name string) Gauge {
	if v := expvar.Get(name); v != nil {
		if g, ok := v.(*expvarGauge); ok {
			return g
		}
	}
	g := &expvarGauge{}
	expvar.Publish(name, g)
	return g
}

// NewHistogram returns a Histogram registered under name.
func NewHistogram(name string) Histogram {
	if v := expvar.Get(name); v != nil {
		if h, ok := v.(*expvarHistogram); ok {
			return h
		}
	}
	h := &expvarHistogram{}
	expvar.Publish(name, h)
	return h
}

// --- expvar implementations ---

type expvarCounter struct{ n atomic.Int64 }

func (c *expvarCounter) Inc()             { c.n.Add(1) }
func (c *expvarCounter) Add(d int64)      { c.n.Add(d) }
func (c *expvarCounter) Value() int64     { return c.n.Load() }
func (c *expvarCounter) String() string   { return i64string(c.n.Load()) }

type expvarGauge struct{ n atomic.Int64 }

func (g *expvarGauge) Set(v int64)      { g.n.Store(v) }
func (g *expvarGauge) Add(d int64)      { g.n.Add(d) }
func (g *expvarGauge) Value() int64     { return g.n.Load() }
func (g *expvarGauge) String() string   { return i64string(g.n.Load()) }

type expvarHistogram struct {
	count atomic.Int64
	sumB  atomic.Uint64 // float64 bits
}

func (h *expvarHistogram) Observe(v float64) {
	h.count.Add(1)
	for {
		old := h.sumB.Load()
		sum := float64frombits(old) + v
		if h.sumB.CompareAndSwap(old, float64tobits(sum)) {
			return
		}
	}
}

func (h *expvarHistogram) String() string {
	count := h.count.Load()
	sum := float64frombits(h.sumB.Load())
	return `{"count":` + i64string(count) + `,"sum":` + f64string(sum) + `}`
}
