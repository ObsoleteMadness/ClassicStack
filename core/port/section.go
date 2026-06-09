package port

import "github.com/ObsoleteMadness/ClassicStack/core/config"

// Section is the typed config one placeholder port carries. It satisfies
// config.Section (§4) so the model can stage/round-trip it and the supervisor
// can hand it back via ApplyConfig.
type Section struct {
	// SKey is the section/component key ("EtherTalk", "LocalTalk", …).
	SKey string `toml:"-"`
	// Iface is the bound interface ("eth0", "ipx:0550", …).
	Iface string `toml:"iface"`
	// IsEnabled mirrors the configured-enabled flag (≠ running).
	IsEnabled bool `toml:"enabled"`
}

// Key returns the section key (matches the component/registry name).
func (s *Section) Key() string { return s.SKey }

// Clone returns a deep copy.
func (s *Section) Clone() config.Section {
	cp := *s
	return &cp
}

// Validate checks the section in isolation. A placeholder accepts anything.
func (s *Section) Validate() error { return nil }

// compile-time assertion: *Section satisfies config.Section.
var _ config.Section = (*Section)(nil)

// SectionFromModel resolves the placeholder Section registered under key, falling
// back to a fresh default when the model has none.
func SectionFromModel(m *config.Model, key string) *Section {
	if m != nil {
		if s, ok := m.Get(key); ok {
			if ps, ok := s.(*Section); ok {
				return ps
			}
		}
	}
	return &Section{SKey: key}
}
