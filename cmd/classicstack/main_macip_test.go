package main

import "testing"

func TestSelectPreferredIPv4_PrefersRoutableAddress(t *testing.T) {
	got, ok := selectPreferredIPv4([]string{"169.254.10.20", "192.168.1.25"})
	if !ok {
		t.Fatal("selectPreferredIPv4 returned ok=false")
	}
	if got != "192.168.1.25" {
		t.Fatalf("selectPreferredIPv4 = %q, want %q", got, "192.168.1.25")
	}
}

func TestSelectPreferredIPv4_FallsBackToLinkLocal(t *testing.T) {
	got, ok := selectPreferredIPv4([]string{"169.254.10.20", "127.0.0.1"})
	if !ok {
		t.Fatal("selectPreferredIPv4 returned ok=false")
	}
	if got != "169.254.10.20" {
		t.Fatalf("selectPreferredIPv4 = %q, want %q", got, "169.254.10.20")
	}
}

func TestSelectPreferredIPv4_RejectsInvalidInputs(t *testing.T) {
	if got, ok := selectPreferredIPv4([]string{"", "not-an-ip", "127.0.0.1"}); ok || got != "" {
		t.Fatalf("selectPreferredIPv4 = (%q, %t), want (\"\", false)", got, ok)
	}
}
