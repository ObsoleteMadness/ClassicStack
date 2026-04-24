// Package logging is a thin wrapper around log/slog providing:
//   - dual-mode output: a human-readable console handler and a structured
//     JSON handler, both of which can be active simultaneously;
//   - per-component source tagging ([AFP], [ASP], [EtherTalk], ...) rendered
//     as a prefix in console output and emitted as "source":"AFP" in JSON;
//   - context-carried loggers so correlation fields (session, volume) flow
//     through call chains without threading a logger parameter everywhere.
//
// Construct one root logger in main via New(root, opts), then derive
// per-service loggers with logger.With("source", "AFP") or Child(parent,
// "AFP"). Handler wiring is owned here; callers should never touch
// slog.NewJSONHandler/slog.NewTextHandler directly.
package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
)

// Format selects one of the handlers emitted by New.
type Format int

const (
	// FormatConsole is a human-readable single-line format with a [SOURCE]
	// prefix. Intended for TTY/stderr.
	FormatConsole Format = iota
	// FormatJSON is newline-delimited slog JSON. Intended for log pipelines.
	FormatJSON
)

// Sink describes one output. Multiple sinks may be combined via New.
type Sink struct {
	Writer io.Writer
	Format Format
	Level  slog.Level
}

// Options configures New.
type Options struct {
	// Sinks listed here receive every record the root logger emits. If
	// empty, a single console sink at LevelInfo on stderr is used.
	Sinks []Sink
	// Color enables ANSI colouring of the level tag in console output. The
	// zero value is "off"; callers that want auto-detection should pass
	// term.IsTerminal(int(os.Stderr.Fd())).
	Color bool
}

// New returns a root *slog.Logger carrying the given source tag. Pass the
// returned logger into services; each service should further narrow with
// logger.With("source", <its own tag>) via Child to replace (not append)
// the source field.
func New(source string, opts Options) *slog.Logger {
	sinks := opts.Sinks
	if len(sinks) == 0 {
		sinks = []Sink{{Writer: os.Stderr, Format: FormatConsole, Level: slog.LevelInfo}}
	}
	handlers := make([]slog.Handler, 0, len(sinks))
	for _, s := range sinks {
		handlers = append(handlers, newHandler(s, opts.Color))
	}
	var h slog.Handler
	if len(handlers) == 1 {
		h = handlers[0]
	} else {
		h = fanoutHandler(handlers)
	}
	l := slog.New(h)
	if source != "" {
		l = l.With(slog.String("source", source))
	}
	return l
}

// Child derives a sub-logger whose source attribute replaces (not appends)
// the parent's. Useful when a sub-component needs its own tag (e.g. a fork
// subsystem inside AFP wants [AFP.Fork]).
func Child(parent *slog.Logger, source string) *slog.Logger {
	if parent == nil {
		parent = slog.Default()
	}
	return parent.With(slog.String("source", source))
}

type ctxKey struct{}

// WithContext attaches a logger to ctx. FromContext will return it.
func WithContext(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, l)
}

// FromContext returns the logger stored by WithContext, falling back to
// slog.Default() when nothing is attached.
func FromContext(ctx context.Context) *slog.Logger {
	if ctx != nil {
		if l, ok := ctx.Value(ctxKey{}).(*slog.Logger); ok && l != nil {
			return l
		}
	}
	return slog.Default()
}

// SetDefault installs l as slog.Default and returns a restore func for
// tests.
func SetDefault(l *slog.Logger) func() {
	prev := slog.Default()
	slog.SetDefault(l)
	return func() { slog.SetDefault(prev) }
}

// ParseLevel maps "debug" / "info" / "warn" / "warning" / "error" to
// slog.Level. Unknown values return slog.LevelInfo and ok=false.
func ParseLevel(s string) (slog.Level, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, true
	case "info", "":
		return slog.LevelInfo, true
	case "warn", "warning":
		return slog.LevelWarn, true
	case "error":
		return slog.LevelError, true
	}
	return slog.LevelInfo, false
}

func newHandler(s Sink, color bool) slog.Handler {
	if s.Writer == nil {
		s.Writer = os.Stderr
	}
	switch s.Format {
	case FormatJSON:
		return slog.NewJSONHandler(s.Writer, &slog.HandlerOptions{Level: s.Level})
	default:
		return &consoleHandler{w: s.Writer, level: s.Level, color: color, mu: &sync.Mutex{}}
	}
}

// consoleHandler is a minimal slog.Handler that renders
//
//	[2026-04-24 14:05:12] INFO  [AFP] message key=value
//
// It lifts the "source" attribute into the bracketed prefix and formats
// the remaining attributes as key=value pairs. It is deliberately small
// and allocation-light; callers who need slog's full feature set should
// use FormatJSON.
type consoleHandler struct {
	w     io.Writer
	level slog.Level
	color bool
	// mu guards writes to w. It is a pointer so WithAttrs/WithGroup clones
	// share the same lock on the same writer.
	mu *sync.Mutex
	// attrs and groups are accumulated via WithAttrs/WithGroup.
	attrs  []slog.Attr
	groups []string
}

func (h *consoleHandler) Enabled(_ context.Context, l slog.Level) bool {
	return l >= h.level
}

func (h *consoleHandler) Handle(_ context.Context, r slog.Record) error {
	var sb strings.Builder
	sb.WriteByte('[')
	sb.WriteString(r.Time.Format("2006-01-02 15:04:05"))
	sb.WriteString("] ")
	sb.WriteString(levelTag(r.Level, h.color))

	// Extract source from accumulated attrs and record attrs.
	source := ""
	var rest []slog.Attr
	for _, a := range h.attrs {
		if a.Key == "source" {
			source = a.Value.String()
			continue
		}
		rest = append(rest, a)
	}
	var recordAttrs []slog.Attr
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "source" {
			source = a.Value.String()
			return true
		}
		recordAttrs = append(recordAttrs, a)
		return true
	})

	if source != "" {
		sb.WriteString(" [")
		sb.WriteString(source)
		sb.WriteByte(']')
	}
	sb.WriteByte(' ')
	sb.WriteString(r.Message)

	for _, a := range rest {
		appendAttr(&sb, a)
	}
	for _, a := range recordAttrs {
		appendAttr(&sb, a)
	}

	sb.WriteByte('\n')
	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.w, sb.String())
	return err
}

func (h *consoleHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := *h
	clone.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &clone
}

func (h *consoleHandler) WithGroup(name string) slog.Handler {
	clone := *h
	clone.groups = append(append([]string{}, h.groups...), name)
	return &clone
}

func appendAttr(sb *strings.Builder, a slog.Attr) {
	if a.Equal(slog.Attr{}) {
		return
	}
	sb.WriteByte(' ')
	sb.WriteString(a.Key)
	sb.WriteByte('=')
	v := a.Value.String()
	if strings.ContainsAny(v, " \t\"") {
		fmt.Fprintf(sb, "%q", v)
	} else {
		sb.WriteString(v)
	}
}

func levelTag(l slog.Level, color bool) string {
	var tag string
	switch {
	case l >= slog.LevelError:
		tag = "ERROR"
	case l >= slog.LevelWarn:
		tag = "WARN "
	case l >= slog.LevelInfo:
		tag = "INFO "
	default:
		tag = "DEBUG"
	}
	if !color {
		return tag
	}
	switch {
	case l >= slog.LevelError:
		return "\x1b[31m" + tag + "\x1b[0m"
	case l >= slog.LevelWarn:
		return "\x1b[33m" + tag + "\x1b[0m"
	case l >= slog.LevelInfo:
		return "\x1b[32m" + tag + "\x1b[0m"
	default:
		return "\x1b[90m" + tag + "\x1b[0m"
	}
}

// fanoutHandler broadcasts each record to every contained handler whose
// Enabled returns true. Used when Options.Sinks has >1 entry.
type fanout []slog.Handler

func fanoutHandler(hs []slog.Handler) slog.Handler { return fanout(hs) }

func (f fanout) Enabled(ctx context.Context, l slog.Level) bool {
	for _, h := range f {
		if h.Enabled(ctx, l) {
			return true
		}
	}
	return false
}

func (f fanout) Handle(ctx context.Context, r slog.Record) error {
	var firstErr error
	for _, h := range f {
		if !h.Enabled(ctx, r.Level) {
			continue
		}
		if err := h.Handle(ctx, r.Clone()); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (f fanout) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make(fanout, len(f))
	for i, h := range f {
		out[i] = h.WithAttrs(attrs)
	}
	return out
}

func (f fanout) WithGroup(name string) slog.Handler {
	out := make(fanout, len(f))
	for i, h := range f {
		out[i] = h.WithGroup(name)
	}
	return out
}
