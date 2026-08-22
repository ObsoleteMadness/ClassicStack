package finder

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"

	csfuse "github.com/ObsoleteMadness/ClassicStack/client/fuse"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
)

// MountHint is the rebuild message when host mounting is not in this binary.
const MountHint = "Rebuild with -tags fuse (and cgo) for macFUSE/libfuse, or use WinFsp on Windows."

// MountRequest is POST /finder/mount.
type MountRequest struct {
	Kind       string `json:"kind"`
	ID         string `json:"id"`
	Target     string `json:"target"`
	Volume     string `json:"volume"`
	Mountpoint string `json:"mountpoint"`
	User       string `json:"user"`
	Password   string `json:"password"`
	Guest      bool   `json:"guest"`
	ReadOnly   bool   `json:"readOnly"`
	SessionID  string `json:"sessionId,omitempty"`
	IfaceType  string `json:"ifaceType"`
	Iface      string `json:"iface"`
	Transport  string `json:"transport"`
}

// MountInfo is one live host mount.
type MountInfo struct {
	ID         string `json:"id"`
	Mountpoint string `json:"mountpoint"`
	Volume     string `json:"volume"`
	Kind       string `json:"kind"`
	Server     string `json:"server,omitempty"`
}

// MountStatus is GET /finder/mount.
type MountStatus struct {
	Available       bool        `json:"mountAvailable"`
	DefaultMountDir string      `json:"defaultMountDir"`
	Hint            string      `json:"hint,omitempty"`
	Mounts          []MountInfo `json:"mounts"`
}

type liveMount struct {
	info    MountInfo
	unmount func()
	fsys    fs.ForkFS
	reused  bool // fsys borrowed from an existing browse session (do not CloseFS on mount setup failure)
}

// MountStatus reports whether host mounting is in this binary and lists live mounts.
func (s *Service) MountStatus() MountStatus {
	st := MountStatus{
		Available:       platformMountAvailable() && s.mountAllowed(),
		DefaultMountDir: DefaultMountDir(),
		Mounts:          []MountInfo{},
	}
	if !platformMountAvailable() {
		st.Hint = MountHint
	} else if !s.mountAllowed() {
		st.Hint = "Enable [Client].mount in server.toml to allow FUSE/WinFsp host mounts."
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.mounts {
		st.Mounts = append(st.Mounts, m.info)
	}
	return st
}

// DefaultMountDir is the parent directory suggested for a new host mount.
func DefaultMountDir() string {
	switch runtime.GOOS {
	case "darwin":
		return DarwinVolumesDir
	case "windows":
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return `C:\ClassicStack`
		}
		return filepath.Join(home, "ClassicStack")
	default:
		return "/mnt/classicstack"
	}
}

// Mount attaches a remote share at mountpoint via FUSE or WinFsp.
func (s *Service) Mount(ctx context.Context, req MountRequest) (*MountInfo, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.fuseConfig().MountTimeout())
		defer cancel()
	}
	if !platformMountAvailable() {
		s.log.Log0(log.Error, "finder host mount unavailable")
		return nil, ErrMountUnavailable
	}
	if !s.mountAllowed() {
		s.log.Log0(log.Debug, "finder host mount disabled")
		return nil, ErrMountDisabled
	}
	kind, volume, server, fsys, reused, err := s.mountFS(ctx, req)
	if err != nil {
		s.logMountFailure(req, err)
		return nil, err
	}
	releaseOnFail := func() {
		if reused {
			s.clearHostMount(fsys)
			return
		}
		_ = fs.CloseFS(fsys)
	}
	point := strings.TrimSpace(req.Mountpoint)
	if point == "" {
		point = filepath.Join(DefaultMountDir(), sanitizeMountName(volume))
	}
	point, err = csfuse.ResolveMountpoint(point)
	if err != nil {
		releaseOnFail()
		return nil, fmt.Errorf("finder: mountpoint: %w", err)
	}
	s.log.Log1(log.Debug, "finder host mountpoint resolved", log.Str("mountpoint", point))
	if err := prepareMountpoint(point); err != nil {
		releaseOnFail()
		return nil, fmt.Errorf("finder: mountpoint %s: %w", point, err)
	}
	if macFUSECreatesMountpoint(point) {
		s.log.Log1(log.Debug, "finder host mountpoint left for macFUSE", log.Str("mountpoint", point))
	}
	unmount, err := platformMount(fsys, point, volume, req.ReadOnly)
	if err != nil {
		releaseOnFail()
		s.log.Log2(log.Error, "finder host mount failed", log.Str("mountpoint", point), log.Str("err", err.Error()))
		return nil, err
	}
	id := newSessionID()
	info := MountInfo{ID: id, Mountpoint: point, Volume: volume, Kind: kind, Server: server}
	s.mu.Lock()
	s.mounts[id] = &liveMount{info: info, unmount: unmount, fsys: fsys, reused: reused}
	s.mu.Unlock()
	s.log.Log(log.Debug, "finder host mounted",
		log.Str("id", id), log.Str("mountpoint", point), log.Str("volume", volume), log.Bool("reused", reused))
	return &info, nil
}

// logMountAttempt records one host-mount dial (reuse or fresh client.Connect).
func (s *Service) logMountAttempt(req MountRequest, kind, volume, server, rawURI, auth, ifaceType, iface string, reused, browseHasFS bool, reuseSkip string) {
	s.log.Log(log.Debug, "finder host mount attempt",
		log.Str("kind", kind),
		log.Str("volume", volume),
		log.Str("server", server),
		log.Str("target", rawURI),
		log.Str("session", strings.TrimSpace(req.SessionID)),
		log.Str("auth", auth),
		log.Str("ifacetype", ifaceType),
		log.Str("iface", iface),
		log.Bool("reused", reused),
		log.Bool("browse_has_fs", browseHasFS),
		log.Str("reuse_skip", reuseSkip),
		log.Bool("read_only", req.ReadOnly))
}

func (s *Service) logMountFailure(req MountRequest, err error) {
	s.log.Log(log.Warn, "finder host mount connect failed",
		log.Str("session", strings.TrimSpace(req.SessionID)),
		log.Str("volume", strings.TrimSpace(req.Volume)),
		log.Str("kind", strings.TrimSpace(req.Kind)),
		log.Str("target", strings.TrimSpace(req.Target)),
		log.Str("err", err.Error()))
}

// mountAuthLabel returns a log-safe auth mode string (never includes a password).
func mountAuthLabel(guest bool, user string) string {
	if guest || strings.TrimSpace(user) == "" {
		return "guest"
	}
	return "user:" + strings.TrimSpace(user)
}

// Unmount tears down a host mount by id or mountpoint.
func (s *Service) Unmount(idOrPoint string) error {
	idOrPoint = strings.TrimSpace(idOrPoint)
	if idOrPoint == "" {
		return fmt.Errorf("finder: mount id required")
	}
	s.mu.Lock()
	var found *liveMount
	var key string
	for k, m := range s.mounts {
		if m.info.ID == idOrPoint || m.info.Mountpoint == idOrPoint {
			found = m
			key = k
			break
		}
	}
	if found == nil {
		s.mu.Unlock()
		return ErrNotFound
	}
	delete(s.mounts, key)
	s.detachHostMountLocked(found.fsys)
	s.mu.Unlock()
	if found.unmount != nil {
		found.unmount()
	}
	if found.fsys != nil {
		_ = fs.CloseFS(found.fsys)
	}
	s.log.Log1(log.Debug, "finder host unmounted", log.Str("id", found.info.ID))
	return nil
}

// mountIDForFS returns a live host-mount id whose ForkFS equals fsys, or "".
func (s *Service) mountIDForFS(fsys fs.ForkFS) string {
	if fsys == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, m := range s.mounts {
		if m != nil && m.fsys == fsys {
			return id
		}
	}
	return ""
}

// clearHostMount drops hostMount on every session borrowing fsys (mount setup failed).
func (s *Service) clearHostMount(fsys fs.ForkFS) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sess := range s.sess {
		if sess.FS == fsys {
			sess.hostMount = false
		}
	}
}

// detachHostMountLocked clears browse sessions tied to fsys before the host mount
// closes it. Caller must hold s.mu.
func (s *Service) detachHostMountLocked(fsys fs.ForkFS) {
	for _, sess := range s.sess {
		if sess.FS != fsys {
			continue
		}
		sess.hostMount = false
		sess.FS = nil
		sess.Volume = ""
	}
}

func sessionVolumeMatches(sess *Session, volume string) bool {
	if sess.FS == nil {
		return false
	}
	volume = strings.TrimSpace(volume)
	if volume == "" {
		return true
	}
	if sess.Volume != "" {
		return strings.EqualFold(sess.Volume, volume)
	}
	return len(sess.Volumes) == 1 && strings.EqualFold(sess.Volumes[0], volume)
}

func (s *Service) mountFS(ctx context.Context, req MountRequest) (kind, volume, server string, fsys fs.ForkFS, reused bool, err error) {
	kind = strings.ToLower(strings.TrimSpace(req.Kind))
	volume = strings.TrimSpace(req.Volume)
	server = strings.TrimSpace(req.Target)
	if server == "" {
		server = strings.TrimSpace(req.ID)
	}
	user, pass := req.User, req.Password
	if req.Guest {
		user, pass = "", ""
	}
	ifaceType, iface, transport := req.IfaceType, req.Iface, req.Transport
	rawURI := server
	var browse *Session

	if req.SessionID != "" {
		sess, getErr := s.get(req.SessionID)
		if getErr != nil {
			return "", "", "", nil, false, getErr
		}
		if sess.local || sess.Kind == KindLocal {
			return "", "", "", nil, false, ErrLocalMount
		}
		browse = sess
		kind = sess.Kind
		server = sess.ServerName
		rawURI = sess.remoteURI
		user, pass = sess.remoteUser, sess.remotePass
		ifaceType, iface, transport = sess.ifaceType, sess.iface, sess.transport
		if volume == "" {
			if sess.Volume != "" {
				volume = sess.Volume
			} else if len(sess.Volumes) == 1 {
				volume = sess.Volumes[0]
			}
		}
	}
	if kind == KindLocal || strings.HasPrefix(req.ID, "local:") {
		return "", "", "", nil, false, ErrLocalMount
	}
	if kind == "" {
		kind = KindAFP
	}
	if volume == "" {
		return "", "", "", nil, false, fmt.Errorf("finder: volume name required to mount")
	}
	if browse != nil && browse.FS == nil {
		if err := s.connectRemoteVolume(browse, volume); err != nil {
			return "", "", "", nil, false, err
		}
	}
	auth := mountAuthLabel(req.Guest, user)
	browseHasFS := browse != nil && browse.FS != nil
	reuseSkip := ""
	if browse != nil && !sessionVolumeMatches(browse, volume) {
		switch browse.FS {
		case nil:
			reuseSkip = "browse_session_has_no_open_volume"
		default:
			reuseSkip = "volume_mismatch"
		}
	}
	if browse != nil && sessionVolumeMatches(browse, volume) {
		s.mu.Lock()
		browse.hostMount = true
		s.mu.Unlock()
		s.logMountAttempt(req, kind, volume, server, rawURI, auth, ifaceType, iface, true, browseHasFS, "")
		s.log.Log(log.Debug, "finder host mount reusing browse session",
			log.Str("session", browse.ID), log.Str("volume", volume))
		return kind, volume, server, browse.FS, true, nil
	}
	s.logMountAttempt(req, kind, volume, server, rawURI, auth, ifaceType, iface, false, browseHasFS, reuseSkip)
	fsys, err = s.remoteForkFS(ctx, kind, rawURI, server, volume, user, pass, ifaceType, iface, transport, req.ReadOnly)
	if err != nil {
		return "", "", "", nil, false, err
	}
	return kind, volume, server, fsys, false, nil
}

// prepareMountpoint makes sure point can be handed to FUSE/WinFsp. On Darwin a
// missing /Volumes/<name> leaf is left for macFUSE's setuid mount_macfuse to
// create (spaces in the leaf are fine; pre-mkdir by a regular user is not).
func prepareMountpoint(point string) error {
	if macFUSECreatesMountpoint(point) {
		return nil
	}
	return os.MkdirAll(point, 0o755)
}

// macFUSECreatesMountpoint reports whether point is a direct child of /Volumes
// on Darwin. macFUSE 3.5+ creates that leaf automatically; nested paths and
// locations outside /Volumes still need an existing empty directory.
func macFUSECreatesMountpoint(point string) bool {
	return runtime.GOOS == "darwin" && isDarwinVolumesLeaf(point)
}

func isDarwinVolumesLeaf(point string) bool {
	slash := filepath.ToSlash(point)
	if !strings.HasPrefix(slash, DarwinVolumesDir+"/") {
		return false
	}
	p := path.Clean(slash)
	return path.Dir(p) == DarwinVolumesDir && path.Base(p) != "." && path.Base(p) != "/"
}

func sanitizeMountName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "ClassicStack"
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|':
			b.WriteByte('_')
		case unicode.IsControl(r):
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "" || out == "." || out == ".." {
		return "ClassicStack"
	}
	return out
}
