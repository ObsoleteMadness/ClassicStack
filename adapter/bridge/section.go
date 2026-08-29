package bridge

import (
	"errors"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
)

// SectionKey is the config-section / registry key for the proxy-AARP bridge. It matches
// the component Name ("ProxyAARP"), the singleton convention.
const SectionKey = Name

// Section is the proxy-AARP bridge's singleton config: the two interfaces it bridges and
// the egress MAC AARP Replies are rewritten to. TunnelInterface is the tunnel/local side
// (a wired NIC or tunnel); EgressInterface is the egress side (typically Wi-Fi). Both are
// interface-namespace NAMES resolved against the model, like a port's Iface. EgressMAC is
// the egress interface's own hardware address; empty means "use the interface's own MAC",
// resolved at open time by the device-link builder. Satisfies config.Section.
type Section struct {
	// SKey is the section key; always "ProxyAARP". Stored so Key() is a plain getter.
	SKey string `toml:"-"`
	// Enabled gates the bridge. A disabled section builds no component.
	Enabled bool `toml:"enabled"`
	// TunnelInterface is the interface-namespace name of the tunnel/local side (the side
	// whose AARP Replies get rewritten toward egress). Empty disables the bridge.
	TunnelInterface string `toml:"tunnel_interface"`
	// EgressInterface is the interface-namespace name of the egress side (typically the
	// Wi-Fi interface). Empty disables the bridge.
	EgressInterface string `toml:"egress_interface"`
	// EgressMAC is the egress interface's Ethernet MAC — the address AARP Replies (and
	// their outer Ethernet source) are rewritten to. Colon/dash hex. Empty → resolved
	// from the egress interface's own hardware address at open time.
	EgressMAC string `toml:"egress_mac"`
}

// Key returns the section key.
func (s *Section) Key() string { return SectionKey }

// Clone returns a deep copy (all fields are value types).
func (s *Section) Clone() config.Section {
	cp := *s
	return &cp
}

// Validate checks the section in isolation: when enabled, both interfaces must be named
// and the egress MAC, if set, must parse. An unset egress MAC is allowed (auto-detected).
func (s *Section) Validate() error {
	if !s.Enabled {
		return nil
	}
	if strings.TrimSpace(s.TunnelInterface) == "" || strings.TrimSpace(s.EgressInterface) == "" {
		return errors.New("proxyaarp: both tunnel_interface and egress_interface are required when enabled")
	}
	if strings.TrimSpace(s.EgressMAC) != "" {
		if _, err := port.ParseMAC(s.EgressMAC); err != nil {
			return errors.New("proxyaarp: invalid egress_mac: " + strings.TrimSpace(s.EgressMAC))
		}
	}
	return nil
}

// compile-time assertion: *Section satisfies config.Section.
var _ config.Section = (*Section)(nil)

// SectionFromModel resolves the ProxyAARP section from the model, or nil when none is set.
func SectionFromModel(m *config.Model) *Section {
	if m == nil {
		return nil
	}
	if s, ok := m.Get(SectionKey); ok {
		if bs, ok := s.(*Section); ok {
			return bs
		}
	}
	return nil
}

// RegisterSection installs the ProxyAARP section schema so codecs round-trip it. Called
// from the compose registry wiring (kept out of an init() so a build excluding the bridge
// excludes the section too).
func RegisterSection() {
	config.Register(config.SectionSchema{
		Key: SectionKey,
		New: func() config.Section { return &Section{SKey: SectionKey} },
		Validate: func(s config.Section) error {
			if bs, ok := s.(*Section); ok {
				return bs.Validate()
			}
			return nil
		},
	})
}
