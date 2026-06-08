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

	Subscribe(topics ...string) (<-chan bus.Event, func())
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
func (p *plane) Subscribe(topics ...string) (<-chan bus.Event, func()) {
	return p.telemetry.Subscribe(topics...)
}

type unavailableDiagnostics struct{}

func (unavailableDiagnostics) ListZones(context.Context) ([]string, error) {
	return nil, ErrUnavailable
}
