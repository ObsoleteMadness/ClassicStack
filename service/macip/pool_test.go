//go:build macip

package macip

import (
	"net"
	"testing"
	"time"
)

func TestIPPoolRejectsInvalidATEndpointOnAssign(t *testing.T) {
	p := newIPPool(net.ParseIP("192.168.100.0"), net.CIDRMask(24, 32))

	if _, err := p.assign(nil, 0, 1); err == nil {
		t.Fatal("assign with network 0 should fail")
	}
	if _, err := p.assign(nil, 1, 0); err == nil {
		t.Fatal("assign with node 0 should fail")
	}
	if _, err := p.assign(nil, 1, 0xFF); err == nil {
		t.Fatal("assign with broadcast node should fail")
	}
}

func TestIPPoolIgnoresInvalidDHCPRegistrations(t *testing.T) {
	p := newIPPool(net.ParseIP("192.168.100.0"), net.CIDRMask(24, 32))
	ip := net.ParseIP("192.168.100.50")

	p.registerDHCP(ip, 0, 1)
	p.registerDHCP(ip, 1, 0)

	if _, _, ok := p.lookupByIP(ip); ok {
		t.Fatal("lookupByIP should not return invalid DHCP registrations")
	}

	p.registerDHCP(ip, 1, 42)
	atNet, atNode, ok := p.lookupByIP(ip)
	if !ok {
		t.Fatal("lookupByIP should return valid DHCP registration")
	}
	if atNet != 1 || atNode != 42 {
		t.Fatalf("lookupByIP = %d.%d, want 1.42", atNet, atNode)
	}
}

func TestIPPoolPinnedStaticLeaseSurvivesExpiryUntilSessionClose(t *testing.T) {
	p := newIPPool(net.ParseIP("192.168.100.0"), net.CIDRMask(24, 32))
	ip, err := p.assign(nil, 1, 42)
	if err != nil {
		t.Fatalf("assign failed: %v", err)
	}

	i, ok := p.ipToIndex(ip)
	if !ok {
		t.Fatalf("assigned IP %s not in pool", ip)
	}
	p.entries[i].lastSeen = time.Now().Add(-leaseDuration - time.Minute)
	p.pinSessionLease(1, 42, 7)

	p.expireLeases()
	if _, _, ok := p.lookupByIP(ip); !ok {
		t.Fatal("pinned static lease should not expire while session is active")
	}

	p.unpinSessionLease(7)
	p.expireLeases()
	if _, _, ok := p.lookupByIP(ip); ok {
		t.Fatal("static lease should expire after session unpin")
	}
}

func TestIPPoolPinnedDHCPLeaseSurvivesExpiryUntilSessionClose(t *testing.T) {
	p := newIPPool(net.ParseIP("192.168.100.0"), net.CIDRMask(24, 32))
	ip := net.ParseIP("192.168.100.77")
	p.registerDHCP(ip, 3, 9)

	key := [3]byte{0, 3, 9}
	p.dhcpMu.Lock()
	p.dhcpSeen[key] = time.Now().Add(-leaseDuration - time.Minute)
	p.dhcpMu.Unlock()

	p.pinSessionLease(3, 9, 11)
	p.expireLeases()
	if _, _, ok := p.lookupByIP(ip); !ok {
		t.Fatal("pinned DHCP lease should not expire while session is active")
	}

	p.unpinSessionLease(11)
	p.expireLeases()
	if _, _, ok := p.lookupByIP(ip); ok {
		t.Fatal("DHCP lease should expire after session unpin")
	}
}

func TestIPPoolExpiredPinSafetyCapAllowsLeaseExpiry(t *testing.T) {
	p := newIPPool(net.ParseIP("192.168.100.0"), net.CIDRMask(24, 32))
	ip, err := p.assign(nil, 2, 33)
	if err != nil {
		t.Fatalf("assign failed: %v", err)
	}

	i, ok := p.ipToIndex(ip)
	if !ok {
		t.Fatalf("assigned IP %s not in pool", ip)
	}
	p.entries[i].lastSeen = time.Now().Add(-leaseDuration - time.Minute)
	p.pinSessionLease(2, 33, 21)

	key := [3]byte{0, 2, 33}
	p.mu.Lock()
	p.pinSeenByAT[key] = time.Now().Add(-pinnedLeaseHardTimeout - time.Minute)
	p.mu.Unlock()

	p.expireLeases()
	if _, _, ok := p.lookupByIP(ip); ok {
		t.Fatal("lease should expire after pin safety timeout elapses")
	}

	p.mu.Lock()
	_, stillPinned := p.pinBySession[21]
	p.mu.Unlock()
	if stillPinned {
		t.Fatal("expired pin should be removed from session map")
	}
}
