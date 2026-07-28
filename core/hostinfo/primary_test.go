package hostinfo

import (
	"errors"
	"net"
	"testing"
)

// TestPrimaryIP checks that the routing-table probe returns a usable, non-loopback,
// non-unspecified IPv4 address on a host with a default route. On a CI box with no
// outbound route it may legitimately fail; that path returns ErrNoPrimaryInterface and
// is skipped rather than failed so the test does not depend on network topology.
func TestPrimaryIP(t *testing.T) {
	ip, err := PrimaryIP()
	if errors.Is(err, ErrNoPrimaryInterface) {
		t.Skip("no default route on this host; nothing to verify")
	}
	if err != nil {
		t.Fatalf("PrimaryIP: unexpected error: %v", err)
	}
	if ip.To4() == nil {
		t.Errorf("PrimaryIP = %v, want an IPv4 address", ip)
	}
	if ip.IsLoopback() {
		t.Errorf("PrimaryIP = %v, want a non-loopback address", ip)
	}
	if ip.IsUnspecified() {
		t.Errorf("PrimaryIP = %v, want a specific address", ip)
	}
}

// TestPrimaryInterface checks the primary IP resolves to a real, up, non-loopback NIC
// whose bound addresses include that IP — the invariant DefaultInterface relies on.
func TestPrimaryInterface(t *testing.T) {
	ifi, err := PrimaryInterface()
	if errors.Is(err, ErrNoPrimaryInterface) {
		t.Skip("no default route on this host; nothing to verify")
	}
	if err != nil {
		t.Fatalf("PrimaryInterface: unexpected error: %v", err)
	}
	if ifi.Flags&net.FlagLoopback != 0 {
		t.Errorf("PrimaryInterface %q is loopback, want a real NIC", ifi.Name)
	}

	ip, err := PrimaryIP()
	if err != nil {
		t.Fatalf("PrimaryIP after PrimaryInterface: %v", err)
	}
	addrs, err := ifi.Addrs()
	if err != nil {
		t.Fatalf("Addrs on primary interface %q: %v", ifi.Name, err)
	}
	found := false
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && ipn.IP.Equal(ip) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("primary IP %v not bound to reported primary interface %q", ip, ifi.Name)
	}
}
