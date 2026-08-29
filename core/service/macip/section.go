package macip

import (
	"errors"
	"strconv"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/csnet"
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
	Enabled bool `toml:"enabled" display:"Enabled" desc:"Whether the MacIP gateway is configured on." default:"false"`
	// Mode selects bridge (proxy-ARP onto an existing subnet) or nat (hand out a
	// private subnet). Empty defaults to bridge.
	Mode string `toml:"mode,omitempty" display:"Mode" desc:"bridge = proxy-ARP onto an existing subnet; nat = hand out a private subnet." example:"bridge" default:"bridge" widget:"mode"`
	// Zone is the AppleTalk zone the IPGATEWAY NBP name registers in. Empty = the
	// router's first zone (resolved at Start).
	Zone string `toml:"zone,omitempty" display:"Zone" desc:"AppleTalk zone the IPGATEWAY NBP name registers in. Empty = router's first zone." example:"EtherTalk Network" widget:"zone"`
	// GatewayIP / Network / Nameserver / Broadcast / SubnetMask are the IP-side
	// parameters advertised to MacIP clients, as dotted-quad strings.
	GatewayIP  string `toml:"gateway_ip,omitempty" display:"Gateway IP" desc:"IP address advertised to MacIP clients as their gateway." example:"192.168.1.1"`
	Network    string `toml:"network,omitempty" display:"Network" desc:"Subnet network base advertised to clients." example:"192.168.1.0"`
	Nameserver string `toml:"nameserver,omitempty" display:"Nameserver" desc:"DNS server advertised to MacIP clients." example:"192.168.1.1"`
	Broadcast  string `toml:"broadcast,omitempty" display:"Broadcast" desc:"Subnet broadcast address." example:"192.168.1.255"`
	SubnetMask string `toml:"subnet_mask,omitempty" display:"Subnet mask" desc:"IPv4 subnet mask advertised to clients." example:"255.255.255.0"`
	// HostCount is the lease-pool size (incl. the reserved gateway slot). 0 → 254.
	HostCount int `toml:"host_count,omitempty" display:"Host count" desc:"Lease-pool size including the reserved gateway slot (0 = 254)." default:"0" example:"254"`

	// ── IP-side egress (adapter) parameters ───────────────────────────────────
	// These describe the physical-network side of the gateway. They are read at the
	// compose edge to build the macipgw IPEgress adapter (proxy-ARP / NAT / DHCP-relay
	// over a pcap raw-Ethernet link); core never touches them. Empty fields are
	// auto-detected from the chosen Interface where possible.

	// Iface is the NAME of the interface (the [[interface]] namespace entry) the
	// gateway bridges IP traffic onto. The Interface() method resolves it through the
	// namespace to the real pcap device at the compose edge. Empty disables IP egress
	// → AppleTalk-only mode. The toml key stays "interface" for config compatibility.
	Iface string `toml:"interface,omitempty" display:"Interface" desc:"Named interface for IP egress. Empty = AppleTalk-only (no IP bridge)." example:"br-lan" widget:"iface"`
	// HostMAC is the IP-side Ethernet MAC the gateway sources frames from and answers
	// proxy-ARP with (colon/dash hex). Empty → auto-detected from Interface.
	HostMAC string `toml:"host_mac,omitempty" display:"Host MAC" desc:"IP-side Ethernet MAC (proxy-ARP source). Empty = auto-detect." example:"DE:AD:BE:EF:00:01"`
	// HostIP is the host's own IPv4 on the IP-side network (used for ARP probe
	// sender-IP and local identity). Empty → auto-detected from Interface.
	HostIP string `toml:"host_ip,omitempty" display:"Host IP" desc:"Host IPv4 on the IP-side network. Empty = auto-detect." example:"192.168.1.10"`
	// DefaultGateway is the IP-side upstream router used for off-subnet egress in
	// bridge mode. Empty → auto-detected (default route).
	DefaultGateway string `toml:"default_gateway,omitempty" display:"Default gateway" desc:"Upstream IPv4 router for off-subnet egress (bridge mode). Empty = auto-detect." example:"192.168.1.1"`
	// DHCPRelay makes the gateway obtain client addresses by relaying DHCP onto the
	// IP-side network (fabricating a per-Mac MAC) instead of the static pool.
	DHCPRelay bool `toml:"dhcp_relay,omitempty" display:"DHCP relay" desc:"Relay DHCP onto the IP-side network instead of the static lease pool." default:"false"`
}

// Key returns the section key.
func (s *Section) Key() string { return SectionKey }

// Interface satisfies config.InterfaceProvider so Model.EffectiveInterfaceFor
// resolves the section's Interface NAME through the [[interface]] namespace — the
// same override every pcap-bound port declares (core/port.Section.Interface). This
// is what turns the operator-friendly name ("br-lan") into the real pcap device
// (Npcap's "\Device\NPF_{GUID}") at the compose edge; without it the raw name was
// handed to libpcap and the IP-side egress silently failed to open. An empty
// Interface returns an empty reference (no override → AppleTalk-only).
func (s *Section) Interface() config.InterfaceSection {
	return config.InterfaceSection{Name: s.Iface}
}

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
			return errors.New("macip: invalid IPv4 address: " + strconv.Quote(f))
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

// EgressParams is the IP-side configuration the compose edge needs to build the
// macipgw IPEgress adapter. It is a plain DTO (strings as configured); the adapter
// parses/auto-detects. Kept here so the section is the single source of truth and the
// compose edge does not reach into Section fields directly.
type EgressParams struct {
	Interface      string // pcap device for the IP-side network ("" = no egress)
	HostMAC        string // IP-side host MAC ("" = auto-detect)
	HostIP         string // IP-side host IPv4 ("" = auto-detect)
	DefaultGateway string // upstream gateway IPv4 ("" = auto-detect)
	GatewayIP      IPv4   // gateway IP advertised to clients (pool slot 0)
	Network        IPv4   // subnet network base
	Nameserver     IPv4   // nameserver advertised to clients
	Broadcast      IPv4   // subnet broadcast
	SubnetMask     IPv4   // subnet mask
	NATEnabled     bool   // OS-stack NAT for off-subnet traffic
	DHCPRelay      bool   // relay DHCP for client addresses
	Zone           []byte // AppleTalk zone (informational; for logging)
}

// EgressParams builds the IP-side adapter DTO from the section. The compose edge calls
// this when Interface is set to construct the macipgw egress; an empty Interface means
// the gateway stays AppleTalk-only.
func (s *Section) EgressParams() EgressParams {
	parse := func(v string) IPv4 { ip, _ := ParseIPv4(v); return ip }
	return EgressParams{
		Interface:      strings.TrimSpace(s.Iface),
		HostMAC:        strings.TrimSpace(s.HostMAC),
		HostIP:         strings.TrimSpace(s.HostIP),
		DefaultGateway: strings.TrimSpace(s.DefaultGateway),
		GatewayIP:      parse(s.GatewayIP),
		Network:        parse(s.Network),
		Nameserver:     parse(s.Nameserver),
		Broadcast:      parse(s.Broadcast),
		SubnetMask:     parse(s.SubnetMask),
		NATEnabled:     s.EffectiveMode() == ModeNAT,
		DHCPRelay:      s.DHCPRelay,
		Zone:           []byte(s.Zone),
	}
}

// compile-time assertions: *Section satisfies config.Section and, so its interface
// NAME resolves through the namespace, config.InterfaceProvider.
var (
	_ config.Section           = (*Section)(nil)
	_ config.InterfaceProvider = (*Section)(nil)
)

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
		DisplayName: "MacIP gateway",
		Description: "IP-over-AppleTalk gateway (MacTCP / MacIP clients). Bridge or NAT onto a host interface; optional DHCP relay.",
	})
}

// ParseIPv4 parses a dotted-quad string into an IPv4. ok is false for a malformed
// address. Delegates to core/csnet.ParseIPv4, the shared implementation core/adapter
// IPv4 parsers now converge on (desktop wraps net.ParseIP; TinyGo hand-rolls it, so
// this package stays TinyGo-clean either way).
func ParseIPv4(s string) (IPv4, bool) {
	ip, err := csnet.ParseIPv4(s)
	return IPv4(ip), err == nil
}
