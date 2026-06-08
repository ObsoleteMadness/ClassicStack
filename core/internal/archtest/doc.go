// Package archtest holds the executable form of the dependency rule (§1): a test
// that walks the import graph of every core/... package and fails if any of them
// imports a forbidden package (pcap, gopacket, koanf, net/http, sqlite/
// database/sql, reflect, encoding/json, slog).
//
// Ring: CORE/internal. The TEST may use heavier tooling deps (go/packages); only
// core/ *runtime* packages are constrained. There is no runtime code here.
package archtest
