package finder

import (
	"testing"

	clientlink "github.com/ObsoleteMadness/ClassicStack/client/link"
	"github.com/ObsoleteMadness/ClassicStack/client/uri"
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
)

func TestSpecFromInterface_bridgePcapDevice(t *testing.T) {
	// server.toml: [[interface]] name=br-lan kind=bridge backend=pcap device=en0
	got := SpecFromInterface(config.InterfaceSection{
		Name:    "br-lan",
		Kind:    config.IfaceKindBridge,
		Default: true,
		Backend: config.IfaceBackendPcap,
		Device:  "en0",
	})
	if got.Kind != clientlink.KindPcap || got.Name != "en0" {
		t.Fatalf("got %+v, want pcap/en0", got)
	}
}

func TestSpecFromInterface_nicFallsBackToName(t *testing.T) {
	got := SpecFromInterface(config.InterfaceSection{
		Name: "eth0",
		Kind: config.IfaceKindNIC,
	})
	if got.Kind != clientlink.KindPcap || got.Name != "eth0" {
		t.Fatalf("got %+v, want pcap/eth0", got)
	}
}

func TestSpecFromInterface_serial(t *testing.T) {
	got := SpecFromInterface(config.InterfaceSection{
		Name:   "ttyUSB-attic",
		Kind:   config.IfaceKindSerial,
		Device: "/dev/ttyUSB0",
		Baud:   1000000,
	})
	if got.Kind != clientlink.KindTashTalk || got.Name != "/dev/ttyUSB0" || got.Baud != 1000000 {
		t.Fatalf("got %+v, want tashtalk /dev/ttyUSB0 1000000", got)
	}
}

func TestSpecFromInterface_multicast(t *testing.T) {
	got := SpecFromInterface(config.InterfaceSection{
		Name: "ltoudp",
		Kind: config.IfaceKindMulticast,
	})
	if got.Kind != clientlink.KindLToUDP {
		t.Fatalf("got %+v, want ltoudp", got)
	}
}

func TestSpecFromInterface_tap(t *testing.T) {
	got := SpecFromInterface(config.InterfaceSection{
		Name:    "tap0",
		Kind:    config.IfaceKindNIC,
		Backend: config.IfaceBackendTap,
	})
	if got.Kind != clientlink.KindTap || got.Name != "tap0" {
		t.Fatalf("got %+v, want tap/tap0", got)
	}
}

func TestSpecFromInterface_empty(t *testing.T) {
	got := SpecFromInterface(config.InterfaceSection{})
	if got != (clientlink.Spec{}) {
		t.Fatalf("got %+v, want zero", got)
	}
}

func TestSpecFromInterface_tunIgnored(t *testing.T) {
	got := SpecFromInterface(config.InterfaceSection{
		Name:    "tun0",
		Kind:    config.IfaceKindNIC,
		Backend: config.IfaceBackendTun,
	})
	if got != (clientlink.Spec{}) {
		t.Fatalf("got %+v, want zero (no client tun transport)", got)
	}
}

func bridgeEn0() config.InterfaceSection {
	return config.InterfaceSection{
		Name:    "br-lan",
		Kind:    config.IfaceKindBridge,
		Default: true,
		Backend: config.IfaceBackendPcap,
		Device:  "en0",
	}
}

func TestResolveLink_usesConfiguredPcapForAFP(t *testing.T) {
	svc := New(nil, nil)
	svc.SetLinkConfig(func() config.InterfaceSection { return bridgeEn0() })
	got, err := svc.resolveLink(KindAFP, "", "", "", uri.Target{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != clientlink.KindPcap || got.Name != "en0" {
		t.Fatalf("got %+v, want pcap/en0 (not the AFP LToUDP default)", got)
	}
}

func TestResolveLink_usesConfiguredPcapForSMB(t *testing.T) {
	svc := New(nil, nil)
	svc.SetLinkConfig(func() config.InterfaceSection { return bridgeEn0() })
	got, err := svc.resolveLink(KindSMB, "", "", "", uri.Target{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != clientlink.KindPcap || got.Name != "en0" {
		t.Fatalf("got %+v, want pcap/en0 (not the OS default-route NIC)", got)
	}
}

func TestResolveLink_explicitRequestWins(t *testing.T) {
	svc := New(nil, nil)
	svc.SetLinkConfig(func() config.InterfaceSection { return bridgeEn0() })
	got, err := svc.resolveLink(KindAFP, clientlink.KindLToUDP, "", "", uri.Target{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != clientlink.KindLToUDP {
		t.Fatalf("got kind %q, want ltoudp", got.Kind)
	}
	if got.Name == "en0" {
		t.Fatalf("explicit ltoudp should not inherit pcap device, got %+v", got)
	}
}

func TestResolveLink_explicitIfaceWins(t *testing.T) {
	svc := New(nil, nil)
	svc.SetLinkConfig(func() config.InterfaceSection { return bridgeEn0() })
	got, err := svc.resolveLink(KindSMB, "", "en1", "", uri.Target{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != clientlink.KindPcap || got.Name != "en1" {
		t.Fatalf("got %+v, want pcap/en1", got)
	}
}

func TestResolveLink_noConfigFallsBackToSchemeDefault(t *testing.T) {
	svc := New(nil, nil)
	got, err := svc.resolveLink(KindAFP, "", "", "", uri.Target{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != clientlink.KindLToUDP {
		t.Fatalf("got kind %q, want ltoudp scheme default", got.Kind)
	}
}

func TestResolveLink_serialAFP(t *testing.T) {
	svc := New(nil, nil)
	svc.SetLinkConfig(func() config.InterfaceSection {
		return config.InterfaceSection{
			Name:   "tty",
			Kind:   config.IfaceKindSerial,
			Device: "/dev/ttyUSB0",
			Baud:   1000000,
		}
	})
	got, err := svc.resolveLink(KindAFP, "", "", "", uri.Target{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != clientlink.KindTashTalk || got.Name != "/dev/ttyUSB0" || got.Baud != 1000000 {
		t.Fatalf("got %+v, want tashtalk", got)
	}
}

func TestResolveLink_serialDoesNotForceSMB(t *testing.T) {
	svc := New(nil, nil)
	svc.SetLinkConfig(func() config.InterfaceSection {
		return config.InterfaceSection{
			Name:   "tty",
			Kind:   config.IfaceKindSerial,
			Device: "/dev/ttyUSB0",
		}
	})
	got, err := svc.resolveLink(KindSMB, "", "", "", uri.Target{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != clientlink.KindPcap {
		t.Fatalf("got kind %q, want pcap (SMB cannot ride TashTalk)", got.Kind)
	}
	if got.Name == "/dev/ttyUSB0" {
		t.Fatalf("SMB should not inherit the serial device, got %+v", got)
	}
}

func TestResolveLink_uriTransportWins(t *testing.T) {
	svc := New(nil, nil)
	svc.SetLinkConfig(func() config.InterfaceSection { return bridgeEn0() })
	got, err := svc.resolveLink(KindAFP, "", "", "", uri.Target{Transport: clientlink.KindLToUDP})
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != clientlink.KindLToUDP {
		t.Fatalf("got kind %q, want ltoudp from URI", got.Kind)
	}
}

func TestResolveLink_smbURICarrierNBF(t *testing.T) {
	svc := New(nil, nil)
	svc.SetLinkConfig(func() config.InterfaceSection { return bridgeEn0() })
	got, err := svc.resolveLink(KindSMB, "", "", "", uri.Target{Scheme: KindSMB, Server: "FOO", Transport: "nbf"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != clientlink.KindPcap || got.Name != "en0" || got.Carrier != "nbf" {
		t.Fatalf("got %+v, want pcap/en0 carrier nbf", got)
	}
}

func TestResolveLink_smbURITCPUsesServer(t *testing.T) {
	svc := New(nil, nil)
	svc.SetLinkConfig(func() config.InterfaceSection { return bridgeEn0() })
	got, err := svc.resolveLink(KindSMB, "", "", "", uri.Target{Scheme: KindSMB, Server: "192.168.0.10", Transport: clientlink.KindTCP})
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != clientlink.KindTCP || got.Name != "192.168.0.10" {
		t.Fatalf("got %+v, want tcp/192.168.0.10 (not the pcap device)", got)
	}
}

// modelStub is a componentSource that also carries a Model, the same optional
// capability *runtime.Runtime exposes. New must bind [[interface]] from it without
// the cmd edge calling SetLinkConfig.
type modelStub struct{ m *config.Model }

func (modelStub) Component(string) component.Component { return nil }
func (modelStub) Built() []string                      { return nil }
func (s modelStub) Model() *config.Model               { return s.m }

func TestNew_bindsDefaultInterfaceFromModel(t *testing.T) {
	m := config.NewModel()
	m.SetInterface(bridgeEn0())
	svc := New(modelStub{m: m}, nil)
	got, err := svc.resolveLink(KindAFP, "", "", "", uri.Target{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != clientlink.KindPcap || got.Name != "en0" {
		t.Fatalf("New(src) got %+v, want pcap/en0 from src.Model() (not a CLI SetLinkConfig)", got)
	}
}
