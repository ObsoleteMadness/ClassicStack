package macip

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/core/config"
)

// SectionKey is the config-section / registry name for the MacIP gateway. It matches
// the component Name ("MacIP"), the singleton convention. MacIP previously had NO
// config section (the service was an inert placeholder); this makes its IP-side
// identity + mode operator-editable.
const SectionKey = Name

// Gateway modes for Section.Mode.
const (
	ModeBridge = "bridge" // proxy-ARP / raw-Ethernet bridge onto an existing subnet
	ModeNAT    = "nat"    // hand out a private subnet and NAT to the upstream
)

// Section is the MacIP gateway's singleton config: the IP-side identity advertised to
// MacIP clients plus the gateway mode. IP fields are dotted-quad strings (operator-
// friendly); ToConfig parses them to the service's IPv4 [4]byte form. Satisfies
// config.Section so the model round-trips it.
type Section struct {
	// SKey is the section key; always "MacIP". Stored so Key() is a plain getter.
	SKey string `toml:"-"`
	// Enabled gates the gateway. A disabled section builds no service (the registry
	// returns nil), matching the other optional services.
	Enabled bool `toml:"enabled"`
	// Mode selects bridge (proxy-ARP onto an existing subnet) or nat (hand out a
	// private subnet). Empty defaults to bridge.
	Mode string `toml:"mode"`
	// Zone is the AppleTalk zone the IPGATEWAY NBP name registers in. Empty = the
	// router's first zone (resolved at Start).
	Zone string `toml:"zone"`
	// GatewayIP / Network / Nameserver / Broadcast / SubnetMask are the IP-side
	// parameters advertised to MacIP clients, as dotted-quad strings.
	GatewayIP  string `toml:"gateway_ip"`
	Network    string `toml:"network"`
	Nameserver string `toml:"nameserver"`
	Broadcast  string `toml:"broadcast"`
	SubnetMask string `toml:"subnet_mask"`
	// HostCount is the lease-pool size (incl. the reserved gateway slot). 0 → 254.
	HostCount int `toml:"host_count"`
}

// Key returns the section key.
func (s *Section) Key() string { return SectionKey }

// Clone returns a deep copy (all fields are value types).
func (s *Section) Clone() config.Section {
	cp := *s
	return &cp
}

// Validate checks the section in isolation: when enabled, the IP fields that are set
// must parse as dotted quads. Empty fields are allowed (defaults / resolved elsewhere).
func (s *Section) Validate() error {
	for _, f := range []string{s.GatewayIP, s.Network, s.Nameserver, s.Broadcast, s.SubnetMask} {
		if f == "" {
			continue
		}
		if _, ok := ParseIPv4(f); !ok {
			return fmt.Errorf("macip: invalid IPv4 address: %q", f)
		}
	}
	return nil
}

// EffectiveMode returns the gateway mode, defaulting to bridge.
func (s *Section) EffectiveMode() string {
	if s.Mode == "" {
		return ModeBridge
	}
	return s.Mode
}

// ToConfig builds the service Config from the section, parsing the dotted-quad strings
// (a bad/empty field yields the zero IPv4, which the service treats as unset).
func (s *Section) ToConfig() Config {
	parse := func(v string) IPv4 { ip, _ := ParseIPv4(v); return ip }
	return Config{
		GatewayIP:  parse(s.GatewayIP),
		Network:    parse(s.Network),
		Nameserver: parse(s.Nameserver),
		Broadcast:  parse(s.Broadcast),
		SubnetMask: parse(s.SubnetMask),
		HostCount:  s.HostCount,
		Zone:       []byte(s.Zone),
		NATEnabled: s.EffectiveMode() == ModeNAT,
	}
}

// compile-time assertion: *Section satisfies config.Section.
var _ config.Section = (*Section)(nil)

// SectionFromModel resolves the MacIP section from the model, or nil when none is set.
func SectionFromModel(m *config.Model) *Section {
	if m != nil {
		if s, ok := m.Get(SectionKey); ok {
			if ms, ok := s.(*Section); ok {
				return ms
			}
		}
	}
	return nil
}

// RegisterSection installs the MacIP section schema so codecs round-trip it. Called
// from the compose registry wiring (kept out of an init() so a build excluding MacIP
// excludes the section too).
func RegisterSection() {
	config.Register(config.SectionSchema{
		Key: SectionKey,
		New: func() config.Section { return &Section{SKey: SectionKey} },
		Validate: func(s config.Section) error {
			if ms, ok := s.(*Section); ok {
				return ms.Validate()
			}
			return nil
		},
	})
}

// ParseIPv4 parses a dotted-quad string into an IPv4. ok is false for a malformed
// address. Reflection-free (no net package — core stays TinyGo-clean).
func ParseIPv4(s string) (IPv4, bool) {
	parts := strings.Split(s, ".")
	if len(parts) != 4 {
		return IPv4{}, false
	}
	var ip IPv4
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || n > 255 {
			return IPv4{}, false
		}
		ip[i] = byte(n)
	}
	return ip, true
}
