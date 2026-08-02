package port

import "github.com/ObsoleteMadness/ClassicStack/core/config"

// EtherTalkSection is the codec-facing config for an EtherTalk port instance.
// It embeds Base + AppleTalk seed + wire capture — never IPX framing or serial.
type EtherTalkSection struct {
	Base
	SeedFields
	CaptureFields
}

// PortSection projects onto the flattened runtime view.
func (s *EtherTalkSection) PortSection() *Section {
	return &Section{
		SKey: s.SKey, Name: s.Name, Iface: s.Iface, IsEnabled: s.IsEnabled, MAC: s.MAC,
		SeedNetwork: s.SeedNetwork, SeedNetworkEnd: s.SeedNetworkEnd, SeedZone: s.SeedZone,
		Capture: s.Capture, CaptureSnaplen: s.CaptureSnaplen,
	}
}

// Clone returns a deep copy.
func (s *EtherTalkSection) Clone() config.Section {
	cp := *s
	return &cp
}

// Validate checks MAC and seed range.
func (s *EtherTalkSection) Validate() error {
	if err := validateMAC(s.MAC); err != nil {
		return err
	}
	return validateSeed(s.SeedFields)
}

var (
	_ config.Section           = (*EtherTalkSection)(nil)
	_ config.NamedSection      = (*EtherTalkSection)(nil)
	_ config.InterfaceProvider = (*EtherTalkSection)(nil)
	_ PortSectioner            = (*EtherTalkSection)(nil)
	_ CaptureProvider          = (*EtherTalkSection)(nil)
	_ SeedProvider             = (*EtherTalkSection)(nil)
)

// IPXSection is the codec-facing config for an IPX port instance.
// It embeds Base + IPX framing + IPX network number + wire capture — never
// AppleTalk seed or serial.
type IPXSection struct {
	Base
	IPXFrameFields
	IPXNetworkFields
	CaptureFields
}

// PortSection projects onto the flattened runtime view.
func (s *IPXSection) PortSection() *Section {
	return &Section{
		SKey: s.SKey, Name: s.Name, Iface: s.Iface, IsEnabled: s.IsEnabled, MAC: s.MAC,
		IPXFrameType: s.IPXFrameType, IPXFrameTypes: cloneIPXFrameTypes(s.IPXFrameTypes),
		IPXNetwork: s.IPXNetwork,
		Capture:    s.Capture, CaptureSnaplen: s.CaptureSnaplen,
	}
}

// Clone returns a deep copy.
func (s *IPXSection) Clone() config.Section {
	cp := *s
	cp.IPXFrameTypes = cloneIPXFrameTypes(s.IPXFrameTypes)
	return &cp
}

// Validate checks MAC.
func (s *IPXSection) Validate() error { return validateMAC(s.MAC) }

var (
	_ config.Section           = (*IPXSection)(nil)
	_ config.NamedSection      = (*IPXSection)(nil)
	_ config.InterfaceProvider = (*IPXSection)(nil)
	_ PortSectioner            = (*IPXSection)(nil)
	_ CaptureProvider          = (*IPXSection)(nil)
	_ IPXNetworkProvider       = (*IPXSection)(nil)
)

// NetBEUISection is the codec-facing config for a NetBEUI port instance.
// It embeds Base + wire capture — never IPX framing, AppleTalk seed, or serial.
type NetBEUISection struct {
	Base
	CaptureFields
}

// PortSection projects onto the flattened runtime view.
func (s *NetBEUISection) PortSection() *Section {
	return &Section{
		SKey: s.SKey, Name: s.Name, Iface: s.Iface, IsEnabled: s.IsEnabled, MAC: s.MAC,
		Capture: s.Capture, CaptureSnaplen: s.CaptureSnaplen,
	}
}

// Clone returns a deep copy.
func (s *NetBEUISection) Clone() config.Section {
	cp := *s
	return &cp
}

// Validate checks MAC.
func (s *NetBEUISection) Validate() error { return validateMAC(s.MAC) }

var (
	_ config.Section           = (*NetBEUISection)(nil)
	_ config.NamedSection      = (*NetBEUISection)(nil)
	_ config.InterfaceProvider = (*NetBEUISection)(nil)
	_ PortSectioner            = (*NetBEUISection)(nil)
	_ CaptureProvider          = (*NetBEUISection)(nil)
)

// LToUDPSection is the codec-facing config for an LToUDP LocalTalk port instance.
// It embeds Base + AppleTalk seed + capture + pacing — never IPX framing or serial.
type LToUDPSection struct {
	Base
	SeedFields
	CaptureFields
	// PaceMs is the minimum inter-frame gap in milliseconds (LocalTalk). 0 = transport default.
	PaceMs int `toml:"pace_ms,omitempty" display:"Pace (ms)" desc:"Minimum inter-frame gap per destination on LocalTalk. 0 = transport default; negative disables pacing." default:"0" example:"30" capability:"localtalk_pace"`
}

// PortSection projects onto the flattened runtime view.
func (s *LToUDPSection) PortSection() *Section {
	return &Section{
		SKey: s.SKey, Name: s.Name, Iface: s.Iface, IsEnabled: s.IsEnabled, MAC: s.MAC,
		SeedNetwork: s.SeedNetwork, SeedNetworkEnd: s.SeedNetworkEnd, SeedZone: s.SeedZone,
		Capture: s.Capture, CaptureSnaplen: s.CaptureSnaplen,
		PaceMs: s.PaceMs,
	}
}

// Clone returns a deep copy.
func (s *LToUDPSection) Clone() config.Section {
	cp := *s
	return &cp
}

// Validate checks MAC and seed range.
func (s *LToUDPSection) Validate() error {
	if err := validateMAC(s.MAC); err != nil {
		return err
	}
	return validateSeed(s.SeedFields)
}

var (
	_ config.Section           = (*LToUDPSection)(nil)
	_ config.NamedSection      = (*LToUDPSection)(nil)
	_ config.InterfaceProvider = (*LToUDPSection)(nil)
	_ PortSectioner            = (*LToUDPSection)(nil)
	_ CaptureProvider          = (*LToUDPSection)(nil)
	_ SeedProvider             = (*LToUDPSection)(nil)
)

// TashTalkSection is the codec-facing config for a TashTalk LocalTalk port instance.
// It embeds Base + serial binding + AppleTalk seed + capture + pacing.
type TashTalkSection struct {
	Base
	SerialFields
	SeedFields
	CaptureFields
	// PaceMs is the minimum inter-frame gap in milliseconds (LocalTalk). 0 = transport default.
	PaceMs int `toml:"pace_ms,omitempty" display:"Pace (ms)" desc:"Minimum inter-frame gap per destination on LocalTalk. 0 = transport default; negative disables pacing." default:"0" example:"30" capability:"localtalk_pace"`
}

// PortSection projects onto the flattened runtime view.
func (s *TashTalkSection) PortSection() *Section {
	return &Section{
		SKey: s.SKey, Name: s.Name, Iface: s.Iface, IsEnabled: s.IsEnabled, MAC: s.MAC,
		Device: s.Device, Baud: s.Baud,
		SeedNetwork: s.SeedNetwork, SeedNetworkEnd: s.SeedNetworkEnd, SeedZone: s.SeedZone,
		Capture: s.Capture, CaptureSnaplen: s.CaptureSnaplen,
		PaceMs: s.PaceMs,
	}
}

// Clone returns a deep copy.
func (s *TashTalkSection) Clone() config.Section {
	cp := *s
	return &cp
}

// Validate checks MAC and seed range.
func (s *TashTalkSection) Validate() error {
	if err := validateMAC(s.MAC); err != nil {
		return err
	}
	return validateSeed(s.SeedFields)
}

var (
	_ config.Section           = (*TashTalkSection)(nil)
	_ config.NamedSection      = (*TashTalkSection)(nil)
	_ config.InterfaceProvider = (*TashTalkSection)(nil)
	_ PortSectioner            = (*TashTalkSection)(nil)
	_ CaptureProvider          = (*TashTalkSection)(nil)
	_ SeedProvider             = (*TashTalkSection)(nil)
)
