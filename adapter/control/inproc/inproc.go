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
type Client interface {
	Status() ([]control.Unit, error)
	Reconfigure(ctx context.Context, name string, section config.Section) error
	Start(ctx context.Context, name string) error
	Stop(ctx context.Context, name string) error
	Restart(ctx context.Context, name string) error
	ListFSTypes() ([]string, error)
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

// Status returns the component status snapshot.
func (a *Adapter) Status() ([]control.Unit, error) { return a.plane.Status(), nil }

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
