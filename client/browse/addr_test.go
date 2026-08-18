package browse

import (
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/client/netbios"
)

func TestMergeKeepsCarrierAddressSeparateFromComment(t *testing.T) {
	agg := map[string]*Server{}
	merge(agg, "FOO", netbios.NBF, SourceAnnouncement, "", "ClassicStack", "", "00:11:22:33:44:55")
	s := agg["FOO"]
	if s == nil {
		t.Fatal("missing server")
	}
	if s.Comment != "ClassicStack" {
		t.Fatalf("Comment = %q", s.Comment)
	}
	if s.AddressFor(netbios.NBF) != "00:11:22:33:44:55" {
		t.Fatalf("NBF addr = %q", s.AddressFor(netbios.NBF))
	}
	if s.Address != "" {
		t.Fatalf("TCP Address = %q, want empty", s.Address)
	}

	merge(agg, "FOO", netbios.NBIPX, SourceAnnouncement, "", "", "", "00000010:02:aa:bb:cc:dd:ee")
	if got := s.AddressFor(netbios.NBIPX); got != "00000010:02:aa:bb:cc:dd:ee" {
		t.Fatalf("NBIPX addr = %q", got)
	}

	merge(agg, "FOO", netbios.TCP, SourceMaster, "", "", "", "192.168.0.10")
	if s.AddressFor(netbios.TCP) != "192.168.0.10" || s.Address != "192.168.0.10" {
		t.Fatalf("TCP addr = %q Address = %q", s.AddressFor(netbios.TCP), s.Address)
	}
}
