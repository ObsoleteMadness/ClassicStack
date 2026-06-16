// Package toml is the TOML config Codec adapter over core/config.Model (§4).
// It lives in the ADAPTER ring, so it may use reflection-based marshalling
// (go-toml) — the no-reflection rule binds core/, not adapters.
//
// Round-trip is schema-driven: well-known sections (Logging/Router/Bridge) are
// fixed fields; component sections are (un)marshalled via the config schema
// registry, so a new component round-trips without editing this codec.
package toml

import (
	"strings"

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
	Identity  config.Identity         `toml:"identity"`
	AdminAuth config.AdminAuth        `toml:"adminauth"`
	Logging   config.LoggingSection   `toml:"logging"`
	Router    config.RouterSection    `toml:"router"`
	Bridge    config.InterfaceSection `toml:"bridge"`
}

// Marshal renders the model: the well-known sections under their fixed keys, then
// each component section under its own Key(). The contract is Unmarshal(Marshal(m)) == m.
func (c *Codec) Marshal(m *config.Model) ([]byte, error) {
	// Build a top-level table so go-toml emits one [section] per entry.
	top := map[string]any{
		"identity": m.Identity,
		"logging":  m.Logging,
		"router":   m.Router,
		"bridge":   m.Bridge,
	}
	// Only emit [adminauth] once an admin is configured, so a fresh server.toml has
	// no empty credential block (and first-run detection stays unambiguous).
	if m.AdminAuth.Configured() {
		top["adminauth"] = m.AdminAuth
	}
	for key, sec := range m.Sections {
		top[key] = sec
	}
	// Repeated (named-instance) sections render as an array-of-tables under the
	// lowercased schema key (e.g. [[afpvolumes]]), one table per instance. go-toml
	// emits a []Section as a TOML array; each element marshals via its own tags.
	for key, list := range m.Lists {
		if len(list) == 0 {
			continue
		}
		top[strings.ToLower(key)] = list
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
	m.Identity = wk.Identity
	m.AdminAuth = wk.AdminAuth
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
	if m.Lists == nil {
		m.Lists = make(map[string][]config.Section)
	}
	for _, schema := range config.Schemas() {
		if schema.Repeated {
			if err := unmarshalRepeated(raw, schema, m); err != nil {
				return err
			}
			continue
		}
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

// unmarshalRepeated decodes a repeated schema's array-of-tables (keyed by the
// lowercased schema key) into one NamedSection per element, appended in document
// order. A document with no such table leaves the instance list empty.
func unmarshalRepeated(raw map[string]any, schema config.SectionSchema, m *config.Model) error {
	sub, ok := raw[strings.ToLower(schema.Key)]
	if !ok {
		return nil
	}
	elems, ok := sub.([]any)
	if !ok {
		// A single inline table (not an array) is tolerated as one instance.
		elems = []any{sub}
	}
	for _, el := range elems {
		elBytes, err := gotoml.Marshal(el)
		if err != nil {
			return err
		}
		sec := schema.New()
		if err := gotoml.Unmarshal(elBytes, sec); err != nil {
			return err
		}
		m.Lists[schema.Key] = append(m.Lists[schema.Key], sec)
	}
	return nil
}
