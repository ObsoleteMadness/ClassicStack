// Package protolog carries raw+parsed protocol traffic on a channel
// separate from the main log. It exists because protocol debugging needs
// every wire byte and every decoded struct — far too noisy for normal
// logs, but invaluable when something misbehaves. Callers emit Events;
// Sinks render them (console hex-dump, JSON for shipping, pcapng for
// Wireshark). Per-source + per-direction filtering keeps a trace focused
// (an AFP trace should not drown in DDP chatter).
//
// This package is intentionally I/O-free at construction: give Logger a
// slice of Sinks and it fans events out. It is the *application's* job to
// route Events to the right Logger (e.g. each service owns one).
package protolog

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// Direction identifies whether a packet is inbound (received) or
// outbound (transmitted).
type Direction uint8

const (
	DirIn Direction = iota
	DirOut
)

func (d Direction) String() string {
	if d == DirOut {
		return "out"
	}
	return "in"
}

// Event is one protocol exchange record.
type Event struct {
	Time      time.Time
	Source    string // "AFP", "ASP", "DDP", ...
	Dir       Direction
	Peer      string // "net=1.23 node=0x42" or tcp addr
	Raw       []byte // wire bytes (may be nil if not captured)
	Parsed    any    // decoded struct, nil if decode failed
	DecodeErr error
	Session   string // correlation id, if any
}

// Sink consumes Events. Implementations must be safe for concurrent use.
type Sink interface {
	Record(Event)
}

// Filter decides whether a given source+direction should be recorded.
// Returning false drops the event before any Sink is touched.
type Filter func(source string, dir Direction) bool

// AllowAll records everything. Good for unit tests; expensive in prod.
func AllowAll() Filter { return func(string, Direction) bool { return true } }

// DenyAll drops everything. The zero-cost default when protolog is off.
func DenyAll() Filter { return func(string, Direction) bool { return false } }

// Logger dispatches Events through a Filter to a list of Sinks.
type Logger struct {
	mu     sync.RWMutex
	filter Filter
	sinks  []Sink
}

// New builds a Logger with the given filter (DenyAll if nil) and sinks.
func New(filter Filter, sinks ...Sink) *Logger {
	if filter == nil {
		filter = DenyAll()
	}
	return &Logger{filter: filter, sinks: sinks}
}

// SetFilter swaps the active filter atomically.
func (l *Logger) SetFilter(f Filter) {
	if f == nil {
		f = DenyAll()
	}
	l.mu.Lock()
	l.filter = f
	l.mu.Unlock()
}

// In is shorthand for Record with DirIn.
func (l *Logger) In(source, peer string, raw []byte, parsed any, err error) {
	l.Record(Event{Time: time.Now(), Source: source, Dir: DirIn, Peer: peer, Raw: raw, Parsed: parsed, DecodeErr: err})
}

// Out is shorthand for Record with DirOut.
func (l *Logger) Out(source, peer string, raw []byte, parsed any) {
	l.Record(Event{Time: time.Now(), Source: source, Dir: DirOut, Peer: peer, Raw: raw, Parsed: parsed})
}

// Record fans the event out to every sink when the filter admits it.
func (l *Logger) Record(e Event) {
	l.mu.RLock()
	f := l.filter
	sinks := l.sinks
	l.mu.RUnlock()
	if f == nil || !f(e.Source, e.Dir) {
		return
	}
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	for _, s := range sinks {
		s.Record(e)
	}
}

// ConsoleSink renders events as human-readable single-line headers
// followed by an optional hex dump. It is safe for concurrent use.
type ConsoleSink struct {
	W io.Writer
	// MaxBytes truncates the hex dump after N bytes. 0 means "no dump",
	// negative means "no limit".
	MaxBytes int

	mu sync.Mutex
}

func (c *ConsoleSink) Record(e Event) {
	var sb strings.Builder
	sb.WriteByte('[')
	sb.WriteString("PROTO ")
	sb.WriteString(e.Source)
	if e.Dir == DirIn {
		sb.WriteString("<-")
	} else {
		sb.WriteString("->")
	}
	sb.WriteString(e.Peer)
	sb.WriteString("] ")
	fmt.Fprintf(&sb, "%dB ", len(e.Raw))
	if e.Parsed != nil {
		fmt.Fprintf(&sb, "%T%+v", e.Parsed, e.Parsed)
	}
	if e.DecodeErr != nil {
		fmt.Fprintf(&sb, " decodeErr=%v", e.DecodeErr)
	}
	if e.Session != "" {
		fmt.Fprintf(&sb, " session=%s", e.Session)
	}
	sb.WriteByte('\n')
	if c.MaxBytes != 0 && len(e.Raw) > 0 {
		end := len(e.Raw)
		if c.MaxBytes > 0 && end > c.MaxBytes {
			end = c.MaxBytes
		}
		sb.WriteString(hex.Dump(e.Raw[:end]))
		if end < len(e.Raw) {
			fmt.Fprintf(&sb, "... (%d bytes truncated)\n", len(e.Raw)-end)
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, _ = io.WriteString(c.W, sb.String())
}

// JSONSink emits newline-delimited JSON. Raw bytes are encoded as hex so
// the output remains replayable through standard tooling.
type JSONSink struct {
	W io.Writer

	mu sync.Mutex
}

type jsonEvent struct {
	Time      string `json:"time"`
	Source    string `json:"source"`
	Dir       string `json:"dir"`
	Peer      string `json:"peer,omitempty"`
	RawHex    string `json:"raw_hex,omitempty"`
	Parsed    any    `json:"parsed,omitempty"`
	DecodeErr string `json:"decode_err,omitempty"`
	Session   string `json:"session,omitempty"`
}

func (j *JSONSink) Record(e Event) {
	rec := jsonEvent{
		Time:    e.Time.UTC().Format(time.RFC3339Nano),
		Source:  e.Source,
		Dir:     e.Dir.String(),
		Peer:    e.Peer,
		Parsed:  e.Parsed,
		Session: e.Session,
	}
	if len(e.Raw) > 0 {
		rec.RawHex = hex.EncodeToString(e.Raw)
	}
	if e.DecodeErr != nil {
		rec.DecodeErr = e.DecodeErr.Error()
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	_, _ = j.W.Write(b)
	_, _ = j.W.Write([]byte{'\n'})
}

// FilterConfig expresses per-source direction settings like
// {"AFP":"in+out", "DDP":"off"}. Build a Filter from it via Compile.
type FilterConfig map[string]string

// Compile turns a FilterConfig into a Filter. Unknown sources default to
// the value of "*" if present, otherwise "off".
func (fc FilterConfig) Compile() Filter {
	want := make(map[string]struct{ in, out bool }, len(fc))
	for src, spec := range fc {
		spec = strings.ToLower(strings.TrimSpace(spec))
		var in, out bool
		switch spec {
		case "in":
			in = true
		case "out":
			out = true
		case "in+out", "both", "on":
			in, out = true, true
		}
		want[src] = struct{ in, out bool }{in, out}
	}
	fallback := want["*"]
	return func(source string, dir Direction) bool {
		w, ok := want[source]
		if !ok {
			w = fallback
		}
		if dir == DirIn {
			return w.in
		}
		return w.out
	}
}
