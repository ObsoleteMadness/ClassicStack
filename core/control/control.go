package control

import (
	"context"
	"errors"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/hostinfo"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/metastore"
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
	// SetWellKnown updates a well-known Model field (Identity, Router, Logging, HTTP,
	// Client, FUSE) outside the registered Sections map. section is the opaque
	// encoded body (JSON at the HTTP adapter); core passes it through without
	// decoding, so the codec stays an adapter concern (§1).
	SetWellKnown(ctx context.Context, key string, section []byte) error
	Save(ctx context.Context) (revision string, err error)
	// MarshalConfig serialises the live (masked) model through the configured codec —
	// the on-disk form (TOML/UCI) — so a front-end can offer a faithful "download
	// server.toml" backup rather than the JSON shape Config() returns. Secrets are
	// masked, exactly as Config(). ErrUnavailable when no codec is wired.
	MarshalConfig() ([]byte, error)
	// ValidateConfig parses codec bytes (TOML/UCI) into a fresh model and runs
	// Model.Validate without touching the live stack — the TOML editor's "check"
	// action. ApplyConfigBytes parses, validates, installs the model via
	// Supervisor.ReplaceModel, and persists — the editor's "apply & save".
	ValidateConfig(data []byte) error
	ApplyConfigBytes(ctx context.Context, data []byte) (revision string, err error)
	// Schemas returns the self-describing section catalogue for this build (keys,
	// capabilities, field metadata). Adapters may enrich Fields via reflection;
	// the plane returns whatever the schema registry carries plus optional
	// Describe enrichment installed by SetSchemaDescriber.
	Schemas() []config.SectionInfo
	// SetSchemaDescriber installs an optional enricher that turns registered
	// SectionSchema values into SectionInfo (typically adapter/config/describe).
	// Nil keeps Schemas() returning bare registry metadata without reflected fields.
	SetSchemaDescriber(fn func() []config.SectionInfo)

	// HostInfo returns static board/build details and dynamic OS/system metrics.
	HostInfo() (hostinfo.HostInfo, error)

	Start(ctx context.Context, name string) error
	Stop(ctx context.Context, name string) error
	Restart(ctx context.Context, name string) error

	Status() []Unit
	// HostnameConstraints returns the active consumer-gated hostname constraint keys
	// across the live component set (e.g. "netbios" when NetBIOS is enabled). The plane
	// forwards them to Model.Validate so the right hostname rules apply WITHOUT control
	// naming any specific service (§4-bis). A component declares its own constraint via
	// component.HostnameConstrainer; the supervisor aggregates them.
	HostnameConstraints() []string
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
	// ShareBackends returns the share/volume picker catalogues (fs types, fork
	// adapters, codecs, metastores, meta engines) plus per-fs_type param schemas
	// so a UI can render selects and backend-specific Options without N+1 calls.
	ShareBackends() ShareBackends
	Diagnostics() Diagnostics
	// SetDiagnostics installs a real diagnostics probe surface (replacing the default
	// "unavailable" one). The cmd/compose edge wires it after the runtime is built, when
	// the router (the probe's data source) exists — core ships only the unavailable
	// default, keeping core/control free of router knowledge. A nil impl is ignored.
	SetDiagnostics(d Diagnostics)
	// SetLogger installs the management-action logger used for Info audit lines when an
	// operator Start/Stop/Restart/Reconfigure/Save (and related) through any front-end
	// (HTTP web UI, ubus, inproc). A nil logger keeps the sink-less no-op default.
	SetLogger(l log.Logger)

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
	// Error is the last Start failure for this unit (empty when the last Start succeeded).
	Error string
}

// InterfaceInfo is the management view of one host NIC for the UI's device picker.
// Name is the RAW pcap device string a config stores (on Windows the
// "\Device\NPF_{GUID}", on Linux "eth0"); Description is the human-friendly label the
// picker shows (e.g. the adaptor model) — display only, never stored. Addr is the
// device's first address, if any.
type InterfaceInfo struct{ Name, Description, Addr string }

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

// ShareBackends is the one-shot catalogue a share/volume editor uses to populate
// filesystem-type / fork / codec / metastore / meta-backend selects and to render
// backend-specific Options from each fs_type's Param schema.
type ShareBackends struct {
	FSTypes        []string               `json:"fs_types"`
	ForkBackends   []string               `json:"fork_backends"`
	FilenameCodecs []string               `json:"filename_codecs"`
	Metastores     []string               `json:"metastores"`
	MetaBackends   []string               `json:"meta_backends"`
	FSParams       map[string][]ParamInfo `json:"fs_params"`
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
	// HostnameConstraints aggregates the active consumer-gated hostname constraint keys
	// across the live component set (see the Plane doc above). The plane forwards them to
	// Model.Validate so control names no specific service.
	HostnameConstraints() []string
	ListInterfaces() ([]InterfaceInfo, error)
	// SetInterface / RemoveInterface mutate the interface namespace (Model.Interfaces)
	// under the supervisor lock and reconcile the ports that reference the changed
	// interface so the change goes live.
	SetInterface(ctx context.Context, iface config.InterfaceSection) error
	RemoveInterface(ctx context.Context, name string) error
	// SetWellKnown updates a well-known Model field (Identity, Router, Logging, HTTP,
	// Client, FUSE) outside the registered Sections map. section is the opaque
	// encoded body (JSON at the HTTP adapter); core passes it through without
	// decoding, so the codec stays an adapter concern (§1).
	SetWellKnown(ctx context.Context, key string, section []byte) error
	ListFSTypes() []string
	// ReplaceModel installs a new config model as the live source of truth and
	// reconciles the running component set (stop → swap → rebuild → start). Used by
	// the TOML editor Apply path.
	ReplaceModel(ctx context.Context, m *config.Model) error
	// SetAdminAuth stamps the web-admin credential (§4-ter) into the model under the
	// supervisor's lock. The plane calls it from SetAdmin, then persists via Save.
	SetAdminAuth(a config.AdminAuth)
}

// Diagnostics is the optional read-only probe surface on the neutral management plane.
// It carries ONLY protocol-neutral probes: ListZones returns the AppleTalk router's zone
// list as plain strings. The PROTOCOL-SPECIFIC drill-downs (NBP names, MacIP leases) do
// NOT live here — they would leak a protocol DTO into the neutral contract; instead a
// dedicated diagnostics ADAPTER (adapter/control/diag, which may import the service
// packages) bridges those to the front-ends, the read-only sibling of the transport
// cross-wire. So core/control names no protocol.
type Diagnostics interface {
	ListZones(ctx context.Context) ([]string, error)
}

type plane struct {
	sup       Supervisor
	codec     config.Codec
	store     config.Store
	telemetry bus.Bus
	diag      Diagnostics
	logger    log.Logger
	describe  func() []config.SectionInfo
}

// New builds a Plane over a Supervisor, a config Codec/Store, and the telemetry bus.
// The plane starts with a sink-less no-op logger; the compose edge installs a real one
// via SetLogger so operator actions produce Info audit lines on stderr and the bus.
func New(sup Supervisor, codec config.Codec, store config.Store, telemetry bus.Bus) Plane {
	return &plane{
		sup:       sup,
		codec:     codec,
		store:     store,
		telemetry: telemetry,
		diag:      unavailableDiagnostics{},
		logger:    log.New("control"),
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

// SetSchemaDescriber installs the optional SectionInfo enricher (adapter/config/describe).
func (p *plane) SetSchemaDescriber(fn func() []config.SectionInfo) { p.describe = fn }

// Schemas returns the self-describing section catalogue. When a describer is installed
// it is preferred; otherwise a bare list is built from the registry (no reflected fields).
func (p *plane) Schemas() []config.SectionInfo {
	if p.describe != nil {
		return p.describe()
	}
	schemas := config.Schemas()
	out := make([]config.SectionInfo, 0, len(schemas))
	for _, sc := range schemas {
		info := config.SectionInfo{
			Key: sc.Key, Repeated: sc.Repeated,
			DisplayName: sc.DisplayName, Description: sc.Description,
			Capabilities: append([]string(nil), sc.Capabilities...),
			Fields:       append([]config.FieldInfo(nil), sc.Fields...),
		}
		if info.DisplayName == "" {
			info.DisplayName = sc.Key
		}
		out = append(out, info)
	}
	return out
}

// ValidateConfig parses codec bytes into a fresh model and validates without applying.
func (p *plane) ValidateConfig(data []byte) error {
	if p.codec == nil {
		return ErrUnavailable
	}
	m := config.NewModel()
	if err := p.codec.Unmarshal(data, m); err != nil {
		return err
	}
	return m.Validate(config.ValidateOptions{HostnameConstraints: p.sup.HostnameConstraints()})
}

// ApplyConfigBytes parses, validates, replaces the live model, and persists.
func (p *plane) ApplyConfigBytes(ctx context.Context, data []byte) (string, error) {
	if p.codec == nil || p.store == nil {
		return "", errPersistence
	}
	m := config.NewModel()
	if err := p.codec.Unmarshal(data, m); err != nil {
		p.logger.Log1(log.Error, "control: config apply parse failed", log.Str("err", err.Error()))
		return "", err
	}
	if err := m.Validate(config.ValidateOptions{HostnameConstraints: p.sup.HostnameConstraints()}); err != nil {
		p.logger.Log1(log.Error, "control: config apply validate failed", log.Str("err", err.Error()))
		return "", err
	}
	if err := p.sup.ReplaceModel(ctx, m); err != nil {
		p.logger.Log1(log.Error, "control: config apply replace failed", log.Str("err", err.Error()))
		return "", err
	}
	revision, err := p.persist()
	if err != nil {
		p.logger.Log1(log.Error, "control: config apply save failed", log.Str("err", err.Error()))
		return "", err
	}
	p.logger.Log1(log.Info, "control: configuration applied from editor", log.Str("revision", revision))
	return revision, nil
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
	if err := p.sup.Reconfigure(ctx, name, section); err != nil {
		p.logger.Log2(log.Error, "control: reconfigure failed",
			log.Str("component", name), log.Str("err", err.Error()))
		return err
	}
	p.logger.Log1(log.Info, "control: configuration applied", log.Str("component", name))
	return nil
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
	if err := p.sup.AddInstance(ctx, owner, ns); err != nil {
		p.logger.Log(log.Error, "control: add instance failed",
			log.Str("owner", owner), log.Str("key", ns.Key()),
			log.Str("instance", ns.InstanceName()), log.Str("err", err.Error()))
		return err
	}
	p.logger.Log(log.Info, "control: instance added",
		log.Str("owner", owner), log.Str("key", ns.Key()),
		log.Str("instance", ns.InstanceName()))
	return nil
}

// RemoveInstance deletes the named instance and reconciles the owner. No secret
// handling: a delete carries no values.
func (p *plane) RemoveInstance(ctx context.Context, owner, key, instanceName string) error {
	if err := p.sup.RemoveInstance(ctx, owner, key, instanceName); err != nil {
		p.logger.Log(log.Error, "control: remove instance failed",
			log.Str("owner", owner), log.Str("key", key),
			log.Str("instance", instanceName), log.Str("err", err.Error()))
		return err
	}
	p.logger.Log(log.Info, "control: instance removed",
		log.Str("owner", owner), log.Str("key", key),
		log.Str("instance", instanceName))
	return nil
}

func (p *plane) Save(ctx context.Context) (revision string, err error) {
	_ = ctx
	revision, err = p.persist()
	if err != nil {
		p.logger.Log1(log.Error, "control: config save failed", log.Str("err", err.Error()))
		return "", err
	}
	p.logger.Log1(log.Info, "control: configuration saved", log.Str("revision", revision))
	return revision, nil
}

// persist validates the live model and writes it to the store, returning the new
// revision. It is the shared body behind Save and SetAdmin (which stamps the admin
// credential into the model first, then persists). Validation rejects an invalid
// section or a hostname that violates a consumer-gated rule before it reaches the
// store, rather than serialising a config that would mangle a name on the wire. The
// active consumer-gated hostname constraints are reported by the supervisor (aggregated
// from the live components implementing component.HostnameConstrainer), so control names
// no specific service — it forwards whatever constraint keys are active.
func (p *plane) persist() (revision string, err error) {
	if p.codec == nil || p.store == nil {
		return "", errPersistence
	}
	m := p.sup.Model()
	if err := m.Validate(config.ValidateOptions{HostnameConstraints: p.sup.HostnameConstraints()}); err != nil {
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
	revision, err = p.persist()
	if err != nil {
		p.logger.Log1(log.Error, "control: admin credential save failed", log.Str("err", err.Error()))
		return "", err
	}
	p.logger.Log1(log.Info, "control: admin credential saved", log.Str("revision", revision))
	return revision, nil
}

func (p *plane) Start(ctx context.Context, name string) error {
	if err := p.sup.Start(ctx, name); err != nil {
		p.logger.Log2(log.Error, "control: start failed",
			log.Str("component", name), log.Str("err", err.Error()))
		return err
	}
	p.logger.Log1(log.Info, "control: started", log.Str("component", name))
	return nil
}

func (p *plane) Stop(ctx context.Context, name string) error {
	if err := p.sup.Stop(ctx, name); err != nil {
		p.logger.Log2(log.Error, "control: stop failed",
			log.Str("component", name), log.Str("err", err.Error()))
		return err
	}
	p.logger.Log1(log.Info, "control: stopped", log.Str("component", name))
	return nil
}

func (p *plane) Restart(ctx context.Context, name string) error {
	if err := p.sup.Restart(ctx, name); err != nil {
		p.logger.Log2(log.Error, "control: restart failed",
			log.Str("component", name), log.Str("err", err.Error()))
		return err
	}
	p.logger.Log1(log.Info, "control: restarted", log.Str("component", name))
	return nil
}

func (p *plane) Status() []Unit                           { return p.sup.Status() }
func (p *plane) HostInfo() (hostinfo.HostInfo, error)     { return hostinfo.Get(), nil }
func (p *plane) HostnameConstraints() []string            { return p.sup.HostnameConstraints() }
func (p *plane) ListInterfaces() ([]InterfaceInfo, error) { return p.sup.ListInterfaces() }

// SetInterface stages a named interface-namespace entry and reconciles referencing
// ports (forwarded to the supervisor, which holds the model lock).
func (p *plane) SetInterface(ctx context.Context, iface config.InterfaceSection) error {
	if err := p.sup.SetInterface(ctx, iface); err != nil {
		p.logger.Log2(log.Error, "control: set interface failed",
			log.Str("interface", iface.Name), log.Str("err", err.Error()))
		return err
	}
	p.logger.Log1(log.Info, "control: interface configured", log.Str("interface", iface.Name))
	return nil
}

// RemoveInterface drops a named interface-namespace entry and reconciles referencing
// ports.
func (p *plane) RemoveInterface(ctx context.Context, name string) error {
	if err := p.sup.RemoveInterface(ctx, name); err != nil {
		p.logger.Log2(log.Error, "control: remove interface failed",
			log.Str("interface", name), log.Str("err", err.Error()))
		return err
	}
	p.logger.Log1(log.Info, "control: interface removed", log.Str("interface", name))
	return nil
}

func (p *plane) SetWellKnown(ctx context.Context, key string, section []byte) error {
	if err := p.sup.SetWellKnown(ctx, key, section); err != nil {
		p.logger.Log2(log.Error, "control: set well-known failed",
			log.Str("key", key), log.Str("err", err.Error()))
		return err
	}
	p.logger.Log1(log.Info, "control: well-known section updated", log.Str("key", key))
	return nil
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

// SetLogger installs the management-action logger. A nil logger keeps the current
// logger (the sink-less default from New, or a previously installed one).
func (p *plane) SetLogger(l log.Logger) {
	if l != nil {
		p.logger = l
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

// ShareBackends returns the share/volume picker catalogues from the linked
// registries (fs factories, fork/meta adapters, filename codecs, metastore kinds).
func (p *plane) ShareBackends() ShareBackends {
	types := fs.Types()
	params := make(map[string][]ParamInfo, len(types))
	for _, t := range types {
		params[t] = p.ParamsFor(t)
	}
	return ShareBackends{
		FSTypes:        types,
		ForkBackends:   fs.ForkBackends(),
		FilenameCodecs: fs.FilenameCodecs(),
		Metastores:     metastore.Kinds(),
		MetaBackends:   fs.MetaBackends(),
		FSParams:       params,
	}
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
