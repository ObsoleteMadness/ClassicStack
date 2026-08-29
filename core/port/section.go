package port

import (
	"errors"

	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/csnet"
)

// Section is the flattened runtime view of a port's config. Ports, ApplyConfig,
// and link openers consume *Section. Codec-facing per-transport types (EtherTalkSection,
// IPXSection, …) embed only the field groups that apply to them and project onto
// Section via PortSectioner — so Save never emits IPX framing on a NetBEUI row.
//
// Optional fields carry omitempty so a Save of the flattened view (tests, legacy
// model entries) also stays free of blank keys. enabled is never omitempty: a
// missing key must not silently decode as false and disable a port.
type Section struct {
	// SKey is the section/SCHEMA key shared by every instance of a transport
	// ("EtherTalk", "LToUDP", "IPX", …). It is the registry/codec key, NOT the
	// per-instance identity — see Name.
	SKey string `toml:"-"`
	// Name is the per-INSTANCE identity (§M11). "" means the lone default instance.
	Name string `toml:"name,omitempty"`
	// Iface is the NAME of the interface this instance binds to. Empty inherits
	// the namespace's default interface (Model.DefaultInterface).
	Iface string `toml:"iface,omitempty"`
	// IsEnabled mirrors the configured-enabled flag (≠ running).
	IsEnabled bool `toml:"enabled"`

	// MAC is the station hardware address used as the Ethernet source on
	// outbound frames. "" means "use the interface hw_address, else the NIC's own MAC".
	MAC string `toml:"mac,omitempty"`

	// SeedNetwork / SeedNetworkEnd / SeedZone are AppleTalk seed config
	// (EtherTalk/LocalTalk). Zero range = non-seed.
	SeedNetwork    uint16 `toml:"seed_network,omitempty"`
	SeedNetworkEnd uint16 `toml:"seed_network_end,omitempty"`
	SeedZone       string `toml:"seed_zone,omitempty"`

	// Device / Baud / NoFlowControl are the SERIAL binding a TashTalk port opens
	// directly. RTS/CTS is on unless NoFlowControl (see adapter/serial.DefaultRTSCTS).
	Device        string `toml:"device,omitempty"`
	Baud          int    `toml:"baud,omitempty"`
	NoFlowControl bool   `toml:"no_flow_control,omitempty"`

	// IPXFrameType / IPXFrameTypes select Novell Ethernet encapsulation (IPX only).
	IPXFrameType  string   `toml:"ipx_frame_type,omitempty"`
	IPXFrameTypes []string `toml:"ipx_frame_types,omitempty"`
	// IPXNetwork is the IPX network number for this port's segment (IPX only).
	// 0 = local/unknown (mini-router default). Shared spelling with [IPXGW].
	IPXNetwork uint32 `toml:"ipx_network,omitempty"`

	// Capture / CaptureSnaplen tee this port's wire traffic to a pcap file.
	Capture        string `toml:"capture,omitempty"`
	CaptureSnaplen int    `toml:"capture_snaplen,omitempty"`

	// PaceMs is the minimum inter-frame gap in milliseconds (LocalTalk only).
	// 0 selects the transport default; negative disables pacing.
	PaceMs int `toml:"pace_ms,omitempty"`
}

// PortSectioner is the capability a typed transport section implements to project
// onto the flattened *Section runtime view. InstanceFromModel / ApplyConfig use it
// so the model can store EtherTalkSection / IPXSection / … while ports keep a
// single ApplyConfig path.
type PortSectioner interface {
	PortSection() *Section
}

// AsSection unwraps a model section into the flattened *Section runtime view.
// It accepts *Section directly, any PortSectioner (typed transport / EtherDFS),
// or nil.
func AsSection(s any) *Section {
	if s == nil {
		return nil
	}
	if ps, ok := s.(*Section); ok {
		return ps
	}
	if ps, ok := s.(PortSectioner); ok {
		return ps.PortSection()
	}
	return nil
}

// PortSection returns the receiver — *Section is already the runtime view.
func (s *Section) PortSection() *Section { return s }

// Key returns the shared SCHEMA key (the registry/codec key).
func (s *Section) Key() string { return s.SKey }

// InstanceName returns the per-instance identity (config.NamedSection).
func (s *Section) InstanceName() string {
	if s.Name != "" {
		return s.Name
	}
	return s.SKey
}

// Interface makes a port Section a config.InterfaceProvider.
func (s *Section) Interface() config.InterfaceSection {
	return config.InterfaceSection{Name: s.Iface}
}

// CapturePath implements CaptureProvider.
func (s *Section) CapturePath() string { return s.Capture }

// CaptureSnapLen implements CaptureProvider.
func (s *Section) CaptureSnapLen() int { return s.CaptureSnaplen }

// ConfiguredIPXNetwork implements IPXNetworkProvider.
func (s *Section) ConfiguredIPXNetwork() uint32 { return s.IPXNetwork }

// Clone returns a deep copy.
func (s *Section) Clone() config.Section {
	cp := *s
	cp.IPXFrameTypes = cloneIPXFrameTypes(s.IPXFrameTypes)
	return &cp
}

// Validate checks the section in isolation.
func (s *Section) Validate() error {
	if err := validateMAC(s.MAC); err != nil {
		return err
	}
	return validateSeed(SeedFields{SeedNetwork: s.SeedNetwork, SeedNetworkEnd: s.SeedNetworkEnd, SeedZone: s.SeedZone})
}

// ErrSeedRange reports a seed network range whose end precedes its start.
var ErrSeedRange = errors.New("port: seed_network_end precedes seed_network")

// ErrBadMAC reports a MAC string ParseMAC could not parse as six hex octets.
var ErrBadMAC = errors.New("port: MAC must be six hex octets, e.g. 00:11:22:aa:bb:cc")

// ParseMAC parses a colon-, dash-, dot-, or bare-hex six-octet hardware address
// into a fixed [6]byte. Delegates to core/csnet.ParseMAC (net.ParseMAC on
// desktop, hand-rolled under TinyGo), the shared implementation core/adapter/
// client MAC parsers now converge on — so a station address string is accepted
// or rejected identically everywhere in the codebase. Note this is stricter
// than the previous hand-rolled parser about octet width: "0:11:22:aa:bb:cc"
// (a single-nibble octet) is no longer accepted, matching net.ParseMAC.
func ParseMAC(s string) ([6]byte, error) {
	mac, err := csnet.ParseMAC(s)
	if err != nil {
		return [6]byte{}, ErrBadMAC
	}
	return mac, nil
}

// compile-time assertions.
var (
	_ config.Section           = (*Section)(nil)
	_ config.NamedSection      = (*Section)(nil)
	_ config.InterfaceProvider = (*Section)(nil)
	_ PortSectioner            = (*Section)(nil)
	_ CaptureProvider          = (*Section)(nil)
	_ IPXNetworkProvider       = (*Section)(nil)
)

// SectionFromModel resolves the SINGLETON section under key (Model.Sections),
// falling back to a fresh default when the model has none. Typed transport
// sections are projected via PortSectioner.
func SectionFromModel(m *config.Model, key string) *Section {
	if m != nil {
		if s, ok := m.Get(key); ok {
			if ps := AsSection(s); ps != nil {
				return ps
			}
		}
	}
	return &Section{SKey: key}
}

// InstanceFromModel resolves one repeated port instance (Model.Lists[key]) by its
// instance name. An empty instance name, or no matching instance, falls through to
// the singleton SectionFromModel. Typed transport sections are projected via
// PortSectioner.
func InstanceFromModel(m *config.Model, key, instance string) *Section {
	if m != nil && instance != "" {
		if s, ok := m.Instance(key, instance); ok {
			if ps := AsSection(s); ps != nil {
				return ps
			}
		}
	}
	return SectionFromModel(m, key)
}
