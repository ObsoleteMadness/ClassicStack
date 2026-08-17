package finder

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/client"
	afpclient "github.com/ObsoleteMadness/ClassicStack/client/afp"
	"github.com/ObsoleteMadness/ClassicStack/client/atalk"
	"github.com/ObsoleteMadness/ClassicStack/client/browse"
	clientlink "github.com/ObsoleteMadness/ClassicStack/client/link"
	ncpclient "github.com/ObsoleteMadness/ClassicStack/client/ncp"
	smbclient "github.com/ObsoleteMadness/ClassicStack/client/smb"
	"github.com/ObsoleteMadness/ClassicStack/client/uri"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
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

	target, err := parseConnectTarget(kind, req)
	if err != nil {
		return nil, err
	}
	opener, err := openerFor(kind, req.IfaceType, req.Iface, req.Transport, target)
	if err != nil {
		return nil, err
	}
	opts := client.Options{Opener: opener}
	if req.Guest {
		target.User, target.Pass = "", ""
	}

	var volumes []string
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
	case "ncp":
		listing, err := ncpclient.Browse(target, opts)
		if err != nil {
			return nil, err
		}
		serverName = listing.ServerName
		volumes = listing.Volumes
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
		ifaceType:  req.IfaceType,
		iface:      req.Iface,
		transport:  req.Transport,
		touched:    time.Now(),
	}
	if req.Guest {
		sess.remoteUser, sess.remotePass = "", ""
	}
	s.put(sess)
	s.log.Log2(log.Debug, "finder remote session",
		log.Str("session", sess.ID), log.Str("server", serverName))
	return &SessionInfo{
		SessionID:  sess.ID,
		ServerName: serverName,
		Kind:       kind,
		Volumes:    volumes,
		AllowGuest: req.Guest || req.User == "",
		UAMs:       []string{"No User Authent", "Cleartxt Passwrd"},
	}, nil
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

func openerFor(scheme, ifaceType, iface, transport string, target uri.Target) (*clientlink.Opener, error) {
	transports := client.TransportsFor(scheme)
	kind := ifaceType
	if kind == "" && target.Transport != "" {
		kind = target.Transport
	}
	if kind == "" {
		kind = transports.Default
	}
	if kind == "" {
		return nil, fmt.Errorf("finder: ifaceType required for %s", scheme)
	}
	if iface == "" && clientlink.IsRawEtherKind(kind) {
		if def, err := clientlink.DefaultInterface(); err == nil {
			iface = def.Name
		}
	}
	carrier := transport
	if carrier == "" && target.Transport != "" && target.Transport != kind {
		carrier = target.Transport
	}
	return clientlink.NewOpener(clientlink.Spec{Kind: kind, Name: iface, Carrier: carrier}), nil
}

func (s *Service) connectRemoteVolume(sess *Session, volume string) error {
	kind := sess.Kind
	raw := sess.remoteURI
	if raw == "" || !strings.Contains(raw, "://") {
		raw = kind + "://" + sess.ServerName + "/" + volume
	}
	target, err := uri.Parse(raw)
	if err != nil {
		target = uri.Target{Scheme: kind, Server: sess.ServerName, Volume: volume}
	}
	target.Volume = volume
	target.Path = ""
	if sess.remoteUser != "" || sess.remotePass != "" {
		target.User = sess.remoteUser
		target.Pass = sess.remotePass
		target.HasCreds = true
	}
	opener, err := openerFor(kind, sess.ifaceType, sess.iface, sess.transport, target)
	if err != nil {
		return err
	}
	ffs, err := client.Connect(context.Background(), target, client.Options{Opener: opener})
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
	var out []VolumeInfo
	var err error
	switch scheme {
	case KindAFP:
		out, err = discoverAFP(req)
	case KindSMB:
		out, err = discoverSMB(req)
	case KindNCP, KindEtherDFS:
		// SAP / EtherDFS probes need a live NIC; an empty list is a valid miss.
		out = nil
	default:
		return nil, fmt.Errorf("finder: unknown discover scheme %q", scheme)
	}
	if err != nil {
		return nil, err
	}
	s.log.Log2(log.Debug, "finder discover", log.Str("scheme", scheme), log.Int("count", int64(len(out))))
	return out, nil
}

func discoverAFP(req DiscoverRequest) ([]VolumeInfo, error) {
	kind := req.IfaceType
	if kind == "" {
		kind = clientlink.KindLToUDP
	}
	iface := req.Iface
	if iface == "" && clientlink.IsRawEtherKind(kind) {
		if def, err := clientlink.DefaultInterface(); err == nil {
			iface = def.Name
		}
	}
	opener := clientlink.NewOpener(clientlink.Spec{Kind: kind, Name: iface})
	dl, err := opener.DatagramLinkDDP()
	if err != nil {
		return nil, err
	}
	ep := atalk.NewEndpoint(dl, atalk.Addr{Network: opener.Net, Node: opener.Node})
	defer ep.Close()
	ents, err := ep.Lookup("=", atalk.AFPServerType, "*")
	if err != nil {
		return nil, err
	}
	out := make([]VolumeInfo, 0, len(ents))
	for _, e := range ents {
		out = append(out, VolumeInfo{
			ID:        e.Object,
			Kind:      KindAFP,
			Title:     e.Object,
			Subtitle:  e.Zone,
			Protocol:  KindAFP,
			Transport: TransportNBP,
		})
	}
	return out, nil
}

func discoverSMB(req DiscoverRequest) ([]VolumeInfo, error) {
	kind := req.IfaceType
	if kind == "" {
		kind = clientlink.KindPcap
	}
	iface := req.Iface
	if iface == "" && clientlink.IsRawEtherKind(kind) {
		if def, err := clientlink.DefaultInterface(); err == nil {
			iface = def.Name
		}
	}
	servers, _ := browse.Enumerate(browse.Options{
		Device:    iface,
		Kind:      kind,
		Window:    4 * time.Second,
		Workgroup: req.Workgroup,
	})
	out := make([]VolumeInfo, 0, len(servers))
	for _, srv := range servers {
		out = append(out, VolumeInfo{
			ID:        srv.Name,
			Kind:      KindSMB,
			Title:     srv.Name,
			Subtitle:  srv.Comment,
			Protocol:  KindSMB,
			Transport: TransportTCP,
		})
	}
	return out, nil
}
