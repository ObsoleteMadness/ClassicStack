package finder

import (
	"testing"

	ncpproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ncp"
)

func TestNCPVolumeURI(t *testing.T) {
	v := ncpVolume(ncpproto.SAPEntry{
		Name:    "NW311",
		Network: [4]byte{0, 0, 0, 0x10},
		Node:    [6]byte{1, 2, 3, 4, 5, 6},
	})
	if v.ID != "ncp://NW311/SYS" || v.URI != "ncp://NW311,ipx" {
		t.Fatalf("id/uri = %q %q", v.ID, v.URI)
	}
	if v.Address != "00000010:01:02:03:04:05:06" {
		t.Fatalf("Address = %q", v.Address)
	}
}

func TestFormatNCPLogin(t *testing.T) {
	got := formatNCPLogin(false)
	if len(got) != 1 || got[0] != "Unencrypted" {
		t.Fatalf("cleartext = %v", got)
	}
	got = formatNCPLogin(true)
	if len(got) != 2 || got[0] != "Encrypted bindery" || got[1] != "Unencrypted" {
		t.Fatalf("encrypted = %v", got)
	}
}
