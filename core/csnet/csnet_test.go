package csnet_test

import (
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/csnet"
)

func TestParseMAC(t *testing.T) {
	want := [6]byte{0x00, 0x11, 0x22, 0xAA, 0xBB, 0xCC}
	cases := []string{
		"00:11:22:AA:BB:CC",
		"00:11:22:aa:bb:cc",
		"00-11-22-AA-BB-CC",
		"001122AABBCC",
		"  00:11:22:AA:BB:CC  ",
	}
	for _, in := range cases {
		got, err := csnet.ParseMAC(in)
		if err != nil {
			t.Fatalf("ParseMAC(%q): unexpected error: %v", in, err)
		}
		if got != want {
			t.Errorf("ParseMAC(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestParseMAC_Rejects(t *testing.T) {
	cases := []string{"", "00:11:22:AA:BB", "00:11:22:AA:BB:CC:DD", "not-a-mac", "00:11:22:AA:BB:GG"}
	for _, in := range cases {
		if _, err := csnet.ParseMAC(in); err == nil {
			t.Errorf("ParseMAC(%q): expected error, got none", in)
		}
	}
}

func TestFormatMAC(t *testing.T) {
	mac := [6]byte{0x00, 0x11, 0x22, 0xAA, 0xBB, 0xCC}
	if got, want := csnet.FormatMAC(mac), "00:11:22:AA:BB:CC"; got != want {
		t.Errorf("FormatMAC(%v) = %q, want %q", mac, got, want)
	}
}

func TestParseMAC_FormatMAC_RoundTrip(t *testing.T) {
	mac, err := csnet.ParseMAC("aa:bb:cc:11:22:33")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := csnet.FormatMAC(mac), "AA:BB:CC:11:22:33"; got != want {
		t.Errorf("round trip = %q, want %q", got, want)
	}
}

func TestParseIPv4(t *testing.T) {
	got, err := csnet.ParseIPv4("10.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	want := csnet.IPv4{10, 0, 0, 1}
	if got != want {
		t.Errorf("ParseIPv4(%q) = %v, want %v", "10.0.0.1", got, want)
	}
	if got.String() != "10.0.0.1" {
		t.Errorf("String() = %q, want %q", got.String(), "10.0.0.1")
	}
}

func TestParseIPv4_Rejects(t *testing.T) {
	cases := []string{"", "10.0.0", "10.0.0.256", "10.0.0.1.2", "not-an-ip", "::1"}
	for _, in := range cases {
		if _, err := csnet.ParseIPv4(in); err == nil {
			t.Errorf("ParseIPv4(%q): expected error, got none", in)
		}
	}
}
