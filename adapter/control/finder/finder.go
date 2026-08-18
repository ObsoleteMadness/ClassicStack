// Package finder is the operator file-browser surface served by the HTTP control
// adapter: it lists this instance’s live AFP/SMB/NCP/EtherDFS shares and opens
// sessioned catalogs over fs.ForkFS (local live shares, or remote servers via the
// client SDK). It is NOT part of core/control.Plane — browsing is a distinct
// concern from config/lifecycle, and protocol types stay in this adapter the
// same way adapter/control/diag keeps them out of the neutral plane.
//
// Ring: ADAPTER.
package finder

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/client/uri"
	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/log"

	// Register client schemes so Connect/Browse work when this adapter is linked.
	_ "github.com/ObsoleteMadness/ClassicStack/client/afp"
	_ "github.com/ObsoleteMadness/ClassicStack/client/etherdfs"
	_ "github.com/ObsoleteMadness/ClassicStack/client/ncp"
	_ "github.com/ObsoleteMadness/ClassicStack/client/smb"
)

// sessionIdle is the unused-session timeout when [Client] is omitted (10 minutes).
const sessionIdle = 10 * time.Minute

// ErrNotFound is returned when a session, node, or volume does not exist.
var ErrNotFound = errors.New("finder: not found")

// ErrReadOnly is returned when a mutation is attempted on a read-only catalog.
var ErrReadOnly = errors.New("finder: volume is read-only")

// ErrMountUnavailable is returned when the binary has no FUSE/WinFsp host.
var ErrMountUnavailable = errors.New("finder: host mount requires FUSE (macFUSE/libfuse, -tags fuse) or WinFsp")

// ErrLocalMount is returned when the operator tries to FUSE-mount a live local share.
var ErrLocalMount = errors.New("finder: cannot mount this instance's own shares")

// ErrClientDisabled is returned when remote client ops run with [Client] off.
var ErrClientDisabled = errors.New("finder: client is disabled ([Client].enabled)")

// ErrServiceDisabled is returned when a scheme is not in [Client].services.
var ErrServiceDisabled = errors.New("finder: client service is not enabled")

// ErrMountDisabled is returned when [Client].mount is false.
var ErrMountDisabled = errors.New("finder: host mounting is disabled ([Client].mount)")

// componentSource is the read-only lookup the finder needs. *runtime.Runtime
// satisfies it (Component + Built), matching adapter/control/diag. When the source
// also exposes Model() *config.Model (Runtime does), New binds the outbound client
// to that model's default [[interface]] so every in-process client — web Finder,
// FUSE/WinFsp mount, later per-protocol overrides — shares the server config
// without the cmd edge teaching it.
type componentSource interface {
	Component(name string) component.Component
	Built() []string
}

// Service holds live-share resolution and the session table.
type Service struct {
	src  componentSource
	log  log.Logger
	mu   sync.Mutex
	sess map[string]*Session
	// mounts are host FUSE/WinFsp attachments independent of browse sessions.
	mounts map[string]*liveMount
	// seen is the last successful Discover result per scheme. GET /finder/discover
	// returns it instantly; a new scan replaces that scheme when it finishes.
	seen     map[string][]VolumeInfo
	idle     time.Duration
	scanning bool
	cancel   context.CancelFunc
	reapStop chan struct{}
	// defaultLink supplies the live [[interface]] used when a request omits
	// ifaceType/iface. New binds it from src.Model() when present; SetLinkConfig
	// overrides (tests, a later per-protocol panel).
	defaultLink func() config.InterfaceSection
	// pub, if set, carries AFP client pop-ups (login greeting / attention) onto
	// the telemetry bus so the web UI can show them. Messenger WinPopup events
	// are published by the messenger service itself; this is the AFP-client path.
	pub bus.Bus
}

// New builds a finder Service over the runtime (or any component source). A nil
// source still serves remote connect/discover; local listing is empty. A source
// that implements Model() *config.Model (as *runtime.Runtime does) is the default
// outbound client link: discover/connect/FUSE inherit [[interface]] unless a
// request names ifaceType/iface.
func New(src componentSource, logger log.Logger) *Service {
	if logger == nil {
		logger = log.New("finder")
	}
	s := &Service{
		src:    src,
		log:    logger,
		sess:   make(map[string]*Session),
		mounts: make(map[string]*liveMount),
		seen:   make(map[string][]VolumeInfo),
		idle:   sessionIdle,
	}
	s.bindModelLink()
	if cfg := s.clientConfig(); cfg.MaxIdleMinutes != 0 {
		s.idle = cfg.IdleDuration()
	}
	s.reapStop = make(chan struct{})
	go s.reapLoop()
	return s
}

// SetPublisher installs the telemetry bus used to surface AFP client pop-ups
// (FPGetSrvrMsg login greeting and attention messages) to the web UI. Nil
// disables publishing. Messenger / WinPopup events do not go through here —
// core/service/messenger already publishes on TopicMessage.
func (s *Service) SetPublisher(p bus.Bus) { s.pub = p }

// modelSource is the optional capability a componentSource implements when it can
// supply the live config Model. *runtime.Runtime satisfies it.
type modelSource interface {
	Model() *config.Model
}

func (s *Service) bindModelLink() {
	if s.src == nil {
		return
	}
	ms, ok := s.src.(modelSource)
	if !ok || ms == nil {
		return
	}
	s.defaultLink = func() config.InterfaceSection {
		m := ms.Model()
		if m == nil {
			return config.InterfaceSection{}
		}
		if name := strings.TrimSpace(m.Client.Iface); name != "" {
			if iface, ok := m.Interface(name); ok {
				return iface
			}
			return m.ResolveInterface(config.InterfaceSection{Name: name})
		}
		return m.DefaultInterface()
	}
}

func (s *Service) reapLoop() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-s.reapStop:
			return
		case <-t.C:
			s.reapIdle()
		}
	}
}

func (s *Service) reapIdle() {
	s.mu.Lock()
	defer s.mu.Unlock()
	idle := s.idle
	if idle <= 0 {
		idle = sessionIdle
	}
	now := time.Now()
	for id, sess := range s.sess {
		if sess.FS != nil {
			continue // mounted volumes are global until eject
		}
		if now.Sub(sess.touched) > idle {
			sess.closeLocked()
			delete(s.sess, id)
			s.log.Log1(log.Debug, "finder session expired", log.Str("session", id))
		}
	}
}

// VolumeInfo is one operator-visible share on this instance or a remote server.
type VolumeInfo struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"` // local | afp | smb | ncp | etherdfs
	Title     string `json:"title"`
	Subtitle  string `json:"subtitle,omitempty"`
	Protocol  string `json:"protocol,omitempty"`
	Transport string `json:"transport,omitempty"` // tcp | ddp | ipx | netbeui | etherdfs (remote clients)
	Address   string `json:"address,omitempty"`   // protocol-native: DDP net.node + zone, IP, IPX net:node, MAC
	URI       string `json:"uri,omitempty"`       // copyable connect URI (no volume, no trailing slash)
	OS        string `json:"os,omitempty"`        // SMB: announced OS (e.g. "Windows 98 (4.10)")
	Version   string `json:"version,omitempty"`   // SMB: negotiated dialect (e.g. "SMB 1.0 (NT LM 0.12)")
	ReadOnly  bool   `json:"readOnly"`
}

// serverURI is the operator-facing connect URI for a discovered server: scheme, native
// server identity, and optional ",transport" tail, with no volume or trailing slash.
func serverURI(scheme, server, transport string) string {
	return uri.Target{Scheme: scheme, Server: server, Transport: transport}.String()
}

// SessionInfo is returned after connect/login.
type SessionInfo struct {
	SessionID  string   `json:"sessionId"`
	ServerName string   `json:"serverName"`
	Kind       string   `json:"kind"`
	Volumes    []string `json:"volumes"`
	AllowGuest bool     `json:"allowGuest"`
	UAMs       []string `json:"uams,omitempty"` // AFP UAMs, SMB capabilities, or NCP login methods
	RootID     uint32   `json:"rootId,omitempty"`
	Volume     string   `json:"volume,omitempty"` // currently open volume, if any
	Target     string   `json:"target,omitempty"`
	Transport  string   `json:"transport,omitempty"`
	OS         string   `json:"os,omitempty"`
	Dialect    string   `json:"dialect,omitempty"`
}

// MountedVolume is one volume this instance currently has open as a client
// (Finder browse or FUSE/WinFsp host mount). The list is process-global: every
// web client sees the same mounts.
type MountedVolume struct {
	SessionID  string `json:"sessionId"`
	Kind       string `json:"kind"`
	ServerName string `json:"serverName"`
	Volume     string `json:"volume"`
	Target     string `json:"target,omitempty"`
	Transport  string `json:"transport,omitempty"`
	RootID     uint32 `json:"rootId,omitempty"`
	Mountpoint string `json:"mountpoint,omitempty"`
}

// Node is the JSON catalog node (CNID-addressed). Fork bytes are omitted; use
// Fork/EnsureContent to hydrate them.
type Node struct {
	ID            uint32 `json:"id"`
	ParentID      uint32 `json:"parentId"`
	Name          string `json:"name"`
	IsDir         bool   `json:"isDir"`
	DataBytes     int64  `json:"dataBytes"`
	ResourceBytes int64  `json:"resourceBytes"`
	FinderInfo    []byte `json:"finderInfo"`
	CreateDate    uint32 `json:"createDate"` // AFP Mac time (seconds since 2000-01-01 UTC)
	ModDate       uint32 `json:"modDate"`
}

// Session is one operator catalog: a live local ForkFS or a remote client.Connect.
type Session struct {
	ID         string
	Kind       string
	ServerName string
	Volumes    []string
	Volume     string
	FS         fs.ForkFS
	local      bool // live share: Close must NOT tear down the service FS
	hostMount  bool // FUSE/WinFsp owns FS; CloseSession must not CloseFS
	readOnly   bool
	touched    time.Time
	// remotePending holds connect parameters until OpenVolume.
	remoteURI  string
	remoteUser string
	remotePass string
	ifaceType  string
	iface      string
	transport  string
	os         string
	dialect    string
	uams       []string // AFP UAMs, SMB capabilities, or NCP login methods
	allowGuest bool
}

func (sess *Session) touch() { sess.touched = time.Now() }

func (sess *Session) closeLocked() {
	if sess.FS != nil && !sess.local && !sess.hostMount {
		_ = fs.CloseFS(sess.FS)
	}
	sess.FS = nil
}

func (s *Service) get(id string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sess[id]
	if !ok {
		return nil, ErrNotFound
	}
	sess.touch()
	return sess, nil
}

func (s *Service) put(sess *Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sess[sess.ID] = sess
}

// CloseSession drops a session and releases a remote ForkFS. A host-mount
// session ejects the FUSE/WinFsp attachment as well — mounts are global.
func (s *Service) CloseSession(id string) error {
	s.mu.Lock()
	sess, ok := s.sess[id]
	if !ok {
		s.mu.Unlock()
		return ErrNotFound
	}
	host := sess.hostMount
	fsys := sess.FS
	sess.closeLocked()
	delete(s.sess, id)
	s.mu.Unlock()
	s.log.Log1(log.Debug, "finder session closed", log.Str("session", id))
	if host {
		if mountID := s.mountIDForFS(fsys); mountID != "" {
			_ = s.Unmount(mountID)
		}
	}
	return nil
}
