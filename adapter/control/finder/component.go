package finder

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
)

// Name is the supervised component / [Client] section key.
const Name = config.ClientKey

// RuntimeSource is a componentSource backed by a built runtime's component map.
// The compose registry factory passes the map populated after ports and services
// are built so LocalVolumes can resolve live AFP/SMB/NCP/EtherDFS shares.
type RuntimeSource struct {
	Comps       map[string]component.Component
	ConfigModel *config.Model
}

func (r *RuntimeSource) Component(name string) component.Component {
	if r == nil || r.Comps == nil {
		return nil
	}
	return r.Comps[name]
}

func (r *RuntimeSource) Built() []string {
	if r == nil || r.Comps == nil {
		return nil
	}
	out := make([]string, 0, len(r.Comps))
	for n := range r.Comps {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func (r *RuntimeSource) Model() *config.Model {
	if r == nil {
		return nil
	}
	return r.ConfigModel
}

// Enabled reports [Client].enabled (component.Enableable).
func (s *Service) Enabled() bool {
	if !s.clientConfigured() {
		return true
	}
	return s.clientConfig().Enabled
}

// Kind labels the in-process client for the dashboard (component.Describable).
func (s *Service) Kind() string { return "client" }

// Props surfaces client config for the dashboard.
func (s *Service) Props() map[string]string {
	cfg := s.clientConfig()
	svc := strings.Join(cfg.EnabledServices(), ",")
	if svc == "" {
		svc = "none"
	}
	return map[string]string{
		"iface":    strings.TrimSpace(cfg.Iface),
		"services": svc,
		"mount":    fmt.Sprintf("%t", cfg.Mount && platformMountAvailable()),
	}
}

// Name implements component.Component.
func (s *Service) Name() string { return Name }

// Stop tears down LAN scan, host mounts, and remote sessions (component.Component).
func (s *Service) Stop(ctx context.Context) error {
	_ = ctx
	s.shutdown()
	return nil
}

func (s *Service) shutdown() {
	s.log.Log0(log.Debug, "client shutting down")
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	if s.reapStop != nil {
		close(s.reapStop)
		s.reapStop = nil
	}
	mountIDs := make([]string, 0, len(s.mounts))
	for id := range s.mounts {
		mountIDs = append(mountIDs, id)
	}
	sessionIDs := make([]string, 0, len(s.sess))
	for id, sess := range s.sess {
		if sess != nil && !sess.local {
			sessionIDs = append(sessionIDs, id)
		}
	}
	s.mu.Unlock()
	for _, id := range mountIDs {
		if err := s.Unmount(id); err != nil && !errors.Is(err, ErrNotFound) {
			s.log.Log2(log.Error, "client unmount on stop failed",
				log.Str("id", id), log.Str("err", err.Error()))
		}
	}
	for _, id := range sessionIDs {
		if err := s.CloseSession(id); err != nil && !errors.Is(err, ErrNotFound) {
			s.log.Log2(log.Error, "client session close on stop failed",
				log.Str("session", id), log.Str("err", err.Error()))
		}
	}
	s.log.Log0(log.Debug, "client stopped")
}

var (
	_ component.Component   = (*Service)(nil)
	_ component.Enableable  = (*Service)(nil)
	_ component.Describable = (*Service)(nil)
)
