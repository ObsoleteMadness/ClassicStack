// Package control is ClassicStack's transport-agnostic management API: the
// single implementation of every operator action (status, live stats,
// config staging/apply/save, service restart, diagnostics). UIs are thin
// adapters over it — the web UI maps HTTP/SSE onto these methods, and a
// future text/telnet UI can call them directly — so management logic is
// never duplicated per front-end.
//
// The package is untagged and depends only on neutral packages (config
// model, status, metrics), keeping it linkable in every build variant.
package control

import (
	"context"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/pkg/metrics"
	"github.com/ObsoleteMadness/ClassicStack/pkg/serialport"
	"github.com/ObsoleteMadness/ClassicStack/pkg/status"
)

// Supervisor is the lifecycle controller the plane drives. It is satisfied
// by cmd/classicstack's *Supervisor; declaring it here as an interface
// keeps pkg/control free of the cmd package and its build tags.
type Supervisor interface {
	// Apply re-wires the running stack to match cfg, restarting only the
	// units whose configuration changed.
	Apply(ctx context.Context, cfg ConfigModel) error
	// StartService starts a single named unit.
	StartService(ctx context.Context, name string) error
	// StopService stops a single named unit (and its dependents).
	StopService(name string) error
	// RestartService restarts a single named unit (and its dependents).
	RestartService(ctx context.Context, name string) error
	// ListInterfaces returns the host's network interface names for the
	// EtherTalk/IPX/NetBEUI dropdowns.
	ListInterfaces() ([]string, error)
}

// ConfigModel is the in-memory configuration the plane stages and applies.
// It is an opaque handle from the plane's perspective: defined as an
// interface so pkg/control does not depend on the concrete config.Model
// (which lives in package config and is satisfied by *config.Model). The
// plane only needs to serialise it for download/save; cloning for staged
// edits is the caller's responsibility (the UI clones before mutating).
type ConfigModel interface {
	ToTOML() ([]byte, error)
}

// Plane is the management API. It owns the live and staged config models
// and the dirty flag, and delegates lifecycle actions to the Supervisor.
type Plane struct {
	sup Supervisor
	reg *status.Registry
	hub *metrics.Hub

	mu     sync.Mutex
	live   ConfigModel
	staged ConfigModel
	dirty  bool
	path   string // backing file path for Save; "" disables Save
	diag   Diagnostics
	stats  *statsBroadcaster
}

// Deps bundles the plane's collaborators.
type Deps struct {
	Supervisor Supervisor
	Registry   *status.Registry // defaults to status.Default when nil
	Hub        *metrics.Hub     // defaults to metrics.Default when nil
	Config     ConfigModel      // the live config at startup
	ConfigPath string           // file Save writes to ("" = Save disabled)
}

// New constructs a Plane.
func New(d Deps) *Plane {
	reg := d.Registry
	if reg == nil {
		reg = status.Default
	}
	hub := d.Hub
	if hub == nil {
		hub = metrics.Default
	}
	return &Plane{
		sup:  d.Supervisor,
		reg:  reg,
		hub:  hub,
		live: d.Config,
		path: d.ConfigPath,
	}
}

// Status returns a snapshot of all registered service/port/hook units.
func (p *Plane) Status() []status.Unit { return p.reg.Snapshot() }

// ListInterfaces returns host network interface names.
func (p *Plane) ListInterfaces() ([]string, error) {
	if p.sup == nil {
		return nil, nil
	}
	return p.sup.ListInterfaces()
}

// ListSerialPorts returns the host's serial ports for the TashTalk dropdown.
func (p *Plane) ListSerialPorts() ([]serialport.Info, error) {
	return serialport.List()
}

// StartService starts a single named unit.
func (p *Plane) StartService(ctx context.Context, name string) error {
	if p.sup == nil {
		return ErrNoSupervisor
	}
	return p.sup.StartService(ctx, name)
}

// StopService stops a single named unit (and any units depending on it).
func (p *Plane) StopService(name string) error {
	if p.sup == nil {
		return ErrNoSupervisor
	}
	return p.sup.StopService(name)
}

// RestartService restarts a single named unit (and its dependents).
func (p *Plane) RestartService(ctx context.Context, name string) error {
	if p.sup == nil {
		return ErrNoSupervisor
	}
	return p.sup.RestartService(ctx, name)
}
