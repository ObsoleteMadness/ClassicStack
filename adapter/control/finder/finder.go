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
	"errors"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/log"

	// Register client schemes so Connect/Browse work when this adapter is linked.
	_ "github.com/ObsoleteMadness/ClassicStack/client/afp"
	_ "github.com/ObsoleteMadness/ClassicStack/client/etherdfs"
	_ "github.com/ObsoleteMadness/ClassicStack/client/ncp"
	_ "github.com/ObsoleteMadness/ClassicStack/client/smb"
)

// sessionIdle is how long an unused remote (or local-open) session is kept.
const sessionIdle = 15 * time.Minute

// ErrNotFound is returned when a session, node, or volume does not exist.
var ErrNotFound = errors.New("finder: not found")

// ErrReadOnly is returned when a mutation is attempted on a read-only catalog.
var ErrReadOnly = errors.New("finder: volume is read-only")

// componentSource is the read-only lookup the finder needs. *runtime.Runtime
// satisfies it (Component + Built), matching adapter/control/diag.
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
}

// New builds a finder Service over the runtime (or any component source). A nil
// source still serves remote connect/discover; local listing is empty.
func New(src componentSource, logger log.Logger) *Service {
	if logger == nil {
		logger = log.New("finder")
	}
	s := &Service{
		src:  src,
		log:  logger,
		sess: make(map[string]*Session),
	}
	go s.reapLoop()
	return s
}

func (s *Service) reapLoop() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for range t.C {
		s.reapIdle()
	}
}

func (s *Service) reapIdle() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for id, sess := range s.sess {
		if now.Sub(sess.touched) > sessionIdle {
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
	Transport string `json:"transport,omitempty"` // tcp | ddp | ipx | nbp | etherdfs (remote clients)
	ReadOnly  bool   `json:"readOnly"`
}

// SessionInfo is returned after connect/login.
type SessionInfo struct {
	SessionID  string   `json:"sessionId"`
	ServerName string   `json:"serverName"`
	Kind       string   `json:"kind"`
	Volumes    []string `json:"volumes"`
	AllowGuest bool     `json:"allowGuest"`
	UAMs       []string `json:"uams,omitempty"`
	RootID     uint32   `json:"rootId,omitempty"`
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
	readOnly   bool
	touched    time.Time
	// remotePending holds connect parameters until OpenVolume.
	remoteURI  string
	remoteUser string
	remotePass string
	ifaceType  string
	iface      string
	transport  string
}

func (sess *Session) touch() { sess.touched = time.Now() }

func (sess *Session) closeLocked() {
	if sess.FS != nil && !sess.local {
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

// CloseSession drops a session and releases a remote ForkFS.
func (s *Service) CloseSession(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sess[id]
	if !ok {
		return ErrNotFound
	}
	sess.closeLocked()
	delete(s.sess, id)
	s.log.Log1(log.Debug, "finder session closed", log.Str("session", id))
	return nil
}
