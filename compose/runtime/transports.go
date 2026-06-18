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
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	ipxrouter "github.com/ObsoleteMadness/ClassicStack/core/router/ipx"
	netbeuirouter "github.com/ObsoleteMadness/ClassicStack/core/router/netbeui"
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
func crossWireTransports(comps map[string]component.Component) {
	nb := netbiosService(comps)
	if nb == nil {
		return // no NetBIOS layer → the transports have nothing to carry
	}

	wireNetBEUI(nb, comps)
	wireIPX(nb, comps)

	// Install SMB as the upper-layer session consumer for BOTH families: every
	// circuit the NBF/NBIPX engines bring up routes its reassembled SMB messages
	// here. Done once after both engines are built so a late-built SMB still reaches
	// them (SetSessionConsumer is read live by the engines).
	if sm := smbService(comps); sm != nil {
		nb.SetSessionConsumer(smbSessionBridge{adapter: smb.ConsumerAdapter{Service: sm}})
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

// wireIPX builds the IPX mini-router (when any IPX port was built), attaches every
// IPX port instance, and registers the NBIPX session engine on the NB-IPX session
// socket 0x0455. A build with no IPX port does nothing.
func wireIPX(nb *netbios.Service, comps map[string]component.Component) {
	var ports []ipxrouter.Port
	for _, c := range comps {
		if p, ok := c.(ipxrouter.Port); ok {
			ports = append(ports, p)
		}
	}
	if len(ports) == 0 {
		return
	}

	r := ipxrouter.NewRouter(nil)
	for _, p := range ports {
		r.AddPort(p)
	}

	eng := nb.NewIPXEngine(r)
	_ = r.RegisterSocket(netbios.NBIPXSessionSocket, eng)
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
