// Package metrics is ClassicStack's streaming-stats layer. Services push
// Samples into a Hub, which fans them out to registered Sinks. Two sinks
// ship today: an expvar/telemetry sink (so counters stay visible at
// /debug/vars and to the existing telemetry backend) and — when the web UI
// is built — an SSE sink that computes per-second rates and broadcasts
// them to dashboard clients.
//
// The hub is untagged so the core can always publish samples; only the SSE
// consumer is gated behind the webui build tag.
package metrics

import "sync"

// SampleKind distinguishes a monotonic counter from an instantaneous gauge.
type SampleKind int

const (
	// KindCounter is a monotonically increasing total (e.g. bytes
	// transferred). Sinks may derive a per-second rate from successive
	// values.
	KindCounter SampleKind = iota
	// KindGauge is a point-in-time value (e.g. active sessions).
	KindGauge
)

// Sample is a single metric observation pushed by a service.
type Sample struct {
	Name  string     `json:"name"`
	Value int64      `json:"value"`
	Kind  SampleKind `json:"kind"`
}

// Sink consumes Samples. Implementations must be safe for concurrent use;
// the hub serialises calls per Push but multiple Push callers may run
// concurrently, so the hub holds a lock around fan-out.
type Sink interface {
	Write(Sample)
}

// Hub fans Samples out to all registered Sinks.
type Hub struct {
	mu    sync.RWMutex
	sinks []Sink
}

// NewHub returns an empty hub.
func NewHub() *Hub { return &Hub{} }

// Default is the process-global hub services push into.
var Default = NewHub()

// AddSink registers a sink to receive future samples.
func (h *Hub) AddSink(s Sink) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sinks = append(h.sinks, s)
}

// Push delivers a sample to every sink.
func (h *Hub) Push(s Sample) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, sink := range h.sinks {
		sink.Write(s)
	}
}

// Push is a convenience wrapper over the default hub.
func Push(s Sample) { Default.Push(s) }
