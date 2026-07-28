package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/adapter/capture/pcapfile"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
)

// nicLinkOpener binds a NIC interface name to a per-Start FrameLink opener over the
// injected BuildContext.Opener (pcap at the cmd edge), programming the transport's own
// BPF filter onto the handle. It is the kind=nic / kind=bridge branch of the opener
// dispatch (M11.c/D6): a NIC-bound transport (EtherTalk, IPX, NetBEUI, EtherDFS) resolves
// its effective interface and opens it through this. A nil ctx.Opener yields a nil opener,
// the caller's signal to build the inert-but-routed form. The pcap opener is handed the
// interface's PcapDevice (Device when set, else Name).
//
// bpf is the caller's per-transport capture filter (each NIC transport owns one; see the
// port packages' BPFFilter const). A promiscuous handle sees ALL NIC traffic, so a shared
// filter is wrong: historically every NIC port opened with the EtherTalk filter, which
// dropped every NBF/IPX frame before the NetBEUI/IPX read loops saw it. An empty bpf
// captures everything and demuxes in userland.
//
// sec is the port section, consulted only for its Capture path: every NIC transport's
// frames are Ethernet (DLT_EN10MB), so a configured Section.Capture tees them to a pcap
// file uniformly for EtherTalk/IPX/NetBEUI/EtherDFS. A nil sec (or empty Capture) opens
// undecorated.
func nicLinkOpener(ctx *BuildContext, sec *port.Section, iface config.InterfaceSection, bpf string) func() (link.FrameLink, error) {
	if ctx.Opener == nil {
		return nil
	}
	// Dispatch on the nic link backend (pcap/tap/tun). Only pcap is wired today; an
	// unimplemented backend yields a nil opener (the inert-but-routed form), the same
	// graceful degradation as a missing pcap backend, rather than a hard failure. When
	// tap/tun adapters land they slot in here keyed off EffectiveBackend.
	if iface.EffectiveBackend() != config.IfaceBackendPcap {
		return nil
	}
	open := ctx.Opener
	// pcap/Npcap opens by DEVICE, not friendly name: on Windows the device is the
	// "\Device\NPF_{GUID}" string in iface.Device; on Linux Device is empty and the
	// friendly Name ("eth0") is itself the pcap device. PcapDevice picks the right one.
	device := iface.PcapDevice()
	// "Easy mode" auto-NIC: a NIC port with no interface configured (and no namespace
	// default) resolves to an empty device. When the cmd edge injected a DefaultDevice
	// resolver, fall back to the host's primary (default-route) NIC so a single-NIC
	// server works out of the box. A configured device always wins (this is skipped when
	// non-empty); a resolver error or no resolver leaves device empty → inert-but-routed,
	// the same degradation as before. We announce the auto-picked NIC so it is never a
	// hidden default.
	if device == "" && ctx.DefaultDevice != nil {
		if dev, err := ctx.DefaultDevice(); err == nil && dev != "" {
			device = dev
			ctx.Logger(sec.InstanceName()).Log1(log.Info, "auto-selected primary NIC", log.Str("device", dev))
		}
	}
	base := func() (link.FrameLink, error) { return open(device, bpf) }
	return captureOpener(sec, pcapfile.LinkTypeEthernet, base)
}

// serialLinkOpener binds a serial interface (device path + baud) to a per-Start
// FrameLink opener: it opens the byte stream via the injected BuildContext.Serial
// opener (adapter/serial at the cmd edge) and wraps it with the transport's framer
// (tashtalk.NewStream). This is the kind=serial branch of the opener dispatch
// (M11.c/D6/D7): the device-open is shared and cmd-edge-injected; the framing is the
// transport adapter's. A nil ctx.Serial yields a nil opener (inert form). On an open
// success but framer error the freshly-opened stream is closed so a failed Start
// leaks no handle.
func serialLinkOpener(ctx *BuildContext, iface config.InterfaceSection, framer SerialFramer) func() (link.FrameLink, error) {
	if ctx.Serial == nil {
		return nil
	}
	open := ctx.Serial
	device := iface.Device
	baud := uint(iface.Baud)
	return func() (link.FrameLink, error) {
		s, err := open(device, baud)
		if err != nil {
			return nil, err
		}
		fl, err := framer(s)
		if err != nil {
			_ = s.Close()
			return nil, err
		}
		return fl, nil
	}
}
