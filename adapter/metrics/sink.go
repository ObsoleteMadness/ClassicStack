//go:build perfcounters || all

package metrics

import (
	"expvar"
	"fmt"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
)

// rootVar is the single expvar.Map all component stats hang under
// ("classicstack"). Created once; expvar.Publish panics on a duplicate name, so a
// sync.Once guards it (a process builds at most one sink, but defend anyway).
var (
	rootOnce sync.Once
	root     *expvar.Map
)

func rootMap() *expvar.Map {
	rootOnce.Do(func() { root = expvar.NewMap("classicstack") })
	return root
}

// Sink subscribes to the telemetry bus stats topic and mirrors each component's
// counters/gauges into expvar under classicstack.<component>.<key>. Start to begin,
// Stop to detach.
type Sink struct {
	telemetry bus.Bus
	cancel    func()

	mu   sync.Mutex
	vars map[string]*expvar.Int // "comp/key" -> counter var
	flo  map[string]*expvar.Float
}

// New builds a Sink bound to the telemetry bus.
func New(telemetry bus.Bus) *Sink {
	return &Sink{
		telemetry: telemetry,
		vars:      make(map[string]*expvar.Int),
		flo:       make(map[string]*expvar.Float),
	}
}

// Start subscribes to the stats topic and consumes samples until Stop. A nil bus makes
// Start a no-op (nothing to mirror).
func (s *Sink) Start() {
	if s.telemetry == nil {
		return
	}
	ch, cancel := s.telemetry.Subscribe(bus.TopicStats)
	s.cancel = cancel
	go s.consume(ch)
}

// Stop unsubscribes, ending the consume goroutine.
func (s *Sink) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *Sink) consume(ch <-chan bus.Event) {
	for ev := range ch {
		ss, ok := ev.(bus.StatSample)
		if !ok {
			continue
		}
		s.apply(ss)
	}
}

// apply mirrors one sample into expvar, creating vars on first sight of a key.
func (s *Sink) apply(ss bus.StatSample) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range ss.Stats.Counters {
		s.counter(ss.Component, k).Set(int64(v))
	}
	for k, v := range ss.Stats.Gauges {
		s.gauge(ss.Component, k).Set(v)
	}
}

func (s *Sink) counter(comp, key string) *expvar.Int {
	id := comp + "/" + key
	if v, ok := s.vars[id]; ok {
		return v
	}
	v := new(expvar.Int)
	rootMap().Set(fmt.Sprintf("%s.%s", comp, key), v)
	s.vars[id] = v
	return v
}

func (s *Sink) gauge(comp, key string) *expvar.Float {
	id := comp + "/" + key
	if v, ok := s.flo[id]; ok {
		return v
	}
	v := new(expvar.Float)
	rootMap().Set(fmt.Sprintf("%s.%s", comp, key), v)
	s.flo[id] = v
	return v
}
