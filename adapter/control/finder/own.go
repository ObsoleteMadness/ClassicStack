package finder

import (
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/service/afp"
	"github.com/ObsoleteMadness/ClassicStack/core/service/etherdfs"
	"github.com/ObsoleteMadness/ClassicStack/core/service/ncp"
	"github.com/ObsoleteMadness/ClassicStack/core/service/smb"
)

// dropOwnServers removes LAN-scan hits that are this ClassicStack instance.
// The in-process client shares the server's station MAC, so a self-mount over
// NBF/NBIPX/EtherDFS cannot complete; those servers already appear under Local.
func (s *Service) dropOwnServers(scheme string, vols []VolumeInfo) []VolumeInfo {
	if len(vols) == 0 {
		return vols
	}
	names := s.ownNames(scheme)
	mac := s.ownStationMAC()
	if len(names) == 0 && mac == "" {
		return vols
	}
	out := make([]VolumeInfo, 0, len(vols))
	for _, v := range vols {
		if s.isOwnServer(v, names, mac) {
			s.log.Log(log.Debug, "finder hid own server",
				log.Str("scheme", scheme), log.Str("title", v.Title), log.Str("id", v.ID))
			continue
		}
		out = append(out, v)
	}
	return out
}

func (s *Service) isOwnServer(v VolumeInfo, names []string, mac string) bool {
	for _, name := range names {
		if strings.EqualFold(strings.TrimSpace(v.Title), name) {
			return true
		}
	}
	if mac != "" && addressHasMAC(v.Address, mac) {
		return true
	}
	return false
}

// ownNames is the Chooser / browse / SAP / EtherDFS name this instance advertises
// for scheme. Empty when the service is neither configured nor built, so a
// neighbour that happens to share a default name is not hidden.
func (s *Service) ownNames(scheme string) []string {
	m := s.model()
	if m == nil {
		return nil
	}
	host := strings.TrimSpace(m.Identity.Hostname)
	switch strings.ToLower(strings.TrimSpace(scheme)) {
	case KindAFP:
		ss := afp.ServerSectionFromModel(m)
		if !s.advertising(afp.Name, ss.Enabled) {
			return nil
		}
		n := ss.EffectiveServerName(host)
		if n == "" {
			n = "ClassicStack"
		}
		return compactNames(n)
	case KindSMB:
		ss := smb.ServerSectionFromModel(m)
		if !s.advertising(smb.Name, ss.Enabled) {
			return nil
		}
		n := m.Identity.NetBIOSName()
		if n == "" {
			n = "CLASSICSTACK"
		}
		return compactNames(n, host)
	case KindNCP:
		ss := ncp.ServerSectionFromModel(m)
		if !s.advertising(ncp.Name, ss.Enabled) {
			return nil
		}
		n := ss.EffectiveServerName(host)
		if n == "" {
			n = "CLASSICSTACK"
		}
		return compactNames(n)
	case KindEtherDFS:
		ss := etherdfs.ServerSectionFromModel(m)
		if !s.advertising(etherdfs.Name, ss.IsEnabled) {
			return nil
		}
		n := strings.TrimSpace(ss.ServerName)
		if n == "" {
			n = host
		}
		if n == "" {
			n = "CLASSICSTACK"
		}
		return compactNames(n)
	default:
		return nil
	}
}

// advertising reports whether scheme's file service is live or configured on.
func (s *Service) advertising(key string, enabled bool) bool {
	if s.src != nil && s.src.Component(key) != nil {
		return true
	}
	m := s.model()
	if m == nil {
		return false
	}
	if _, ok := m.Get(key); !ok {
		return false
	}
	return enabled
}

func (s *Service) ownStationMAC() string {
	if ed := etherdfs.ServerSectionFromModel(s.model()); ed != nil {
		if mac := strings.TrimSpace(ed.MAC); mac != "" {
			return mac
		}
	}
	return strings.TrimSpace(s.configuredInterface().HWAddress)
}

func compactNames(names ...string) []string {
	var out []string
	seen := map[string]bool{}
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		k := strings.ToLower(n)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, n)
	}
	return out
}

func addressHasMAC(addr, mac string) bool {
	a := macDigits(addr)
	m := macDigits(mac)
	return m != "" && strings.Contains(a, m)
}

func macDigits(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
			b.WriteRune(r)
		}
	}
	return b.String()
}
