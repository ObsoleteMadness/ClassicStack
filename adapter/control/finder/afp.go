package finder

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	afpclient "github.com/ObsoleteMadness/ClassicStack/client/afp"
	"github.com/ObsoleteMadness/ClassicStack/client/atalk"
	clientlink "github.com/ObsoleteMadness/ClassicStack/client/link"
	"github.com/ObsoleteMadness/ClassicStack/client/uri"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
)

const afpBrowseWindow = 2 * time.Second

func (s *Service) discoverAFP(req DiscoverRequest) ([]VolumeInfo, error) {
	def := s.configuredSpec()
	if req.IfaceType != "" {
		def.Kind = req.IfaceType
		if req.Iface != "" {
			def.Name = req.Iface
		}
	}

	wantIface, wantLToUDP, wantTCP := afpScanFlags(req.Transport)
	specs := afpDDPSpecs(def, wantIface, wantLToUDP)

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
	for _, spec := range specs {
		spec := spec
		wg.Add(1)
		go func() {
			defer wg.Done()
			vols, err := s.lookupAFPDDP(spec)
			if err != nil {
				s.log.Log2(log.Debug, "finder afp ddp scan",
					log.Str("ifacetype", spec.Kind), log.Str("err", err.Error()))
				return
			}
			s.log.Log(log.Debug, "finder afp ddp scan",
				log.Str("ifacetype", spec.Kind),
				log.Str("iface", spec.Name),
				log.Int("count", int64(len(vols))))
			for _, v := range vols {
				add(v)
			}
		}()
	}
	if wantTCP {
		wg.Add(1)
		go func() {
			defer wg.Done()
			device := s.afpTCPDevice(req)
			servers, err := afpclient.DiscoverTCP(device, afpBrowseWindow)
			if err != nil {
				s.log.Log1(log.Debug, "finder afp tcp scan", log.Str("err", err.Error()))
				return
			}
			s.log.Log2(log.Debug, "finder afp tcp scan",
				log.Str("iface", device), log.Int("count", int64(len(servers))))
			for _, srv := range servers {
				if v, ok := afpTCPVolume(srv); ok {
					add(v)
				}
			}
		}()
	}
	wg.Wait()
	return dedupAFPVolumes(out), nil
}

func (s *Service) lookupAFPDDP(spec clientlink.Spec) ([]VolumeInfo, error) {
	opener, err := s.openerFor(KindAFP, spec.Kind, spec.Name, "", uri.Target{})
	if err != nil {
		return nil, err
	}
	dl, err := opener.DatagramLinkDDP()
	if err != nil {
		return nil, err
	}
	ep := atalk.NewEndpoint(dl, atalk.Addr{Network: opener.Net, Node: opener.Node})
	defer func() { _ = ep.Close() }()
	ents, err := ep.LookupAllZones("=", atalk.AFPServerType, afpBrowseWindow)
	if err != nil {
		return nil, err
	}
	out := make([]VolumeInfo, 0, len(ents))
	for _, e := range ents {
		out = append(out, afpNBPVolume(e, spec.Kind))
	}
	return out, nil
}

func (s *Service) afpTCPDevice(req DiscoverRequest) string {
	if strings.TrimSpace(req.Iface) != "" {
		return req.Iface
	}
	def := s.configuredSpec()
	if clientlink.IsRawEtherKind(def.Kind) && def.Name != "" {
		return def.Name
	}
	if d, err := clientlink.DefaultInterface(); err == nil {
		return d.Name
	}
	return ""
}

// afpScanFlags picks which AFP families to probe. Empty / unknown transport means
// DDP on the configured interface, DDP over LToUDP, and TCP/mDNS. An explicit
// request restricts the sweep: "ddp"/"nbp" skip TCP; "tcp" is mDNS only;
// "pcap"/"ltoudp"/"tashtalk" pin a single DDP path.
func afpScanFlags(transport string) (iface, ltoudp, tcp bool) {
	switch strings.ToLower(strings.TrimSpace(transport)) {
	case "", "*":
		return true, true, true
	case "tcp":
		return false, false, true
	case clientlink.KindLToUDP:
		return false, true, false
	case clientlink.KindPcap, clientlink.KindTap, "ethertalk", clientlink.KindTashTalk:
		return true, false, false
	case TransportDDP, TransportNBP:
		return true, true, false
	default:
		return true, true, true
	}
}

// afpDDPSpecs is the NBP lookup set: the configured DDP interface (pcap / TashTalk /
// LToUDP) when wantIface, plus LToUDP when wantLToUDP and that is not already the
// configured kind. A multicast-only [[interface]] therefore yields one LToUDP scan,
// not two. tap is treated as pcap (same NIC name) because AFP's DDP opener is pcap.
func afpDDPSpecs(def clientlink.Spec, wantIface, wantLToUDP bool) []clientlink.Spec {
	if def.Kind == clientlink.KindTap {
		def.Kind = clientlink.KindPcap
	}
	var out []clientlink.Spec
	seen := map[string]bool{}
	add := func(sp clientlink.Spec) {
		if !ddpLinkKind(sp.Kind) || seen[sp.Kind] {
			return
		}
		seen[sp.Kind] = true
		out = append(out, sp)
	}
	if wantIface && ddpLinkKind(def.Kind) {
		add(def)
	}
	if wantLToUDP {
		add(clientlink.Spec{Kind: clientlink.KindLToUDP})
	}
	return out
}

func ddpLinkKind(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case clientlink.KindLToUDP, clientlink.KindTashTalk, clientlink.KindPcap:
		return true
	}
	return false
}

func afpNBPVolume(e atalk.NBPEntity, linkKind string) VolumeInfo {
	server := strings.TrimSpace(e.Object)
	zone := afpNormZone(e.Zone)
	if zone != "" {
		server = server + ":" + zone
	}
	return VolumeInfo{
		ID:        fmt.Sprintf("afp://%s,%s/", server, linkKind),
		Kind:      KindAFP,
		Title:     e.Object,
		Subtitle:  e.Zone,
		Protocol:  KindAFP,
		Transport: TransportDDP,
		Address:   ddpServerAddress(e),
		URI:       serverURI(KindAFP, server, ""),
	}
}

// ddpServerAddress is the Get Info address: DDP net.node, plus the NBP zone when known.
func ddpServerAddress(e atalk.NBPEntity) string {
	node := ""
	if e.Addr.Network != 0 || e.Addr.Node != 0 {
		node = fmt.Sprintf("%d.%d", e.Addr.Network, e.Addr.Node)
	}
	zone := afpNormZone(e.Zone)
	switch {
	case node != "" && zone != "":
		return node + ", " + zone
	case node != "":
		return node
	default:
		return zone
	}
}

// dedupAFPVolumes collapses duplicate NBP hits: the same server seen with a named
// zone and with "*", or reachable on both pcap and LToUDP.
func dedupAFPVolumes(vols []VolumeInfo) []VolumeInfo {
	var ddp, other []VolumeInfo
	for _, v := range vols {
		if v.Kind == KindAFP && v.Transport != TransportTCP {
			ddp = append(ddp, v)
		} else {
			other = append(other, v)
		}
	}
	if len(ddp) == 0 {
		return vols
	}
	return append(other, collapseAFPDDP(ddp)...)
}

func collapseAFPDDP(vols []VolumeInfo) []VolumeInfo {
	groups := [][]int{{0}}
	for i := 1; i < len(vols); i++ {
		merged := false
		for gi := range groups {
			for _, j := range groups[gi] {
				if afpSameServer(vols[i], vols[j]) {
					groups[gi] = append(groups[gi], i)
					merged = true
					break
				}
			}
			if merged {
				break
			}
		}
		if !merged {
			groups = append(groups, []int{i})
		}
	}
	out := make([]VolumeInfo, 0, len(groups))
	for _, g := range groups {
		best := vols[g[0]]
		for _, idx := range g[1:] {
			if preferAFPVolume(vols[idx], best) {
				best = vols[idx]
			}
		}
		out = append(out, best)
	}
	return out
}

func afpSameServer(a, b VolumeInfo) bool {
	if a.Title != b.Title {
		return false
	}
	az, bz := afpNormZone(a.Subtitle), afpNormZone(b.Subtitle)
	return az == "" || bz == "" || strings.EqualFold(az, bz)
}

func afpNormZone(zone string) string {
	zone = strings.TrimSpace(zone)
	if zone == "" || zone == "*" {
		return ""
	}
	return zone
}

// preferAFPVolume reports whether a should replace b in dedupAFPVolumes.
func preferAFPVolume(a, b VolumeInfo) bool {
	az, bz := afpNormZone(a.Subtitle), afpNormZone(b.Subtitle)
	if az == "" && bz != "" {
		return false
	}
	if az != "" && bz == "" {
		return true
	}
	return afpLinkRank(a.ID) < afpLinkRank(b.ID)
}

func afpLinkRank(id string) int {
	i := strings.LastIndex(id, ",")
	if i < 0 {
		return 99
	}
	rest := id[i+1:]
	j := strings.IndexByte(rest, '/')
	if j >= 0 {
		rest = rest[:j]
	}
	switch strings.ToLower(rest) {
	case clientlink.KindPcap, clientlink.KindTap, "ethertalk":
		return 0
	case clientlink.KindTashTalk:
		return 1
	case clientlink.KindLToUDP:
		return 2
	default:
		return 99
	}
}

func afpTCPVolume(srv afpclient.TCPServer) (VolumeInfo, bool) {
	host := strings.TrimSpace(srv.Host)
	if host == "" {
		host = strings.TrimSpace(srv.Name)
	}
	if host == "" {
		return VolumeInfo{}, false
	}
	if srv.Port != 0 && srv.Port != afpclient.DSIPort {
		host = net.JoinHostPort(host, fmt.Sprintf("%d", srv.Port))
	}
	title := strings.TrimSpace(srv.Name)
	if title == "" {
		title = host
	}
	return VolumeInfo{
		ID:        fmt.Sprintf("afp://%s,tcp/", host),
		Kind:      KindAFP,
		Title:     title,
		Subtitle:  srv.Host,
		Protocol:  KindAFP,
		Transport: TransportTCP,
		Address:   host,
		URI:       serverURI(KindAFP, host, TransportTCP),
	}, true
}
