// Package portbase holds the shared placeholder port implementation the
// per-transport port packages (ethertalk, localtalk, ipx, netbeui) embed in
// Phase 1. It satisfies component.Component plus the Bindable/Statful/
// Configurable/Enableable capabilities, takes a link, and no-ops the data path.
//
// Ring: CORE (stdlib + core interfaces only). Real per-transport ports over
// real links land in Phase 2; this base exists only so the harness is runnable.
package portbase
