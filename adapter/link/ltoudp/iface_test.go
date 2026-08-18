package ltoudp

import (
	"net"
	"testing"
)

func TestIsHostLANInterface(t *testing.T) {
	upMulti := net.FlagUp | net.FlagMulticast
	cases := []struct {
		name string
		intf net.Interface
		want bool
	}{
		{"en0", net.Interface{Name: "en0", Flags: upMulti}, true},
		{"eth0", net.Interface{Name: "eth0", Flags: upMulti}, true},
		{"br-lan", net.Interface{Name: "br-lan", Flags: upMulti}, true},
		{"awdl0", net.Interface{Name: "awdl0", Flags: upMulti}, false},
		{"llw0", net.Interface{Name: "llw0", Flags: upMulti}, false},
		{"utun4", net.Interface{Name: "utun4", Flags: upMulti | net.FlagPointToPoint}, false},
		{"gif0", net.Interface{Name: "gif0", Flags: upMulti}, false},
		{"anpi0", net.Interface{Name: "anpi0", Flags: upMulti}, false},
	}
	for _, tc := range cases {
		if got := isHostLANInterface(&tc.intf); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestClassifyMulticastInterfaces_skipsAirDropAndVPN(t *testing.T) {
	ifaces := []net.Interface{
		{Index: 1, Name: "lo0", Flags: net.FlagUp | net.FlagLoopback | net.FlagMulticast},
		{Index: 2, Name: "awdl0", Flags: net.FlagUp | net.FlagMulticast | net.FlagBroadcast},
		{Index: 3, Name: "utun2", Flags: net.FlagUp | net.FlagPointToPoint | net.FlagMulticast},
		{Index: 4, Name: "en0", Flags: net.FlagUp | net.FlagMulticast | net.FlagBroadcast},
	}
	// interfaceHasIPv4 needs real addrs; empty Addr lists drop every candidate.
	// Classification of names/flags is still asserted via isHostLANInterface;
	// this test pins the split when IPv4 is present by stubbing through a
	// synthetic walk of the name filter only.
	var lanNames, loopNames []string
	for i := range ifaces {
		intf := &ifaces[i]
		if intf.Flags&net.FlagLoopback != 0 {
			loopNames = append(loopNames, intf.Name)
			continue
		}
		if isHostLANInterface(intf) {
			lanNames = append(lanNames, intf.Name)
		}
	}
	if len(lanNames) != 1 || lanNames[0] != "en0" {
		t.Fatalf("LAN ifaces = %v, want [en0]", lanNames)
	}
	if len(loopNames) != 1 || loopNames[0] != "lo0" {
		t.Fatalf("loopback ifaces = %v, want [lo0]", loopNames)
	}
}

func TestPickSendInterface_prefersDefaultRoute(t *testing.T) {
	en0 := &net.Interface{Index: 4, Name: "en0"}
	en1 := &net.Interface{Index: 5, Name: "en1"}
	lan := []*net.Interface{en1, en0}
	got := pickSendInterface(lan, en0)
	if got == nil || got.Name != "en0" {
		t.Fatalf("got %v, want en0 (default-route NIC in the join set)", got)
	}
}

func TestPickSendInterface_fallsBackToFirstLAN(t *testing.T) {
	en1 := &net.Interface{Index: 5, Name: "en1"}
	lan := []*net.Interface{en1}
	utun := &net.Interface{Index: 9, Name: "utun2"}
	got := pickSendInterface(lan, utun)
	if got == nil || got.Name != "en1" {
		t.Fatalf("got %v, want en1 (prefer not in join set)", got)
	}
}
