// Package log is scoped, levelled, typed-field logging fanning to multiple sinks
// (§6). Zero reflection: fields are typed scalars, never ...any. The bus is just
// one sink (an adapter); stdlib-only ring/stderr sinks live here.
//
// Ring: CORE (stdlib only — no slog/reflect). Real types land in step B5.
package log
