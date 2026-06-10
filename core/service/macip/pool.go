package macip

import (
	"sync"
	"time"
)

// leaseDuration bounds how long a static lease survives without being seen.
const leaseDuration = 5 * time.Minute

// IPv4 is a 32-bit IPv4 address held host-byte-order-free as four octets. Core
// avoids the stdlib net package (which can pull reflect on some targets and is
// heavier than embedded targets want), so MacIP addresses are plain [4]byte.
type IPv4 [4]byte

// u32 renders the address as a big-endian uint32 for arithmetic.
func (a IPv4) u32() uint32 {
	return uint32(a[0])<<24 | uint32(a[1])<<16 | uint32(a[2])<<8 | uint32(a[3])
}

// fromU32 builds an IPv4 from a big-endian uint32.
func fromU32(v uint32) IPv4 {
	return IPv4{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}
}

// IsZero reports the unspecified address 0.0.0.0.
func (a IPv4) IsZero() bool { return a == IPv4{} }

// validATEndpoint reports whether an AppleTalk (network, node) pair is a usable
// unicast endpoint (non-zero, not broadcast).
func validATEndpoint(atNetwork uint16, atNode uint8) bool {
	return atNetwork != 0 && atNode != 0 && atNode != 0xFF
}

type leaseEntry struct {
	used      bool
	atNetwork uint16
	atNode    uint8
	lastSeen  time.Time
}

// ipPool manages a contiguous pool of IPv4 addresses for assignment to MacIP
// clients. Index i maps to base+i+1; index 0 is the gateway's own IP and is
// never assigned. The pool is the static-assignment path; DHCP-relayed leases
// are an adapter concern injected through RegisterExternal.
type ipPool struct {
	mu      sync.Mutex
	base    uint32       // network base address
	entries []leaseEntry // index 0 = gateway IP (reserved), 1..n = client IPs

	// external holds adapter-assigned (e.g. DHCP) leases that may fall outside
	// the static range, keyed both ways.
	extByAT map[[3]byte]uint32
	extByIP map[uint32][3]byte
	extSeen map[[3]byte]time.Time
}

// atKey packs an AppleTalk endpoint into a comparable map key.
func atKey(atNetwork uint16, atNode uint8) [3]byte {
	return [3]byte{byte(atNetwork >> 8), byte(atNetwork), atNode}
}

// newIPPool builds a pool for the given network base and host-mask size. size is
// the number of host addresses (including the reserved gateway slot at index 0).
func newIPPool(network IPv4, hostCount int) *ipPool {
	if hostCount < 1 {
		hostCount = 1
	}
	return &ipPool{
		base:    network.u32(),
		entries: make([]leaseEntry, hostCount),
		extByAT: make(map[[3]byte]uint32),
		extByIP: make(map[uint32][3]byte),
		extSeen: make(map[[3]byte]time.Time),
	}
}

// assign returns the IP for an AppleTalk endpoint, honouring a prior lease (or
// a requested IP that is free) before allocating the next free slot. Returns the
// zero IP and false when the pool is exhausted.
func (p *ipPool) assign(requested IPv4, atNetwork uint16, atNode uint8) (IPv4, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Reuse an existing lease for this endpoint.
	for i := 1; i < len(p.entries); i++ {
		e := &p.entries[i]
		if e.used && e.atNetwork == atNetwork && e.atNode == atNode {
			e.lastSeen = time.Now()
			return fromU32(p.base + uint32(i)), true
		}
	}

	// Honour a specific requested IP if it falls in range and is free.
	if !requested.IsZero() {
		idx := int(requested.u32() - p.base)
		if idx >= 1 && idx < len(p.entries) && !p.entries[idx].used {
			p.entries[idx] = leaseEntry{used: true, atNetwork: atNetwork, atNode: atNode, lastSeen: time.Now()}
			return requested, true
		}
	}

	// Allocate the next free slot.
	for i := 1; i < len(p.entries); i++ {
		if !p.entries[i].used {
			p.entries[i] = leaseEntry{used: true, atNetwork: atNetwork, atNode: atNode, lastSeen: time.Now()}
			return fromU32(p.base + uint32(i)), true
		}
	}
	return IPv4{}, false
}

// lookupByIP resolves the AppleTalk endpoint that owns an IP, checking static
// then external leases.
func (p *ipPool) lookupByIP(ip IPv4) (uint16, uint8, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	idx := int(ip.u32() - p.base)
	if idx >= 1 && idx < len(p.entries) && p.entries[idx].used {
		e := p.entries[idx]
		return e.atNetwork, e.atNode, true
	}
	if k, ok := p.extByIP[ip.u32()]; ok {
		return uint16(k[0])<<8 | uint16(k[1]), k[2], true
	}
	return 0, 0, false
}

// lookupIPByAT resolves the IP leased to an AppleTalk endpoint.
func (p *ipPool) lookupIPByAT(atNetwork uint16, atNode uint8) (IPv4, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := 1; i < len(p.entries); i++ {
		e := p.entries[i]
		if e.used && e.atNetwork == atNetwork && e.atNode == atNode {
			return fromU32(p.base + uint32(i)), true
		}
	}
	if v, ok := p.extByAT[atKey(atNetwork, atNode)]; ok {
		return fromU32(v), true
	}
	return IPv4{}, false
}

// RegisterExternal records an adapter-assigned (e.g. DHCP relay) lease that may
// lie outside the static pool. Exposed for the IP-side adapter; core's static
// path does not call it.
func (p *ipPool) RegisterExternal(ip IPv4, atNetwork uint16, atNode uint8) {
	p.mu.Lock()
	defer p.mu.Unlock()
	k := atKey(atNetwork, atNode)
	if old, ok := p.extByAT[k]; ok {
		delete(p.extByIP, old)
	}
	p.extByAT[k] = ip.u32()
	p.extByIP[ip.u32()] = k
	p.extSeen[k] = time.Now()
}

// updateSeen refreshes the lease timestamp for an endpoint (static or external).
func (p *ipPool) updateSeen(atNetwork uint16, atNode uint8) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := 1; i < len(p.entries); i++ {
		e := &p.entries[i]
		if e.used && e.atNetwork == atNetwork && e.atNode == atNode {
			e.lastSeen = time.Now()
			return
		}
	}
	k := atKey(atNetwork, atNode)
	if _, ok := p.extByAT[k]; ok {
		p.extSeen[k] = time.Now()
	}
}

// expire evicts static leases unseen for longer than leaseDuration.
func (p *ipPool) expire() {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	for i := 1; i < len(p.entries); i++ {
		e := &p.entries[i]
		if e.used && now.Sub(e.lastSeen) > leaseDuration {
			*e = leaseEntry{}
		}
	}
}

// poolStats is a point-in-time count of active leases.
type poolStats struct {
	activeLeases int
}

func (p *ipPool) stats() poolStats {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for i := 1; i < len(p.entries); i++ {
		if p.entries[i].used {
			n++
		}
	}
	n += len(p.extByAT)
	return poolStats{activeLeases: n}
}

// LeaseInfo is one IP lease for diagnostics. Source is "static" or "external".
type LeaseInfo struct {
	IP        IPv4
	ATNetwork uint16
	ATNode    uint8
	Source    string
}

func (p *ipPool) leases() []LeaseInfo {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]LeaseInfo, 0, len(p.entries))
	for i := 1; i < len(p.entries); i++ {
		e := p.entries[i]
		if !e.used {
			continue
		}
		out = append(out, LeaseInfo{IP: fromU32(p.base + uint32(i)), ATNetwork: e.atNetwork, ATNode: e.atNode, Source: "static"})
	}
	for k, v := range p.extByAT {
		out = append(out, LeaseInfo{
			IP:        fromU32(v),
			ATNetwork: uint16(k[0])<<8 | uint16(k[1]),
			ATNode:    k[2],
			Source:    "external",
		})
	}
	return out
}
