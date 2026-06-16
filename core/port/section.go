package port

import (
	"errors"

	"github.com/ObsoleteMadness/ClassicStack/core/config"
)

// Section is the typed config one placeholder port carries. It satisfies
// config.Section (§4) so the model can stage/round-trip it and the supervisor
// can hand it back via ApplyConfig.
//
// One Section type serves every transport port (EtherTalk, LocalTalk, IPX,
// NetBEUI). The fields below are a superset: a given transport reads only the
// ones that apply to it and ignores the rest (an LToUDP LocalTalk port has no
// MAC; an IPX port has no AppleTalk seed network). This keeps a single
// stage/round-trip/ApplyConfig path rather than one section type per transport —
// the same "placeholder accepts anything" stance the schema already takes.
type Section struct {
	// SKey is the section/component key ("EtherTalk", "LocalTalk", …).
	SKey string `toml:"-"`
	// Iface is the bound interface ("eth0", "ipx:0550", …).
	Iface string `toml:"iface"`
	// IsEnabled mirrors the configured-enabled flag (≠ running).
	IsEnabled bool `toml:"enabled"`

	// MAC is the station hardware address used as the Ethernet source on
	// outbound frames (EtherTalk; consumed by the framing.EtherTalk SrcMAC).
	// "" means "use the interface's own MAC" — the device-link builder resolves
	// it at open time. Written as the canonical colon-hex form ("00:11:22:aa:bb:cc").
	MAC string `toml:"mac"`

	// SeedNetwork / SeedNetworkEnd bound the AppleTalk network number range this
	// port seeds (EtherTalk/LocalTalk). A zero range means "non-seed": learn the
	// network from a peer router rather than asserting one. SeedNetworkEnd == 0 is
	// taken as a single-number range (== SeedNetwork) for an extended network.
	SeedNetwork    uint16 `toml:"seed_network"`
	SeedNetworkEnd uint16 `toml:"seed_network_end"`
	// SeedZone is the default zone name this port seeds ("" = non-seed / inherit).
	SeedZone string `toml:"seed_zone"`

	// Transport selects the physical medium for a port that has more than one
	// (currently only LocalTalk: LToUDP multicast vs TashTalk serial). "" defaults
	// to the port's primary transport (LocalTalk → LToUDP). The meaning of Iface
	// follows from it: for LToUDP it is the local IPv4 ADDRESS to bind/join on, for
	// serial it is the device path (COM3, /dev/ttyUSB0). Single-transport ports
	// (EtherTalk/IPX/NetBEUI) ignore this field.
	Transport string `toml:"transport"`
}

// LocalTalk transport selectors carried in Section.Transport.
const (
	TransportLToUDP = "ltoudp" // LocalTalk over UDP multicast (the default)
	TransportSerial = "serial" // LocalTalk over a TashTalk serial line
)

// Key returns the section key (matches the component/registry name).
func (s *Section) Key() string { return s.SKey }

// Interface makes a port Section a config.InterfaceProvider: its Iface is a
// per-port OVERRIDE of the shared Bridge interface (§4/§9d). An empty Iface
// yields an empty InterfaceSection, so Model.EffectiveInterface falls through to
// the Bridge NIC — i.e. several ports with no iface of their own all bind to the
// one shared interface, and only a port that names its own iface diverges.
func (s *Section) Interface() config.InterfaceSection {
	return config.InterfaceSection{Name: s.Iface}
}

// compile-time assertion: *Section is an InterfaceProvider (bridge override).
var _ config.InterfaceProvider = (*Section)(nil)

// Clone returns a deep copy.
func (s *Section) Clone() config.Section {
	cp := *s
	return &cp
}

// Validate checks the section in isolation. It accepts a placeholder/disabled
// section freely; for an enabled one it only rejects values that cannot be
// turned into a live link: a malformed MAC, or a seed range whose end precedes
// its start. Cross-field/seed-vs-router consistency is the model-level concern.
func (s *Section) Validate() error {
	if s.MAC != "" {
		if _, err := ParseMAC(s.MAC); err != nil {
			return err
		}
	}
	if s.SeedNetworkEnd != 0 && s.SeedNetworkEnd < s.SeedNetwork {
		return ErrSeedRange
	}
	switch s.Transport {
	case "", TransportLToUDP, TransportSerial:
		// "" = port default; the two named selectors are the only valid values.
	default:
		return ErrBadTransport
	}
	return nil
}

// ErrSeedRange reports a seed network range whose end precedes its start.
var ErrSeedRange = errors.New("port: seed_network_end precedes seed_network")

// ErrBadTransport reports a Transport value that is not "" or a known selector.
var ErrBadTransport = errors.New(`port: transport must be "", "ltoudp", or "serial"`)

// ErrBadMAC reports a MAC string that is not six colon- or dash-separated hex octets.
var ErrBadMAC = errors.New("port: MAC must be six hex octets, e.g. 00:11:22:aa:bb:cc")

// ParseMAC parses a colon- or dash-separated six-octet hardware address into a
// fixed [6]byte. It is hand-rolled rather than using net.ParseMAC so core stays
// free of net (TinyGo / allocation discipline) and accepts only the EUI-48 form
// a station address takes. Hex is case-insensitive.
func ParseMAC(s string) ([6]byte, error) {
	var mac [6]byte
	idx, nibbles := 0, 0
	var cur byte
	flush := func() bool {
		if nibbles == 0 || idx > 5 {
			return false
		}
		mac[idx] = cur
		idx++
		cur, nibbles = 0, 0
		return true
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ':' || c == '-' {
			if !flush() {
				return [6]byte{}, ErrBadMAC
			}
			continue
		}
		v, ok := hexNibble(c)
		if !ok || nibbles >= 2 {
			return [6]byte{}, ErrBadMAC
		}
		cur = cur<<4 | v
		nibbles++
	}
	if !flush() {
		return [6]byte{}, ErrBadMAC
	}
	if idx != 6 {
		return [6]byte{}, ErrBadMAC
	}
	return mac, nil
}

// hexNibble maps a single hex digit to its 0–15 value.
func hexNibble(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

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
