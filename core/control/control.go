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
	return m.Clone(), nil
}

func (p *plane) Reconfigure(ctx context.Context, name string, section config.Section) error {
	return p.sup.Reconfigure(ctx, name, section)
}

func (p *plane) Save(ctx context.Context) (revision string, err error) {
	_ = ctx
	if p.codec == nil || p.store == nil {
		return "", errPersistence
	}
	data, err := p.codec.Marshal(p.sup.Model())
	if err != nil {
		return "", err
	}
	return p.store.Save(data)
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
