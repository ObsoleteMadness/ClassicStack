//go:build fswatch || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/adapter/fswatch"
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
)

// The §10e host-filesystem watcher is not a config-section component (it watches
// whatever shares exist, with no section of its own) and it has lifecycle, so — like
// the auth store's BuildUserStore — the registry exposes a builder the compose root
// calls, rather than a name→factory entry. It is built only when the fswatch adapter
// is linked (this file shares its build tag), so a build without it carries no
// fsnotify dependency and runs no watcher.

// BuildHostWatcher constructs the §10e watcher over the host directories backing the
// model's AFP volumes / SMB shares (config.HostPaths), publishing changes onto the
// SAME per-host-path bus the file services hold (the fsBus broker) stamped
// Origin:"fsnotify". The compose root adds the returned component to the supervisor
// so it starts/stops with the server. A model with no host-backed shares yields a
// watcher with no roots (inert). The returned component is always non-nil.
func BuildHostWatcher(m *config.Model, logger fswatch.Logger) component.Component {
	return fswatch.New(logger, fsBus.busForPath, m.HostPaths())
}
