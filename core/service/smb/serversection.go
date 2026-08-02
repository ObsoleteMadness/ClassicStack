package smb

import (
	"slices"

	"github.com/ObsoleteMadness/ClassicStack/core/config"
)

// ServerKey is the config-section / registry name for SMB's server-level settings.
// It is the SINGLETON section (one per server), distinct from SharesKey (the repeated
// per-share schema): SharesKey carries the exported trees, ServerKey the transports the
// SMB service binds. Server identity (hostname/workgroup/description) is NOT here — it
// lives on the shared config.Identity (§4-bis) so SMB and NetBIOS cannot diverge.
const ServerKey = "SMB"

// Transport tokens for ServerSection.Transports. SMB rides several transport families
// (smb-transport-families): the NetBIOS-based ones (NBF over NetBEUI, NB-IPX over IPX,
// NBT over TCP) and the direct/NetBIOS-less ones (direct-hosted SMB over IPX socket
// 0x0550 — implied by TransportIPX — and direct-TCP :445). The list names which the
// operator wants bound; an empty list means "bind whatever transports were built"
// (the historical implicit behaviour), so an unset section keeps prior deployments
// working.
const (
	TransportNetBEUI = "netbeui" // NBF: SMB over NetBIOS over 802.2 LLC
	TransportIPX     = "ipx"     // NB-IPX + direct-hosted SMB over IPX
	TransportNBT     = "nbt"     // NetBIOS over TCP/IP (ports 137-139)
	TransportTCP     = "tcp"     // direct-hosted SMB over TCP :445 (NetBIOS-less)
)

// DefaultDirectTCPAddr is the conventional direct-hosted-SMB-over-TCP port (:445).
// NOTE: it is NOT used as an automatic default — the TCP transport binds ONLY an
// EXPLICITLY configured tcp_addr. On Windows the OS lanmanserver already owns :445, and
// on Unix :445/:139 are privileged; defaulting to them would collide or need root. So
// an operator who wants SMB-over-TCP must set tcp_addr (e.g. ":4450" or "0.0.0.0:445"
// after disabling the native server). This constant documents the convention and seeds
// the UI's placeholder, nothing more.
const DefaultDirectTCPAddr = ":445"

// ServerSection is SMB's singleton server config: which transports to bind. It is a
// flat, codec-friendly view satisfying config.Section so the model round-trips it.
type ServerSection struct {
	// SKey is the section key; always "SMB". Stored so Key() is a plain getter.
	SKey string `toml:"-"`
	// Transports lists the transport tokens (netbeui/ipx/nbt/tcp) the SMB service
	// binds. Empty = bind every transport that was built (back-compat).
	Transports []string `toml:"transports,omitempty" display:"Transports" desc:"netbeui, ipx, nbt, and/or tcp. Empty = bind every transport built into this binary." example:"netbeui,ipx"`
	// TCPAddr overrides the direct-TCP (:445) listen address. Empty = do not bind.
	TCPAddr string `toml:"tcp_addr,omitempty" display:"TCP address" desc:"Direct-hosted SMB over TCP listen address. Empty = do not bind :445." example:":4450"`
}

// DirectTCPAddr returns the configured direct-TCP listen address, or "" when none is
// set. It does NOT fall back to :445 (which Windows' native server owns and Unix
// guards as privileged) — an empty result means "do not bind direct-TCP", so the
// transport stays inert unless the operator names an address explicitly.
func (s *ServerSection) DirectTCPAddr() string { return s.TCPAddr }

// Key returns the section key.
func (s *ServerSection) Key() string { return ServerKey }

// Clone returns a deep copy (Transports is the only reference field; the addr strings
// are values copied by the struct assignment).
func (s *ServerSection) Clone() config.Section {
	cp := *s
	cp.Transports = append([]string(nil), s.Transports...)
	return &cp
}

// Validate checks the section in isolation. Unknown transport tokens are not rejected
// (the compose cross-wire ignores ones it cannot serve), so a config naming a transport
// a given build lacks does not hard-fail the model.
func (s *ServerSection) Validate() error { return nil }

// Binds reports whether the named transport should be bound: true when Transports is
// empty (bind-all back-compat) or explicitly lists the token. The compose transport
// cross-wire consults this to gate each family.
func (s *ServerSection) Binds(transport string) bool {
	return len(s.Transports) == 0 || slices.Contains(s.Transports, transport)
}

// compile-time assertion: *ServerSection satisfies config.Section.
var _ config.Section = (*ServerSection)(nil)

// ServerSectionFromModel resolves the SMB server section from the model, falling back
// to a fresh default (empty Transports → bind-all) when the model carries none.
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

// RegisterServer installs the SMB server-section schema so codecs round-trip it. Kept
// out of an init() so a build excluding SMB excludes the section too (called from the
// compose registry wiring, like RegisterShares).
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
		DisplayName: "SMB server",
		Description: "SMB/CIFS file server transport bindings (NetBEUI, IPX, NBT, direct TCP).",
	})
}
