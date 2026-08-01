package macip

import (
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/config"
)

// TestSectionInterfaceResolvesToPcapDevice is the regression for the MacIP egress
// silently failing to open: the section's interface NAME ("br-lan") must resolve
// through the [[interface]] namespace to the real pcap device (Npcap's
// "\Device\NPF_{GUID}") — the string libpcap is handed — exactly as every other
// pcap-bound port does. Before the fix the raw name flowed to the egress opener and
// libpcap could not open it, leaving the gateway AppleTalk-only so MacTCP got no
// usable address.
func TestSectionInterfaceResolvesToPcapDevice(t *testing.T) {
	const device = `\Device\NPF_{B7D4E073-2185-4912-BBE8-3948C6636D02}`

	m := config.NewModel()
	m.SetInterface(config.InterfaceSection{
		Name:   "br-lan",
		Kind:   config.IfaceKindBridge,
		Device: device,
	})
	sec := &Section{SKey: SectionKey, Enabled: true, Iface: "br-lan"}

	// This is the resolution the compose registry now performs (reg_macip.go).
	got := m.EffectiveInterfaceFor(sec).PcapDevice()
	if got != device {
		t.Fatalf("PcapDevice = %q, want %q (interface name did not resolve to the pcap device)", got, device)
	}
}

// TestSectionInterfaceProvider proves the section implements the InterfaceProvider
// override so EffectiveInterfaceFor picks up its named interface at all (an empty
// Iface yields no override → the gateway inherits nothing and stays AppleTalk-only).
func TestSectionInterfaceProvider(t *testing.T) {
	if got := (&Section{Iface: "br-lan"}).Interface().Name; got != "br-lan" {
		t.Fatalf("Interface().Name = %q, want %q", got, "br-lan")
	}
	if got := (&Section{}).Interface().Name; got != "" {
		t.Fatalf("empty Iface: Interface().Name = %q, want empty (no override)", got)
	}
}
