// Package config is the pure in-memory configuration model, the section registry
// that lets new components add config without editing a central struct, and the
// Codec/Store adapter seams (§4).
//
// Ring: CORE (stdlib only — no struct tags, no reflection, no koanf/toml/uci;
// those are adapters). Real types land in step B6.
package config
