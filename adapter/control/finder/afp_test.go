package finder

import (
	"testing"

	afpclient "github.com/ObsoleteMadness/ClassicStack/client/afp"
	"github.com/ObsoleteMadness/ClassicStack/client/atalk"
	clientlink "github.com/ObsoleteMadness/ClassicStack/client/link"
	"github.com/ObsoleteMadness/ClassicStack/client/uri"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
)

func TestAFPScanFlags(t *testing.T) {
	iface, ltoudp, tcp := afpScanFlags("")
	if !iface || !ltoudp || !tcp {
		t.Fatalf("empty = %v %v %v, want all true", iface, ltoudp, tcp)
	}
	iface, ltoudp, tcp = afpScanFlags("ddp")
	if !iface || !ltoudp || tcp {
		t.Fatalf("ddp = %v %v %v", iface, ltoudp, tcp)
	}
	iface, ltoudp, tcp = afpScanFlags("tcp")
	if iface || ltoudp || !tcp {
		t.Fatalf("tcp = %v %v %v", iface, ltoudp, tcp)
	}
	iface, ltoudp, tcp = afpScanFlags("ltoudp")
	if iface || !ltoudp || tcp {
		t.Fatalf("ltoudp = %v %v %v", iface, ltoudp, tcp)
	}
	iface, ltoudp, tcp = afpScanFlags("pcap")
	if !iface || ltoudp || tcp {
		t.Fatalf("pcap = %v %v %v", iface, ltoudp, tcp)
	}
}

func TestAFPDDPSpecs_bridgeAddsLToUDP(t *testing.T) {
	def := SpecFromInterface(bridgeEn0())
	got := afpDDPSpecs(def, true, true)
	if len(got) != 2 {
		t.Fatalf("got %+v, want pcap + ltoudp", got)
	}
	if got[0].Kind != clientlink.KindPcap || got[0].Name != "en0" {
		t.Errorf("first = %+v, want pcap/en0", got[0])
	}
	if got[1].Kind != clientlink.KindLToUDP {
		t.Errorf("second = %+v, want ltoudp", got[1])
	}
}

func TestAFPDDPSpecs_multicastDoesNotDuplicate(t *testing.T) {
	def := SpecFromInterface(config.InterfaceSection{
		Name: "ltoudp",
		Kind: config.IfaceKindMulticast,
	})
	got := afpDDPSpecs(def, true, true)
	if len(got) != 1 || got[0].Kind != clientlink.KindLToUDP {
		t.Fatalf("got %+v, want one ltoudp", got)
	}
}

func TestAFPDDPSpecs_serialAddsLToUDP(t *testing.T) {
	def := clientlink.Spec{Kind: clientlink.KindTashTalk, Name: "/dev/ttyUSB0"}
	got := afpDDPSpecs(def, true, true)
	if len(got) != 2 || got[0].Kind != clientlink.KindTashTalk || got[1].Kind != clientlink.KindLToUDP {
		t.Fatalf("got %+v, want tashtalk + ltoudp", got)
	}
}

func TestAFPDDPSpecs_emptyConfigIsLToUDP(t *testing.T) {
	got := afpDDPSpecs(clientlink.Spec{}, true, true)
	if len(got) != 1 || got[0].Kind != clientlink.KindLToUDP {
		t.Fatalf("got %+v, want ltoudp scheme default", got)
	}
}

func TestAFPNBPVolumeURI(t *testing.T) {
	v := afpNBPVolume(atalk.NBPEntity{
		Object: "ClassicStack",
		Zone:   "EtherTalk Network",
		Addr:   atalk.Addr{Network: 65280, Node: 128},
	}, clientlink.KindPcap)
	if v.ID != "afp://ClassicStack:EtherTalk Network,pcap/" {
		t.Errorf("ID = %q", v.ID)
	}
	if v.Transport != TransportDDP || v.Title != "ClassicStack" || v.Subtitle != "EtherTalk Network" {
		t.Errorf("got %+v", v)
	}
	if v.Address != "65280.128, EtherTalk Network" {
		t.Errorf("Address = %q", v.Address)
	}
	if v.URI != "afp://ClassicStack:EtherTalk Network" {
		t.Errorf("URI = %q", v.URI)
	}
	lt := afpNBPVolume(atalk.NBPEntity{Object: "ClassicStack", Zone: "*", Addr: atalk.Addr{Network: 1, Node: 4}}, clientlink.KindLToUDP)
	if lt.ID != "afp://ClassicStack,ltoudp/" {
		t.Errorf("wildcard zone ID = %q", lt.ID)
	}
	if lt.Address != "1.4" || lt.URI != "afp://ClassicStack" {
		t.Errorf("wildcard address/uri = %q %q", lt.Address, lt.URI)
	}
}

func TestDedupAFPVolumesMergesWildcardAndNamedZone(t *testing.T) {
	wild := afpNBPVolume(atalk.NBPEntity{Object: "snow", Zone: "*"}, clientlink.KindLToUDP)
	named := afpNBPVolume(atalk.NBPEntity{Object: "snow", Zone: "EtherTalk"}, clientlink.KindPcap)
	got := dedupAFPVolumes([]VolumeInfo{wild, named})
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1: %+v", len(got), got)
	}
	if got[0].Subtitle != "EtherTalk" || got[0].ID != named.ID {
		t.Fatalf("got %+v, want named pcap entry", got[0])
	}
}

func TestAFPTCPVolumeURI(t *testing.T) {
	v, ok := afpTCPVolume(afpclient.TCPServer{Name: "Files", Host: "192.168.1.9", Port: 548})
	if !ok || v.ID != "afp://192.168.1.9,tcp/" || v.Transport != TransportTCP || v.Title != "Files" {
		t.Fatalf("got %+v ok=%v", v, ok)
	}
	if v.Address != "192.168.1.9" || v.URI != "afp://192.168.1.9,tcp" {
		t.Fatalf("tcp address/uri = %q %q", v.Address, v.URI)
	}
	v, ok = afpTCPVolume(afpclient.TCPServer{Name: "Files", Host: "files.local", Port: 10548})
	if !ok || v.ID != "afp://files.local:10548,tcp/" {
		t.Fatalf("non-default port ID = %q", v.ID)
	}
}

func TestResolveLink_afpURITCPUsesServer(t *testing.T) {
	svc := New(nil, nil)
	svc.SetLinkConfig(func() config.InterfaceSection { return bridgeEn0() })
	got, err := svc.resolveLink(KindAFP, "", "", "", uri.Target{Scheme: KindAFP, Server: "192.168.1.9", Transport: clientlink.KindTCP})
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != clientlink.KindTCP || got.Name != "192.168.1.9" {
		t.Fatalf("got %+v, want tcp/192.168.1.9", got)
	}
}

func TestResolveLink_afpURIPcapKeepsDevice(t *testing.T) {
	svc := New(nil, nil)
	svc.SetLinkConfig(func() config.InterfaceSection { return bridgeEn0() })
	got, err := svc.resolveLink(KindAFP, "", "", "", uri.Target{Transport: clientlink.KindPcap})
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != clientlink.KindPcap || got.Name != "en0" {
		t.Fatalf("got %+v, want pcap/en0", got)
	}
}
