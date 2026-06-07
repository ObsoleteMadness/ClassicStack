// Package logbuf is an in-memory ring buffer of recent log records plus a
// live broadcaster, used by the management plane to serve a log viewer.
//
// A Buffer is both a slog.Handler (installed alongside the console sink via
// pkg/logging's Options.Extra) and a fan-out source: it retains the most
// recent entries for an initial history load and pushes new entries to any
// SSE/TUI subscribers. It is untagged so the control plane (and a future
// text UI) can read logs in every build variant; only the HTTP front-end is
// build-tag gated.
package logbuf

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// DefaultCapacity is the number of entries Default retains.
const DefaultCapacity = 500

// Entry is a single captured log record.
type Entry struct {
	UnixMilli int64  `json:"t"`
	Level     string `json:"level"`
	Message   string `json:"msg"`
}

// Buffer retains the most recent log entries in a ring and fans new entries
// out to subscribers. The zero value is not usable; construct with New.
type Buffer struct {
	mu        sync.Mutex
	ring      []Entry // len == cap once filled; head/count track the window
	head      int     // index of the oldest entry
	count     int     // number of valid entries in ring
	subs      map[int]chan Entry
	nextSubID int
}

// New returns a Buffer retaining up to capacity entries (clamped to >= 1).
func New(capacity int) *Buffer {
	if capacity < 1 {
		capacity = 1
	}
	return &Buffer{
		ring: make([]Entry, capacity),
		subs: make(map[int]chan Entry),
	}
}

// Default is the process-global buffer the control plane reads by default.
var Default = New(DefaultCapacity)

// Append stores e in the ring (evicting the oldest entry when full) and
// fans it out to subscribers without blocking; entries are dropped for slow
// subscribers, matching the stats broadcaster.
func (b *Buffer) Append(e Entry) {
	b.mu.Lock()
	if b.count < len(b.ring) {
		b.ring[(b.head+b.count)%len(b.ring)] = e
		b.count++
	} else {
		b.ring[b.head] = e
		b.head = (b.head + 1) % len(b.ring)
	}
	subs := make([]chan Entry, 0, len(b.subs))
	for _, ch := range b.subs {
		subs = append(subs, ch)
	}
	b.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- e:
		default: // drop for slow subscribers
		}
	}
}

// Snapshot returns the retained entries oldest-first.
func (b *Buffer) Snapshot() []Entry {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Entry, b.count)
	for i := range b.count {
		out[i] = b.ring[(b.head+i)%len(b.ring)]
	}
	return out
}

// Subscribe registers a subscriber and returns its receive channel plus a
// cancel func that unsubscribes and closes the channel.
func (b *Buffer) Subscribe() (<-chan Entry, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := b.nextSubID
	b.nextSubID++
	ch := make(chan Entry, 64)
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

// Handler is a slog.Handler that records each emitted log record into a
// Buffer. Install it on the root logger via pkg/logging's Options.Extra so
// every line is captured for the log viewer in addition to its normal sink.
type Handler struct {
	buf   *Buffer
	level slog.Level
	attrs []slog.Attr
}

// NewHandler returns a Handler appending records at or above level to buf.
func NewHandler(buf *Buffer, level slog.Level) *Handler {
	return &Handler{buf: buf, level: level}
}

// Enabled reports whether records at l should be captured.
func (h *Handler) Enabled(_ context.Context, l slog.Level) bool {
	return l >= h.level
}

// Handle records r into the buffer. The "source" attribute is lifted into a
// bracketed prefix (matching the console handler); remaining attributes are
// rendered as key=value pairs appended to the message.
func (h *Handler) Handle(_ context.Context, r slog.Record) error {
	source := ""
	var sb strings.Builder

	emit := func(a slog.Attr) {
		if a.Key == "source" {
			source = a.Value.String()
			return
		}
		sb.WriteByte(' ')
		sb.WriteString(a.Key)
		sb.WriteByte('=')
		sb.WriteString(a.Value.String())
	}
	for _, a := range h.attrs {
		emit(a)
	}
	r.Attrs(func(a slog.Attr) bool {
		emit(a)
		return true
	})

	var msg strings.Builder
	if source != "" {
		msg.WriteByte('[')
		msg.WriteString(source)
		msg.WriteString("] ")
	}
	msg.WriteString(r.Message)
	msg.WriteString(sb.String())

	ts := r.Time
	if ts.IsZero() {
		ts = time.Now()
	}
	h.buf.Append(Entry{
		UnixMilli: ts.UnixMilli(),
		Level:     r.Level.String(),
		Message:   msg.String(),
	})
	return nil
}

// WithAttrs returns a handler that prepends attrs to every record it handles.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := *h
	clone.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &clone
}

// WithGroup is a no-op for this flat handler; groups are not rendered.
func (h *Handler) WithGroup(string) slog.Handler { return h }
