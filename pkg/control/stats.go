package control

import (
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/pkg/metrics"
)

// Frame is a per-second snapshot of streamed statistics pushed to UI
// subscribers. Rates holds derived per-second deltas for counter metrics;
// Totals holds the latest cumulative value for those same counters; Gauges
// holds the latest absolute value for gauge metrics.
type Frame struct {
	UnixMilli int64            `json:"t"`
	Rates     map[string]int64 `json:"rates,omitempty"`
	Totals    map[string]int64 `json:"totals,omitempty"`
	Gauges    map[string]int64 `json:"gauges,omitempty"`
}

// statsBroadcaster is a metrics.Sink that accumulates samples and, once per
// tick, computes counter rates and fans a Frame out to all subscribers. It
// is the server-side half of the SSE stream; the web UI's SSE handler is a
// subscriber.
type statsBroadcaster struct {
	mu        sync.Mutex
	counters  map[string]int64 // latest absolute counter values
	prev      map[string]int64 // previous tick's counter values
	gauges    map[string]int64
	subs      map[int]chan Frame
	nextSubID int
	stop      chan struct{}
}

func newStatsBroadcaster() *statsBroadcaster {
	return &statsBroadcaster{
		counters: make(map[string]int64),
		prev:     make(map[string]int64),
		gauges:   make(map[string]int64),
		subs:     make(map[int]chan Frame),
		stop:     make(chan struct{}),
	}
}

// Write records the latest value for a metric (metrics.Sink).
func (b *statsBroadcaster) Write(s metrics.Sample) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if s.Kind == metrics.KindGauge {
		b.gauges[s.Name] = s.Value
		return
	}
	b.counters[s.Name] = s.Value
}

// run ticks every second, builds a Frame, and broadcasts it. It returns
// when stop is closed.
func (b *statsBroadcaster) run() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-b.stop:
			return
		case <-ticker.C:
			b.broadcast()
		}
	}
}

func (b *statsBroadcaster) broadcast() {
	b.mu.Lock()
	frame := Frame{
		UnixMilli: time.Now().UnixMilli(),
		Rates:     make(map[string]int64, len(b.counters)),
		Totals:    make(map[string]int64, len(b.counters)),
		Gauges:    make(map[string]int64, len(b.gauges)),
	}
	for name, v := range b.counters {
		frame.Rates[name] = v - b.prev[name]
		frame.Totals[name] = v
		b.prev[name] = v
	}
	for name, v := range b.gauges {
		frame.Gauges[name] = v
	}
	subs := make([]chan Frame, 0, len(b.subs))
	for _, ch := range b.subs {
		subs = append(subs, ch)
	}
	b.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- frame:
		default: // drop for slow subscribers; next tick carries fresh data
		}
	}
}

func (b *statsBroadcaster) subscribe() (<-chan Frame, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := b.nextSubID
	b.nextSubID++
	ch := make(chan Frame, 4)
	b.subs[id] = ch
	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if c, ok := b.subs[id]; ok {
			delete(b.subs, id)
			close(c)
		}
	}
}

// Subscribe registers a stats subscriber and returns the receive channel
// plus a cancel func that unsubscribes and closes the channel. The first
// call lazily starts the broadcaster and attaches it to the metrics hub.
func (p *Plane) Subscribe() (<-chan Frame, func()) {
	p.mu.Lock()
	if p.stats == nil {
		p.stats = newStatsBroadcaster()
		p.hub.AddSink(p.stats)
		go p.stats.run()
	}
	b := p.stats
	p.mu.Unlock()
	return b.subscribe()
}
