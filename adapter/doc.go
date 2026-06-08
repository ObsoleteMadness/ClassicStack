// Package adapter is the outer ring of the hexagonal architecture (§14).
//
// Ring: ADAPTER. Adapters implement core/ interfaces using the outside world —
// pcap/gopacket links, koanf/toml + UCI config codecs, net/http + ubus control
// front-ends, sqlite metastores, S3/WebDAV filesystems, OS service integration.
// Adapters may import heavy third-party dependencies that core/ forbids.
//
// Adapters depend on core/ (to implement its interfaces) but never on compose/.
// They are selected at build time via build tags and registered through the
// registries in core/ (config.RegisterFS, registry.Register, etc.).
package adapter
