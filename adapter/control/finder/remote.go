package finder

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/client"
	afpclient "github.com/ObsoleteMadness/ClassicStack/client/afp"
	ncpclient "github.com/ObsoleteMadness/ClassicStack/client/ncp"
	smbclient "github.com/ObsoleteMadness/ClassicStack/client/smb"
	"github.com/ObsoleteMadness/ClassicStack/client/uri"
	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	afpproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/afp"
)

// ConnectRequest is POST /finder/sessions.
type ConnectRequest struct {
	Kind      string `json:"kind"` // local | afp | smb | ncp | etherdfs
	ID        string `json:"id"`
	Target    string `json:"target"` // URI or server name
	User      string `json:"user"`
	Password  string `json:"password"`
	Guest     bool   `json:"guest"`
	IfaceType string `json:"ifaceType"`
	Iface     string `json:"iface"`
	Transport string `json:"transport"`
}

// Connect logs into a remote server (or opens a local volume) and returns volumes.
func (s *Service) Connect(ctx context.Context, req ConnectRequest) (*SessionInfo, error) {
	_ = ctx
	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	if kind == "local" || strings.HasPrefix(req.ID, "local:") {
		id := req.ID
		if id == "" {
			id = req.Target
		}
		return s.OpenLocal(id)
	}
	if kind == "" {
		if t, err := uri.Parse(req.Target); err == nil {
			kind = t.Scheme
		}
	}
	if kind == "" {
		return nil, fmt.Errorf("finder: kind or target URI is required")
	}
	if err := s.requireClient(kind); err != nil {
		return nil, err
	}

	rawTarget := strings.TrimSpace(req.Target)
	if rawTarget == "" {
		rawTarget = strings.TrimSpace(req.ID)
	}
	if existing := s.existingMounted(kind, rawTarget); existing != nil {
		info := existing.info()
		// Reuse the login, but do not pretend this connect opened that volume.
		// The Finder lists every share; OpenVolume binds a catalog per volume.
		info.AllowGuest = true
		info.UAMs = nil // empty auth-methods → Finder skips the password prompt
		info.RootID = 0
		info.Volume = ""
		return info, nil
	}

	target, err := parseConnectTarget(kind, req)
	if err != nil {
		return nil, err
	}
	opener, err := s.openerFor(kind, req.IfaceType, req.Iface, req.Transport, target)
	if err != nil {
		return nil, err
	}
	spec := opener.Spec
	opts := client.Options{Opener: opener}
	if req.Guest {
		target.User, target.Pass = "", ""
	}

	var volumes []string
	var uams []string
	var osName, dialect string
	allowGuest := true
	serverName := target.Server
	switch kind {
	case "afp":
		listing, err := afpclient.Browse(target, opts)
		if err != nil {
			return nil, err
		}
		serverName = listing.ServerName
		if serverName == "" {
			serverName = target.Server
		}
		for _, v := range listing.Volumes {
			volumes = append(volumes, v.Name)
		}
		uams = listing.UAMs
		allowGuest = guestAllowed(listing.UAMs, req.Guest, req.User)
	case "smb":
		listing, err := smbclient.Browse(target, opts)
		if err != nil {
			return nil, err
		}
		serverName = listing.ServerName
		if serverName == "" {
			serverName = target.Server
		}
		for _, sh := range listing.Shares {
			if !sh.IsIPC {
				volumes = append(volumes, sh.Name)
			}
		}
		dialect = formatSMBVersion(listing.Dialect)
		osName = s.smbOSFor(serverName)
		uams = formatSMBAuth(listing.UserSecurity, listing.EncryptPasswords, listing.Capabilities)
		allowGuest = true
	case "ncp":
		listing, err := ncpclient.Browse(target, opts)
		if err != nil {
			return nil, err
		}
		serverName = listing.ServerName
		volumes = listing.Volumes
		uams = formatNCPLogin(listing.Encrypted)
		allowGuest = true
	case "etherdfs":
		if target.Volume != "" {
			volumes = []string{target.Volume}
		} else {
			volumes = []string{"C"}
		}
	default:
		return nil, fmt.Errorf("finder: unknown kind %q", kind)
	}

	sess := &Session{
		ID:         newSessionID(),
		Kind:       kind,
		ServerName: serverName,
		Volumes:    volumes,
		remoteURI:  req.Target,
		remoteUser: target.User,
		remotePass: target.Pass,
		ifaceType:  spec.Kind,
		iface:      spec.Name,
		transport:  spec.Carrier,
		os:         osName,
		dialect:    dialect,
		uams:       uams,
		allowGuest: allowGuest,
		touched:    time.Now(),
	}
	if req.Guest {
		sess.remoteUser, sess.remotePass = "", ""
	}
	s.put(sess)
	s.log.Log(log.Debug, "finder remote session",
		log.Str("session", sess.ID), log.Str("server", serverName),
		log.Str("kind", kind), log.Str("auth", strings.Join(uams, "|")))
	return &SessionInfo{
		SessionID:  sess.ID,
		ServerName: serverName,
		Kind:       kind,
		Volumes:    volumes,
		AllowGuest: allowGuest,
		UAMs:       uams,
		OS:         osName,
		Dialect:    dialect,
	}, nil
}

func guestAllowed(advertised []string, guestLogin bool, user string) bool {
	if guestLogin || user == "" {
		return true
	}
	for _, u := range advertised {
		if strings.EqualFold(u, afpproto.UAMNoUserAuthent) {
			return true
		}
	}
	return len(advertised) == 0
}

func parseConnectTarget(kind string, req ConnectRequest) (uri.Target, error) {
	raw := strings.TrimSpace(req.Target)
	if raw == "" {
		raw = req.ID
	}
	if strings.Contains(raw, "://") {
		t, err := uri.Parse(raw)
		if err != nil {
			return uri.Target{}, err
		}
		if req.User != "" {
			t.User = req.User
			t.Pass = req.Password
			t.HasCreds = true
		}
		return t, nil
	}
	if raw == "" {
		return uri.Target{}, fmt.Errorf("finder: missing server")
	}
	return uri.Target{
		Scheme:   kind,
		Server:   raw,
		User:     req.User,
		Pass:     req.Password,
		HasCreds: req.User != "" || req.Password != "",
	}, nil
}

func (s *Service) connectRemoteVolume(sess *Session, volume string) error {
	ffs, err := s.remoteForkFS(context.Background(), sess.Kind, sess.remoteURI, sess.ServerName, volume, sess.remoteUser, sess.remotePass, sess.ifaceType, sess.iface, sess.transport, false)
	if err != nil {
		return err
	}
	if sess.FS != nil && !sess.local {
		_ = fs.CloseFS(sess.FS)
	}
	sess.FS = ffs
	sess.Volume = volume
	sess.local = false
	sess.readOnly = ffs.Capabilities().ReadOnly
	_ = ffs.Meta().EnsureCNID("")
	s.log.Log2(log.Debug, "finder mounted remote volume",
		log.Str("session", sess.ID), log.Str("volume", volume))
	return nil
}

// remoteForkFS opens a dedicated client ForkFS for a remote volume (browse or FUSE mount).
func (s *Service) remoteForkFS(ctx context.Context, kind, rawURI, server, volume, user, pass, ifaceType, iface, transport string, readOnly bool) (fs.ForkFS, error) {
	raw := strings.TrimSpace(rawURI)
	if raw == "" || !strings.Contains(raw, "://") {
		raw = kind + "://" + server + "/" + volume
	}
	target, err := uri.Parse(raw)
	if err != nil {
		target = uri.Target{Scheme: kind, Server: server, Volume: volume}
	}
	target.Volume = volume
	target.Path = ""
	if user != "" || pass != "" {
		target.User = user
		target.Pass = pass
		target.HasCreds = true
	}
	opener, err := s.openerFor(kind, ifaceType, iface, transport, target)
	if err != nil {
		return nil, err
	}
	auth := mountAuthLabel(user == "" && !target.HasCreds, user)
	s.log.Log(log.Debug, "finder remote connect",
		log.Str("scheme", kind),
		log.Str("server", target.Redacted()),
		log.Str("volume", volume),
		log.Str("auth", auth),
		log.Str("ifacetype", opener.Spec.Kind),
		log.Str("iface", opener.Spec.Name))
	ffs, err := client.Connect(ctx, target, client.Options{
		Opener:          opener,
		ReadOnly:        readOnly,
		OnServerMessage: s.onServerMessage,
	})
	if err != nil {
		s.log.Log(log.Warn, "finder remote connect failed",
			log.Str("scheme", kind),
			log.Str("server", target.Redacted()),
			log.Str("volume", volume),
			log.Str("auth", auth),
			log.Str("ifacetype", opener.Spec.Kind),
			log.Str("iface", opener.Spec.Name),
			log.Str("err", err.Error()))
		return nil, err
	}
	return ffs, nil
}

// onServerMessage publishes an AFP client pop-up (login greeting or attention
// message) on the telemetry bus for the web UI. Empty text is ignored.
func (s *Service) onServerMessage(kind, from, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	s.log.Log(log.Debug, "finder AFP server message",
		log.Str("kind", kind), log.Str("from", from), log.Str("text", text))
	if s.pub == nil {
		return
	}
	s.pub.Publish(bus.MessageReceived{
		Kind: bus.MessageKindAFP,
		From: from,
		Text: text,
		Time: time.Now(),
	})
}

// DiscoverRequest is POST /finder/discover.
type DiscoverRequest struct {
	Scheme    string `json:"scheme"`
	IfaceType string `json:"ifaceType"`
	Iface     string `json:"iface"`
	Transport string `json:"transport"`
	Workgroup string `json:"workgroup"`
}

// Discover probes the LAN for file servers of one scheme (csfs discover).
func (s *Service) Discover(req DiscoverRequest) ([]VolumeInfo, error) {
	scheme := strings.ToLower(strings.TrimSpace(req.Scheme))
	if scheme == "" {
		scheme = KindAFP
	}
	if err := s.requireClient(scheme); err != nil {
		return nil, err
	}
	var out []VolumeInfo
	var err error
	switch scheme {
	case KindAFP:
		out, err = s.discoverAFP(req)
	case KindSMB:
		out, err = s.discoverSMB(req)
	case KindNCP:
		out, err = s.discoverNCP(req)
	case KindEtherDFS:
		out, err = s.discoverEtherDFS(req)
	default:
		return nil, fmt.Errorf("finder: unknown discover scheme %q", scheme)
	}
	if err != nil {
		cached := s.LastSeen(scheme)
		if len(cached) == 0 {
			return nil, err
		}
		s.log.Log(log.Debug, "finder discover using last-seen",
			log.Str("scheme", scheme), log.Str("err", err.Error()), log.Int("count", int64(len(cached))))
		return cached, nil
	}
	s.remember(scheme, out)
	s.log.Log2(log.Debug, "finder discover", log.Str("scheme", scheme), log.Int("count", int64(len(out))))
	return s.LastSeen(scheme), nil
}
