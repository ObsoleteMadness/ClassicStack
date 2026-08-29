//go:build !perfcounters && !all

package metrics

import "github.com/ObsoleteMadness/ClassicStack/core/bus"

// Sink is the no-op form built when the `perfcounters` tag is absent: the default
// build carries no expvar export surface. Start/Stop do nothing, so the cmd edge can
// construct and drive a Sink unconditionally and only the tagged build mirrors stats.
type Sink struct{}

// New returns the no-op sink (the bus is ignored without the perfcounters tag).
func New(_ bus.Bus) *Sink { return &Sink{} }

// Start is a no-op without the perfcounters tag.
func (*Sink) Start() {}

// Stop is a no-op without the perfcounters tag.
func (*Sink) Stop() {}
