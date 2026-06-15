// Package inproc is the in-process control adapter: a direct caller of
// core/control.Plane used by tests and the embedded CLI (§7). It is the baseline
// front-end the multi-front-end parity test (E3) compares http and ubus against —
// there is no serialization, so its results define "correct".
package inproc

import (
	"context"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/control"
)

// Client is the front-end-agnostic surface every control adapter (inproc, http,
// ubus) exposes, so the parity test can drive them uniformly. It mirrors the
// request/response half of control.Plane plus the live subscription.
//
// User administration and Diagnostics may be unavailable in a given build / config
// (no user store wired; a probe not supported); those methods surface
// control.ErrUnavailable, which every transport round-trips so a client can
// errors.Is it the same way the in-process caller does.
type Client interface {
	Config() (*config.Model, error)
	Status() ([]control.Unit, error)
	Reconfigure(ctx context.Context, name string, section config.Section) error
	Save(ctx context.Context) (revision string, err error)
	Start(ctx context.Context, name string) error
	Stop(ctx context.Context, name string) error
	Restart(ctx context.Context, name string) error
	ListFSTypes() ([]string, error)
	ListInterfaces() ([]control.InterfaceInfo, error)
	ListZones(ctx context.Context) ([]string, error)

	Users() ([]control.UserInfo, error)
	SetUser(name, password string) error
	SetUserDisabled(name string, disabled bool) error
	RemoveUser(name string) error

	Subscribe(topics ...string) (<-chan bus.Event, func(), error)
}

// Adapter is the in-process Client: it forwards straight to a control.Plane.
type Adapter struct {
	plane control.Plane
}

// New wraps a Plane as an in-process Client.
func New(plane control.Plane) *Adapter { return &Adapter{plane: plane} }

// compile-time assertion: *Adapter satisfies Client.
var _ Client = (*Adapter)(nil)

// Config returns a clone of the live config model.
func (a *Adapter) Config() (*config.Model, error) { return a.plane.Config() }

// Status returns the component status snapshot.
func (a *Adapter) Status() ([]control.Unit, error) { return a.plane.Status(), nil }

// Save validates and persists the live model, returning the store revision.
func (a *Adapter) Save(ctx context.Context) (string, error) { return a.plane.Save(ctx) }

// ListInterfaces returns the enumerable network interfaces.
func (a *Adapter) ListInterfaces() ([]control.InterfaceInfo, error) {
	return a.plane.ListInterfaces()
}

// ListZones runs the Diagnostics zone probe (control.ErrUnavailable when unsupported).
func (a *Adapter) ListZones(ctx context.Context) ([]string, error) {
	return a.plane.Diagnostics().ListZones(ctx)
}

// Users lists stored identities (control.ErrUnavailable when no store is wired).
func (a *Adapter) Users() ([]control.UserInfo, error) { return a.plane.Users() }

// SetUser adds a user or resets a password.
func (a *Adapter) SetUser(name, password string) error { return a.plane.SetUser(name, password) }

// SetUserDisabled parks/unparks an account.
func (a *Adapter) SetUserDisabled(name string, disabled bool) error {
	return a.plane.SetUserDisabled(name, disabled)
}

// RemoveUser deletes a user.
func (a *Adapter) RemoveUser(name string) error { return a.plane.RemoveUser(name) }

// Reconfigure applies a new section to a named component.
func (a *Adapter) Reconfigure(ctx context.Context, name string, section config.Section) error {
	return a.plane.Reconfigure(ctx, name, section)
}

// Start starts a named component.
func (a *Adapter) Start(ctx context.Context, name string) error { return a.plane.Start(ctx, name) }

// Stop stops a named component.
func (a *Adapter) Stop(ctx context.Context, name string) error { return a.plane.Stop(ctx, name) }

// Restart restarts a named component.
func (a *Adapter) Restart(ctx context.Context, name string) error {
	return a.plane.Restart(ctx, name)
}

// ListFSTypes returns the registered filesystem types.
func (a *Adapter) ListFSTypes() ([]string, error) { return a.plane.ListFSTypes(), nil }

// Subscribe returns the live telemetry channel for the requested topics.
func (a *Adapter) Subscribe(topics ...string) (<-chan bus.Event, func(), error) {
	ch, cancel := a.plane.Subscribe(topics...)
	return ch, cancel, nil
}
