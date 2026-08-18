package finder

import (
	"strings"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/client/browse"
	"github.com/ObsoleteMadness/ClassicStack/client/netbios"
	smbproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/smb"
)

func TestSMBScanFlags(t *testing.T) {
	nbf, ipx, tcp := smbScanFlags("")
	if !nbf || !ipx || !tcp {
		t.Fatalf("empty = %v %v %v, want all true", nbf, ipx, tcp)
	}
	nbf, ipx, tcp = smbScanFlags("netbeui")
	if !nbf || ipx || tcp {
		t.Fatalf("netbeui = %v %v %v", nbf, ipx, tcp)
	}
	nbf, ipx, tcp = smbScanFlags("ipx")
	if nbf || !ipx || tcp {
		t.Fatalf("ipx = %v %v %v", nbf, ipx, tcp)
	}
	nbf, ipx, tcp = smbScanFlags("tcp")
	if nbf || ipx || !tcp {
		t.Fatalf("tcp = %v %v %v", nbf, ipx, tcp)
	}
}

func TestSMBVolumePerCarrier(t *testing.T) {
	srv := browse.Server{Name: "FOO", Comment: "ClassicStack", Address: "192.168.0.10"}
	nbf, ok := smbVolume(srv, netbios.NBF)
	if !ok || nbf.ID != "smb://FOO,nbf/" || nbf.Transport != TransportNetBEUI || nbf.Title != "FOO" {
		t.Fatalf("nbf = %+v ok=%v", nbf, ok)
	}
	if nbf.URI != "smb://FOO,nbf" {
		t.Fatalf("nbf URI = %q", nbf.URI)
	}
	if nbf.Address != "" {
		t.Fatalf("nbf Address = %q, want empty (comment is not an address)", nbf.Address)
	}
	if nbf.Subtitle != "ClassicStack" {
		t.Fatalf("nbf Subtitle = %q", nbf.Subtitle)
	}
	ipx, ok := smbVolume(srv, netbios.NBIPX)
	if !ok || ipx.ID != "smb://FOO,nbipx/" || ipx.Transport != TransportIPX {
		t.Fatalf("ipx = %+v ok=%v", ipx, ok)
	}
	if ipx.URI != "smb://FOO,nbipx" {
		t.Fatalf("ipx URI = %q", ipx.URI)
	}
	tcp, ok := smbVolume(srv, netbios.TCP)
	if !ok || tcp.ID != "smb://192.168.0.10,tcp/" || tcp.Transport != TransportTCP || tcp.Title != "FOO" {
		t.Fatalf("tcp = %+v ok=%v", tcp, ok)
	}
	if tcp.Address != "192.168.0.10" || tcp.URI != "smb://192.168.0.10,tcp" {
		t.Fatalf("tcp address/uri = %q %q", tcp.Address, tcp.URI)
	}
}

func TestSMBVolumeOSFromAnnouncement(t *testing.T) {
	srv := browse.Server{Name: "WIN98", Comment: "Pete's PC", OSVersion: "4.10"}
	v, ok := smbVolume(srv, netbios.NBF)
	if !ok {
		t.Fatal("expected volume")
	}
	if v.OS != "Windows 98 (4.10)" {
		t.Fatalf("OS = %q", v.OS)
	}
	if v.Subtitle != "Pete's PC" {
		t.Fatalf("Subtitle = %q", v.Subtitle)
	}
}

func TestFormatSMBOS(t *testing.T) {
	if formatSMBOS("") != "" || formatSMBOS("0.0") != "" {
		t.Fatal("empty/zero should be blank")
	}
	if got := formatSMBOS("4.10"); got != "Windows 98 (4.10)" {
		t.Fatalf("4.10 = %q", got)
	}
	if got := formatSMBOS("4.0"); got != "Windows 95 / NT 4.0 (4.0)" {
		t.Fatalf("4.0 = %q", got)
	}
	if got := formatSMBOS("12.3"); got != "12.3" {
		t.Fatalf("unknown = %q", got)
	}
}

func TestFormatSMBVersion(t *testing.T) {
	if formatSMBVersion("") != "" {
		t.Fatal("empty")
	}
	if got := formatSMBVersion("NT LM 0.12"); got != "SMB 1.0 (NT LM 0.12)" {
		t.Fatalf("ntlm = %q", got)
	}
	if got := formatSMBVersion("LANMAN2.1"); got != "LAN Manager 2.1" {
		t.Fatalf("lanman = %q", got)
	}
}

func TestFormatSMBAuth(t *testing.T) {
	got := formatSMBAuth(false, false, 0)
	want := []string{"Share-level security", "Plaintext passwords"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("core = %v, want %v", got, want)
	}
	got = formatSMBAuth(true, true, smbproto.CapNTSMBs|smbproto.CapNTStatus|smbproto.CapNTFind|smbproto.CapLargeFiles)
	if got[0] != "User-level security" || got[1] != "Encrypted passwords" {
		t.Fatalf("security = %v", got)
	}
	joined := strings.Join(got, ",")
	for _, name := range []string{"NT SMBs", "NT status", "NT Find", "Large files"} {
		if !strings.Contains(joined, name) {
			t.Fatalf("%q missing from %v", name, got)
		}
	}
}
