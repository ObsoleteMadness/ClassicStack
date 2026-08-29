package etherdfs

import (
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
)

// ServerKey is the config-section / registry name for EtherDFS's server-level
// settings. It is the SINGLETON section (one per server), distinct from DrivesKey
// (the repeated per-drive schema). Because the EtherDFS service is BOTH the wire
// endpoint and the file server (it embeds the frame port), this section also
// carries the wire binding: the NIC to bind, an optional MAC override, and the
// enabled flag.
const ServerKey = "EtherDFS"

// ServerSection is EtherDFS's singleton server config: the wire binding plus the
// advertised server name. It embeds port.CaptureFields (the CaptureProvider
// capability) so wire dumps share the same TOML keys and compose path as the
// other NIC transports, without re-declaring capture fields.
type ServerSection struct {
	// SKey is the section key; always "EtherDFS". Stored so Key() is a plain getter.
	SKey string `toml:"-"`
	// IsEnabled mirrors the configured-enabled flag (≠ running). A disabled section
	// builds the service inert (no link), like a disabled port.
	IsEnabled bool `toml:"enabled" display:"Enabled" desc:"Whether EtherDFS is configured on (≠ currently running)."`
	// Interface is the NAME of the NIC the EtherDFS service binds to ("eth0",
	// "br-lan"); resolved against the interface namespace. Empty inherits the
	// default Bridge interface.
	Interface string `toml:"iface,omitempty" display:"Interface" desc:"NIC this service binds to. Empty inherits the default Bridge interface." widget:"iface" example:"br-lan"`
	// MAC is the station hardware address used as the Ethernet source on outbound
	// reply frames, and the address inbound frames must target (besides broadcast).
	// "" means "use the interface's own MAC", resolved at open time.
	MAC string `toml:"mac,omitempty" display:"Station MAC" desc:"Ethernet source/target address for EtherDFS. Empty = use the NIC's own MAC." example:"00:11:22:33:44:55"`
	// ServerName is the name advertised in AL_INSTALLCHK replies. Empty falls back
	// to the shared Identity.Hostname.
	ServerName string `toml:"server_name,omitempty" display:"Server name" desc:"Name advertised to EtherDFS clients. Empty falls back to the host name." example:"CLASSICSTACK"`

	port.CaptureFields
}

// Key returns the section key.
func (s *ServerSection) Key() string { return ServerKey }

// Clone returns a deep copy (all fields are values).
func (s *ServerSection) Clone() config.Section {
	cp := *s
	return &cp
}

// Validate checks the section in isolation: a configured MAC must parse.
func (s *ServerSection) Validate() error {
	if s.MAC != "" {
		if _, err := port.ParseMAC(s.MAC); err != nil {
			return err
		}
	}
	return nil
}

// PortSection projects the server section onto a port.Section so the embedded
// frame port and Model.EffectiveInterfaceFor consume the same fields the other
// raw-L2 transports do (Iface/MAC/IsEnabled). The instance name is the schema key
// (EtherDFS is a singleton wire endpoint, not a repeated port).
func (s *ServerSection) PortSection() *port.Section {
	return &port.Section{
		SKey:           ServerKey,
		Iface:          s.Interface,
		MAC:            s.MAC,
		IsEnabled:      s.IsEnabled,
		Capture:        s.Capture,
		CaptureSnaplen: s.CaptureSnaplen,
	}
}

// compile-time assertions.
var (
	_ config.Section       = (*ServerSection)(nil)
	_ port.PortSectioner   = (*ServerSection)(nil)
	_ port.CaptureProvider = (*ServerSection)(nil)
)

// ServerSectionFromModel resolves the EtherDFS server section from the model,
// falling back to a fresh disabled default when the model carries none.
func ServerSectionFromModel(m *config.Model) *ServerSection {
	if m != nil {
		if s, ok := m.Get(ServerKey); ok {
			if ss, ok := s.(*ServerSection); ok {
				return ss
			}
		}
	}
	return &ServerSection{SKey: ServerKey}
}

// RegisterServer installs the EtherDFS server-section schema so codecs round-trip
// it. Kept out of an init() so a build excluding EtherDFS excludes the section
// too (called from the compose registry wiring, like RegisterDrives).
func RegisterServer() {
	config.Register(config.SectionSchema{
		Key: ServerKey,
		New: func() config.Section { return &ServerSection{SKey: ServerKey} },
		Validate: func(s config.Section) error {
			if ss, ok := s.(*ServerSection); ok {
				return ss.Validate()
			}
			return nil
		},
	})
}
