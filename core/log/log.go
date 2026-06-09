package log

import (
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

type Level uint8

const (
	// Trace is the most verbose level: per-request/per-element protocol & service narration
	// (e.g. "AFP FPOpenFork path=…"). Raw wire bytes are NOT logged here — they go to a pcap
	// capture (see core/link CaptureSink); Trace is for the human-readable protocol event.
	Trace Level = iota
	// Debug is verbose diagnostic logging.
	Debug
	// Info is the normal informational log level.
	Info
	// Warn reports a recoverable problem.
	Warn
	// Error reports a failed operation.
	Error
)

// LevelVar is a threshold that can be changed at runtime (e.g. the control plane setting
// "AFP=debug, everything else=info", §6b). A sink holds a *LevelVar so verbosity retunes live
// without rebuilding loggers or sinks. The zero value is Debug (emit everything).
type LevelVar struct {
	v atomic.Uint32
}

// NewLevelVar returns a LevelVar initialised to min.
func NewLevelVar(min Level) *LevelVar {
	lv := &LevelVar{}
	lv.Set(min)
	return lv
}

// Set updates the threshold; safe for concurrent use.
func (v *LevelVar) Set(min Level) { v.v.Store(uint32(min)) }

// Level reports the current threshold; safe for concurrent use.
func (v *LevelVar) Level() Level { return Level(v.v.Load()) }

// Field is a typed key/value (no interface{} boxing).
type Field struct {
	Key  string
	Kind Kind
	s    string
	i    int64
	b    bool
}

type Kind uint8

const (
	// KindStr marks a string field value.
	KindStr Kind = iota
	// KindInt marks an integer field value.
	KindInt
	// KindBool marks a boolean field value.
	KindBool
)

// Str builds a string field.
func Str(k, v string) Field {
	return Field{Key: k, Kind: KindStr, s: v}
}

// Int builds an integer field.
func Int(k string, v int64) Field {
	return Field{Key: k, Kind: KindInt, i: v}
}

// Bool builds a boolean field.
func Bool(k string, v bool) Field {
	return Field{Key: k, Kind: KindBool, b: v}
}

// String returns the string value when KindStr is set, else "".
func (f Field) String() string { return f.s }

// Int64 returns the int value when KindInt is set, else 0.
func (f Field) Int64() int64 { return f.i }

// BoolValue returns the bool value when KindBool is set, else false.
func (f Field) BoolValue() bool { return f.b }

type Logger interface {
	// With returns a child logger with additional bound fields.
	With(fields ...Field) Logger
	// Log writes one record at the supplied level.
	Log(lvl Level, msg string, fields ...Field)
	// Enabled reports whether the supplied level is enabled.
	Enabled(lvl Level) bool

	// Log0 writes a record with no call-site fields.
	Log0(lvl Level, msg string)
	// Log1 writes a record with one call-site field.
	Log1(lvl Level, msg string, f Field)
	// Log2 writes a record with two call-site fields.
	Log2(lvl Level, msg string, f1, f2 Field)
}

// Record is the finished log entry delivered to sinks.
type Record struct {
	Scope  string
	Level  Level
	Msg    string
	Fields []Field
	Time   time.Time
}

// Sink consumes finished log records. The level threshold lives at this boundary, not at
// logger construction: a sink emits only records at/above its own min level and drops the
// rest, so the same logger can feed a debug ring-buffer and an info-only stderr at once (§6b).
type Sink interface {
	// Write delivers one record to the sink. The sink itself enforces its threshold.
	Write(rec Record)
	// Min reports the sink's current threshold so a logger's Enabled() guard can fold across
	// all sinks (the cheap hot-path check: build no fields if no sink would emit the level).
	Min() Level
	// Close releases sink resources.
	Close() error
}

type logger struct {
	mu      sync.Mutex
	scope   string
	sinks   []Sink
	bound   []Field
	scratch Record
	buf     [8]Field
}

// New builds a root logger writing to the supplied sinks. The per-call lvl on Log/Log0/Log1/
// Log2 is the record's level; the threshold that decides what is emitted lives on each sink.
func New(scope string, sinks ...Sink) Logger {
	cp := append([]Sink(nil), sinks...)
	return &logger{scope: scope, sinks: cp}
}

// With returns a child logger that appends additional bound fields.
func (l *logger) With(fields ...Field) Logger {
	child := &logger{
		scope: l.scope,
		sinks: l.sinks,
	}
	if len(l.bound) == 0 && len(fields) == 0 {
		return child
	}
	child.bound = make([]Field, 0, len(l.bound)+len(fields))
	child.bound = append(child.bound, l.bound...)
	child.bound = append(child.bound, fields...)
	return child
}

// Enabled reports whether ANY sink would emit at the supplied level. Hot paths call this
// before building fields so a level no sink wants costs nothing.
func (l *logger) Enabled(lvl Level) bool {
	for _, s := range l.sinks {
		if lvl >= s.Min() {
			return true
		}
	}
	return false
}

// Log writes one record with variadic fields.
func (l *logger) Log(lvl Level, msg string, fields ...Field) {
	if !l.Enabled(lvl) || len(l.sinks) == 0 {
		return
	}
	l.emit(lvl, msg, fields)
}

// Log0 writes one record without call-site fields.
func (l *logger) Log0(lvl Level, msg string) {
	if !l.Enabled(lvl) || len(l.sinks) == 0 {
		return
	}
	l.emit(lvl, msg, nil)
}

// Log1 writes one record with a single call-site field.
func (l *logger) Log1(lvl Level, msg string, f Field) {
	if !l.Enabled(lvl) || len(l.sinks) == 0 {
		return
	}
	fields := [1]Field{f}
	l.emit(lvl, msg, fields[:])
}

// Log2 writes one record with two call-site fields.
func (l *logger) Log2(lvl Level, msg string, f1, f2 Field) {
	if !l.Enabled(lvl) || len(l.sinks) == 0 {
		return
	}
	fields := [2]Field{f1, f2}
	l.emit(lvl, msg, fields[:])
}

// emit formats one record and fans it out to the configured sinks.
func (l *logger) emit(lvl Level, msg string, fields []Field) {
	l.mu.Lock()
	rec := &l.scratch
	rec.Scope = l.scope
	rec.Level = lvl
	rec.Msg = msg
	rec.Time = time.Now()
	sz := len(l.bound) + len(fields)
	if sz > 0 {
		if sz <= len(l.buf) {
			idx := 0
			for _, f := range l.bound {
				l.buf[idx] = f
				idx++
			}
			for _, f := range fields {
				l.buf[idx] = f
				idx++
			}
			rec.Fields = l.buf[:idx]
		} else {
			rec.Fields = make([]Field, 0, sz)
			rec.Fields = append(rec.Fields, l.bound...)
			rec.Fields = append(rec.Fields, fields...)
		}
	} else {
		rec.Fields = nil
	}
	for _, s := range l.sinks {
		if lvl >= s.Min() {
			s.Write(*rec)
		}
	}
	l.mu.Unlock()
}

type ringSink struct {
	mu       sync.Mutex
	min      *LevelVar
	buf      []Record
	next     int
	wrapped  bool
	closed   bool
	capacity int
}

// NewRingSink builds an in-memory tail sink with the requested capacity. min is the threshold
// (a *LevelVar so it retunes live); a nil min emits every level.
func NewRingSink(capacity int, min *LevelVar) Sink {
	if capacity <= 0 {
		capacity = 1
	}
	return &ringSink{min: min, buf: make([]Record, capacity), capacity: capacity}
}

// Min reports the sink's current threshold (Debug when unset).
func (s *ringSink) Min() Level {
	if s.min == nil {
		return Debug
	}
	return s.min.Level()
}

// Write stores the newest record in the ring buffer.
func (s *ringSink) Write(rec Record) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	rec.Fields = append([]Field(nil), rec.Fields...)
	s.buf[s.next] = rec
	s.next++
	if s.next == s.capacity {
		s.next = 0
		s.wrapped = true
	}
}

// Close marks the ring sink closed.
func (s *ringSink) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	return nil
}

// records returns snapshots in chronological order. Tests in this package use this.
func (s *ringSink) records() []Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.wrapped {
		out := make([]Record, s.next)
		copy(out, s.buf[:s.next])
		return out
	}
	out := make([]Record, s.capacity)
	n := copy(out, s.buf[s.next:])
	copy(out[n:], s.buf[:s.next])
	return out
}

type stderrSink struct {
	mu  sync.Mutex
	min *LevelVar
}

// NewStderrSink builds a sink that renders records to standard error. min is the threshold
// (a *LevelVar so it retunes live); a nil min emits every level.
func NewStderrSink(min *LevelVar) Sink {
	return &stderrSink{min: min}
}

// Min reports the sink's current threshold (Debug when unset).
func (s *stderrSink) Min() Level {
	if s.min == nil {
		return Debug
	}
	return s.min.Level()
}

// Write renders one record to standard error.
func (s *stderrSink) Write(rec Record) {
	s.mu.Lock()
	defer s.mu.Unlock()

	b := make([]byte, 0, 128)
	b = append(b, rec.Scope...)
	b = append(b, ' ', '[')
	b = append(b, levelString(rec.Level)...)
	b = append(b, ']', ' ')
	b = append(b, rec.Msg...)
	for _, f := range rec.Fields {
		b = append(b, ' ')
		b = append(b, f.Key...)
		b = append(b, '=')
		switch f.Kind {
		case KindStr:
			b = strconv.AppendQuote(b, f.s)
		case KindInt:
			b = strconv.AppendInt(b, f.i, 10)
		case KindBool:
			b = strconv.AppendBool(b, f.b)
		}
	}
	b = append(b, '\n')
	_, _ = os.Stderr.Write(b)
}

// Close releases the stderr sink.
func (s *stderrSink) Close() error { return nil }

// levelString converts a level to its textual form.
func levelString(lvl Level) string {
	switch lvl {
	case Trace:
		return "trace"
	case Debug:
		return "debug"
	case Info:
		return "info"
	case Warn:
		return "warn"
	case Error:
		return "error"
	default:
		return "unknown"
	}
}
