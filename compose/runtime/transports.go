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
	"context"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/adapter/smbtcp"
	mailslotwire "github.com/ObsoleteMadness/ClassicStack/core/protocol/mailslot"
	ipxrouter "github.com/ObsoleteMadness/ClassicStack/core/router/ipx"
	netbeuirouter "github.com/ObsoleteMadness/ClassicStack/core/router/netbeui"

	"github.com/ObsoleteMadness/ClassicStack/core/auth"
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	diagproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx/diag"
	ncpproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ncp"
	protocol "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbios"
	ripproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/rip"
	"github.com/ObsoleteMadness/ClassicStack/core/service/afp"
	"github.com/ObsoleteMadness/ClassicStack/core/service/browser"
	"github.com/ObsoleteMadness/ClassicStack/core/service/ipxdiag"
	"github.com/ObsoleteMadness/ClassicStack/core/service/ipxgw"
	"github.com/ObsoleteMadness/ClassicStack/core/service/macip"
	"github.com/ObsoleteMadness/ClassicStack/core/service/mailslot"
	"github.com/ObsoleteMadness/ClassicStack/core/service/messenger"
	"github.com/ObsoleteMadness/ClassicStack/core/service/nbp"
	"github.com/ObsoleteMadness/ClassicStack/core/service/ncp"
	"github.com/ObsoleteMadness/ClassicStack/core/service/netbios"
	"github.com/ObsoleteMadness/ClassicStack/core/service/netboot"
	"github.com/ObsoleteMadness/ClassicStack/core/service/rip"
	"github.com/ObsoleteMadness/ClassicStack/core/service/sap"
	"github.com/ObsoleteMadness/ClassicStack/core/service/smb"
)

// transportWiring is the retained result of crossWireTransports: the IPX/NetBEUI
// mini-routers (each nil when its family was not wired) plus the MacIP IP-side egress.
// The runtime keeps it so a port instance ADDED AT RUNTIME (via the config-builder UI,
// supervisor.AddInstance) can be attached to the already-running mini-router — the
// mini-routers have no lifecycle of their own and are built once here, so without
// retaining them a late port would come up supervised but never carry NBF/NBIPX
// traffic until a Save+restart rebuilt the stack. AttachPort is the seam the supervisor
// calls after it builds+starts the new port node (§M11 dynamic transport wiring).
type transportWiring struct {
	ipx     *ipxrouter.Router     // IPX mini-router (nil when the IPX family was not wired)
	netbeui *netbeuirouter.Router // NetBEUI mini-router (nil when the NetBEUI family was not wired)
	egress  MacIPEgress           // MacIP IP-side egress (nil when AppleTalk-only)
}

// AttachPort attaches a newly-built port component to whichever mini-router carries its
// family, so a port added at runtime immediately joins the live NBF/NBIPX dispatch (the
// engines + SMB consumer were registered once at build). It is best-effort by type
// assertion, mirroring wireIPX/wireNetBEUI: a component that is neither an IPX nor a
// NetBEUI port (or a family whose router was not wired — the service absent or its
// binding off) is left alone. Safe to call with a nil receiver (a build that wired no
// transports). A port may satisfy BOTH interfaces only in principle; in practice each
// port type rides one family, and AddPort on the non-matching router simply never fires.
func (w *transportWiring) AttachPort(c component.Component) {
	if w == nil || c == nil {
		return
	}
	if w.ipx != nil {
		if p, ok := c.(ipxrouter.Port); ok {
			w.ipx.AddPort(p)
		}
	}
	if w.netbeui != nil {
		if p, ok := c.(netbeuirouter.Port); ok {
			w.netbeui.AddPort(p)
		}
	}
}

// crossWireTransports stands up the NetBIOS-transport mini-routers and wires the
// IPX/NetBEUI ports through the NetBIOS session engines to SMB. It is best-effort by
// type assertion: a component that is not a NetBIOS port, the NetBIOS service, or
// the SMB service is left alone. With no NetBIOS service in the build it does
// nothing (the transports have nothing to feed); with NetBIOS but no SMB the session
// engines run but drop session data after reassembly (no consumer), exactly the
// graceful-degradation contract the seams already document.
//
// It returns a transportWiring retaining the built mini-routers so the runtime can
// attach ports added later at runtime (AttachPort). The mini-routers are built whenever
// their consuming service exists (even with ZERO ports at startup), so the first port of
// a family added from the config-builder UI has a live router to join.
func crossWireTransports(comps map[string]component.Component, egressOpener MacIPEgressOpener, mkLogger func(scope string) log.Logger) *transportWiring {
	nb := netbiosService(comps)
	sm := smbService(comps)
	w := &transportWiring{}

	// Explicit transport bindings (§smb-transport-families / netbios-transport-bindings):
	// which transport families each service wants bound is the SERVICE's own intent — the
	// SMB/NetBIOS services hold their config (component.TransportBinder), so we ask THEM
	// (sm.Binds / nb.Binds) instead of re-reading the model here (§B). An empty binding
	// list binds every built transport (Binds returns true), so an unset section keeps the
	// historical implicit behaviour. A family is wired only when the relevant service AND
	// its own binding allow it.

	// The NetBEUI family and the connectionless-datagram (mailslot) path are
	// NetBIOS-only: with no NetBIOS service there is nothing to carry them. NetBEUI is
	// gated by the NetBIOS transport binding (NBF rides NetBEUI).
	if nb != nil {
		if nb.Binds(netbios.TransportNetBEUI) {
			w.netbeui = wireNetBEUI(nb, comps)
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
	nbIPXBound := nb != nil && nb.Binds(netbios.TransportIPX)
	smbIPXBound := sm != nil && sm.Binds(smb.TransportIPX)
	w.ipx = wireIPX(nb, sm, comps, nbIPXBound, smbIPXBound, mkLogger)

	// The TCP family (direct-hosted SMB over :445; NBT over :139) is a supervised
	// adapter listener built inert in the registry; wire its SMB consumer + address
	// here when SMB is present and the tcp binding is on. Direct-TCP needs only SMB
	// (NetBIOS-less); NBT (gated by the SMB nbt binding) shares the same framing.
	wireSMBTCP(sm, nb, comps)

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
	w.egress = wireMacIP(comps, egressOpener)
	return w
}

// wireMacIP injects the NBP service into the AppleTalk gateway services (MacIP's
// IPGATEWAY name, IPXGW's "IPX Gateway" names) when NBP was built, and — when the MacIP
// service DECLARES it wants IP egress (EgressParams, §B) and an egress opener was
// supplied — builds the IP-side egress adapter and injects it via SetEgress, returning
// it so the runtime can manage its lifecycle. The IPX mini-router is handed to IPXGW
// separately in wireIPX (which owns that router). Returns nil egress when none was built.
func wireMacIP(comps map[string]component.Component, egressOpener MacIPEgressOpener) MacIPEgress {
	names := nbpService(comps)
	mi := macipService(comps)
	if names != nil {
		if mi != nil {
			mi.SetNBP(names)
		}
		if gw := ipxgwService(comps); gw != nil {
			gw.SetNBP(names)
		}
		// AFP advertises its server name (serverName:AFPServer@zone) via NBP so it appears
		// in the Chooser; without this wiring the file server is reachable by address but
		// invisible to name discovery — the "zone shows but no server" symptom.
		if af := afpService(comps); af != nil {
			af.SetNBP(names)
		}
		// Netboot advertises its any-object BootServer name via NBP; booting ROMs
		// look up their PRAM serverNum against type "BootServer" before speaking ABP.
		if nb := netbootService(comps); nb != nil {
			nb.SetNBP(names)
		}
	}

	// Build + inject the IP-side egress when the MacIP SERVICE declares an egress intent
	// (a configured interface) and the cmd edge supplied an opener (the pcap/cgo
	// dependency lives there). The service holds its own egress params, so the root asks
	// it (EgressParams) rather than re-reading the section. A nil opener, no MacIP
	// service, or no declared egress keeps MacIP AppleTalk-only. An open error is logged
	// via the opener; here we just leave egress unwired.
	if mi == nil || egressOpener == nil {
		return nil
	}
	params, ok := mi.EgressParams()
	if !ok {
		return nil
	}
	eg, err := egressOpener(params, mi.OwnsIP)
	if err != nil || eg == nil {
		return nil
	}
	mi.SetEgress(eg)
	return eg
}

// wireAuthenticator installs the shared user store as the login Authenticator on every
// built file service (AFP, SMB). The store is an auth.UserStore, whose method set is a
// superset of each service's local Authenticator interface (just Authenticate), so the
// interface value is assignable directly — no per-package import of core/auth here. A
// service not built is simply absent from comps and skipped. With a nil store the caller
// does not invoke this (services stay guest-only).
func wireAuthenticator(comps map[string]component.Component, store auth.Authenticator) {
	if af := afpService(comps); af != nil {
		af.SetAuthenticator(store)
	}
	if sm := smbService(comps); sm != nil {
		sm.SetAuthenticator(store)
	}
	if nc := ncpService(comps); nc != nil {
		nc.SetAuthenticator(store)
	}
}

// ncpService returns the built NCP service, or nil when none was built.
func ncpService(comps map[string]component.Component) *ncp.Service {
	if c, ok := comps[ncp.Name]; ok {
		if s, ok := c.(*ncp.Service); ok {
			return s
		}
	}
	return nil
}

// afpService returns the built AFP service, or nil when none was built.
func afpService(comps map[string]component.Component) *afp.Service {
	if c, ok := comps[afp.Name]; ok {
		if s, ok := c.(*afp.Service); ok {
			return s
		}
	}
	return nil
}

// wireNetBEUI builds the NetBEUI mini-router, attaches every NetBEUI port instance
// present at build time, and registers the NetBIOS NBF session engine as the router's
// session/broadcast/name handlers. It returns the router so the runtime can attach
// ports added LATER at runtime (transportWiring.AttachPort).
//
// The router is built even when NO NetBEUI port exists yet: a port added from the
// config-builder UI after startup must have a live, engine-bound router to join, and an
// empty router with no ports is harmless (its Send returns "no ports attached" until one
// joins). This mirrors the AppleTalk router, which is likewise built independent of its
// members. The engine + name registrations depend only on the NetBIOS service, not on any
// port, so they are installed once here regardless of the current port count.
func wireNetBEUI(nb *netbios.Service, comps map[string]component.Component) *netbeuirouter.Router {
	r := netbeuirouter.NewRouter(nil)
	for _, c := range comps {
		if p, ok := c.(netbeuirouter.Port); ok {
			r.AddPort(p)
		}
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
	return r
}

// wireIPX builds the IPX mini-router (when at least one IPX consumer exists), attaches
// every IPX port instance present at build time, and registers the two independent IPX
// session transports on their sockets:
//
//   - NB-IPX (NetBIOS-over-IPX / NWLink) session traffic on socket 0x0455, when the
//     NetBIOS service is present (nb != nil).
//   - SMB direct-hosted-over-IPX (NWLink direct hosting, NetBIOS-LESS) on socket
//     0x0550, when the SMB service is present (sm != nil) — this path needs no
//     NetBIOS layer, so it is wired even in a NetBIOS-free build.
//
// It returns the router so the runtime can attach ports added LATER at runtime
// (transportWiring.AttachPort). The router is built whenever ANY consumer wants it
// (NB-IPX, direct-SMB, NCP, the MacIPX gateway, or the diagnostic responder) — even with
// NO IPX port yet, so the first port added from the config-builder UI has a live,
// socket-bound router to join (mirroring the AppleTalk router, built independent of its
// members). With no consumer at all it returns nil (nothing would drive the router).
// nbIPXBound / smbIPXBound gate the two IPX legs by the operator's transport bindings:
// the NB-IPX session leg by NetBIOS's ipx binding, the direct-hosted-SMB leg by SMB's
// ipx binding. A leg whose service is present but whose binding is off is not wired.
func wireIPX(nb *netbios.Service, sm *smb.Service, comps map[string]component.Component, nbIPXBound, smbIPXBound bool, mkLogger func(scope string) log.Logger) *ipxrouter.Router {
	// Resolve the effective consumers after applying bindings: a service whose ipx
	// binding is off contributes nothing to the IPX mini-router.
	if nb != nil && !nbIPXBound {
		nb = nil
	}
	if sm != nil && !smbIPXBound {
		sm = nil
	}
	// NCP is its own file service (not a sub-transport of SMB/NetBIOS), so it has no
	// transport-binding gate: it is wired whenever it was built.
	nc := ncpService(comps)
	gw := ipxgwService(comps)
	rd := ipxDiagResponder(comps)

	// Build the router when SOMETHING will consume IPX. Ports are no longer part of this
	// gate: a zero-port build still builds the router so a runtime-added first port has a
	// live target — the ports are the mini-router's link layer, the consumers are its
	// reason to exist. With no consumer at all there is nothing to wire.
	if nb == nil && sm == nil && nc == nil && gw == nil && rd == nil {
		return nil
	}

	r := ipxrouter.NewRouter(nil)
	var node [6]byte
	for _, c := range comps {
		if p, ok := c.(ipxrouter.Port); ok {
			r.AddPort(p)
			if node == ([6]byte{}) {
				node = p.SrcMAC()
			}
		}
	}
	if node != ([6]byte{}) {
		r.SetIdentity(r.Network(), node)
	}

	// One SHARED SAP advertiser (socket 0x0452) serves every IPX-discoverable service:
	// the router allows a single handler per socket, so NCP and NB-IPX both register
	// their SAP entry through this one advertiser rather than each owning 0x0452. Built
	// lazily on the first registration below; started + socket-registered afterwards.
	var sapAdv *sap.Advertiser
	sapReg := func(e ncpproto.SAPEntry) {
		if sapAdv == nil {
			sapAdv = sap.New(r)
			sapAdv.SetIdentity(r.Network(), r.Node())
			if mkLogger != nil {
				sapAdv.SetLogger(mkLogger(sap.Name))
			}
		}
		sapAdv.Register(e)
	}

	if nb != nil {
		eng := nb.NewIPXEngine(r)
		// The engine serves the session socket (0x0455, SESSION_*/DATA and the IPX
		// type-20 NBIPX Find-name broadcast), the NMPI name-query socket (0x0551, the
		// "where is CLASSICSTACK?" query a Win9x/WfW client broadcasts before opening a
		// session), and the NB-IPX datagram socket (0x0553, the NMPI mailslot sends that
		// carry browser HostAnnounce / AnnouncementRequest / GetBackupList). Without
		// 0x0551 the name query is dropped and the client never finds the server; without
		// 0x0553 the browser never sees the client's browse traffic and ClassicStack does
		// not appear in "net view".
		_ = r.RegisterSocket(netbios.NBIPXSessionSocket, eng)
		_ = r.RegisterSocket(netbios.NBIPXNameQuerySocket, eng)
		_ = r.RegisterSocket(netbios.NBIPXDatagramSocket, eng)
		// 0x0554: the alternative name-service socket some stacks use for name
		// claim/query instead of the session socket's type-20 broadcast. The
		// legacy over_ipx transport claimed all four sockets; register it too so
		// those name-service packets are delivered.
		_ = r.RegisterSocket(netbios.NBIPXNameSocket, eng)
		// Claim our NetBIOS server name on the segment (type-20 Find-name + NMPI
		// ClaimName, 6×500ms), then — if uncontested — advertise it via SAP under the
		// NetBIOS type (0x0640) pointing at the session socket (0x0455), so a
		// SAP-browsing NWLink station discovers us. This mirrors the legacy over_ipx
		// claim-then-advertise. The claim blocks ~3s, so run it off the wiring path.
		for _, n := range nb.LocalNames() {
			if n.Type() != protocol.NameTypeFileServer {
				continue // advertise the <20> file-server identity, one entry
			}
			name := n
			serverName := name.String()
			self := r.Node()
			go func() {
				if err := eng.ClaimName(context.Background(), self, name, 6, 500*time.Millisecond); err != nil {
					return // name in use on the segment — do not advertise it
				}
				sapReg(ncpproto.SAPEntry{
					Type:   ncpproto.SAPServerTypeNetBIOS,
					Name:   serverName,
					Socket: netbios.NBIPXSessionSocket,
					Hops:   1,
				})
			}()
		}
	}
	if sm != nil {
		direct := sm.NewDirectIPX(r)
		_ = r.RegisterSocket(smb.DirectSMBSocket, direct)
	}
	// NCP file service over IPX (socket 0x0451): the transport drives the command
	// engine; its SAP entry (File Server 0x0004 @ 0x0451) is registered with the shared
	// advertiser so NETx/VLM discover it. The transport holds the advertiser handle for
	// its "sap: advertising" dashboard prop and to stop it on teardown.
	//
	// Discovery plumbing (the NetWare client attach sequence, per mars_nwe): the SAP
	// entry advertises the server at its INTERNAL network address (internal-net:
	// 00-00-00-00-00-01:0451), never the wire address — the client then broadcasts a
	// RIP request for that network (GetLocalTarget) and will not open an NCP connection
	// until it is answered, taking the answer's source MAC as the frame address. So the
	// mini-router is given the internal identity (it must accept datagrams addressed to
	// it) and a RIP responder is stood up on socket 0x0453 owning that network.
	if nc != nil {
		internalNet := ipxrouter.DeriveInternalNetwork(r.Node())
		r.SetInternalNetwork(internalNet)

		t := nc.NewOverIPX(r)
		_ = r.RegisterSocket(ncpproto.NCPSocket, t)
		e := nc.SAPEntry()
		e.Network = internalNet
		e.Node = ipxrouter.InternalNode
		sapReg(e)
		t.SetSAP(sapAdv)

		responder := rip.New(r)
		responder.SetNetworks(internalNet)
		_ = r.RegisterSocket(ripproto.Socket, responder)
		responder.Start()
		t.SetRIP(responder)
	}
	// Start the shared advertiser and register it on the SAP socket once any service
	// registered an entry. (The NB-IPX entry may register later, from the async claim
	// goroutine — the advertiser picks it up live.)
	if sapAdv != nil {
		sapAdv.Start()
		_ = r.RegisterSocket(ncpproto.SAPSocket, sapAdv)
	}
	// The IPX gateway (MacIPX) forwards encapsulated IPX from MacIPX clients onto this
	// same mini-router (and routes native IPX replies back over DDP). It is an AppleTalk
	// DDP service, so its component lives under the router cross-wire; here we just hand
	// it the mini-router. Without an IPX port it stays log-only (no router to forward to).
	if gw != nil {
		gw.SetIPXRouter(r)
	}
	// The IPX Diagnostic Responder (IPXPING reachability, socket 0x0456) rides the
	// same mini-router but needs neither NetBIOS nor SMB — it answers any station
	// probing the segment. Wire it whenever the responder component was built: hand it
	// the router as its reply egress and the router's node for the self-exclusion check,
	// then register it on the diagnostic socket.
	if rd != nil {
		rd.SetSender(r)
		rd.SetNode(r.Node())
		_ = r.RegisterSocket(diagproto.Socket, rd)
	}
	return r
}

// wireSMBTCP injects the SMB session consumer and listen address into the built
// SMB-over-TCP transport when SMB is present and the tcp binding is on. The transport
// is registered inert (no consumer); this is the analogue of installing SMB as the
// NetBIOS session consumer, for the direct-TCP path. NBT (:139) shares the same
// transport and framing; when only the nbt binding is on, the :139 address is used.
// With no SMB service, or with both tcp+nbt bindings off, the transport stays inert.
func wireSMBTCP(sm *smb.Service, nb *netbios.Service, comps map[string]component.Component) {
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

	// Ask the SERVICE for its bindings + addresses (§B) — the SMB service holds its own
	// config, so the root does not re-read the section here.
	tcpOn := sm.Binds(smb.TransportTCP)
	nbtOn := sm.Binds(smb.TransportNBT)
	if !tcpOn && !nbtOn {
		return // neither TCP transport requested
	}
	// Bind ONLY an explicitly configured address — never an implicit :445/:139, which
	// Windows' native lanmanserver already owns and Unix guards as privileged. Prefer
	// the direct-TCP address; use the NBT address when only nbt is bound. An empty
	// address (the default) leaves the transport inert, so a config that lists the tcp
	// binding but sets no tcp_addr does not collide with the OS SMB server.
	//
	// NBT (:139) is a NetBIOS transport, so its address is the NetBIOS section's NBTAddr,
	// read from the NetBIOS service (§B). Direct-TCP (:445) is SMB's own.
	addr := sm.DirectTCPListenAddr()
	if addr == "" && nbtOn && nb != nil {
		addr = nb.NBTListenAddr()
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

// netbootService returns the built netboot service, or nil when none was built.
func netbootService(comps map[string]component.Component) *netboot.Service {
	if c, ok := comps[netboot.Name]; ok {
		if s, ok := c.(*netboot.Service); ok {
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

// NewConn opens an SMB circuit for the transport remote-endpoint label client and
// presents it as a netbios.SessionCircuit.
func (b smbSessionBridge) NewConn(client string) netbios.SessionCircuit {
	return smbCircuitBridge{c: b.adapter.NewConn(client)}
}

// smbCircuitBridge re-types an smb.SessionCircuit as a netbios.SessionCircuit. The
// method sets are identical, so it is a pure forwarding shim.
type smbCircuitBridge struct{ c smb.SessionCircuit }

func (b smbCircuitBridge) ServeMessage(req []byte) []byte { return b.c.ServeMessage(req) }
func (b smbCircuitBridge) SetPushWriter(w func([]byte))   { b.c.SetPushWriter(w) }
func (b smbCircuitBridge) Close()                         { b.c.Close() }

// SetNetBIOSName forwards the calling NetBIOS name to the wrapped SMB circuit if it
// accepts one (implements netbios.NetBIOSNamer via *smb.Conn), so the bridge itself
// satisfies netbios.NetBIOSNamer and NBF's type assertion on it succeeds.
func (b smbCircuitBridge) SetNetBIOSName(name string) {
	if namer, ok := b.c.(netbios.NetBIOSNamer); ok {
		namer.SetNetBIOSName(name)
	}
}

// compile-time assertion: the circuit bridge also satisfies the optional
// NetBIOSNamer capability so NBF's type assertion on the wrapped circuit succeeds.
var _ netbios.NetBIOSNamer = smbCircuitBridge{}

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
