package config

import "strings"

// Model is the in-memory, mutable, serialisable representation of the whole
// ClassicStack configuration. It is the source of truth the management
// plane stages edits against and writes back to server.toml. Field names
// and `toml` tags mirror the section/key layout of server.toml so a
// round-trip through ToTOML reproduces an equivalent file (comments are not
// preserved — the UI warns about this before saving).
//
// Model lives in package config (untagged) and uses neutral volume/share
// types rather than importing service/afp or service/smb (which are behind
// build tags); the cmd-layer wiring converts between Model and those
// packages' own config structs.
type Model struct {
	Logging   LoggingModel   `toml:"Logging" json:"Logging"`
	Router    RouterModel    `toml:"Router" json:"Router"`
	Bridge    BridgeModel    `toml:"Bridge" json:"Bridge"`
	LToUDP    LToUDPModel    `toml:"LToUdp" json:"LToUdp"`
	TashTalk  TashTalkModel  `toml:"TashTalk" json:"TashTalk"`
	EtherTalk EtherTalkModel `toml:"EtherTalk" json:"EtherTalk"`
	Capture   CaptureModel   `toml:"Capture" json:"Capture"`
	MacIP     MacIPModel     `toml:"MacIP" json:"MacIP"`
	IPX       IPXModel       `toml:"IPX" json:"IPX"`
	IPXGW     IPXGWModel     `toml:"IPXGW" json:"IPXGW"`
	NetBEUI   NetBEUIModel   `toml:"NetBEUI" json:"NetBEUI"`
	NetBIOS   NetBIOSModel   `toml:"NetBIOS" json:"NetBIOS"`
	SMB       SMBModel       `toml:"SMB" json:"SMB"`
	AFP       AFPModel       `toml:"AFP" json:"AFP"`
	Shortname ShortnameModel `toml:"Shortname" json:"Shortname"`
	WebUI     WebUIModel     `toml:"WebUI" json:"WebUI"`
}

// LoggingModel is the [Logging] section.
type LoggingModel struct {
	Level        string `toml:"level" json:"level"`
	ParsePackets bool   `toml:"parse_packets" json:"parse_packets"`
	LogTraffic   bool   `toml:"log_traffic" json:"log_traffic"`
	ParseOutput  string `toml:"parse_output,omitempty" json:"parse_output,omitempty"`
}

// InterfaceModel is a virtual/physical interface definition: the link backend
// (Mode), the device it binds to, an optional hardware address, and — for the
// pcap backend — the bridge mode. It is reused by the shared [Bridge] section
// and by any protocol that defines its own [Section.Custom] interface instead
// of inheriting [Bridge].
type InterfaceModel struct {
	Mode       string `toml:"mode,omitempty" json:"mode,omitempty"`               // pcap | tap | tun (link backend)
	Device     string `toml:"device,omitempty" json:"device,omitempty"`           // pcap device name / tap device
	HWAddress  string `toml:"hw_address,omitempty" json:"hw_address,omitempty"`   // virtual hardware address
	BridgeMode string `toml:"bridge_mode,omitempty" json:"bridge_mode,omitempty"` // pcap only: auto | ethernet | wifi
}

// BridgeModel is the [Bridge] section: the shared virtual interface protocols
// inherit unless they define their own. It is an InterfaceModel; the alias
// keeps the [Bridge] section name and TOML keys unchanged.
type BridgeModel = InterfaceModel

// RouterModel is the [Router] section. It declares which transports the
// AppleTalk router binds to. Ports lists the transport section names
// ("LToUdp", "TashTalk", "EtherTalk") the router participates in; an enabled
// transport that is NOT listed runs standalone (it comes up and receives but is
// not part of the router — no RTMP/ZIP, no inter-port forwarding). An empty/
// unset Ports means "bind every enabled transport", which is the sensible
// default — a config that omits [Router] gets the full router it expects.
type RouterModel struct {
	Ports []string `toml:"ports,omitempty" json:"ports,omitempty"`
}

// Canonical [Router].ports transport names. These match the TOML section names
// so a config author lists the same identifier they configure the transport
// under.
const (
	RouterPortLToUDP    = "LToUdp"
	RouterPortTashTalk  = "TashTalk"
	RouterPortEtherTalk = "EtherTalk"
)

// BindsPort reports whether the router should attach the named transport. With
// an empty Ports list every enabled transport attaches (the default); otherwise
// only listed transports attach. Matching is case-insensitive so "ethertalk"
// and "EtherTalk" are equivalent.
func (r RouterModel) BindsPort(name string) bool {
	if len(r.Ports) == 0 {
		return true
	}
	for _, p := range r.Ports {
		if strings.EqualFold(strings.TrimSpace(p), name) {
			return true
		}
	}
	return false
}

// LToUDPModel is the [LToUdp] section.
type LToUDPModel struct {
	Enabled     bool   `toml:"enabled" json:"enabled"`
	Interface   string `toml:"interface,omitempty" json:"interface,omitempty"`
	SeedNetwork uint   `toml:"seed_network" json:"seed_network"`
	SeedZone    string `toml:"seed_zone" json:"seed_zone"`
}

// TashTalkModel is the [TashTalk] section.
type TashTalkModel struct {
	Port        string `toml:"port" json:"port"`
	SeedNetwork uint   `toml:"seed_network" json:"seed_network"`
	SeedZone    string `toml:"seed_zone" json:"seed_zone"`
}

// EtherTalkModel is the [EtherTalk] section (bridge keys live in [Bridge]).
type EtherTalkModel struct {
	BridgeHostMAC  string `toml:"bridge_host_mac,omitempty" json:"bridge_host_mac,omitempty"`
	Filter         string `toml:"filter,omitempty" json:"filter,omitempty"`
	SeedNetworkMin uint   `toml:"seed_network_min" json:"seed_network_min"`
	SeedNetworkMax uint   `toml:"seed_network_max" json:"seed_network_max"`
	SeedZone       string `toml:"seed_zone" json:"seed_zone"`
	DesiredNetwork uint   `toml:"desired_network,omitempty" json:"desired_network,omitempty"`
	DesiredNode    uint   `toml:"desired_node,omitempty" json:"desired_node,omitempty"`
}

// CaptureModel is the [Capture] section.
type CaptureModel struct {
	LocalTalk string `toml:"localtalk,omitempty" json:"localtalk,omitempty"`
	EtherTalk string `toml:"ethertalk,omitempty" json:"ethertalk,omitempty"`
	IPX       string `toml:"ipx,omitempty" json:"ipx,omitempty"`
	NetBEUI   string `toml:"netbeui,omitempty" json:"netbeui,omitempty"`
	Snaplen   uint32 `toml:"snaplen,omitempty" json:"snaplen,omitempty"`
}

// MacIPModel is the [MacIP] section.
type MacIPModel struct {
	Enabled    bool   `toml:"enabled" json:"enabled"`
	Mode       string `toml:"mode,omitempty" json:"mode,omitempty"` // pcap or nat
	Zone       string `toml:"zone,omitempty" json:"zone,omitempty"`
	NATSubnet  string `toml:"nat_subnet,omitempty" json:"nat_subnet,omitempty"`
	NATGW      string `toml:"nat_gw,omitempty" json:"nat_gw,omitempty"`
	LeaseFile  string `toml:"lease_file,omitempty" json:"lease_file,omitempty"`
	IPGateway  string `toml:"ip_gateway,omitempty" json:"ip_gateway,omitempty"`
	DHCPRelay  bool   `toml:"dhcp_relay,omitempty" json:"dhcp_relay,omitempty"`
	Nameserver string `toml:"nameserver,omitempty" json:"nameserver,omitempty"`
	Filter     string `toml:"filter,omitempty" json:"filter,omitempty"`
	// Custom, when set, is MacIP's own [MacIP.Custom] IP-side interface; nil
	// means inherit the shared [Bridge] interface. (Distinct from Mode above,
	// which selects the gateway behaviour — pcap vs nat.)
	Custom *InterfaceModel `toml:"Custom,omitempty" json:"Custom,omitempty"`
}

// IPXModel is the [IPX] section.
type IPXModel struct {
	Enabled         bool   `toml:"enabled" json:"enabled"`
	Interface       string `toml:"interface,omitempty" json:"interface,omitempty"`
	Framing         string `toml:"framing,omitempty" json:"framing,omitempty"`
	InternalNetwork string `toml:"internal_network,omitempty" json:"internal_network,omitempty"`
	Filter          string `toml:"filter,omitempty" json:"filter,omitempty"`
	// Custom, when set, is the protocol's own [IPX.Custom] interface; when nil
	// the protocol inherits the shared [Bridge] interface.
	Custom *InterfaceModel `toml:"Custom,omitempty" json:"Custom,omitempty"`
}

// IPXGWModel is the [IPXGW] section.
type IPXGWModel struct {
	Enabled  bool     `toml:"enabled" json:"enabled"`
	Bindings []string `toml:"bindings,omitempty" json:"bindings,omitempty"` // "Object:Zone" entries
}

// NetBEUIModel is the [NetBEUI] section.
type NetBEUIModel struct {
	Enabled   bool   `toml:"enabled" json:"enabled"`
	Interface string `toml:"interface,omitempty" json:"interface,omitempty"`
	Filter    string `toml:"filter,omitempty" json:"filter,omitempty"`
	// Custom, when set, is the protocol's own [NetBEUI.Custom] interface; nil
	// means inherit the shared [Bridge] interface.
	Custom *InterfaceModel `toml:"Custom,omitempty" json:"Custom,omitempty"`
}

// NetBIOSModel is the [NetBIOS] section.
type NetBIOSModel struct {
	Enabled    bool     `toml:"enabled" json:"enabled"`
	Transports []string `toml:"transports,omitempty" json:"transports,omitempty"`
	ScopeID    string   `toml:"scope_id,omitempty" json:"scope_id,omitempty"`
}

// SMBModel is the [SMB] section, including [SMB.Volumes.*] shares.
type SMBModel struct {
	Enabled       bool                  `toml:"enabled" json:"enabled"`
	NBTBinding    string                `toml:"nbt_binding,omitempty" json:"nbt_binding,omitempty"`
	DirectBinding string                `toml:"direct_binding,omitempty" json:"direct_binding,omitempty"`
	GuestOk       bool                  `toml:"guest_ok,omitempty" json:"guest_ok,omitempty"`
	ServerName    string                `toml:"server_name,omitempty" json:"server_name,omitempty"`
	Workgroup     string                `toml:"workgroup,omitempty" json:"workgroup,omitempty"`
	Volumes       map[string]ShareModel `toml:"Volumes,omitempty" json:"Volumes,omitempty"`
}

// ShareModel is one [SMB.Volumes.<key>] entry.
type ShareModel struct {
	Name     string `toml:"name,omitempty" json:"name,omitempty"`
	Path     string `toml:"path" json:"path"`
	FSType   string `toml:"fs_type,omitempty" json:"fs_type,omitempty"`
	ReadOnly bool   `toml:"read_only,omitempty" json:"read_only,omitempty"`
}

// AFPModel is the [AFP] section, including [AFP.Volumes.*] volumes.
type AFPModel struct {
	Enabled            bool                   `toml:"enabled" json:"enabled"`
	Name               string                 `toml:"name,omitempty" json:"name,omitempty"`
	Zone               string                 `toml:"zone,omitempty" json:"zone,omitempty"`
	Protocols          string                 `toml:"protocols,omitempty" json:"protocols,omitempty"`
	Binding            string                 `toml:"binding,omitempty" json:"binding,omitempty"`
	ExtensionMap       string                 `toml:"extension_map,omitempty" json:"extension_map,omitempty"`
	CNIDBackend        string                 `toml:"cnid_backend,omitempty" json:"cnid_backend,omitempty"`
	UseDecomposedNames bool                   `toml:"use_decomposed_names,omitempty" json:"use_decomposed_names,omitempty"`
	AppleDoubleMode    string                 `toml:"appledouble_mode,omitempty" json:"appledouble_mode,omitempty"`
	Volumes            map[string]VolumeModel `toml:"Volumes,omitempty" json:"Volumes,omitempty"`
}

// VolumeModel is one [AFP.Volumes.<key>] entry.
type VolumeModel struct {
	Name             string `toml:"name,omitempty" json:"name,omitempty"`
	Path             string `toml:"path,omitempty" json:"path,omitempty"`
	FSType           string `toml:"fs_type,omitempty" json:"fs_type,omitempty"`
	Password         string `toml:"password,omitempty" json:"password,omitempty"`
	ReadOnly         bool   `toml:"read_only,omitempty" json:"read_only,omitempty"`
	RebuildDesktopDB bool   `toml:"rebuild_desktop_db,omitempty" json:"rebuild_desktop_db,omitempty"`
	AppleDoubleMode  string `toml:"appledouble_mode,omitempty" json:"appledouble_mode,omitempty"`
}

// ShortnameModel is the [Shortname] section.
type ShortnameModel struct {
	WindowsShortnames bool   `toml:"windows_shortnames,omitempty" json:"windows_shortnames,omitempty"`
	Backend           string `toml:"backend,omitempty" json:"backend,omitempty"`
	DBPath            string `toml:"db_path,omitempty" json:"db_path,omitempty"`
}

// WebUIModel is the [WebUI] section.
type WebUIModel struct {
	Enabled bool   `toml:"enabled" json:"enabled"`
	Bind    string `toml:"bind,omitempty" json:"bind,omitempty"`
	TLS     bool   `toml:"tls" json:"tls"`
	CertPEM string `toml:"cert_pem,omitempty" json:"cert_pem,omitempty"`
	KeyPEM  string `toml:"key_pem,omitempty" json:"key_pem,omitempty"`
}
