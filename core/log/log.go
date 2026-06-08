package log

import (
	"os"
	"strconv"
	"sync"
	"time"
)

type Level uint8

const (
	Debug Level = iota
	Info
	Warn
	Error
)

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
	KindStr Kind = iota
	KindInt
	KindBool
)

func Str(k, v string) Field {
	return Field{Key: k, Kind: KindStr, s: v}
}

func Int(k string, v int64) Field {
	return Field{Key: k, Kind: KindInt, i: v}
}

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
	With(fields ...Field) Logger
	Log(lvl Level, msg string, fields ...Field)
	Enabled(lvl Level) bool

	Log0(lvl Level, msg string)
	Log1(lvl Level, msg string, f Field)
	Log2(lvl Level, msg string, f1, f2 Field)
}

type Record struct {
	Scope  string
	Level  Level
	Msg    string
	Fields []Field
	Time   time.Time
}

type Sink interface {
	Write(rec Record)
	Close() error
}

type logger struct {
	mu      sync.Mutex
	scope   string
	min     Level
	sinks   []Sink
	bound   []Field
	scratch Record
	buf     [8]Field
}

func New(scope string, min Level, sinks ...Sink) Logger {
	cp := append([]Sink(nil), sinks...)
	return &logger{scope: scope, min: min, sinks: cp}
}

func (l *logger) With(fields ...Field) Logger {
	child := &logger{
		scope: l.scope,
		min:   l.min,
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

func (l *logger) Enabled(lvl Level) bool {
	return lvl >= l.min
}

func (l *logger) Log(lvl Level, msg string, fields ...Field) {
	if !l.Enabled(lvl) || len(l.sinks) == 0 {
		return
	}
	l.emit(lvl, msg, fields)
}

func (l *logger) Log0(lvl Level, msg string) {
	if !l.Enabled(lvl) || len(l.sinks) == 0 {
		return
	}
	l.emit(lvl, msg, nil)
}

func (l *logger) Log1(lvl Level, msg string, f Field) {
	if !l.Enabled(lvl) || len(l.sinks) == 0 {
		return
	}
	fields := [1]Field{f}
	l.emit(lvl, msg, fields[:])
}

func (l *logger) Log2(lvl Level, msg string, f1, f2 Field) {
	if !l.Enabled(lvl) || len(l.sinks) == 0 {
		return
	}
	fields := [2]Field{f1, f2}
	l.emit(lvl, msg, fields[:])
}

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
		s.Write(*rec)
	}
	l.mu.Unlock()
}

type ringSink struct {
	mu       sync.Mutex
	buf      []Record
	next     int
	wrapped  bool
	closed   bool
	capacity int
}

func NewRingSink(capacity int) Sink {
	if capacity <= 0 {
		capacity = 1
	}
	return &ringSink{buf: make([]Record, capacity), capacity: capacity}
}

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
	mu sync.Mutex
}

func NewStderrSink() Sink {
	return &stderrSink{}
}

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

func (s *stderrSink) Close() error { return nil }

func levelString(lvl Level) string {
	switch lvl {
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
