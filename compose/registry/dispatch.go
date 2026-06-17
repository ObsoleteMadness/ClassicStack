package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
)

// nicLinkOpener binds a NIC interface name to a per-Start FrameLink opener over the
// injected BuildContext.Opener (pcap at the cmd edge). It is the kind=nic / kind=bridge
// branch of the opener dispatch (M11.c/D6): a NIC-bound transport (EtherTalk, and —
// when their device-link injection lands — IPX/NetBEUI) resolves its effective
// interface and opens it through this. A nil ctx.Opener yields a nil opener, the
// caller's signal to build the inert-but-routed form. iface is the resolved interface
// NAME (the pcap opener takes a name).
func nicLinkOpener(ctx *BuildContext, iface string) func() (link.FrameLink, error) {
	if ctx.Opener == nil {
		return nil
	}
	open := ctx.Opener
	return func() (link.FrameLink, error) { return open(iface) }
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

// effectiveSerialInterface resolves a port instance's effective interface and, when
// it is a serial-kind interface, returns it. It lets a serial transport factory read
// the device/baud from the named interface namespace rather than from the port
// section (the §3b/D7 move: the interface owns the device parameters). ok is false
// when the resolved interface is not serial (the caller then falls back to its own
// transport, e.g. LToUDP's multicast open).
func effectiveSerialInterface(m *config.Model, sec *port.Section) (config.InterfaceSection, bool) {
	iface := m.EffectiveInterfaceFor(sec)
	if iface.EffectiveKind() != config.IfaceKindSerial {
		return config.InterfaceSection{}, false
	}
	return iface, true
}
