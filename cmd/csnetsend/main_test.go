package main

import (
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/client/netbios"
)

// csnetsend is a thin consumer of client/netbios: the wire logic (messenger frame,
// mailslot envelope, datagram framing) is tested in that SDK package. Here we only cover
// the tool-local concern — the "<name>,<protocol>" recipient parse the SDK backs — so a
// regression in the CLI's own surface is caught. The -mac parser now lives in the shared
// cmd/internal/csconnect (csconnect.ParseMAC) and is tested there.

func TestParseTargetRecipient(t *testing.T) {
	got, err := netbios.ParseTarget("SERVER,nbipx", netbios.MessengerNameType)
	if err != nil {
		t.Fatalf("ParseTarget: %v", err)
	}
	if got.Name.String() != "SERVER" || got.Protocol != netbios.NBIPX {
		t.Fatalf("target = %q/%q, want SERVER/nbipx", got.Name.String(), got.Protocol)
	}
}
