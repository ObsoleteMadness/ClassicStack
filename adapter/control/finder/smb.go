package finder

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/client/browse"
	clientlink "github.com/ObsoleteMadness/ClassicStack/client/link"
	"github.com/ObsoleteMadness/ClassicStack/client/netbios"
	smbclient "github.com/ObsoleteMadness/ClassicStack/client/smb"
	"github.com/ObsoleteMadness/ClassicStack/client/uri"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	smbproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/smb"
)

const smbBrowseWindow = 4 * time.Second

func (s *Service) discoverSMB(req DiscoverRequest) ([]VolumeInfo, error) {
	spec, err := s.resolveLink(KindSMB, req.IfaceType, req.Iface, req.Transport, uri.Target{})
	if err != nil {
		return nil, err
	}
	workgroup := strings.TrimSpace(req.Workgroup)
	if workgroup == "" {
		workgroup = s.identityWorkgroup()
	}

	wantNBF, wantIPX, wantTCP := smbScanFlags(req.Transport)
	cfg := s.clientConfig()
	opts := browse.Options{
		Device:    spec.Name,
		Kind:      spec.Kind,
		Window:    smbBrowseWindow,
		Workgroup: workgroup,
		Station:   s.clientName(),
		Trace: func(line string) {
			s.log.Log1(log.Debug, "finder smb scan", log.Str("trace", line))
		},
	}
	// The browse sweep opens its own raw links (one per carrier) rather than riding
	// openerFor's, so [Client] capture has to be threaded in explicitly or the
	// discovery half of an SMB scan — the solicit/FindMaster/GetBackupList exchange
	// that decides whether a master browser is found at all — never reaches the
	// operator's capture file.
	if path := strings.TrimSpace(cfg.Capture); path != "" {
		opts.CapturePath = path
		if n := cfg.CaptureSnaplen; n > 0 {
			opts.CaptureSnaplen = uint32(n)
		}
	}

	var (
		mu  sync.Mutex
		out []VolumeInfo
	)
	add := func(v VolumeInfo) {
		mu.Lock()
		out = append(out, v)
		mu.Unlock()
	}

	var wg sync.WaitGroup
	if wantNBF || wantIPX {
		wg.Add(1)
		go func() {
			defer wg.Done()
			nbfOpts := opts
			if wantNBF {
				nbfOpts.Carriers = append(nbfOpts.Carriers, netbios.NBF)
			}
			if wantIPX {
				nbfOpts.Carriers = append(nbfOpts.Carriers, netbios.NBIPX)
			}
			servers, results := browse.Enumerate(nbfOpts)
			for _, r := range results {
				if r.Err != nil {
					s.log.Log2(log.Debug, "finder smb carrier unavailable",
						log.Str("carrier", string(r.Protocol)), log.Str("err", r.Err.Error()))
				}
			}
			for _, srv := range servers {
				for _, c := range srv.Carriers {
					if v, ok := smbVolume(srv, c, workgroup); ok {
						add(v)
					}
				}
			}
		}()
	}
	if wantTCP {
		wg.Add(1)
		go func() {
			defer wg.Done()
			servers, res := browse.EnumerateTCP(opts)
			if res.Err != nil {
				s.log.Log1(log.Debug, "finder smb tcp scan", log.Str("err", res.Err.Error()))
			}
			for _, srv := range servers {
				if v, ok := smbVolume(srv, netbios.TCP, workgroup); ok {
					add(v)
				}
			}
		}()
	}
	wg.Wait()
	return out, nil
}

func (s *Service) identityWorkgroup() string {
	ms, ok := s.src.(modelSource)
	if !ok || ms == nil {
		return ""
	}
	m := ms.Model()
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m.Identity.Workgroup)
}

// identityHostname returns the shared server identity's hostname (§4-bis), for the
// outbound NetBIOS session carriers' calling name (link.go's openerFor) — a caller
// running as part of the ClassicStack server presents this identity instead of a
// throwaway MAC-derived name.
func (s *Service) identityHostname() string {
	ms, ok := s.src.(modelSource)
	if !ok || ms == nil {
		return ""
	}
	m := ms.Model()
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m.Identity.Hostname)
}

// clientName is the outbound client's own presented name: the configured [Client]
// name when set, else the server's shared Identity.Hostname — one identity for the
// whole box by default. Used for the NetBIOS session carriers' calling name AND the
// browse/discovery station name (link.go's openerFor, smb.go's discoverSMB), so a
// name set (or left to default) here shows up consistently everywhere the client
// presents itself, instead of only on the final session dial. Empty when neither is
// configured, leaving each carrier's own MAC-derived fallback in place.
func (s *Service) clientName() string {
	if name := strings.TrimSpace(s.clientConfig().Name); name != "" {
		return name
	}
	return s.identityHostname()
}

// smbScanFlags picks which SMB families to probe. Empty / unknown transport means
// all three (tcp, ipx, netbeui). An explicit request restricts the sweep.
func smbScanFlags(transport string) (nbf, ipx, tcp bool) {
	switch strings.ToLower(strings.TrimSpace(transport)) {
	case "", "*":
		return true, true, true
	case "nbf", "netbeui":
		return true, false, false
	case "nbipx", "ipx":
		return false, true, false
	case "tcp", "nbt":
		return false, false, true
	default:
		return true, true, true
	}
}

func smbVolume(srv browse.Server, carrier netbios.Protocol, workgroup string) (VolumeInfo, bool) {
	badge, uriCarrier := smbCarrier(carrier)
	if badge == "" {
		return VolumeInfo{}, false
	}
	name := strings.TrimSpace(srv.Name)
	if name == "" {
		return VolumeInfo{}, false
	}
	host := name
	if carrier == netbios.TCP && srv.Address != "" {
		host = srv.Address
	}
	subtitle := srv.Comment
	if srv.Role != "" && subtitle == "" {
		subtitle = srv.Role
	}
	nb := strings.TrimSpace(workgroup)
	if nb == "" {
		nb = "Workgroup"
	}
	return VolumeInfo{
		ID:           fmt.Sprintf("smb://%s,%s/", host, uriCarrier),
		Kind:         KindSMB,
		Title:        name,
		Subtitle:     subtitle,
		Protocol:     KindSMB,
		Transport:    badge,
		Address:      srv.AddressFor(carrier),
		URI:          serverURI(KindSMB, host, uriCarrier),
		OS:           formatSMBOS(srv.OSVersion),
		Neighborhood: nb,
	}, true
}

func smbCarrier(p netbios.Protocol) (badge, uriCarrier string) {
	switch p {
	case netbios.NBF:
		return TransportNetBEUI, smbclient.CarrierNBF
	case netbios.NBIPX:
		return TransportIPX, smbclient.CarrierNBIPX
	case netbios.TCP:
		return TransportTCP, clientlink.KindTCP
	default:
		return "", ""
	}
}

// formatSMBOS maps a HostAnnouncement OSVersion "major.minor" to a Get Info label.
func formatSMBOS(ver string) string {
	ver = strings.TrimSpace(ver)
	if ver == "" || ver == "0.0" {
		return ""
	}
	var name string
	switch ver {
	case "1.0", "1.1":
		name = "MS-DOS"
	case "2.0", "2.1":
		name = "OS/2 / LAN Manager 2"
	case "3.10", "3.11":
		name = "Windows for Workgroups 3.11"
	case "3.50", "3.51":
		name = "Windows NT 3.5"
	case "4.0":
		name = "Windows 95 / NT 4.0"
	case "4.10":
		name = "Windows 98"
	case "4.90":
		name = "Windows Me"
	case "5.0":
		name = "Windows 2000"
	case "5.1":
		name = "Windows XP"
	case "5.2":
		name = "Windows Server 2003"
	case "6.0":
		name = "Windows Vista"
	case "6.1":
		name = "Windows 7"
	case "6.2":
		name = "Windows 8"
	case "6.3":
		name = "Windows 8.1"
	case "10.0":
		name = "Windows 10"
	}
	if name == "" {
		return ver
	}
	return name + " (" + ver + ")"
}

// formatSMBVersion maps a negotiated dialect string to a Get Info label.
func formatSMBVersion(dialect string) string {
	switch strings.TrimSpace(dialect) {
	case "":
		return ""
	case smbproto.DialectNTLM:
		return "SMB 1.0 (NT LM 0.12)"
	case smbproto.DialectWfW311:
		return "SMB WfW 3.1a"
	case smbproto.DialectLANMAN21, smbproto.DialectDOSLANMAN2:
		return "LAN Manager 2.1"
	case smbproto.DialectLM12X002, smbproto.DialectDOSLM12:
		return "LAN Manager 2.0"
	case smbproto.DialectLANMAN10, smbproto.DialectMSNet30:
		return "LAN Manager 1.0"
	case smbproto.DialectPCNetwork1, smbproto.DialectMSNet103:
		return "SMB Core"
	default:
		return dialect
	}
}

// formatSMBAuth lists negotiated security mode then CAP_* names for the login prompt
// and Get Info. Share-level / plaintext are the Core defaults when the dialect has
// no SecurityMode word.
func formatSMBAuth(userSecurity, encryptPasswords bool, caps uint32) []string {
	out := make([]string, 0, 8)
	if userSecurity {
		out = append(out, "User-level security")
	} else {
		out = append(out, "Share-level security")
	}
	if encryptPasswords {
		out = append(out, "Encrypted passwords")
	} else {
		out = append(out, "Plaintext passwords")
	}
	out = append(out, smbproto.CapabilityNames(caps)...)
	return out
}

func (s *Service) smbOSFor(server string) string {
	server = strings.TrimSpace(server)
	if server == "" {
		return ""
	}
	for _, v := range s.LastSeen(KindSMB) {
		if strings.EqualFold(v.Title, server) && v.OS != "" {
			return v.OS
		}
	}
	return ""
}
