//go:build ipx || all

package main

import (
	"testing"

	routeripx "github.com/ObsoleteMadness/ClassicStack/router/ipx"
)

func TestParseIPXNetwork(t *testing.T) {
	cases := []struct {
		in   string
		want [4]byte
	}{
		{"", routeripx.DefaultNetwork},
		{"DEADBEEF", [4]byte{0xDE, 0xAD, 0xBE, 0xEF}},
		{"deadbeef", [4]byte{0xDE, 0xAD, 0xBE, 0xEF}},
		{"0xDEADBEEF", [4]byte{0xDE, 0xAD, 0xBE, 0xEF}},
		{"  cafef00d  ", [4]byte{0xCA, 0xFE, 0xF0, 0x0D}},
	}
	for _, tc := range cases {
		got, err := parseIPXNetwork(tc.in)
		if err != nil {
			t.Fatalf("parseIPXNetwork(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("parseIPXNetwork(%q): got %x want %x", tc.in, got, tc.want)
		}
	}
}

func TestParseIPXNetworkErrors(t *testing.T) {
	for _, in := range []string{
		"DEAD",      // too short
		"DEADBEEFCC", // too long
		"GHIJKLMN",  // non-hex
	} {
		if _, err := parseIPXNetwork(in); err == nil {
			t.Errorf("parseIPXNetwork(%q) accepted invalid input", in)
		}
	}
}
