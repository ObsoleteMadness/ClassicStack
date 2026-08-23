package finder

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
)

// ClientState is GET /finder/state: the in-process file client's live snapshot.
// The Finder API (and the web SPA) read networks, connections, and open volumes
// from here rather than keeping their own session tables.
type ClientState struct {
	Enabled      bool            `json:"enabled"`
	Scanning     bool            `json:"scanning"`
	MountEnabled bool            `json:"mountEnabled"`
	Iface        string          `json:"iface,omitempty"`
	Services     []string        `json:"services,omitempty"`
	Networks     []VolumeInfo    `json:"networks"`
	Connections  []SessionInfo   `json:"connections"`
	Volumes      []MountedVolume `json:"volumes"`
}

func (s *Service) model() *config.Model {
	if s.src == nil {
		return nil
	}
	ms, ok := s.src.(modelSource)
	if !ok || ms == nil {
		return nil
	}
	return ms.Model()
}

func (s *Service) clientConfig() config.ClientSection {
	m := s.model()
	if m == nil {
		return config.ClientSection{}
	}
	return m.Client
}

// clientConfigured reports whether a live Model is present. Tests that construct
// New(nil, nil) have no model and are not gated by [Client].enabled.
func (s *Service) clientConfigured() bool {
	return s.model() != nil
}

func (s *Service) clientEnabled() bool {
	if !s.clientConfigured() {
		return true
	}
	return s.clientConfig().Enabled
}

func (s *Service) mountAllowed() bool {
	if !s.clientConfigured() {
		return true
	}
	cfg := s.clientConfig()
	return cfg.Enabled && cfg.Mount
}

func (s *Service) sessionIdle() time.Duration {
	s.mu.Lock()
	idle := s.idle
	s.mu.Unlock()
	if idle <= 0 {
		return sessionIdle
	}
	return idle
}

func (s *Service) requireClient(kind string) error {
	if !s.clientConfigured() {
		return nil
	}
	cfg := s.clientConfig()
	if !cfg.Enabled {
		return ErrClientDisabled
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" || kind == KindLocal {
		return nil
	}
	if !cfg.AllowsService(kind) {
		s.log.Log1(log.Debug, "finder client service disabled", log.Str("scheme", kind))
		return fmt.Errorf("%w: %s", ErrServiceDisabled, kind)
	}
	return nil
}

// Start launches the in-process client: when [Client] is enabled it scans the LAN
// for every configured scheme and records the result for the Finder API.
func (s *Service) Start(ctx context.Context) error {
	cfg := s.clientConfig()
	s.mu.Lock()
	s.idle = cfg.IdleDuration()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	if s.reapStop == nil {
		s.reapStop = make(chan struct{})
		go s.reapLoop(s.reapStop)
	}
	s.mu.Unlock()
	if !cfg.Enabled {
		s.log.Log0(log.Debug, "client disabled")
		return nil
	}
	s.log.Log(log.Info, "client starting",
		log.Str("iface", cfg.Iface),
		log.Int("idle_minutes", int64(cfg.MaxIdleMinutes)),
		log.Bool("mount", cfg.Mount))
	scanCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.cancel = cancel
	s.mu.Unlock()
	go s.scanAll(scanCtx)
	go s.autoMountAll(scanCtx)
	return nil
}

// Close shuts down the client (scan, mounts, remote sessions). Prefer Stop when
// the service runs under the supervisor.
func (s *Service) Close() { s.shutdown() }

func (s *Service) scanAll(ctx context.Context) {
	s.mu.Lock()
	s.scanning = true
	s.mu.Unlock()
	s.publishScanning(true)
	defer func() {
		s.mu.Lock()
		s.scanning = false
		s.mu.Unlock()
		s.publishScanning(false)
	}()

	cfg := s.clientConfig()
	schemes := cfg.EnabledServices()
	s.log.Log1(log.Debug, "client network scan start", log.Int("schemes", int64(len(schemes))))
	for _, scheme := range schemes {
		if ctx.Err() != nil {
			s.log.Log0(log.Debug, "client network scan cancelled")
			return
		}
		vols, err := s.Discover(DiscoverRequest{Scheme: scheme})
		if err != nil {
			s.log.Log2(log.Error, "client network scan failed",
				log.Str("scheme", scheme), log.Str("err", err.Error()))
			continue
		}
		s.log.Log2(log.Debug, "client network scan",
			log.Str("scheme", scheme), log.Int("count", int64(len(vols))))
	}
	n := s.LastSeen("")
	s.log.Log1(log.Info, "client network scan complete", log.Int("count", int64(len(n))))
}

// State is GET /finder/state: enabled flag, last LAN scan, live connections, open volumes.
func (s *Service) State() ClientState {
	cfg := s.clientConfig()
	s.mu.Lock()
	scanning := s.scanning
	s.mu.Unlock()
	st := ClientState{
		Enabled:      s.clientEnabled(),
		Scanning:     scanning,
		MountEnabled: s.mountAllowed() && platformMountAvailable(),
		Iface:        strings.TrimSpace(cfg.Iface),
		Services:     cfg.EnabledServices(),
		Networks:     s.LastSeen(""),
		Connections:  s.Connections(),
		Volumes:      s.MountedVolumes(),
	}
	if st.Networks == nil {
		st.Networks = []VolumeInfo{}
	}
	if st.Connections == nil {
		st.Connections = []SessionInfo{}
	}
	if st.Volumes == nil {
		st.Volumes = []MountedVolume{}
	}
	return st
}

// Connections lists remote (non-local) Finder sessions — connected servers,
// whether or not a volume is currently open.
func (s *Service) Connections() []SessionInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]SessionInfo, 0, len(s.sess))
	for _, sess := range s.sess {
		if sess.local {
			continue
		}
		out = append(out, *sess.info())
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ServerName != out[j].ServerName {
			return out[i].ServerName < out[j].ServerName
		}
		return out[i].SessionID < out[j].SessionID
	})
	return out
}
