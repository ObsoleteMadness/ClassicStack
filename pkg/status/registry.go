// Package status is ClassicStack's in-process service-status registry.
// Every port, service, and hook reports a Unit describing whether it is
// enabled and running, what it is bound to, and service-specific detail
// (hostnames, zones, shares). The management plane (pkg/control) reads a
// snapshot to render the dashboard. The registry is untagged so it is
// available to any front-end, including a future text/telnet UI.
package status

import "sync"

// Kind classifies a Unit for grouping in the dashboard.
const (
	KindPort    = "port"
	KindService = "service"
	KindHook    = "hook"
	KindRouter  = "router"
)

// ShareInfo describes a single shared resource (SMB share or AFP volume).
type ShareInfo struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	ReadOnly bool   `json:"read_only"`
}

// Unit is the status of a single managed component.
type Unit struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Enabled bool   `json:"enabled"`
	// Running reflects live lifecycle state (IsRunning) and is updated by
	// SetRunning as the supervisor starts/stops the unit.
	Running bool `json:"running"`
	// Binding is the interface or address the unit is bound to, e.g.
	// "COM1", ":548", "239.192.76.84:1954".
	Binding string `json:"binding,omitempty"`
	// Properties holds generic key/value detail (zone, seed range, …).
	Properties map[string]string `json:"properties,omitempty"`
	// Service-specific structured detail; only the relevant fields are set.
	Hostnames []string    `json:"hostnames,omitempty"`
	Zones     []string    `json:"zones,omitempty"`
	Shares    []ShareInfo `json:"shares,omitempty"`
	// DependsOn names units that must be (re)started around this one, e.g.
	// SMB depends on NetBIOS. Used for dependency-aware restart.
	DependsOn []string `json:"depends_on,omitempty"`
}

// Registry is a concurrency-safe collection of Units keyed by Name.
type Registry struct {
	mu    sync.RWMutex
	units map[string]Unit
	order []string // preserves registration order for stable snapshots
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{units: make(map[string]Unit)}
}

// Default is the process-global registry. Wiring code registers Units here
// without threading a pointer through every constructor, mirroring the
// expvar/telemetry global style.
var Default = NewRegistry()

// Set inserts or replaces the Unit named u.Name.
func (r *Registry) Set(u Unit) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.units[u.Name]; !ok {
		r.order = append(r.order, u.Name)
	}
	r.units[u.Name] = u
}

// SetRunning updates only the Running flag of an existing unit. It is a
// no-op if the unit is not registered.
func (r *Registry) SetRunning(name string, running bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if u, ok := r.units[name]; ok {
		u.Running = running
		r.units[name] = u
	}
}

// Remove deletes a unit by name.
func (r *Registry) Remove(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.units[name]; !ok {
		return
	}
	delete(r.units, name)
	for i, n := range r.order {
		if n == name {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
}

// Snapshot returns a copy of all units in registration order.
func (r *Registry) Snapshot() []Unit {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Unit, 0, len(r.order))
	for _, name := range r.order {
		out = append(out, r.units[name])
	}
	return out
}
