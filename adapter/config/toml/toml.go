// Package toml is the TOML config Codec adapter over core/config.Model (§4).
// It lives in the ADAPTER ring, so it may use reflection-based marshalling
// (go-toml) — the no-reflection rule binds core/, not adapters.
//
// Round-trip is schema-driven: well-known sections (Logging/Router/Bridge) are
// fixed fields; component sections are (un)marshalled via the config schema
// registry, so a new component round-trips without editing this codec.
package toml

import (
	gotoml "github.com/pelletier/go-toml/v2"

	"github.com/ObsoleteMadness/ClassicStack/core/config"
)

// Codec marshals/unmarshals a config.Model to/from TOML bytes.
type Codec struct{}

// New returns a TOML codec.
func New() *Codec { return &Codec{} }

// compile-time assertion: *Codec satisfies config.Codec.
var _ config.Codec = (*Codec)(nil)

// wellKnown mirrors the typed Model fields for TOML (un)marshalling. Component
// sections are handled separately via the schema registry.
type wellKnown struct {
	Logging config.LoggingSection   `toml:"logging"`
	Router  config.RouterSection    `toml:"router"`
	Bridge  config.InterfaceSection `toml:"bridge"`
}

// Marshal renders the model: the well-known sections under their fixed keys, then
// each component section under its own Key(). The contract is Unmarshal(Marshal(m)) == m.
func (c *Codec) Marshal(m *config.Model) ([]byte, error) {
	// Build a top-level table so go-toml emits one [section] per entry.
	top := map[string]any{
		"logging": m.Logging,
		"router":  m.Router,
		"bridge":  m.Bridge,
	}
	for key, sec := range m.Sections {
		top[key] = sec
	}
	return gotoml.Marshal(top)
}

// Unmarshal parses TOML into the model. Well-known sections fill the typed fields;
// every other top-level table is matched to a registered schema (config.Schemas),
// allocated via New(), and decoded into. Unknown tables (no schema) are skipped.
func (c *Codec) Unmarshal(data []byte, m *config.Model) error {
	var wk wellKnown
	if err := gotoml.Unmarshal(data, &wk); err != nil {
		return err
	}
	m.Logging = wk.Logging
	m.Router = wk.Router
	m.Bridge = wk.Bridge

	// Decode the raw document so we can re-marshal each component sub-table and
	// feed it into its typed section (allocated from the schema registry).
	var raw map[string]any
	if err := gotoml.Unmarshal(data, &raw); err != nil {
		return err
	}
	if m.Sections == nil {
		m.Sections = make(map[string]config.Section)
	}
	for _, schema := range config.Schemas() {
		sub, ok := raw[schema.Key]
		if !ok {
			continue
		}
		// Re-marshal the sub-table, then unmarshal into the typed section.
		subBytes, err := gotoml.Marshal(sub)
		if err != nil {
			return err
		}
		sec := schema.New()
		if err := gotoml.Unmarshal(subBytes, sec); err != nil {
			return err
		}
		m.Sections[schema.Key] = sec
	}
	return nil
}
