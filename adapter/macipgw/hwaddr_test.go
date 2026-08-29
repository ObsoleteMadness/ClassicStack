package macipgw

import (
	"net"
	"testing"
)

// Characterization tests for parseEthernet's current behavior, captured before
// core/csnet (P0) consolidates MAC parsing across the repo — see the "MAC string
// parsing has 4 independent implementations" review finding. parseEthernet
// accepts colon/dash-separated or bare hex, but — unlike the net.ParseMAC-backed
// parsers (adapter/control/finder.parseMAC6, cmd/internal/csconnect.ParseMAC) —
// it does not accept the dot-separated Cisco form, since it only strips ':'/'-'.
func TestParseEthernet(t *testing.T) {
	want := net.HardwareAddr{0x00, 0x11, 0x22, 0xAA, 0xBB, 0xCC}
	cases := []struct {
		name string
		in   string
	}{
		{"colon separated", "00:11:22:AA:BB:CC"},
		{"colon separated lowercase", "00:11:22:aa:bb:cc"},
		{"dash separated", "00-11-22-AA-BB-CC"},
		{"bare hex", "001122AABBCC"},
		{"bare hex with surrounding whitespace", "  001122AABBCC  "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseEthernet(tc.in)
			if err != nil {
				t.Fatalf("parseEthernet(%q): unexpected error: %v", tc.in, err)
			}
			if !bytesEqual(got, want) {
				t.Errorf("parseEthernet(%q) = %v, want %v", tc.in, got, want)
			}
		})
	}
}

func TestParseEthernet_Rejects(t *testing.T) {
	cases := []string{
		"",
		"00:11:22:AA:BB",       // too short
		"00:11:22:AA:BB:CC:DD", // too long
		"00:11:22:AA:BB:GG",    // non-hex
		"0011223344",           // bare hex, too short
	}
	for _, in := range cases {
		if _, err := parseEthernet(in); err == nil {
			t.Errorf("parseEthernet(%q): expected error, got none", in)
		}
	}
}

func bytesEqual(a, b net.HardwareAddr) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
