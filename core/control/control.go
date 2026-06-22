package control

import (
	"context"
	"errors"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

var (
	// ErrUnavailable is returned by diagnostics probes unavailable in a given build.
	ErrUnavailable = errors.New("control: unavailable")
	errPersistence = errors.New("control: codec/store not configured")
)

// Plane is the transport-agnostic management surface.
type Plane interface {
	Config() (*config.Model, error)
	Reconfigure(ctx context.Context, name string, section config.Section) error
	// AddInstance stages a new/replacement repeated-section instance (an AFP volume, an
	// SMB share) under its schema key and reconciles the owning service so it serves it
	// live. RemoveInstance drops the named instance and reconciles the owner. owner is
	// the component that consumes the list ("AFP" for "AFPVolumes", "SMB" for "SMBShares").
	// These are the create/delete half of repeated-section config; an in-place edit of an
	// existing instance rides Reconfigure.
	AddInstance(ctx context.Context, owner string, section config.NamedSection) error
	RemoveInstance(ctx context.Context, owner, key, instanceName string) error
	Save(ctx context.Context) (revision string, err error)
	// MarshalConfig serialises the live (masked) model through the configured codec —
	// the on-disk form (TOML/UCI) — so a front-end can offer a faithful "download
	// server.toml" backup rather than the JSON shape Config() returns. Secrets are
	// masked, exactly as Config(). ErrUnavailable when no codec is wired.
	MarshalConfig() ([]byte, error)

	Start(ctx context.Context, name string) error
	Stop(ctx context.Context, name string) error
	Restart(ctx context.Context, name string) error

	Status() []Unit
	ListInterfaces() ([]InterfaceInfo, error)
	// SetInterface adds or replaces a named entry in the interface NAMESPACE
	// (Model.Interfaces) — a NIC, serial, or bridge interface a port references by
	// name (§M11). This is distinct from ListInterfaces (which enumerates the HOST's
	// physical NICs for a picker): the namespace is operator-declared config. The
	// change is staged into the model; it goes live for a port the next time that
	// port is (re)built (Reconfigure/Restart/Save), since EffectiveInterface
	// re-resolves the namespace on every build. RemoveInterface drops the named entry.
	SetInterface(ctx context.Context, iface config.InterfaceSection) error
	RemoveInterface(ctx context.Context, name string) error
	ListFSTypes() []string
	// ParamsFor returns the config-param schema for one fs_type, so a UI can render
	// that backend's per-share form (which keys, which are required, which are Secret
	// → a password field). Unknown/param-less types yield an empty slice. It is the
	// schema half of ListFSTypes (which returns only the names).
	ParamsFor(fsType string) []ParamInfo
	Diagnostics() Diagnostics
	// SetDiagnostics installs a real diagnostics probe surface (replacing the default
	// "unavailable" one). The cmd/compose edge wires it after the runtime is built, when
	// the router (the probe's data source) exists — core ships only the unavailable
	// default, keeping core/control free of router knowledge. A nil impl is ignored.
	SetDiagnostics(d Diagnostics)

	// User administration (the web UI's user CRUD). Users live in the auth store,
	// not the config model, so these are a surface of their own rather than config
	// edits. When no user store is wired (a build with no file services, or none
	// configured) they return ErrUnavailable — the same "not in this build" shape
	// as Diagnostics. Share allow-lists, by contrast, ARE config and ride the
	// Config()/Reconfigure path.
	Users() ([]UserInfo, error)
	SetUser(name, password string) error
	SetUserDisabled(name string, disabled bool) error
	RemoveUser(name string) error

	// Web-management-interface admin credential (§4-ter). AdminConfigured reports
	// whether an admin has been set — the HTTP front-end uses it to drive first-run
	// setup vs. enforce Basic auth. SetAdmin stores an already-derived credential
	// (the adapter ring generates the salt and hashes the password; the plane never
	// sees plaintext beyond forwarding the hash-only DTO) and persists it via the
	// Save path, returning the new config revision. Unlike the file-service user
	// store, AdminAuth always exists on the model, so these are never ErrUnavailable.
	AdminConfigured() bool
	SetAdmin(ctx context.Context, a config.AdminAuth) (revision string, err error)

	Subscribe(topics ...string) (<-chan bus.Event, func())
}

// UserInfo is the management view of one stored identity. It never carries hash
// or password material.
type UserInfo struct {
	Name     string
	Disabled bool
}

// UserAdmin is the optional user-management surface a Supervisor exposes when a
// user store is wired. The plane type-asserts it; absent, the user methods return
// ErrUnavailable. The supervisor satisfies it by delegating to the wired
// auth.UserStore (the concrete auth types stay out of core/control).
type UserAdmin interface {
	Users() ([]UserInfo, error)
	SetUser(name, password string) error
	SetUserDisabled(name string, disabled bool) error
	RemoveUser(name string) error
}

// Unit is one component status snapshot for dashboards.
type Unit struct {
	Name      string
	Kind      string
	Enabled   bool
	Running   bool
	Binding   string
	DependsOn []string
	Props     map[string]string
}

type InterfaceInfo struct{ Name, Addr string }

// ParamInfo is the management view of one fs_type config param (the JSON-friendly
// mirror of fs.Param): the option key, whether it is required, whether it is a Secret
// (the UI renders a password field and the server masks it on a Config round-trip),
// and a short doc string. A UI renders a per-share form from the slice ParamsFor
// returns for the chosen fs_type.
type ParamInfo struct {
	Key      string `json:"key"`
	Required bool   `json:"required"`
	Secret   bool   `json:"secret"`
	Doc      string `json:"doc"`
}

// Supervisor is the lifecycle/model surface a Plane drives.
type Supervisor interface {
	Model() *config.Model
	Reconfigure(ctx context.Context, name string, section config.Section) error
	AddInstance(ctx context.Context, owner string, section config.NamedSection) error
	RemoveInstance(ctx context.Context, owner, key, instanceName string) error
	Start(ctx context.Context, name string) error
	Stop(ctx context.Context, name string) error
	Restart(ctx context.Context, name string) error
	Status() []Unit
	ListInterfaces() ([]InterfaceInfo, error)
	// SetInterface / RemoveInterface mutate the interface namespace (Model.Interfaces)
	// under the supervisor lock and reconcile the ports that reference the changed
	// interface so the change goes live.
	SetInterface(ctx context.Context, iface config.InterfaceSection) error
	RemoveInterface(ctx context.Context, name string) error
	ListFSTypes() []string
	// SetAdminAuth stamps the web-admin credential (§4-ter) into the model under the
	// supervisor's lock. The plane calls it from SetAdmin, then persists via Save.
	SetAdminAuth(a config.AdminAuth)
}

// Diagnostics is the optional read-only probe surface.
type Diagnostics interface {
	ListZones(ctx context.Context) ([]string, error)
	// RegisteredNames returns the NBP name table (the names the server has bound on
	// the AppleTalk internet), the drill-down behind NBP's "registered names" stat.
	// ErrUnavailable when no NBP service is wired.
	RegisteredNames(ctx context.Context) ([]NBPName, error)
	// MacIPLeases returns the MacIP gateway's active IP↔AppleTalk leases, the
	// drill-down behind MacIP's "active leases" stat. ErrUnavailable when no MacIP
	// gateway is wired.
	MacIPLeases(ctx context.Context) ([]MacIPLease, error)
}

// NBPName is the management view of one entry in the NBP name table: the AppleTalk
// NVE tuple (object:type@zone) and the DDP socket it is registered on. The byte
// fields are decoded to display strings at the diagnostics impl (MacRoman → UTF-8),
// so a front-end renders them directly.
type NBPName struct {
	Object string `json:"object"`
	Type   string `json:"type"`
	Zone   string `json:"zone"`
	Socket uint8  `json:"socket"`
}

// MacIPLease is the management view of one MacIP gateway lease: the assigned IPv4
// (dotted-quad string), the AppleTalk network/node it maps to, and the lease source
// ("static" from the pool, or "external" from a DHCP-relay / egress assignment).
type MacIPLease struct {
	IP        string `json:"ip"`
	ATNetwork uint16 `json:"at_network"`
	ATNode    uint8  `json:"at_node"`
	Source    string `json:"source"`
}

type plane struct {
	sup       Supervisor
	codec     config.Codec
	store     config.Store
	telemetry bus.Bus
	diag      Diagnostics
}

// New builds a Plane over a Supervisor, a config Codec/Store, and the telemetry bus.
func New(sup Supervisor, codec config.Codec, store config.Store, telemetry bus.Bus) Plane {
	return &plane{
		sup:       sup,
		codec:     codec,
		store:     store,
		telemetry: telemetry,
		diag:      unavailableDiagnostics{},
	}
}

// MarshalConfig serialises the masked live model through the codec (the on-disk form).
func (p *plane) MarshalConfig() ([]byte, error) {
	if p.codec == nil {
		return nil, ErrUnavailable
	}
	m := p.sup.Model()
	if m == nil {
		m = config.NewModel()
	}
	return p.codec.Marshal(m.MaskSecrets())
}

func (p *plane) Config() (*config.Model, error) {
	m := p.sup.Model()
	if m == nil {
		return config.NewModel(), nil
	}
	// Redact secret-valued fields (backend passwords in AFP volume / SMB share
	// options) before the model leaves the process. MaskSecrets clones, so the live
	// model is untouched; the inbound Reconfigure path restores any value a UI returns
	// still bearing the placeholder.
	return m.MaskSecrets(), nil
}

func (p *plane) Reconfigure(ctx context.Context, name string, section config.Section) error {
	// Unmask before applying: a front-end edits the masked model (Config above) and
	// submits a section whose secret fields may still hold config.RedactedSecret for
	// values the operator did not change. Restore those from the live stored section so
	// a blind round-trip never overwrites a stored secret with the placeholder.
	section = p.unmaskAgainstLive(name, section)
	return p.sup.Reconfigure(ctx, name, section)
}

// unmaskAgainstLive restores redacted secret fields in an inbound section from the
// matching live section in the model. It is a no-op for a section that carries no
// secrets (not a config.SecretMasker). The live counterpart is resolved as a singleton
// (by Key) or, when the section is a repeated named instance, by its InstanceName.
func (p *plane) unmaskAgainstLive(name string, section config.Section) config.Section {
	sm, ok := section.(config.SecretMasker)
	if !ok {
		return section
	}
	m := p.sup.Model()
	if m == nil {
		return sm.Unmask(nil)
	}
	var prev config.Section
	if ns, ok := section.(config.NamedSection); ok {
		// Repeated instance: match by schema key + instance name.
		prev, _ = m.Instance(ns.Key(), ns.InstanceName())
	} else {
		// Singleton: addressed by the component name (== section key).
		prev, _ = m.Get(name)
	}
	return sm.Unmask(prev)
}

// AddInstance unmasks the inbound instance against any same-named live instance (so a
// re-added share keeps an unchanged stored secret) and delegates to the supervisor,
// which stages it and reconciles the owner.
func (p *plane) AddInstance(ctx context.Context, owner string, section config.NamedSection) error {
	unmasked := p.unmaskAgainstLive(section.Key(), section)
	ns, ok := unmasked.(config.NamedSection)
	if !ok {
		// SecretMasker.Unmask must return the same concrete type; defensively keep the
		// original named section if a masker ever returns a non-named clone.
		ns = section
	}
	return p.sup.AddInstance(ctx, owner, ns)
}

// RemoveInstance deletes the named instance and reconciles the owner. No secret
// handling: a delete carries no values.
func (p *plane) RemoveInstance(ctx context.Context, owner, key, instanceName string) error {
	return p.sup.RemoveInstance(ctx, owner, key, instanceName)
}

func (p *plane) Save(ctx context.Context) (revision string, err error) {
	_ = ctx
	return p.persist()
}

// persist validates the live model and writes it to the store, returning the new
// revision. It is the shared body behind Save and SetAdmin (which stamps the admin
// credential into the model first, then persists). Validation rejects an invalid
// section or a hostname that violates a consumer-gated rule (the NetBIOS ≤15-byte
// limit, §4-bis) before it reaches the store, rather than serialising a config that
// would mangle a name on the wire. NetBIOSEnabled is derived from the live component
// set, since NetBIOS carries no config section of its own.
func (p *plane) persist() (revision string, err error) {
	if p.codec == nil || p.store == nil {
		return "", errPersistence
	}
	m := p.sup.Model()
	if err := m.Validate(config.ValidateOptions{NetBIOSEnabled: p.netbiosEnabled()}); err != nil {
		return "", err
	}
	data, err := p.codec.Marshal(m)
	if err != nil {
		return "", err
	}
	return p.store.Save(data)
}

// AdminConfigured reports whether a web-admin credential is set (§4-ter). The HTTP
// front-end reads it to choose first-run setup vs. enforce Basic auth.
func (p *plane) AdminConfigured() bool {
	m := p.sup.Model()
	return m != nil && m.AdminAuth.Configured()
}

// SetAdmin stamps an already-derived admin credential into the model and persists it,
// returning the new config revision. The credential is hash-only (the adapter ring
// generated the salt and hashed the password); the plane never handles plaintext. This
// is the "set + auto-save" the first-run /setup handler drives — one call both records
// the admin and writes server.toml.
func (p *plane) SetAdmin(ctx context.Context, a config.AdminAuth) (revision string, err error) {
	_ = ctx
	p.sup.SetAdminAuth(a)
	return p.persist()
}

// netbiosComponentName is the component name the supervisor reports for the NetBIOS
// service in Status(). Matched by string (not an import of the service package) to
// keep core/control free of service dependencies. Mirrors netbios.Name.
const netbiosComponentName = "NetBIOS"

// netbiosEnabled reports whether the NetBIOS service is present and configured-enabled
// in the live component set, so Model.Validate applies the NetBIOS hostname rule only
// when NetBIOS is actually in play (§4-bis).
func (p *plane) netbiosEnabled() bool {
	for _, u := range p.sup.Status() {
		if u.Name == netbiosComponentName {
			return u.Enabled
		}
	}
	return false
}

func (p *plane) Start(ctx context.Context, name string) error   { return p.sup.Start(ctx, name) }
func (p *plane) Stop(ctx context.Context, name string) error    { return p.sup.Stop(ctx, name) }
func (p *plane) Restart(ctx context.Context, name string) error { return p.sup.Restart(ctx, name) }

func (p *plane) Status() []Unit                           { return p.sup.Status() }
func (p *plane) ListInterfaces() ([]InterfaceInfo, error) { return p.sup.ListInterfaces() }

// SetInterface stages a named interface-namespace entry and reconciles referencing
// ports (forwarded to the supervisor, which holds the model lock).
func (p *plane) SetInterface(ctx context.Context, iface config.InterfaceSection) error {
	return p.sup.SetInterface(ctx, iface)
}

// RemoveInterface drops a named interface-namespace entry and reconciles referencing
// ports.
func (p *plane) RemoveInterface(ctx context.Context, name string) error {
	return p.sup.RemoveInterface(ctx, name)
}
func (p *plane) ListFSTypes() []string    { return p.sup.ListFSTypes() }
func (p *plane) Diagnostics() Diagnostics { return p.diag }

// SetDiagnostics installs a real diagnostics impl (nil is ignored, keeping the
// unavailable default).
func (p *plane) SetDiagnostics(d Diagnostics) {
	if d != nil {
		p.diag = d
	}
}

// ParamsFor returns the config-param schema for one fs_type as JSON-friendly
// ParamInfo rows, read straight from the fs factory registry (a pure lookup needing
// no supervisor state). The UI renders the chosen backend's per-share form from it,
// marking Secret keys as password fields.
func (p *plane) ParamsFor(fsType string) []ParamInfo {
	params := fs.ParamsFor(fsType)
	out := make([]ParamInfo, len(params))
	for i, pm := range params {
		out[i] = ParamInfo{Key: pm.Key, Required: pm.Required, Secret: pm.Secret, Doc: pm.Doc}
	}
	return out
}

// userAdmin returns the supervisor's user-management surface if it exposes one,
// else nil (no user store wired / not in this build).
func (p *plane) userAdmin() UserAdmin {
	if ua, ok := p.sup.(UserAdmin); ok {
		return ua
	}
	return nil
}

func (p *plane) Users() ([]UserInfo, error) {
	ua := p.userAdmin()
	if ua == nil {
		return nil, ErrUnavailable
	}
	return ua.Users()
}

func (p *plane) SetUser(name, password string) error {
	ua := p.userAdmin()
	if ua == nil {
		return ErrUnavailable
	}
	return ua.SetUser(name, password)
}

func (p *plane) SetUserDisabled(name string, disabled bool) error {
	ua := p.userAdmin()
	if ua == nil {
		return ErrUnavailable
	}
	return ua.SetUserDisabled(name, disabled)
}

func (p *plane) RemoveUser(name string) error {
	ua := p.userAdmin()
	if ua == nil {
		return ErrUnavailable
	}
	return ua.RemoveUser(name)
}
func (p *plane) Subscribe(topics ...string) (<-chan bus.Event, func()) {
	return p.telemetry.Subscribe(topics...)
}

type unavailableDiagnostics struct{}

func (unavailableDiagnostics) ListZones(context.Context) ([]string, error) {
	return nil, ErrUnavailable
}

func (unavailableDiagnostics) RegisteredNames(context.Context) ([]NBPName, error) {
	return nil, ErrUnavailable
}

func (unavailableDiagnostics) MacIPLeases(context.Context) ([]MacIPLease, error) {
	return nil, ErrUnavailable
}
