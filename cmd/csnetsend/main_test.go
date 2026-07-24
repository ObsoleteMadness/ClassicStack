package main

import (
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/client/netbios"
)

// csnetsend is a thin consumer of client/netbios: the wire logic (messenger frame,
// mailslot envelope, datagram framing) is tested in that SDK package. Here we only cover
// the tool-local concerns — the "<name>,<protocol>" recipient parse the SDK backs, and
// the -mac parser — so a regression in the CLI's own surface is caught.

func TestParseTargetRecipient(t *testing.T) {
	got, err := netbios.ParseTarget("SERVER,nbipx", netbios.MessengerNameType)
	if err != nil {
		t.Fatalf("ParseTarget: %v", err)
	}
	if got.Name.String() != "SERVER" || got.Protocol != netbios.NBIPX {
		t.Fatalf("target = %q/%q, want SERVER/nbipx", got.Name.String(), got.Protocol)
	}
}

func TestParseMAC(t *testing.T) {
	if _, err := parseMAC("aa:bb:cc:dd:ee:ff"); err != nil {
		t.Errorf("valid MAC rejected: %v", err)
	}
	if _, err := parseMAC("nope"); err == nil {
		t.Error("invalid MAC accepted")
	}
}
