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
//
// A port owns its OWN binding, because the only INTERFACE is the uplink bridge:
// EtherTalk/IPX/NetBEUI name a bridge via Iface (or inherit the default); a
// TashTalk port names its serial tty via Device/Baud directly (serial is a port
// property, not an interface); an LToUDP port rides host-wide multicast (Iface, if
// set, is an optional bind address). Seed zone/network (below) are the AppleTalk
// segment config the router reads for its member ports.
type Section struct {
	// SKey is the section/SCHEMA key shared by every instance of a transport
	// ("EtherTalk", "LToUDP", "IPX", …). It is the registry/codec key, NOT the
	// per-instance identity — see Name.
	SKey string `toml:"-"`
	// Name is the per-INSTANCE identity (§M11): a transport is a repeated section,
	// so one EtherTalk may have several named instances ("et-lab", "et-dmz"), each
	// its own port/segment/router member. "" means the lone default instance, whose
	// identity falls back to SKey (a singleton config still works).
	Name string `toml:"name"`
	// Iface is the NAME of the interface this instance binds to ("eth0", "br-lan",
	// "ttyUSB-attic"); resolved against the interface namespace (§M11). Empty
	// inherits the namespace's default interface (Model.DefaultInterface).
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

	// Device / Baud are the SERIAL binding a serial-riding port (TashTalk) opens
	// directly. A TashTalk port owns its own tty rather than resolving one from a
	// named serial interface: the operator picks a host serial port (e.g. "COM3",
	// "/dev/ttyUSB0") on the port itself, and Baud is the line speed (0 = adapter
	// default). Ignored by non-serial transports. This keeps "one interface = the
	// uplink bridge" true — a serial line is a port property, not an interface.
	Device string `toml:"device"`
	Baud   int    `toml:"baud"`

	// IPXFrameType selects the Ethernet encapsulation an IPX port uses on OUTBOUND
	// frames (Novell "frame type") when it has not learned a peer's framing. Recognised
	// values are "ethernet_ii", "802.3" (raw / Novell-Ethernet), and "802.2" (IEEE
	// 802.2 LLC). Empty defaults to Ethernet II, which is what a MacIPX client speaks —
	// see the ipx port's ParseFrameType. Inbound frames are always accepted in every
	// framing regardless of this setting, and a unicast reply is sent in the SAME frame
	// type the request arrived in (like a real NetWare server bound to several frame
	// types at once). Ignored by non-IPX transports.
	IPXFrameType string `toml:"ipx_frame_type"`

	// IPXFrameTypes optionally lists EVERY Ethernet encapsulation the IPX port advertises
	// on (SAP/RIP broadcasts are emitted once per listed frame type), so clients bound to
	// any of raw-802.3 / 802.2 / Ethernet II all discover the server — the multi-frame-type
	// binding a real NetWare server offers. Each entry uses the ParseFrameType spellings.
	// Empty falls back to the single IPXFrameType (or its Ethernet-II default). Unicast
	// replies still mirror the request's frame type regardless of this list.
	IPXFrameTypes []string `toml:"ipx_frame_types,omitempty"`

	// Capture is a pcap file path for THIS port's wire traffic ("" = no capture).
	// Capture is a property of the port that owns the segment (like Device/SeedZone),
	// not a central table: every frame the port's link reads or writes is tee'd to
	// this file, written with the transport's data-link type (Ethernet for EtherTalk,
	// LocalTalk/DLT_LTALK for LToUDP/TashTalk). CaptureSnaplen caps the bytes stored
	// per frame (0 = full frame). Best-effort: an unopenable path never fails Start.
	Capture        string `toml:"capture"`
	CaptureSnaplen int    `toml:"capture_snaplen"`

	// PaceMs is the minimum inter-frame gap, in milliseconds, enforced per
	// DESTINATION NODE on outbound frames (LocalTalk transports only: LToUDP,
	// TashTalk). A classic-Mac LLAP receiver drops frames that arrive back-to-back
	// with no gap; LToUDP has no link backpressure (RTS/CTS is synthesised locally
	// and never sent, LLAP is unacknowledged), so an open-loop per-node pace is the
	// only lever the port has to keep a fast producer (AFP bulk replies, MacIP data,
	// netboot floods) from overrunning a slow receiver. 0 selects the transport's
	// default (LToUDP: a light 3 ms floor; TashTalk self-paces on the serial line, so
	// its default is 0). A negative value disables pacing entirely. Ignored by
	// non-LocalTalk transports (EtherTalk/IPX ride real NICs with their own flow control).
	PaceMs int `toml:"pace_ms"`
}

// Key returns the shared SCHEMA key (the registry/codec key, matched per transport
// type — "EtherTalk"). Every instance of a transport shares it.
func (s *Section) Key() string { return s.SKey }

// InstanceName returns the per-instance identity (config.NamedSection): Name when
// set, else the schema key so a singleton/default port keeps a stable, deterministic
// name ("EtherTalk"). It is the name the supervisor addresses the component by and
// the name a [Router].members list references.
func (s *Section) InstanceName() string {
	if s.Name != "" {
		return s.Name
	}
	return s.SKey
}

// compile-time assertion: *Section is a NamedSection (a repeated transport instance).
var _ config.NamedSection = (*Section)(nil)

// Interface makes a port Section a config.InterfaceProvider: its Iface is a
// per-port OVERRIDE of the shared default interface (§4/§9d). An empty Iface
// yields an empty InterfaceSection, so Model.EffectiveInterface falls through to
// the namespace's default interface — i.e. several ports with no iface of their
// own all bind to the one shared default, and only a port that names its own iface
// diverges.
func (s *Section) Interface() config.InterfaceSection {
	return config.InterfaceSection{Name: s.Iface}
}

// compile-time assertion: *Section is an InterfaceProvider (interface override).
var _ config.InterfaceProvider = (*Section)(nil)

// Clone returns a deep copy.
func (s *Section) Clone() config.Section {
	cp := *s
	if s.IPXFrameTypes != nil {
		cp.IPXFrameTypes = append([]string(nil), s.IPXFrameTypes...)
	}
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
	return nil
}

// ErrSeedRange reports a seed network range whose end precedes its start.
var ErrSeedRange = errors.New("port: seed_network_end precedes seed_network")

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

// SectionFromModel resolves the SINGLETON section under key (Model.Sections),
// falling back to a fresh default when the model has none. It is the back-compat
// resolver for a transport with one default instance; InstanceFromModel is the
// repeated-instance form.
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

// InstanceFromModel resolves one repeated port instance (Model.Lists[key]) by its
// instance name. An empty instance name, or no matching instance, falls through to
// the singleton SectionFromModel — so a config that still uses a single [EtherTalk]
// section (no instance name) keeps working, and a fresh default is returned when
// neither is present. This is the resolver a port factory uses with
// BuildContext.Instance.
func InstanceFromModel(m *config.Model, key, instance string) *Section {
	if m != nil && instance != "" {
		if s, ok := m.Instance(key, instance); ok {
			if ps, ok := s.(*Section); ok {
				return ps
			}
		}
	}
	return SectionFromModel(m, key)
}
