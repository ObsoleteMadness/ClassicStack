package ipxgw

import (
	"strconv"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/core/config"
)

// SectionKey is the config-section / registry name for the IPX gateway (MacIPX). It
// matches the component Name ("IPXGW"), the singleton convention. IPXGW previously had
// NO config section and was NOT registered in compose at all; this makes it a real,
// operator-configurable service.
const SectionKey = Name

// Section is the IPX-gateway singleton config: enable flag, the announced IPX network
// number, and the NBP zone bindings the gateway advertises ("IPX Gateway" objects).
// Satisfies config.Section so the model round-trips it.
type Section struct {
	// SKey is the section key; always "IPXGW". Stored so Key() is a plain getter.
	SKey string `toml:"-"`
	// Enabled gates the gateway (component.Enableable). Disabled builds the service but
	// reports Disabled; the supervisor's enable-aware start can skip it.
	Enabled bool `toml:"enabled"`
	// IPXNetwork is the IPX network number the gateway announces. 0 → DefaultIPXNetwork
	// (0x10), matching NetWare's MACIPXGW default.
	IPXNetwork uint32 `toml:"ipx_network"`
	// Bindings are the NBP registrations the gateway publishes, each "Object:Zone" (the
	// object name to advertise in that AppleTalk zone). Empty = the service's own
	// default registration per known zone.
	Bindings []string `toml:"bindings"`
}

// Key returns the section key.
func (s *Section) Key() string { return SectionKey }

// Clone returns a deep copy (Bindings is the only reference field).
func (s *Section) Clone() config.Section {
	cp := *s
	cp.Bindings = append([]string(nil), s.Bindings...)
	return &cp
}

// Validate checks the section in isolation. Binding strings that carry a ':' split into
// object + zone; a malformed entry is tolerated (the parser drops it), so config does
// not hard-fail on a stray binding.
func (s *Section) Validate() error { return nil }

// Config builds the service Config from the section.
func (s *Section) Config() Config { return Config{IPXNetwork: s.IPXNetwork} }

// ZoneBindings parses the "Object:Zone" strings into ZoneBinding values, dropping any
// entry without a ':' separator (an object with no zone is meaningless for NBP).
func (s *Section) ZoneBindings() []ZoneBinding {
	out := make([]ZoneBinding, 0, len(s.Bindings))
	for _, b := range s.Bindings {
		obj, zone, ok := strings.Cut(b, ":")
		if !ok || obj == "" || zone == "" {
			continue
		}
		out = append(out, ZoneBinding{Object: []byte(obj), Zone: []byte(zone)})
	}
	return out
}

// compile-time assertion: *Section satisfies config.Section.
var _ config.Section = (*Section)(nil)

// SectionFromModel resolves the IPXGW section from the model, or nil when none is set.
func SectionFromModel(m *config.Model) *Section {
	if m != nil {
		if s, ok := m.Get(SectionKey); ok {
			if gs, ok := s.(*Section); ok {
				return gs
			}
		}
	}
	return nil
}

// RegisterSection installs the IPXGW section schema so codecs round-trip it. Called
// from the compose registry wiring (kept out of an init() so a build excluding IPXGW
// excludes the section too).
func RegisterSection() {
	config.Register(config.SectionSchema{
		Key: SectionKey,
		New: func() config.Section { return &Section{SKey: SectionKey} },
		Validate: func(s config.Section) error {
			if gs, ok := s.(*Section); ok {
				return gs.Validate()
			}
			return nil
		},
	})
}

// formatIPXNetwork renders an IPX network number as 8 hex digits, for diagnostics /
// the dashboard Props (a uint32 is opaque on the wire).
func formatIPXNetwork(n uint32) string {
	if n == 0 {
		n = DefaultIPXNetwork
	}
	return "0x" + strconv.FormatUint(uint64(n), 16)
}
