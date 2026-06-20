package netbios

import (
	"slices"

	"github.com/ObsoleteMadness/ClassicStack/core/config"
)

// SectionKey is the config-section / registry name for the NetBIOS service. It matches
// the component Name ("NetBIOS"), the singleton convention (component name == section
// key). NetBIOS previously carried NO config section — it was enabled purely by being
// built and a transport existing. This section makes its transport bindings + scope
// explicit and operator-editable.
const SectionKey = Name

// Transport tokens for Section.Transports — the NetBIOS-carrying transports the service
// binds. NetBIOS rides three (netbios-transport-bindings): NBF over NetBEUI, NB-IPX over
// IPX, and NBT over TCP. The list names which the operator wants; empty = bind every
// transport that was built (back-compat with the prior implicit behaviour).
const (
	TransportNetBEUI = "netbeui" // NBF: NetBIOS frames over 802.2 LLC
	TransportIPX     = "ipx"     // NB-IPX: NetBIOS over IPX (NWLink)
	TransportNBT     = "nbt"     // NetBIOS over TCP/IP (ports 137-139)
)

// Section is the NetBIOS singleton config: the transports it binds and the NetBIOS
// scope id. Server name is NOT here — it is the shared config.Identity.Hostname
// (§4-bis), upper-cased to the NetBIOS name. Satisfies config.Section so the model
// round-trips it.
type Section struct {
	// SKey is the section key; always "NetBIOS". Stored so Key() is a plain getter.
	SKey string `toml:"-"`
	// Transports lists the transport tokens (netbeui/ipx/nbt) to bind. Empty = bind
	// every built transport (back-compat).
	Transports []string `toml:"transports"`
	// ScopeID is the NetBIOS scope identifier appended to names (rarely used; empty is
	// the universal default scope).
	ScopeID string `toml:"scope_id"`
}

// Key returns the section key.
func (s *Section) Key() string { return SectionKey }

// Clone returns a deep copy (Transports is the only reference field).
func (s *Section) Clone() config.Section {
	cp := *s
	cp.Transports = append([]string(nil), s.Transports...)
	return &cp
}

// Validate checks the section in isolation. Unknown transport tokens are tolerated
// (the compose cross-wire ignores ones it cannot serve).
func (s *Section) Validate() error { return nil }

// Binds reports whether the named transport should be bound: true when Transports is
// empty (bind-all) or explicitly lists the token. The compose transport cross-wire
// consults this to gate each NetBIOS-carrying family.
func (s *Section) Binds(transport string) bool {
	return len(s.Transports) == 0 || slices.Contains(s.Transports, transport)
}

// compile-time assertion: *Section satisfies config.Section.
var _ config.Section = (*Section)(nil)

// SectionFromModel resolves the NetBIOS section from the model, falling back to a fresh
// default (empty Transports → bind-all) when the model carries none.
func SectionFromModel(m *config.Model) *Section {
	if m != nil {
		if s, ok := m.Get(SectionKey); ok {
			if ns, ok := s.(*Section); ok {
				return ns
			}
		}
	}
	return &Section{SKey: SectionKey}
}

// RegisterSection installs the NetBIOS section schema so codecs round-trip it. Kept out
// of an init() so a build excluding NetBIOS excludes the section too (called from the
// compose registry wiring).
func RegisterSection() {
	config.Register(config.SectionSchema{
		Key: SectionKey,
		New: func() config.Section { return &Section{SKey: SectionKey} },
		Validate: func(s config.Section) error {
			if ns, ok := s.(*Section); ok {
				return ns.Validate()
			}
			return nil
		},
	})
}
