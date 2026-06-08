// Package bus is the one typed, topic-scoped, allocation-light pub/sub
// primitive, instantiated per domain (telemetry here; FS-mutation in core/fs),
// plus the telemetry event types (§5/§10c).
//
// Ring: CORE (stdlib only — no slog/reflect/json). Real types land in step B3.
package bus
