//go:build macip || all

package macip

import (
	"encoding/json"
	"net"
	"os"
	"time"

	"github.com/pgodw/omnitalk/netlog"
)

type savedLease struct {
	IP        string `json:"ip"`
	ATNetwork uint16 `json:"atNetwork"`
	ATNode    uint8  `json:"atNode"`
	LastSeen  int64  `json:"lastSeen"` // unix timestamp
}

type savedState struct {
	Static []savedLease `json:"static,omitempty"`
	DHCP   []savedLease `json:"dhcp,omitempty"`
}

// saveToFile writes the current pool state to path atomically (via a temp file).
// Only leases that have not yet expired are included.
func (p *ipPool) saveToFile(path string) {
	if path == "" {
		return
	}
	st := p.snapshot()
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		netlog.Warn("macip: state save marshal: %v", err)
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		netlog.Warn("macip: state save write: %v", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		netlog.Warn("macip: state save rename: %v", err)
	}
}

// snapshot returns a point-in-time copy of all non-expired leases.
func (p *ipPool) snapshot() savedState {
	cutoff := time.Now().Add(-leaseDuration)
	var st savedState

	p.mu.Lock()
	for i := 1; i < len(p.entries); i++ {
		e := &p.entries[i]
		if e.used && !e.lastSeen.Before(cutoff) {
			st.Static = append(st.Static, savedLease{
				IP:        p.indexToIP(i).String(),
				ATNetwork: e.atNetwork,
				ATNode:    e.atNode,
				LastSeen:  e.lastSeen.Unix(),
			})
		}
	}
	p.mu.Unlock()

	p.dhcpMu.Lock()
	for atKey, n := range p.dhcpByAT {
		t := p.dhcpSeen[atKey]
		if t.Before(cutoff) {
			continue
		}
		ip := net.IP{byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}
		st.DHCP = append(st.DHCP, savedLease{
			IP:        ip.String(),
			ATNetwork: uint16(atKey[0])<<8 | uint16(atKey[1]),
			ATNode:    atKey[2],
			LastSeen:  t.Unix(),
		})
	}
	p.dhcpMu.Unlock()

	return st
}

// loadFromFile reads a previously saved state file and restores leases into the
// pool.  Missing or malformed files are silently skipped (a missing file is
// expected on first run).
func (p *ipPool) loadFromFile(path string) {
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			netlog.Warn("macip: state load: %v", err)
		}
		return
	}
	var st savedState
	if err := json.Unmarshal(data, &st); err != nil {
		netlog.Warn("macip: state load parse: %v", err)
		return
	}

	count := 0
	for _, l := range st.Static {
		if !validATEndpoint(l.ATNetwork, l.ATNode) {
			continue
		}
		ip := net.ParseIP(l.IP).To4()
		if ip == nil {
			continue
		}
		if _, err := p.assign(ip, l.ATNetwork, l.ATNode); err == nil {
			count++
		}
	}
	for _, l := range st.DHCP {
		if !validATEndpoint(l.ATNetwork, l.ATNode) {
			continue
		}
		ip := net.ParseIP(l.IP).To4()
		if ip == nil {
			continue
		}
		p.registerDHCP(ip, l.ATNetwork, l.ATNode)
		count++
	}
	if count > 0 {
		netlog.Info("macip: restored %d lease(s) from %s", count, path)
	}
}
