package afp

import (
	"slices"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/core/config"
)

// ServerKey is the config-section / registry name for AFP's server-level settings.
// It is the SINGLETON section (one per server), distinct from VolumesKey (the
// repeated per-volume schema): VolumesKey carries the exported volumes, ServerKey the
// advertised identity (name/zone) and the transports the AFP service binds. It matches
// the component Name ("AFP"), the singleton convention (component name == section key).
//
// Server identity here is AFP-SPECIFIC on purpose: unlike SMB/NetBIOS — which share one
// host name via config.Identity (§4-bis) so they cannot diverge on the wire — the AFP
// server name is a Chooser-visible AppleTalk NBP name (the name a Mac sees in the
// Chooser), historically distinct from a machine's SMB/NetBIOS name. An empty ServerName
// falls back to config.Identity.Hostname, then to the built-in default, so an operator
// who wants one name everywhere just sets Identity.Hostname.
const ServerKey = Name

// Transport tokens for ServerSection.Transports. AFP rides two transport stacks
// (the two-stack design, package doc): the CLASSIC stack (DDP→ATP→ASP→AFP, joined to
// the AppleTalk router by membership) and the MODERN stack (TCP→DSI→AFP, the "AFP over
// TCP" Bonjour-era path). The list names which the operator wants bound; an empty list
// means "bind whatever transports were built" (the historical implicit behaviour), so
// an unset section keeps prior deployments working.
const (
	TransportDDP = "ddp" // classic: AFP over ASP/ATP/DDP — joins the AppleTalk router
	TransportTCP = "tcp" // modern: AFP over DSI/TCP (port 548)
)

// DefaultDSITCPAddr is the conventional AFP-over-TCP (DSI) listen address (:548). Like
// SMB's :445, it is a documented convention seeding the UI placeholder, NOT an automatic
// default — the DSI/TCP transport (adapter/dsi) binds only an EXPLICITLY configured
// tcp_addr; an empty TCPAddr leaves it inert, the same graceful degradation as a
// disabled link backend.
const DefaultDSITCPAddr = ":548"

// ServerSection is AFP's singleton server config: the advertised identity (name/zone)
// and which transports to bind. It is a flat, codec-friendly view satisfying
// config.Section so the model round-trips it. Volumes are the separate repeated
// VolumesKey schema.
type ServerSection struct {
	// AKey is the section key; always "AFP". Stored so Key() is a plain getter.
	AKey string `toml:"-"`
	// Enabled gates the AFP service (component.Enableable). Missing key keeps the
	// New() default of true so existing configs without enabled= stay on.
	Enabled bool `toml:"enabled" display:"Enabled" desc:"Whether the AFP file service is configured on." default:"true"`
	// ServerName is the AppleTalk/Chooser name this server advertises in
	// FPGetSrvrInfo / ASPGetStatus. Empty → fall back to config.Identity.Hostname,
	// then the built-in default ("ClassicStack").
	ServerName string `toml:"server_name,omitempty" display:"Server name" desc:"Chooser/NBP name. Empty = Identity.Hostname, then the built-in default." example:"File Server"`
	// Zone is the AppleTalk zone the AFP service advertises into (NBP registration).
	// Empty → the router's default zone.
	Zone string `toml:"zone,omitempty" display:"Zone" desc:"AppleTalk zone for NBP registration. Empty = router's default zone." example:"EtherTalk Network" widget:"zone"`
	// Transports lists the transport tokens (ddp/tcp) the AFP service binds. Empty =
	// bind every transport that was built (back-compat).
	Transports []string `toml:"transports,omitempty" display:"Transports" desc:"ddp and/or tcp. Empty = bind every transport built into this binary." example:"ddp,tcp"`
	// TCPAddr overrides the modern DSI/TCP (:548) listen address. Empty = do not bind
	// DSI/TCP (no implicit :548) — see adapter/dsi and spec/21-dsi.md.
	TCPAddr string `toml:"tcp_addr,omitempty" display:"TCP address" desc:"AFP-over-TCP (DSI) listen address. Empty = do not bind :548." example:":548"`
	// LoginMessage is the opt-in greeting served as the AFP login message
	// (FPGetSrvrMsg type 0): clients fetch and display it when mounting a volume.
	// Empty (the default) serves no greeting. Truncated on the wire to the AFP
	// 199-byte limit, MacRoman-encoded.
	LoginMessage string `toml:"login_message,omitempty" display:"Login message" desc:"Optional greeting shown when a client mounts a volume." example:"Welcome"`
}

// compile-time assertion: *ServerSection satisfies config.Section.
var _ config.Section = (*ServerSection)(nil)

// Key returns the section key.
func (s *ServerSection) Key() string { return ServerKey }

// Clone returns a deep copy (Transports is the only reference field).
func (s *ServerSection) Clone() config.Section {
	cp := *s
	cp.Transports = append([]string(nil), s.Transports...)
	return &cp
}

// Validate checks the section in isolation. Unknown transport tokens are tolerated
// (the compose wiring ignores ones it cannot serve), so a config naming a transport a
// given build lacks does not hard-fail the model.
func (s *ServerSection) Validate() error { return nil }

// Binds reports whether the named transport should be bound: true when Transports is
// empty (bind-all back-compat) or explicitly lists the token. The compose wiring
// consults this to gate the classic (ddp) and modern (tcp) stacks.
func (s *ServerSection) Binds(transport string) bool {
	return len(s.Transports) == 0 || slices.Contains(s.Transports, transport)
}

// DSITCPAddr returns the configured modern DSI/TCP listen address, or "" when none is
// set. It does NOT fall back to :548 — an empty result means "do not bind DSI/TCP".
func (s *ServerSection) DSITCPAddr() string { return s.TCPAddr }

// EffectiveServerName resolves the advertised name: the explicit ServerName, else the
// shared host name, else "" (the service then applies its built-in default). The caller
// passes config.Identity.Hostname as the fallback so this stays free of the model.
func (s *ServerSection) EffectiveServerName(identityHostname string) string {
	if n := strings.TrimSpace(s.ServerName); n != "" {
		return n
	}
	return strings.TrimSpace(identityHostname)
}

// ServerSectionFromModel resolves the AFP server section from the model, falling back to
// a fresh default (empty Transports → bind-all) when the model carries none.
func ServerSectionFromModel(m *config.Model) *ServerSection {
	if m != nil {
		if s, ok := m.Get(ServerKey); ok {
			if ss, ok := s.(*ServerSection); ok {
				return ss
			}
		}
	}
	return &ServerSection{AKey: ServerKey, Enabled: true}
}

// RegisterServer installs the AFP server-section schema so codecs round-trip it. Kept
// out of an init() so a build excluding AFP excludes the section too (called from the
// compose registry wiring, like RegisterVolumes).
func RegisterServer() {
	config.Register(config.SectionSchema{
		Key: ServerKey,
		New: func() config.Section { return &ServerSection{AKey: ServerKey, Enabled: true} },
		Validate: func(s config.Section) error {
			if ss, ok := s.(*ServerSection); ok {
				return ss.Validate()
			}
			return nil
		},
		DisplayName: "AFP server",
		Description: "Apple Filing Protocol server identity and transports (classic DDP and modern TCP/DSI).",
	})
}
