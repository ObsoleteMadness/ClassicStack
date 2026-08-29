//go:build !fswatch && !all

// Package fswatch stub: when the fswatch build tag is absent, the host-filesystem
// watcher is not linked (its heavy fsnotify dependency is excluded). New returns an
// inert Watcher whose Start/Stop are no-ops, so compose can reference the adapter
// unconditionally and a build without the tag simply runs no host watcher — the §10e
// inbound edge is absent, exactly as on a platform with no external mutator.
package fswatch

import (
	"context"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/component"
)

// Name is the component name for the host watcher (stub).
const Name = "FSWatch"

// BusFor resolves the shared FS-mutation bus for a host path. Unused in the stub.
type BusFor func(hostPath string) bus.Bus

// Logger is the minimal logging seam. Unused in the stub.
type Logger interface {
	Logf(format string, args ...any)
}

// Watcher is the inert stand-in linked when the fswatch tag is absent.
type Watcher struct{}

// New returns an inert watcher (no fsnotify dependency linked).
func New(_ Logger, _ BusFor, _ []string) *Watcher { return &Watcher{} }

// Name returns the component name.
func (*Watcher) Name() string { return Name }

// Start is a no-op (no watcher in this build).
func (*Watcher) Start(context.Context) error { return nil }

// Stop is a no-op.
func (*Watcher) Stop(context.Context) error { return nil }

var _ component.Component = (*Watcher)(nil)
