package toml

import (
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
	"github.com/ObsoleteMadness/ClassicStack/core/service/afp"
	"github.com/ObsoleteMadness/ClassicStack/core/service/etherdfs"
	"github.com/ObsoleteMadness/ClassicStack/core/service/ipxgw"
	"github.com/ObsoleteMadness/ClassicStack/core/service/macip"
	"github.com/ObsoleteMadness/ClassicStack/core/service/netbios"
	"github.com/ObsoleteMadness/ClassicStack/core/service/smb"
)

// TestEtherDFSSectionsRoundTrip proves the EtherDFS singleton server section (the
// NIC binding + advertised name) and a repeated drive section survive a TOML
// round-trip through the schema registry.
func TestEtherDFSSectionsRoundTrip(t *testing.T) {
	etherdfs.RegisterServer()
	etherdfs.RegisterDrives()

	m := config.NewModel()
	m.Set(&etherdfs.ServerSection{SKey: etherdfs.ServerKey, IsEnabled: true, Interface: "eth0", ServerName: "ATTIC"})
	m.AddInstance(&etherdfs.DriveSection{DName: "E", FSType: "local_fs", Path: "/srv/dos", NameEngine: "short"})

	c := New()
	data, err := c.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got := config.NewModel()
	if err := c.Unmarshal(data, got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	srv := etherdfs.ServerSectionFromModel(got)
	if !srv.IsEnabled || srv.Interface != "eth0" || srv.ServerName != "ATTIC" {
		t.Errorf("server section wrong: %+v", srv)
	}
	specs := etherdfs.SpecsFromModel(got)
	if len(specs) != 1 || specs[0].Name != "E" || specs[0].Share.Path != "/srv/dos" {
		t.Errorf("drive specs wrong: %+v", specs)
	}
	if specs[0].Share.NameEngine != "short" {
		t.Errorf("name_engine not round-tripped: %q", specs[0].Share.NameEngine)
	}
}

// TestAFPServerSectionRoundTrip proves the AFP server-level identity (name/zone) and
// transport bindings survive a TOML round-trip through the schema registry.
func TestAFPServerSectionRoundTrip(t *testing.T) {
	afp.RegisterServer()

	m := config.NewModel()
	m.Set(&afp.ServerSection{
		AKey: afp.ServerKey, ServerName: "Attic Mac", Zone: "Eng",
		Transports: []string{afp.TransportDDP}, TCPAddr: ":548",
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

	sec := afp.ServerSectionFromModel(got)
	if sec.EffectiveServerName("") != "Attic Mac" || sec.Zone != "Eng" {
		t.Errorf("identity wrong: name=%q zone=%q", sec.EffectiveServerName(""), sec.Zone)
	}
	if !sec.Binds(afp.TransportDDP) {
		t.Errorf("expected ddp bound, got %v", sec.Transports)
	}
	if sec.Binds(afp.TransportTCP) {
		t.Errorf("tcp should NOT be bound when the list omits it: %v", sec.Transports)
	}
	if sec.DSITCPAddr() != ":548" {
		t.Errorf("tcp_addr: got %q want :548", sec.DSITCPAddr())
	}
}

// TestAFPServerNameFallback proves an empty ServerName falls back to the supplied
// Identity hostname (the "one name everywhere" path).
func TestAFPServerNameFallback(t *testing.T) {
	sec := &afp.ServerSection{AKey: afp.ServerKey}
	if got := sec.EffectiveServerName("studio"); got != "studio" {
		t.Errorf("fallback to identity hostname: got %q want studio", got)
	}
}

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

// TestPortCaptureRoundTrip proves a port's pcap capture path + snaplen (now a property
// of the port section, not a central [capture] table) survive a TOML round-trip.
func TestPortCaptureRoundTrip(t *testing.T) {
	// Register the EtherTalk repeated schema so the codec knows to decode [[ethertalk]]
	// into a port.Section (mirrors the tashtalk round-trip test above).
	config.Register(config.SectionSchema{
		Key:      "EtherTalk",
		New:      func() config.Section { return &port.Section{SKey: "EtherTalk"} },
		Repeated: true,
	})

	m := config.NewModel()
	m.AddInstance(&port.Section{
		SKey: "EtherTalk", Name: "et-cap", IsEnabled: true,
		Capture: "/var/cap/et.pcap", CaptureSnaplen: 256,
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

	sec := port.InstanceFromModel(got, "EtherTalk", "et-cap")
	if sec.Capture != "/var/cap/et.pcap" {
		t.Errorf("capture path: got %q", sec.Capture)
	}
	if sec.CaptureSnaplen != 256 {
		t.Errorf("capture snaplen: got %d want 256", sec.CaptureSnaplen)
	}
}
