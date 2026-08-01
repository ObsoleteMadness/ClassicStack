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
	reserved  bool // gateway/network slot: never assignable, never a lease match
	atNetwork uint16
	atNode    uint8
	lastSeen  time.Time
}

// ipPool manages a contiguous pool of IPv4 addresses for assignment to MacIP
// clients. Slot index i maps to the address base+i, so index 0 is the network
// address (base) and index 1 is the gateway (base+1). BOTH are reserved and never
// handed out: assignment starts at index 2 (base+2), the first true host address.
// This matches the original macipgw (macip.c init_ip: my address = net+1, ipent[0]
// pre-marked ASSIGN_FIXED, lease_ip returns from the next free slot). The pool is the
// static-assignment path; DHCP-relayed leases are an adapter concern injected through
// RegisterExternal.
type ipPool struct {
	mu      sync.Mutex
	base    uint32       // network base address
	entries []leaseEntry // index 0 = network addr, 1 = gateway (both reserved), 2..n = client IPs

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

// newIPPool builds a pool for the given network base and host-mask size. hostCount is
// the number of address slots including the two reserved slots (index 0 = network
// address base, index 1 = gateway base+1); the first assignable client address is
// base+2. The gateway slot is pre-marked reserved so assign() never hands out the
// gateway's own IP — the bug where a Mac was leased 192.168.100.1, the same address
// the gateway advertises as IPGATEWAY. Mirrors macipgw init_ip (ipent[0] ASSIGN_FIXED,
// my address = net+1).
func newIPPool(network IPv4, hostCount int) *ipPool {
	if hostCount < 2 {
		hostCount = 2
	}
	entries := make([]leaseEntry, hostCount)
	entries[1].reserved = true // gateway (base+1); index 0 (base) is the network addr
	return &ipPool{
		base:    network.u32(),
		entries: entries,
		extByAT: make(map[[3]byte]uint32),
		extByIP: make(map[uint32][3]byte),
		extSeen: make(map[[3]byte]time.Time),
	}
}

// assign returns the IP for an AppleTalk endpoint, honouring a prior lease (or
// a requested IP that is free) before allocating the next free slot. Returns the
// zero IP and false when the pool is exhausted. fresh is true when a new slot was
// claimed (as opposed to refreshing an existing lease for the same endpoint).
//
// Slots held by a learnSource claim (a statically addressed Mac snooped on the
// wire) are treated as used and are never handed out — matching the requirement
// that a learned address is taken for subsequent allocations.
func (p *ipPool) assign(requested IPv4, atNetwork uint16, atNode uint8) (ip IPv4, fresh bool, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Reuse an existing lease for this endpoint.
	for i := 1; i < len(p.entries); i++ {
		e := &p.entries[i]
		if e.used && e.atNetwork == atNetwork && e.atNode == atNode {
			e.lastSeen = time.Now()
			return fromU32(p.base + uint32(i)), false, true
		}
	}

	// Honour a specific requested IP if it falls in range and is free (never the
	// reserved network/gateway slots, and never an address held by a learned
	// external binding).
	if !requested.IsZero() {
		idx := int(requested.u32() - p.base)
		if idx >= 1 && idx < len(p.entries) && !p.entries[idx].used && !p.entries[idx].reserved {
			if _, taken := p.extByIP[requested.u32()]; !taken {
				p.entries[idx] = leaseEntry{used: true, atNetwork: atNetwork, atNode: atNode, lastSeen: time.Now()}
				return requested, true, true
			}
		}
	}

	// Allocate the next free slot (index 1 is the reserved gateway; the scan skips it
	// via the reserved flag, so the first assignable address is base+2). Skip any
	// address still held only as an external/learned binding.
	for i := 1; i < len(p.entries); i++ {
		if p.entries[i].used || p.entries[i].reserved {
			continue
		}
		addr := p.base + uint32(i)
		if _, taken := p.extByIP[addr]; taken {
			continue
		}
		p.entries[i] = leaseEntry{used: true, atNetwork: atNetwork, atNode: atNode, lastSeen: time.Now()}
		return fromU32(addr), true, true
	}
	return IPv4{}, false, false
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

// learnSource records the source-IP↔AppleTalk binding observed on an inbound Mac
// data packet, mirroring the original macipgw's arp_set() on every received IP
// packet (macip.c ip_input → arp_set). This is how a STATICALLY addressed Mac —
// one that never took a lease from our pool — becomes reachable for return
// traffic: without it, lookupByIP fails for that Mac's IP and inbound packets are
// dropped. It is a no-op (returns false) when:
//   - the source IP is zero or the AppleTalk endpoint is not a usable unicast, or
//   - the source IP already belongs to a used static-pool slot for a DIFFERENT
//     endpoint (the pool is authoritative there; a snoop must not contradict a
//     real lease), or
//   - the source IP is a reserved slot (network address / gateway).
//
// When the IP falls inside the static pool and the slot is free, the slot is
// CLAIMED (marked used) so assign() never hands that address out later — a
// learned Mac's IP is taken. Out-of-range IPs live in the external map (the seam
// for DHCP / off-subnet bindings) and age via extSeen. Returns true when a
// new/changed binding was recorded.
func (p *ipPool) learnSource(srcIP IPv4, atNetwork uint16, atNode uint8) bool {
	if srcIP.IsZero() || !validATEndpoint(atNetwork, atNode) {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	k := atKey(atNetwork, atNode)

	// In-range: the static pool owns these addresses.
	if idx := int(srcIP.u32() - p.base); idx >= 1 && idx < len(p.entries) {
		e := &p.entries[idx]
		if e.reserved {
			return false
		}
		if e.used {
			if e.atNetwork == atNetwork && e.atNode == atNode {
				e.lastSeen = time.Now()
			}
			return false // another endpoint (or same) already holds the slot
		}
		// Free in-range slot: claim it so subsequent assign() skips this address.
		p.clearExternalLocked(k)
		p.clearStaticForATLocked(atNetwork, atNode)
		// Drop any external binding that previously claimed this IP.
		if oldAT, ok := p.extByIP[srcIP.u32()]; ok {
			delete(p.extByAT, oldAT)
			delete(p.extSeen, oldAT)
			delete(p.extByIP, srcIP.u32())
		}
		p.entries[idx] = leaseEntry{used: true, atNetwork: atNetwork, atNode: atNode, lastSeen: time.Now()}
		return true
	}

	// Out of static range: record in the external map.
	// Already the same binding? Just refresh liveness.
	if cur, ok := p.extByIP[srcIP.u32()]; ok && cur == k {
		p.extSeen[k] = time.Now()
		return false
	}
	// Record (or re-point) the binding, clearing any stale IP this endpoint held.
	if old, ok := p.extByAT[k]; ok {
		delete(p.extByIP, old)
	}
	p.extByAT[k] = srcIP.u32()
	p.extByIP[srcIP.u32()] = k
	p.extSeen[k] = time.Now()
	return true
}

// clearExternalLocked drops any external binding for the given AT key. Caller holds mu.
func (p *ipPool) clearExternalLocked(k [3]byte) {
	if old, ok := p.extByAT[k]; ok {
		delete(p.extByIP, old)
		delete(p.extByAT, k)
		delete(p.extSeen, k)
	}
}

// clearStaticForATLocked frees any static-pool slot held by this AppleTalk endpoint
// so a learn/re-point does not leave the endpoint owning two addresses. Caller holds mu.
func (p *ipPool) clearStaticForATLocked(atNetwork uint16, atNode uint8) {
	for i := 1; i < len(p.entries); i++ {
		e := &p.entries[i]
		if e.used && !e.reserved && e.atNetwork == atNetwork && e.atNode == atNode {
			*e = leaseEntry{}
		}
	}
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

// expire evicts static and external (DHCP/snooped) leases unseen for longer than
// leaseDuration. External bindings age on the same clock as static ones so a
// snooped static-Mac binding does not linger after that Mac goes away.
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
	for k, seen := range p.extSeen {
		if now.Sub(seen) > leaseDuration {
			if ip, ok := p.extByAT[k]; ok {
				delete(p.extByIP, ip)
			}
			delete(p.extByAT, k)
			delete(p.extSeen, k)
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
