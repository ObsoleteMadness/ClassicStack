package control

import (
	"context"
	"errors"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
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
	Save(ctx context.Context) (revision string, err error)

	Start(ctx context.Context, name string) error
	Stop(ctx context.Context, name string) error
	Restart(ctx context.Context, name string) error

	Status() []Unit
	ListInterfaces() ([]InterfaceInfo, error)
	ListFSTypes() []string
	Diagnostics() Diagnostics

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

// Supervisor is the lifecycle/model surface a Plane drives.
type Supervisor interface {
	Model() *config.Model
	Reconfigure(ctx context.Context, name string, section config.Section) error
	Start(ctx context.Context, name string) error
	Stop(ctx context.Context, name string) error
	Restart(ctx context.Context, name string) error
	Status() []Unit
	ListInterfaces() ([]InterfaceInfo, error)
	ListFSTypes() []string
}

// Diagnostics is the optional read-only probe surface.
type Diagnostics interface {
	ListZones(ctx context.Context) ([]string, error)
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

func (p *plane) Save(ctx context.Context) (revision string, err error) {
	_ = ctx
	if p.codec == nil || p.store == nil {
		return "", errPersistence
	}
	m := p.sup.Model()
	// Validate the whole model before persisting it: reject an invalid section or a
	// hostname that violates a consumer-gated rule (the NetBIOS ≤15-byte limit, §4-bis)
	// before it reaches the store, rather than serialising a config that would mangle a
	// name on the wire. NetBIOSEnabled is derived from the live component set, since
	// NetBIOS carries no config section of its own.
	if err := m.Validate(config.ValidateOptions{NetBIOSEnabled: p.netbiosEnabled()}); err != nil {
		return "", err
	}
	data, err := p.codec.Marshal(m)
	if err != nil {
		return "", err
	}
	return p.store.Save(data)
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
func (p *plane) ListFSTypes() []string                    { return p.sup.ListFSTypes() }
func (p *plane) Diagnostics() Diagnostics                 { return p.diag }

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
