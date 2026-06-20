package toml

import (
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/service/ipxgw"
	"github.com/ObsoleteMadness/ClassicStack/core/service/macip"
	"github.com/ObsoleteMadness/ClassicStack/core/service/netbios"
	"github.com/ObsoleteMadness/ClassicStack/core/service/smb"
)

// TestSMBServerSectionRoundTrip proves the SMB server-level transport bindings survive
// a TOML round-trip through the schema registry: the transports the operator sets are
// what the compose cross-wire reads back via smb.ServerSectionFromModel.
func TestSMBServerSectionRoundTrip(t *testing.T) {
	smb.RegisterServer()

	m := config.NewModel()
	m.Set(&smb.ServerSection{SKey: smb.ServerKey, Transports: []string{smb.TransportNBT, smb.TransportTCP}})

	c := New()
	data, err := c.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got := config.NewModel()
	if err := c.Unmarshal(data, got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	sec := smb.ServerSectionFromModel(got)
	if !sec.Binds(smb.TransportNBT) || !sec.Binds(smb.TransportTCP) {
		t.Errorf("expected nbt+tcp bound, got %v", sec.Transports)
	}
	if sec.Binds(smb.TransportIPX) {
		t.Errorf("ipx should NOT be bound when an explicit list omits it: %v", sec.Transports)
	}
}

// TestSMBServerSectionDefaultsBindAll proves a config with no [SMB] section binds every
// transport (empty list → Binds always true), the back-compat default.
func TestSMBServerSectionDefaultsBindAll(t *testing.T) {
	smb.RegisterServer()
	sec := smb.ServerSectionFromModel(config.NewModel())
	for _, tr := range []string{smb.TransportNetBEUI, smb.TransportIPX, smb.TransportNBT, smb.TransportTCP} {
		if !sec.Binds(tr) {
			t.Errorf("empty transports should bind %q (back-compat bind-all)", tr)
		}
	}
}

// TestNetBIOSSectionRoundTrip proves the NetBIOS transport bindings + scope survive a
// TOML round-trip.
func TestNetBIOSSectionRoundTrip(t *testing.T) {
	netbios.RegisterSection()

	m := config.NewModel()
	m.Set(&netbios.Section{SKey: netbios.SectionKey, Transports: []string{netbios.TransportNetBEUI}, ScopeID: "LAB"})

	c := New()
	data, err := c.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got := config.NewModel()
	if err := c.Unmarshal(data, got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	sec := netbios.SectionFromModel(got)
	if !sec.Binds(netbios.TransportNetBEUI) {
		t.Errorf("expected netbeui bound, got %v", sec.Transports)
	}
	if sec.Binds(netbios.TransportIPX) {
		t.Errorf("ipx should NOT be bound when the list omits it: %v", sec.Transports)
	}
	if sec.ScopeID != "LAB" {
		t.Errorf("scope_id: got %q want LAB", sec.ScopeID)
	}
}

// TestMacIPSectionRoundTrip proves the MacIP gateway section (mode + IP-side identity)
// survives a TOML round-trip and ToConfig parses the dotted-quad fields.
func TestMacIPSectionRoundTrip(t *testing.T) {
	macip.RegisterSection()

	m := config.NewModel()
	m.Set(&macip.Section{
		SKey: macip.SectionKey, Enabled: true, Mode: macip.ModeNAT, Zone: "Eng",
		GatewayIP: "192.168.100.1", Network: "192.168.100.0", Nameserver: "1.1.1.1",
		Broadcast: "192.168.100.255", SubnetMask: "255.255.255.0", HostCount: 200,
	})

	c := New()
	data, err := c.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got := config.NewModel()
	if err := c.Unmarshal(data, got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	sec := macip.SectionFromModel(got)
	if sec == nil {
		t.Fatal("MacIP section missing after round-trip")
	}
	if !sec.Enabled || sec.EffectiveMode() != macip.ModeNAT || sec.Zone != "Eng" {
		t.Fatalf("scalar fields wrong: %+v", sec)
	}
	cfg := sec.ToConfig()
	if cfg.GatewayIP != (macip.IPv4{192, 168, 100, 1}) {
		t.Errorf("gateway parse: %v", cfg.GatewayIP)
	}
	if !cfg.NATEnabled || cfg.HostCount != 200 {
		t.Errorf("nat/hostcount: %+v", cfg)
	}
}

// TestIPXGWSectionRoundTrip proves the IPX-gateway section (enable / IPX network / NBP
// zone bindings) survives a TOML round-trip and ZoneBindings parses the Object:Zone
// strings.
func TestIPXGWSectionRoundTrip(t *testing.T) {
	ipxgw.RegisterSection()

	m := config.NewModel()
	m.Set(&ipxgw.Section{
		SKey: ipxgw.SectionKey, Enabled: true, IPXNetwork: 0x20,
		Bindings: []string{"IPX Gateway:Eng", "IPX Gateway:Lab"},
	})

	c := New()
	data, err := c.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got := config.NewModel()
	if err := c.Unmarshal(data, got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	sec := ipxgw.SectionFromModel(got)
	if sec == nil || !sec.Enabled || sec.IPXNetwork != 0x20 {
		t.Fatalf("scalar fields wrong: %+v", sec)
	}
	zb := sec.ZoneBindings()
	if len(zb) != 2 || string(zb[0].Zone) != "Eng" || string(zb[1].Zone) != "Lab" {
		t.Fatalf("zone bindings parse wrong: %+v", zb)
	}
}

// TestCaptureSectionRoundTrip proves the per-interface pcap capture paths + snaplen
// survive a TOML round-trip (the [capture] well-known section).
func TestCaptureSectionRoundTrip(t *testing.T) {
	m := config.NewModel()
	m.Capture = config.CaptureSection{
		Paths:   map[string]string{"eth0": "/var/cap/eth0.pcap", "br-lan": "/var/cap/lan.pcap"},
		Snaplen: 256,
	}

	c := New()
	data, err := c.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got := config.NewModel()
	if err := c.Unmarshal(data, got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.Capture.PathFor("eth0") != "/var/cap/eth0.pcap" {
		t.Errorf("eth0 path: got %q", got.Capture.PathFor("eth0"))
	}
	if got.Capture.PathFor("br-lan") != "/var/cap/lan.pcap" {
		t.Errorf("br-lan path: got %q", got.Capture.PathFor("br-lan"))
	}
	if got.Capture.Snaplen != 256 {
		t.Errorf("snaplen: got %d want 256", got.Capture.Snaplen)
	}
}

// TestCaptureSectionDefaultOmitted proves a model with no capture configured emits no
// [capture] block (Any()==false, snaplen 0) and round-trips to an empty section.
func TestCaptureSectionDefaultOmitted(t *testing.T) {
	m := config.NewModel()
	c := New()
	data, err := c.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got := config.NewModel()
	if err := c.Unmarshal(data, got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Capture.Any() {
		t.Errorf("empty model should round-trip to no capture, got %+v", got.Capture)
	}
}
