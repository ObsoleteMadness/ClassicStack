//go:build macip || all

package macip

import (
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/pgodw/omnitalk/netlog"
)

const leaseDuration = 5 * time.Minute

// pinnedLeaseHardTimeout bounds how long a lease can stay pinned without
// session activity updates, protecting against missed close callbacks.
const pinnedLeaseHardTimeout = 30 * time.Minute

type leaseEntry struct {
	used      bool
	atNetwork uint16
	atNode    uint8
	lastSeen  time.Time
}

// ipPool manages a pool of IP addresses for assignment to MacIP clients.
// Index i maps to IP address base+i+1, where base is the network address.
// Index 0 is the gateway's own IP and is never assigned to clients.
//
// In DHCP mode, IPs come from the network's DHCP server and may lie outside
// the preconfigured subnet. These are tracked in the dhcpByAT/dhcpByIP maps.
type ipPool struct {
	mu      sync.Mutex
	base    uint32       // network base address (e.g. 192.168.100.0 as uint32)
	entries []leaseEntry // index 0 = gateway IP (reserved), 1..n = client IPs

	pinBySession map[uint8][3]byte // ASP session ID -> AT endpoint key
	pinCountByAT map[[3]byte]int   // AT endpoint key -> active session count
	pinSeenByAT  map[[3]byte]time.Time

	dhcpMu   sync.Mutex
	dhcpByAT map[[3]byte]uint32 // AT (net_hi, net_lo, node) → IP as uint32
	dhcpByIP map[uint32][3]byte // IP as uint32 → AT key
	dhcpSeen map[[3]byte]time.Time
}

func validATEndpoint(atNetwork uint16, atNode uint8) bool {
	return atNetwork != 0 && atNode != 0 && atNode != 0xFF
}

func newIPPool(network net.IP, mask net.IPMask) *ipPool {
	base := binary.BigEndian.Uint32(network.To4())
	hostMask := ^binary.BigEndian.Uint32([]byte(mask))
	size := int(hostMask) - 1 // excludes broadcast; index 0 = gateway, 1..size-1 = clients
	if size < 1 {
		size = 1
	}
	entries := make([]leaseEntry, size)
	entries[0].used = true // gateway's own IP — never assigned to clients
	return &ipPool{
		base:         base,
		entries:      entries,
		pinBySession: make(map[uint8][3]byte),
		pinCountByAT: make(map[[3]byte]int),
		pinSeenByAT:  make(map[[3]byte]time.Time),
		dhcpByAT:     make(map[[3]byte]uint32),
		dhcpByIP:     make(map[uint32][3]byte),
		dhcpSeen:     make(map[[3]byte]time.Time),
	}
}

func atKey(atNetwork uint16, atNode uint8) [3]byte {
	return [3]byte{byte(atNetwork >> 8), byte(atNetwork), atNode}
}

func (p *ipPool) indexToIP(i int) net.IP {
	n := p.base + uint32(i) + 1
	return net.IP{byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}
}

func (p *ipPool) ipToIndex(ip net.IP) (int, bool) {
	v := binary.BigEndian.Uint32(ip.To4())
	if v <= p.base {
		return 0, false
	}
	i := int(v - p.base - 1)
	if i >= len(p.entries) {
		return 0, false
	}
	return i, true
}

// assign allocates an IP for the given AppleTalk address.  If requested is
// non-nil and available, it is honoured.  Returns the assigned IP or an error.
func (p *ipPool) assign(requested net.IP, atNetwork uint16, atNode uint8) (net.IP, error) {
	if !validATEndpoint(atNetwork, atNode) {
		return nil, fmt.Errorf("macip: invalid AppleTalk endpoint %d.%d", atNetwork, atNode)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Renew an existing lease for this AT address.
	for i := 1; i < len(p.entries); i++ {
		e := &p.entries[i]
		if e.used && e.atNetwork == atNetwork && e.atNode == atNode {
			e.lastSeen = time.Now()
			return p.indexToIP(i), nil
		}
	}

	// Honour a specific requested IP if it is free.
	if requested != nil && !requested.Equal(net.IPv4zero) {
		if i, ok := p.ipToIndex(requested); ok && i > 0 && !p.entries[i].used {
			p.entries[i] = leaseEntry{used: true, atNetwork: atNetwork, atNode: atNode, lastSeen: time.Now()}
			return p.indexToIP(i), nil
		}
	}

	// Find any free slot (skip index 0 — gateway's own IP).
	for i := 1; i < len(p.entries); i++ {
		if !p.entries[i].used {
			p.entries[i] = leaseEntry{used: true, atNetwork: atNetwork, atNode: atNode, lastSeen: time.Now()}
			return p.indexToIP(i), nil
		}
	}

	return nil, fmt.Errorf("macip: no free IP addresses in pool")
}

// release frees the lease for the given IP.
func (p *ipPool) release(ip net.IP) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if i, ok := p.ipToIndex(ip); ok && i > 0 {
		p.entries[i] = leaseEntry{}
	}
}

// updateSeen records a packet received from the given AT address, refreshing
// the lease expiry timer for both static and DHCP entries.
func (p *ipPool) updateSeen(atNetwork uint16, atNode uint8) {
	if !validATEndpoint(atNetwork, atNode) {
		return
	}

	p.mu.Lock()
	for i := 1; i < len(p.entries); i++ {
		e := &p.entries[i]
		if e.used && e.atNetwork == atNetwork && e.atNode == atNode {
			e.lastSeen = time.Now()
			break
		}
	}
	p.mu.Unlock()

	atKey := atKey(atNetwork, atNode)
	p.dhcpMu.Lock()
	if _, ok := p.dhcpByAT[atKey]; ok {
		p.dhcpSeen[atKey] = time.Now()
	}
	p.dhcpMu.Unlock()
}

// lookupByIP returns the AppleTalk address currently holding the given IP.
// It checks both the static pool and any DHCP-assigned entries.
func (p *ipPool) lookupByIP(ip net.IP) (atNetwork uint16, atNode uint8, ok bool) {
	p.mu.Lock()
	if i, found := p.ipToIndex(ip); found {
		e := &p.entries[i]
		if e.used && validATEndpoint(e.atNetwork, e.atNode) {
			atNetwork, atNode, ok = e.atNetwork, e.atNode, true
		}
	}
	p.mu.Unlock()
	if ok {
		return
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return
	}
	n := binary.BigEndian.Uint32(ip4)
	p.dhcpMu.Lock()
	if atKey, found := p.dhcpByIP[n]; found {
		atNetwork = uint16(atKey[0])<<8 | uint16(atKey[1])
		atNode = atKey[2]
		ok = validATEndpoint(atNetwork, atNode)
	}
	p.dhcpMu.Unlock()
	return
}

// registerDHCP records a DHCP-assigned IP for the given AppleTalk address.
// The IP may lie outside the statically configured subnet.
func (p *ipPool) registerDHCP(ip net.IP, atNetwork uint16, atNode uint8) {
	if !validATEndpoint(atNetwork, atNode) {
		return
	}

	ip4 := ip.To4()
	if ip4 == nil {
		return
	}
	n := binary.BigEndian.Uint32(ip4)
	atKey := atKey(atNetwork, atNode)
	p.dhcpMu.Lock()
	if old, ok := p.dhcpByAT[atKey]; ok {
		delete(p.dhcpByIP, old)
	}
	p.dhcpByAT[atKey] = n
	p.dhcpByIP[n] = atKey
	p.dhcpSeen[atKey] = time.Now()
	p.dhcpMu.Unlock()
	netlog.Debug("macip-dhcp: tracking lease %s for AT %d.%d", ip4, atNetwork, atNode)
}

// lookupIPByAT returns the DHCP-assigned IP for the given AppleTalk address, if any.
func (p *ipPool) lookupIPByAT(atNetwork uint16, atNode uint8) (net.IP, bool) {
	if !validATEndpoint(atNetwork, atNode) {
		return nil, false
	}

	atKey := atKey(atNetwork, atNode)
	p.dhcpMu.Lock()
	n, ok := p.dhcpByAT[atKey]
	p.dhcpMu.Unlock()
	if !ok {
		return nil, false
	}
	return net.IP{byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}, true
}

func (p *ipPool) pinSessionLease(atNetwork uint16, atNode uint8, sessionID uint8) {
	if !validATEndpoint(atNetwork, atNode) || sessionID == 0 {
		return
	}

	key := atKey(atNetwork, atNode)
	now := time.Now()

	p.mu.Lock()
	defer p.mu.Unlock()

	if prev, ok := p.pinBySession[sessionID]; ok {
		if prev == key {
			p.pinSeenByAT[key] = now
			return
		}
		if c := p.pinCountByAT[prev]; c <= 1 {
			delete(p.pinCountByAT, prev)
			delete(p.pinSeenByAT, prev)
		} else {
			p.pinCountByAT[prev] = c - 1
		}
	}

	p.pinBySession[sessionID] = key
	p.pinCountByAT[key]++
	p.pinSeenByAT[key] = now
}

func (p *ipPool) unpinSessionLease(sessionID uint8) {
	if sessionID == 0 {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	key, ok := p.pinBySession[sessionID]
	if !ok {
		return
	}
	delete(p.pinBySession, sessionID)

	if c := p.pinCountByAT[key]; c <= 1 {
		delete(p.pinCountByAT, key)
		delete(p.pinSeenByAT, key)
	} else {
		p.pinCountByAT[key] = c - 1
	}
}

func (p *ipPool) markSessionActivity(sessionID uint8) {
	if sessionID == 0 {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	key, ok := p.pinBySession[sessionID]
	if !ok {
		return
	}
	p.pinSeenByAT[key] = time.Now()
}

func (p *ipPool) cleanupExpiredPins(now time.Time) {
	for key, seen := range p.pinSeenByAT {
		if now.Sub(seen) <= pinnedLeaseHardTimeout {
			continue
		}
		delete(p.pinCountByAT, key)
		delete(p.pinSeenByAT, key)
		for sessionID, sKey := range p.pinBySession {
			if sKey == key {
				delete(p.pinBySession, sessionID)
			}
		}
	}
}

func (p *ipPool) isPinnedLocked(atNetwork uint16, atNode uint8) bool {
	if !validATEndpoint(atNetwork, atNode) {
		return false
	}
	if c := p.pinCountByAT[atKey(atNetwork, atNode)]; c > 0 {
		return true
	}
	return false
}

// expireLeases releases leases that have not been renewed within leaseDuration.
func (p *ipPool) expireLeases() {
	now := time.Now()
	cutoff := now.Add(-leaseDuration)
	p.mu.Lock()
	p.cleanupExpiredPins(now)
	for i := 1; i < len(p.entries); i++ {
		e := &p.entries[i]
		if e.used && e.lastSeen.Before(cutoff) && !p.isPinnedLocked(e.atNetwork, e.atNode) {
			*e = leaseEntry{}
		}
	}
	p.mu.Unlock()

	p.dhcpMu.Lock()
	for atKey, t := range p.dhcpSeen {
		atNetwork := uint16(atKey[0])<<8 | uint16(atKey[1])
		atNode := atKey[2]

		p.mu.Lock()
		isPinned := p.isPinnedLocked(atNetwork, atNode)
		p.mu.Unlock()

		if t.Before(cutoff) && !isPinned {
			if n, ok := p.dhcpByAT[atKey]; ok {
				delete(p.dhcpByIP, n)
			}
			delete(p.dhcpByAT, atKey)
			delete(p.dhcpSeen, atKey)
		}
	}
	p.dhcpMu.Unlock()
}
