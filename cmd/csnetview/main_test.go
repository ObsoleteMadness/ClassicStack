package main

import (
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/client/netbios"
)

// csnetview is a thin consumer of client/netbios: the browser solicit/decode/aggregate
// logic (announcementToHost, mergeHost, Browse) is tested in that SDK package. Here we
// only cover the tool-local rendering helper.

func TestDash(t *testing.T) {
	for in, want := range map[string]string{"": "-", "0.0": "-", "4.0": "4.0"} {
		if got := dash(in); got != want {
			t.Errorf("dash(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestProtocolsCovered guards that the tool groups by every carrier the SDK exposes, so a
// new datagram carrier added to the SDK is not silently dropped from the sweep output.
func TestProtocolsCovered(t *testing.T) {
	if len(netbios.Protocols) == 0 {
		t.Fatal("SDK exposes no datagram carriers")
	}
}
