// Package core is the innermost ring of the hexagonal architecture (§14).
//
// Ring: CORE. Everything under core/ imports only the Go standard library and
// other core/ packages — never pcap, gopacket, koanf, net/http, sqlite,
// database/sql, encoding/binary, encoding/json, or slog (the generic
// reflection-based serialization TinyGo doesn't reliably support — bare
// reflect itself is fine). The import-graph gate (core/internal/archtest)
// enforces this rule executably (§1).
//
// core/ holds the pure contracts (interfaces + value types) and the few pieces
// of pure logic the contracts reference (the DDP codec, MacRoman tables, buses,
// logging, config model). Behaviour that needs the outside world lives in
// adapter/; wiring lives in compose/.
package core
