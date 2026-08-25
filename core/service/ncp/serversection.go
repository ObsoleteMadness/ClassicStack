package ncp

import (
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/core/config"
)

// ServerKey is the config-section / registry name for NCP's server-level settings.
// It is the SINGLETON section (one per server), distinct from VolumesKey (the
// repeated per-volume schema). The component Name ("NCP") matches the section key.
const ServerKey = Name

// ServerSection is NCP's singleton server config: the advertised NetWare server
// name / description and the internal IPX network used for SAP + GetLocalTarget
// discovery (spec/17-ncp.md). Volumes are the separate repeated VolumesKey schema.
//
// An empty ServerName falls back to config.Identity.Hostname (upper-cased on the
// wire); an empty Description falls back to Identity.Description. InternalNetwork
// 0 means "derive from the station MAC" (the compose default).
type ServerSection struct {
	// SKey is the section key; always "NCP". Stored so Key() is a plain getter.
	SKey string `toml:"-"`
	// Enabled gates the NCP service (component.Enableable). Missing key keeps the
	// New() default of true so existing configs without enabled= stay on.
	Enabled bool `toml:"enabled" display:"Enabled" desc:"Whether the NCP (NetWare) file service is configured on." default:"true"`
	// ServerName is the NetWare file-server name advertised via SAP and Get Server
	// Info. Empty → Identity.Hostname, then the built-in default ("CLASSICSTACK").
	ServerName string `toml:"server_name,omitempty" display:"Server name" desc:"NetWare server name (upper-cased on the wire). Empty = Identity.Hostname, then CLASSICSTACK." example:"FILESERVER"`
	// Description is an optional free-text remark reported to clients. Empty →
	// Identity.Description.
	Description string `toml:"description,omitempty" display:"Description" desc:"Optional server description. Empty = Identity.Description." example:"ClassicStack NetWare server"`
	// InternalNetwork is the NetWare internal IPX network number (decimal). Clients
	// learn this via SAP and then RIP GetLocalTarget before opening NCP. 0 = derive
	// from the station MAC (auto). Same spirit as mars_nwe AUTO mode.
	InternalNetwork uint32 `toml:"internal_network,omitempty" display:"Internal network" desc:"NetWare internal IPX network number (decimal). 0 = derive from the station MAC." example:"1"`
}

// compile-time assertion: *ServerSection satisfies config.Section.
var _ config.Section = (*ServerSection)(nil)

// Key returns the section key.
func (s *ServerSection) Key() string { return ServerKey }

// Clone returns a deep copy (all fields are values).
func (s *ServerSection) Clone() config.Section {
	cp := *s
	return &cp
}

// Validate checks the section in isolation. No hard constraints: an empty name is
// resolved at wire time via Identity, and InternalNetwork 0 is the auto default.
func (s *ServerSection) Validate() error { return nil }

// EffectiveServerName resolves the advertised name: the explicit ServerName, else
// the shared host name, else "" (the service then applies its built-in default).
func (s *ServerSection) EffectiveServerName(identityHostname string) string {
	if n := strings.TrimSpace(s.ServerName); n != "" {
		return n
	}
	return strings.TrimSpace(identityHostname)
}

// EffectiveDescription resolves the advertised description: the explicit
// Description, else the shared Identity description.
func (s *ServerSection) EffectiveDescription(identityDescription string) string {
	if d := strings.TrimSpace(s.Description); d != "" {
		return d
	}
	return identityDescription
}

// InternalNetworkBytes returns the configured internal network as a 4-byte big-
// endian IPX network number, or a zero value when InternalNetwork is 0 (caller
// should then derive from the station MAC).
func (s *ServerSection) InternalNetworkBytes() (net [4]byte, ok bool) {
	if s == nil || s.InternalNetwork == 0 {
		return net, false
	}
	// Hand-rolled big-endian: core/ bans encoding/binary (pulls in reflect) — see the
	// archtest §1 rule and core/protocol/ddp.
	n := s.InternalNetwork
	net[0], net[1], net[2], net[3] = byte(n>>24), byte(n>>16), byte(n>>8), byte(n)
	return net, true
}

// ServerSectionFromModel resolves the NCP server section from the model, falling
// back to a fresh default when the model carries none.
func ServerSectionFromModel(m *config.Model) *ServerSection {
	if m != nil {
		if s, ok := m.Get(ServerKey); ok {
			if ss, ok := s.(*ServerSection); ok {
				return ss
			}
		}
	}
	return &ServerSection{SKey: ServerKey, Enabled: true}
}

// RegisterServer installs the NCP server-section schema so codecs round-trip it.
// Kept out of an init() so a build excluding NCP excludes the section too (called
// from the compose registry wiring, like RegisterVolumes).
func RegisterServer() {
	config.Register(config.SectionSchema{
		Key: ServerKey,
		New: func() config.Section { return &ServerSection{SKey: ServerKey, Enabled: true} },
		Validate: func(s config.Section) error {
			if ss, ok := s.(*ServerSection); ok {
				return ss.Validate()
			}
			return nil
		},
		DisplayName: "NCP (NetWare) server",
		Description: "NetWare 3.x file server identity and internal IPX network for SAP/RIP discovery.",
	})
}
