// Package bus is the bus log Sink adapter: a core/log.Sink that republishes each
// log Record as a bus.LogRecord on the telemetry bus "log" topic (§6c). It is the
// source the control plane's Subscribe("log") relays to the web-UI / ubus log
// viewer.
//
// It lives in the ADAPTER ring on purpose: the design keeps the logger free of any
// bus dependency ("the bus sink is just one sink — the logger does not depend on
// the bus", §6c), so a CLI tool or an embedded build can log to a file/UART with no
// bus, SSE, or control plane linked. Only this adapter bridges the two; core/log
// and core/bus never import each other.
//
// Ring: ADAPTER.
package bus

import (
	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
)

// Sink is a core/log.Sink that publishes records onto a bus.Bus. Its threshold is
// a *log.LevelVar so a UI can retune the log-viewer verbosity live (§6b) without
// rebuilding loggers — exactly like the stderr/ring sinks.
type Sink struct {
	b   bus.Bus
	min *log.LevelVar
}

// compile-time assertion: *Sink satisfies log.Sink.
var _ log.Sink = (*Sink)(nil)

// New builds a bus log sink publishing to b. min is the threshold (a *LevelVar so
// it retunes live); a nil min emits every level. A nil bus makes every Write a
// no-op, so wiring code can pass an absent telemetry bus without a guard.
func New(b bus.Bus, min *log.LevelVar) *Sink {
	return &Sink{b: b, min: min}
}

// Min reports the sink's current threshold (Debug when unset), so a logger's
// Enabled() guard folds this sink into its hot-path check.
func (s *Sink) Min() log.Level {
	if s.min == nil {
		return log.Debug
	}
	return s.min.Level()
}

// Write republishes one record as a bus.LogRecord on the "log" topic. The bus
// Publish is itself non-blocking (a slow subscriber drops, §5), so this never
// stalls the logging path. Fields are translated into the bus's own typed Field
// (no interface{}/reflection, §6).
func (s *Sink) Write(rec log.Record) {
	if s.b == nil {
		return
	}
	s.b.Publish(bus.LogRecord{
		Component: rec.Scope,
		Level:     uint8(rec.Level),
		Msg:       rec.Msg,
		Fields:    translateFields(rec.Fields),
		Time:      rec.Time,
	})
}

// Close releases the sink. The bus is owned by the caller, so there is nothing to
// release here; the method exists only to satisfy log.Sink.
func (s *Sink) Close() error { return nil }

// translateFields maps core/log.Field values to the bus.Field mirror. The record's
// Fields slice is reused by the logger after Write returns (it points at a scratch
// buffer), so this allocates a fresh slice the published event can own.
func translateFields(in []log.Field) []bus.Field {
	if len(in) == 0 {
		return nil
	}
	out := make([]bus.Field, len(in))
	for i, f := range in {
		bf := bus.Field{Key: f.Key}
		switch f.Kind {
		case log.KindStr:
			bf.Kind = bus.KindStr
			bf.Str = f.String()
		case log.KindInt:
			bf.Kind = bus.KindInt
			bf.Int = f.Int64()
		case log.KindBool:
			bf.Kind = bus.KindBool
			bf.Bool = f.BoolValue()
		}
		out[i] = bf
	}
	return out
}
