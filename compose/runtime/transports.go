package runtime

// transports.go is the M-ng2 transport↔service cross-wire: it stands up the IPX
// and NetBEUI NetBIOS-transport mini-routers and threads the data path
// port → mini-router → NetBIOS session engine → SMB through the small seams each
// side already exposes. It is the NetBIOS-transport analogue of crossWireRouter
// (which wires the AppleTalk DDP router): the AppleTalk router is an operator-
// configured component with its own lifecycle, but the IPX/NetBEUI mini-routers
// are internal dispatch objects with NO lifecycle of their own — the ports they
// ride own start/stop — so they are built HERE during cross-wiring rather than
// registered as components (§3: IPX/NetBEUI are PEERS of the AppleTalk router,
// not members; each has its own address space and inbound dispatch).
//
// The wiring per transport family (only run when BOTH the NetBIOS service and at
// least one port of that family were built):
//
//   - NetBEUI (NBF over 802.2 LLC): build a core/router/netbeui mini-router, AddPort
//     every built NetBEUI port instance (installs the inbound delivery callback),
//     build the NetBIOS NBF session engine (NewNBFEngine bound to the router as its
//     FrameSender), and register that engine on the router as the SessionHandler,
//     the Broadcast handler, and the NameHandler for every local NetBIOS name.
//   - IPX (NB-IPX / NWLink over IPX type 4): build a core/router/ipx mini-router,
//     AddPort every built IPX port instance, build the NBIPX session engine
//     (NewIPXEngine bound to the router as its DatagramSender), and register it on
//     the NB-IPX session socket 0x0455.
//
// Finally, when an SMB service was built, install it as the NetBIOS upper-layer
// SessionConsumer so every reassembled SMB message reaches the command engine —
// bridging smb.SessionConsumer to netbios.SessionConsumer (two structurally
// identical but distinct interfaces, so neither package imports the other).

import (
	"github.com/ObsoleteMadness/ClassicStack/adapter/smbtcp"
	mailslotwire "github.com/ObsoleteMadness/ClassicStack/core/protocol/mailslot"
	ipxrouter "github.com/ObsoleteMadness/ClassicStack/core/router/ipx"
	netbeuirouter "github.com/ObsoleteMadness/ClassicStack/core/router/netbeui"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	diagproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx/diag"
	"github.com/ObsoleteMadness/ClassicStack/core/service/browser"
	"github.com/ObsoleteMadness/ClassicStack/core/service/ipxdiag"
	"github.com/ObsoleteMadness/ClassicStack/core/service/ipxgw"
	"github.com/ObsoleteMadness/ClassicStack/core/service/macip"
	"github.com/ObsoleteMadness/ClassicStack/core/service/mailslot"
	"github.com/ObsoleteMadness/ClassicStack/core/service/messenger"
	"github.com/ObsoleteMadness/ClassicStack/core/service/nbp"
	"github.com/ObsoleteMadness/ClassicStack/core/service/netbios"
	"github.com/ObsoleteMadness/ClassicStack/core/service/smb"
)

// crossWireTransports stands up the NetBIOS-transport mini-routers and wires the
// IPX/NetBEUI ports through the NetBIOS session engines to SMB. It is best-effort by
// type assertion: a component that is not a NetBIOS port, the NetBIOS service, or
// the SMB service is left alone. With no NetBIOS service in the build it does
// nothing (the transports have nothing to feed); with NetBIOS but no SMB the session
// engines run but drop session data after reassembly (no consumer), exactly the
// graceful-degradation contract the seams already document.
func crossWireTransports(comps map[string]component.Component, m *config.Model) {
	nb := netbiosService(comps)
	sm := smbService(comps)

	// Explicit transport bindings (§smb-transport-families / netbios-transport-bindings):
	// the operator's SMB/NetBIOS config names which transport families to bind. An empty
	// list binds every built transport (Binds returns true), so an unset section keeps
	// the historical implicit behaviour. A family is wired only when the relevant service
	// AND its config both allow it.
	smbSec := smb.ServerSectionFromModel(m)
	nbSec := netbios.SectionFromModel(m)

	// The NetBEUI family and the connectionless-datagram (mailslot) path are
	// NetBIOS-only: with no NetBIOS service there is nothing to carry them. NetBEUI is
	// gated by the NetBIOS transport binding (NBF rides NetBEUI).
	if nb != nil {
		if nbSec.Binds(netbios.TransportNetBEUI) {
			wireNetBEUI(nb, comps)
		}
		wireMailslot(nb, comps)
		// Install SMB as the upper-layer session consumer: every circuit the NBF/NBIPX
		// engines bring up routes its reassembled SMB messages here. Done once so a
		// late-built SMB still reaches the engines (SetSessionConsumer is read live).
		if sm != nil {
			nb.SetSessionConsumer(smbSessionBridge{adapter: smb.ConsumerAdapter{Service: sm}})
		}
	}

	// The IPX family carries TWO independent transports off one mini-router: NB-IPX
	// session traffic (socket 0x0455, needs NetBIOS) and SMB direct-hosted-over-IPX
	// (socket 0x0550, needs only SMB — NetBIOS-less, the "NWLink direct hosting"
	// path). Each leg is gated by the consumer's own transport binding: the NB-IPX leg
	// by the NetBIOS ipx binding, the direct-hosted leg by the SMB ipx binding.
	wireIPX(nb, sm, comps, nbSec.Binds(netbios.TransportIPX), smbSec.Binds(smb.TransportIPX))

	// The TCP family (direct-hosted SMB over :445; NBT over :139) is a supervised
	// adapter listener built inert in the registry; wire its SMB consumer + address
	// here when SMB is present and the tcp binding is on. Direct-TCP needs only SMB
	// (NetBIOS-less); NBT (gated by the SMB nbt binding) shares the same framing.
	wireSMBTCP(sm, smbSec, comps)

	// Browse-list provider (§3-ter, M8a compose wiring): when both SMB and the browser
	// were built, install the browser as SMB's BrowseProvider so the IPC$ \PIPE\LANMAN
	// NetServerEnum2 RAP call answers from the live browse list. This is independent of
	// NetBIOS — SMB serves NetServerEnum2 over ANY transport, including direct-TCP :445.
	// smb.BrowseServer mirrors browser.ServerEntry, so the bridge is a field-for-field
	// copy and neither package imports the other.
	if sm != nil {
		if br := browserService(comps); br != nil {
			sm.SetBrowseProvider(smbBrowseBridge{br: br})
		}
	}

	// MacIP gateway: inject the NBP name-information service so it can register its
	// IPGATEWAY name (Macs discover the gateway via an NBP lookup). The registry builds
	// MacIP before it can reach the NBP component, so the registration is wired here —
	// the DDP-service analogue of installing SMB as the NetBIOS session consumer. The
	// IP-side egress adapter, when one exists, is injected the same way; until then
	// MacIP runs AppleTalk-only (assignment + discovery work, IP data does not).
	wireMacIP(comps)
}

// wireMacIP injects the NBP service into the AppleTalk gateway services (MacIP's
// IPGATEWAY name, IPXGW's "IPX Gateway" names) when NBP was built. The IPX mini-router
// is handed to IPXGW separately in wireIPX (which owns that router). A no-op for a
// gateway that was not built.
func wireMacIP(comps map[string]component.Component) {
	names := nbpService(comps)
	if names == nil {
		return
	}
	if mi := macipService(comps); mi != nil {
		mi.SetNBP(names)
	}
	if gw := ipxgwService(comps); gw != nil {
		gw.SetNBP(names)
	}
}

// wireNetBEUI builds the NetBEUI mini-router (when any NetBEUI port was built),
// attaches every NetBEUI port instance to it, and registers the NetBIOS NBF session
// engine as the router's session/broadcast/name handlers. A build with no NetBEUI
// port does nothing.
func wireNetBEUI(nb *netbios.Service, comps map[string]component.Component) {
	var ports []netbeuirouter.Port
	for _, c := range comps {
		if p, ok := c.(netbeuirouter.Port); ok {
			ports = append(ports, p)
		}
	}
	if len(ports) == 0 {
		return
	}

	r := netbeuirouter.NewRouter(nil)
	for _, p := range ports {
		r.AddPort(p)
	}

	eng := nb.NewNBFEngine(r)
	// The NBF engine is the router's session-command handler (SESSION_*/DATA_*),
	// its broadcast handler (name-claim / group datagrams addressed to no single
	// name), and the per-name handler for every local NetBIOS name (the
	// session-establishment CALL is a non-session frame addressed to our name).
	_ = r.RegisterSession(eng)
	_ = r.RegisterBroadcast(eng)
	for _, n := range nb.LocalNames() {
		_ = r.RegisterName([16]byte(n), eng)
	}
}

// wireIPX builds the IPX mini-router (when any IPX port was built AND at least one
// IPX consumer exists), attaches every IPX port instance, and registers the two
// independent IPX session transports on their sockets:
//
//   - NB-IPX (NetBIOS-over-IPX / NWLink) session traffic on socket 0x0455, when the
//     NetBIOS service is present (nb != nil).
//   - SMB direct-hosted-over-IPX (NWLink direct hosting, NetBIOS-LESS) on socket
//     0x0550, when the SMB service is present (sm != nil) — this path needs no
//     NetBIOS layer, so it is wired even in a NetBIOS-free build.
//
// With no IPX port, or with neither consumer, it does nothing (no router is built).
// nbIPXBound / smbIPXBound gate the two IPX legs by the operator's transport bindings:
// the NB-IPX session leg by NetBIOS's ipx binding, the direct-hosted-SMB leg by SMB's
// ipx binding. A leg whose service is present but whose binding is off is not wired.
func wireIPX(nb *netbios.Service, sm *smb.Service, comps map[string]component.Component, nbIPXBound, smbIPXBound bool) {
	// Resolve the effective consumers after applying bindings: a service whose ipx
	// binding is off contributes nothing to the IPX mini-router.
	if nb != nil && !nbIPXBound {
		nb = nil
	}
	if sm != nil && !smbIPXBound {
		sm = nil
	}

	var ports []ipxrouter.Port
	for _, c := range comps {
		if p, ok := c.(ipxrouter.Port); ok {
			ports = append(ports, p)
		}
	}
	if len(ports) == 0 || (nb == nil && sm == nil) {
		return
	}

	r := ipxrouter.NewRouter(nil)
	for _, p := range ports {
		r.AddPort(p)
	}

	if nb != nil {
		eng := nb.NewIPXEngine(r)
		_ = r.RegisterSocket(netbios.NBIPXSessionSocket, eng)
	}
	if sm != nil {
		direct := sm.NewDirectIPX(r)
		_ = r.RegisterSocket(smb.DirectSMBSocket, direct)
	}
	// The IPX gateway (MacIPX) forwards encapsulated IPX from MacIPX clients onto this
	// same mini-router (and routes native IPX replies back over DDP). It is an AppleTalk
	// DDP service, so its component lives under the router cross-wire; here we just hand
	// it the mini-router. Without an IPX port it stays log-only (no router to forward to).
	if gw := ipxgwService(comps); gw != nil {
		gw.SetIPXRouter(r)
	}
	// The IPX Diagnostic Responder (IPXPING reachability, socket 0x0456) rides the
	// same mini-router but needs neither NetBIOS nor SMB — it answers any station
	// probing the segment. Wire it whenever the responder component was built and an
	// IPX port exists: hand it the router as its reply egress and the router's node
	// for the self-exclusion check, then register it on the diagnostic socket.
	if rd := ipxDiagResponder(comps); rd != nil {
		rd.SetSender(r)
		rd.SetNode(r.Node())
		_ = r.RegisterSocket(diagproto.Socket, rd)
	}
}

// wireSMBTCP injects the SMB session consumer and listen address into the built
// SMB-over-TCP transport when SMB is present and the tcp binding is on. The transport
// is registered inert (no consumer); this is the analogue of installing SMB as the
// NetBIOS session consumer, for the direct-TCP path. NBT (:139) shares the same
// transport and framing; when only the nbt binding is on, the :139 address is used.
// With no SMB service, or with both tcp+nbt bindings off, the transport stays inert.
func wireSMBTCP(sm *smb.Service, smbSec *smb.ServerSection, comps map[string]component.Component) {
	if sm == nil {
		return
	}
	c, ok := comps[smbtcp.Name]
	if !ok {
		return // transport not built (smb tag without the adapter, or a minimal build)
	}
	tr, ok := c.(*smbtcp.Transport)
	if !ok {
		return
	}

	tcpOn := smbSec.Binds(smb.TransportTCP)
	nbtOn := smbSec.Binds(smb.TransportNBT)
	if !tcpOn && !nbtOn {
		return // neither TCP transport requested
	}
	// Bind ONLY an explicitly configured address — never an implicit :445/:139, which
	// Windows' native lanmanserver already owns and Unix guards as privileged. Prefer
	// the direct-TCP address; use the NBT address when only nbt is bound. An empty
	// address (the default) leaves the transport inert, so a config that lists the tcp
	// binding but sets no tcp_addr does not collide with the OS SMB server.
	addr := smbSec.DirectTCPAddr()
	if addr == "" && nbtOn {
		addr = smbSec.NBTListenAddr()
	}
	if addr == "" {
		return // transport requested but no address configured — stay inert
	}
	tr.SetConsumer(smb.ConsumerAdapter{Service: sm})
	tr.SetAddr(addr)
}

// ipxDiagResponder returns the built IPX Diagnostic Responder, or nil when none was
// built (the ipxdiag build tag absent).
func ipxDiagResponder(comps map[string]component.Component) *ipxdiag.Responder {
	if c, ok := comps[ipxdiag.Name]; ok {
		if rd, ok := c.(*ipxdiag.Responder); ok {
			return rd
		}
	}
	return nil
}

// wireMailslot stands up the NetBIOS connectionless-datagram path (§3-quater) when
// a datagram consumer (browser/messenger) was built: build the mailslot dispatch
// router over the NetBIOS service's SendDatagram egress, install it as the NetBIOS
// DatagramConsumer (the inbound seam), and register each built consumer on it for
// its mailslot name with the router as its outbound sink. With neither the browser
// nor the messenger built it does nothing (no consumer to route datagrams to), so
// the NetBIOS service drops connectionless datagrams after decode — the documented
// optional-consumer contract.
func wireMailslot(nb *netbios.Service, comps map[string]component.Component) {
	br := browserService(comps)
	ms := messengerService(comps)
	if br == nil && ms == nil {
		return
	}

	// The mailslot router is an internal dispatch object with no lifecycle of its
	// own (like the mini-routers) — it is built here, not supervised. It sends
	// through the NetBIOS service and is the NetBIOS DatagramConsumer for inbound.
	r := mailslot.NewRouter(nb)
	nb.SetDatagramConsumer(r)

	if br != nil {
		br.SetSink(r)
		r.Register(mailslotwire.NameBrowse, br)
	}
	if ms != nil {
		ms.SetSink(r)
		r.Register(mailslotwire.NameMessenger, ms)
	}
}

// browserService returns the built browser service, or nil when none was built.
func browserService(comps map[string]component.Component) *browser.Service {
	if c, ok := comps[browser.Name]; ok {
		if s, ok := c.(*browser.Service); ok {
			return s
		}
	}
	return nil
}

// messengerService returns the built messenger service, or nil when none was built.
func messengerService(comps map[string]component.Component) *messenger.Service {
	if c, ok := comps[messenger.Name]; ok {
		if s, ok := c.(*messenger.Service); ok {
			return s
		}
	}
	return nil
}

// netbiosService returns the built NetBIOS service, or nil when none was built.
func netbiosService(comps map[string]component.Component) *netbios.Service {
	if c, ok := comps[netbios.Name]; ok {
		if s, ok := c.(*netbios.Service); ok {
			return s
		}
	}
	return nil
}

// smbService returns the built SMB service, or nil when none was built.
func smbService(comps map[string]component.Component) *smb.Service {
	if c, ok := comps[smb.Name]; ok {
		if s, ok := c.(*smb.Service); ok {
			return s
		}
	}
	return nil
}

// macipService returns the built MacIP gateway, or nil when none was built.
func macipService(comps map[string]component.Component) *macip.Service {
	if c, ok := comps[macip.Name]; ok {
		if s, ok := c.(*macip.Service); ok {
			return s
		}
	}
	return nil
}

// nbpService returns the built NBP name-information service, or nil when none was built.
func nbpService(comps map[string]component.Component) *nbp.Service {
	if c, ok := comps[nbp.Name]; ok {
		if s, ok := c.(*nbp.Service); ok {
			return s
		}
	}
	return nil
}

// ipxgwService returns the built IPX gateway, or nil when none was built.
func ipxgwService(comps map[string]component.Component) *ipxgw.Service {
	if c, ok := comps[ipxgw.Name]; ok {
		if s, ok := c.(*ipxgw.Service); ok {
			return s
		}
	}
	return nil
}

// smbSessionBridge adapts an smb.SessionConsumer to a netbios.SessionConsumer. The
// two interfaces are structurally identical (NewConn returning a circuit that
// serves a message, accepts a push writer, and closes) but DISTINCT types in
// distinct packages, so neither imports the other — compose is the single place
// that knows both, so the bridge lives here. Each NewConn opens an SMB circuit and
// re-wraps it behind the netbios.SessionCircuit interface.
type smbSessionBridge struct{ adapter smb.SessionConsumer }

// NewConn opens an SMB circuit and presents it as a netbios.SessionCircuit.
func (b smbSessionBridge) NewConn() netbios.SessionCircuit {
	return smbCircuitBridge{c: b.adapter.NewConn()}
}

// smbCircuitBridge re-types an smb.SessionCircuit as a netbios.SessionCircuit. The
// method sets are identical, so it is a pure forwarding shim.
type smbCircuitBridge struct{ c smb.SessionCircuit }

func (b smbCircuitBridge) ServeMessage(req []byte) []byte { return b.c.ServeMessage(req) }
func (b smbCircuitBridge) SetPushWriter(w func([]byte))   { b.c.SetPushWriter(w) }
func (b smbCircuitBridge) Close()                         { b.c.Close() }

// compile-time assertion: the bridge satisfies the NetBIOS upper-layer seam.
var _ netbios.SessionConsumer = smbSessionBridge{}

// smbBrowseBridge adapts the browser service to SMB's BrowseProvider seam (§3-ter):
// SMB's IPC$ NetServerEnum2 reads the browse list through Available + ServerEntries,
// and smb.BrowseServer mirrors browser.ServerEntry field-for-field, so this is a
// pure copy with no package coupling (neither imports the other). Compose owns the
// shim, exactly like smbSessionBridge.
type smbBrowseBridge struct{ br *browser.Service }

// Available reports whether the browser can serve a list (false → a potential
// browser, which SMB answers with ERROR_REQ_NOT_ACCEP).
func (b smbBrowseBridge) Available() bool { return b.br.Available() }

// ServerEntries copies the browser's browse list into SMB's BrowseServer rows.
func (b smbBrowseBridge) ServerEntries() []smb.BrowseServer {
	in := b.br.ServerEntries()
	out := make([]smb.BrowseServer, len(in))
	for i, e := range in {
		out[i] = smb.BrowseServer{Name: e.Name, Type: e.Type, Comment: e.Comment}
	}
	return out
}

// compile-time assertion: the bridge satisfies SMB's browse-list seam.
var _ smb.BrowseProvider = smbBrowseBridge{}
