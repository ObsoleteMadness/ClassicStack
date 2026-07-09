package runtime

import (
	"context"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	portipx "github.com/ObsoleteMadness/ClassicStack/core/port/ipx"
	portnetbeui "github.com/ObsoleteMadness/ClassicStack/core/port/netbeui"
	ipxproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx"
	nbf "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbeui"
	nbproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbios"
	"github.com/ObsoleteMadness/ClassicStack/core/service/browser"
	"github.com/ObsoleteMadness/ClassicStack/core/service/netbios"
	"github.com/ObsoleteMadness/ClassicStack/core/service/smb"
)

// recordingNetBEUIPort is a test netbeui mini-router Port: it captures the inbound
// delivery callback the cross-wire installs (proving AddPort ran) and records the
// frames the engine sends back through it (proving the registered engine answered).
type recordingNetBEUIPort struct {
	cb        portnetbeui.DeliveryCallback
	sent      []*nbf.Frame
	broadcast []*nbf.Frame
}

func (p *recordingNetBEUIPort) Name() string                { return "test-netbeui" }
func (p *recordingNetBEUIPort) Start(context.Context) error { return nil }
func (p *recordingNetBEUIPort) Stop(context.Context) error  { return nil }
func (p *recordingNetBEUIPort) SetDeliveryCallback(cb portnetbeui.DeliveryCallback) {
	p.cb = cb
}
func (p *recordingNetBEUIPort) Send(_ [6]byte, f *nbf.Frame) error {
	p.sent = append(p.sent, f)
	return nil
}
func (p *recordingNetBEUIPort) SendBroadcast(f *nbf.Frame) error {
	p.broadcast = append(p.broadcast, f)
	return nil
}

// lastSent returns the most recent directed frame of the given command, or nil.
func (p *recordingNetBEUIPort) lastSent(cmd uint8) *nbf.Frame {
	for i := len(p.sent) - 1; i >= 0; i-- {
		if p.sent[i].Command == cmd {
			return p.sent[i]
		}
	}
	return nil
}

// TestCrossWireTransports_NetBEUIToSMB proves the M-ng2 cross-wire stands up the
// NetBEUI mini-router, attaches the port (so the delivery callback is installed),
// registers the NBF session engine for the local name, and routes a session CALL
// through to a NAME_RECOGNIZED reply — i.e. the whole port → mini-router → NBF
// engine → (SMB consumer installed) path is connected by compose alone.
func TestCrossWireTransports_NetBEUIToSMB(t *testing.T) {
	nb := netbios.NewService(nil, "CLASSICSTACK")
	sm := smb.New(nil)
	port := &recordingNetBEUIPort{}
	// Include the browser too, so the mailslot wiring runs alongside the session
	// wiring and a regression in one is caught with the other.
	br := browser.New(nil, nil, "CLASSICSTACK", "WORKGROUP")

	comps := map[string]component.Component{
		netbios.Name: nb,
		smb.Name:     sm,
		"NetBEUI":    port,
		browser.Name: br,
	}

	crossWireTransports(comps, nil)

	// AddPort must have installed the inbound delivery callback on the port.
	if port.cb == nil {
		t.Fatal("cross-wire did not attach the NetBEUI port to the mini-router (no delivery callback)")
	}

	// Drive a CALL (NAME_QUERY) for our file-server name through the port's delivery
	// callback, exactly as an inbound frame would. The engine, registered as the
	// NameHandler for that name, must answer NAME_RECOGNIZED.
	name := nbproto.NewName("CLASSICSTACK", nbproto.NameTypeFileServer)
	clientName := nbproto.NewName("CLIENT", nbproto.NameTypeWorkstation)
	nq := &nbf.Frame{Command: nbf.CmdNameQuery, Data2: 5, RspCorrelator: 0x1234}
	copy(nq.DestinationName[:], name[:])
	copy(nq.SourceName[:], clientName[:])

	peer := [6]byte{0x02, 0, 0, 0, 0, 0x01}
	port.cb(peer, nbf.NetBIOSMulticastMAC, nq)

	if port.lastSent(nbf.CmdNameRecognized) == nil {
		t.Fatal("CALL for our name was not answered with NAME_RECOGNIZED — engine not registered on the mini-router")
	}

	// The mailslot path must be wired too: starting the browser emits a HostAnnounce
	// through its installed sink → the mailslot router → the NetBIOS SendDatagram →
	// the NBF engine's datagram egress → a broadcast UI frame on the port. Observing
	// the broadcast proves browser.SetSink ran AND the engine is registered as the
	// datagram egress, i.e. the whole datagram path is connected by compose.
	before := len(port.broadcast)
	if err := br.Start(context.Background()); err != nil {
		t.Fatalf("browser Start: %v", err)
	}
	defer br.Stop(context.Background())
	if len(port.broadcast) == before {
		t.Fatal("browser HostAnnounce did not reach the wire — mailslot/datagram path not wired")
	}
}

// recordingIPXPort is a test ipxrouter.Port: it captures the inbound delivery
// callback the cross-wire installs (proving AddPort ran) and records sent datagrams.
type recordingIPXPort struct {
	cb   portipx.DeliveryCallback
	sent []*ipxproto.Datagram
}

func (p *recordingIPXPort) Name() string                { return "test-ipx" }
func (p *recordingIPXPort) Start(context.Context) error { return nil }
func (p *recordingIPXPort) Stop(context.Context) error  { return nil }
func (p *recordingIPXPort) SetDeliveryCallback(cb portipx.DeliveryCallback) {
	p.cb = cb
}
func (p *recordingIPXPort) SrcMAC() [6]byte {
	return [6]byte{0x02, 0, 0, 0, 0, 0x02}
}
func (p *recordingIPXPort) Send(_ [6]byte, d *ipxproto.Datagram) error {
	p.sent = append(p.sent, d)
	return nil
}

// TestCrossWireTransports_DirectIPXWithoutNetBIOS proves SMB direct-hosted-over-IPX
// (NWLink direct hosting, socket 0x0550) is wired with NO NetBIOS service present:
// the IPX mini-router is built off the SMB consumer alone, and the IPX port is
// attached (delivery callback installed) so direct-IPX SMB reaches the command core
// in a NetBIOS-free build.
func TestCrossWireTransports_DirectIPXWithoutNetBIOS(t *testing.T) {
	sm := smb.New(nil)
	port := &recordingIPXPort{}

	comps := map[string]component.Component{
		smb.Name: sm,
		"IPX":    port,
	}

	crossWireTransports(comps, nil)

	if port.cb == nil {
		t.Fatal("direct-IPX without NetBIOS did not attach the IPX port (no delivery callback) — the mini-router was not built off the SMB consumer")
	}
}

// TestTransportWiring_AttachPortNetBEUILate proves the dynamic-wiring seam: a NetBEUI
// mini-router is stood up even when NO NetBEUI port existed at build time, and a port
// added LATER (transportWiring.AttachPort — the runtime path for a port added from the
// config-builder UI) is joined to the live, engine-bound router and immediately carries a
// CALL through to a NAME_RECOGNIZED reply. This is the boundary the slice removes: before,
// a runtime-added port stayed dark until a Save+restart rebuilt the stack.
func TestTransportWiring_AttachPortNetBEUILate(t *testing.T) {
	nb := netbios.NewService(nil, "CLASSICSTACK")
	sm := smb.New(nil)

	// No NetBEUI port in the build — only the services. The mini-router must still be
	// built (engine + names registered) so a late port has somewhere to attach.
	comps := map[string]component.Component{
		netbios.Name: nb,
		smb.Name:     sm,
	}
	w := crossWireTransports(comps, nil)
	if w.netbeui == nil {
		t.Fatal("NetBEUI mini-router was not built with zero ports — a late port would have nowhere to attach")
	}

	// Now the operator adds the first NetBEUI port at runtime. AttachPort must join it to
	// the existing router (installing the delivery callback).
	port := &recordingNetBEUIPort{}
	w.AttachPort(port)
	if port.cb == nil {
		t.Fatal("AttachPort did not attach the late NetBEUI port (no delivery callback)")
	}

	// The already-registered engine must answer a CALL for our name on the late port,
	// proving the port carries live traffic without any rebuild.
	name := nbproto.NewName("CLASSICSTACK", nbproto.NameTypeFileServer)
	clientName := nbproto.NewName("CLIENT", nbproto.NameTypeWorkstation)
	nq := &nbf.Frame{Command: nbf.CmdNameQuery, Data2: 5, RspCorrelator: 0x1234}
	copy(nq.DestinationName[:], name[:])
	copy(nq.SourceName[:], clientName[:])
	port.cb([6]byte{0x02, 0, 0, 0, 0, 0x01}, nbf.NetBIOSMulticastMAC, nq)

	if port.lastSent(nbf.CmdNameRecognized) == nil {
		t.Fatal("late-attached port did not carry the CALL to a NAME_RECOGNIZED reply — engine not bound to the retained router")
	}
}

// TestTransportWiring_AttachPortIPXLate is the IPX analogue: the IPX mini-router is built
// off the SMB consumer alone with zero IPX ports, and a port added later via AttachPort is
// joined to it (delivery callback installed), so direct-hosted SMB-over-IPX reaches a
// runtime-added port.
func TestTransportWiring_AttachPortIPXLate(t *testing.T) {
	sm := smb.New(nil)
	comps := map[string]component.Component{smb.Name: sm}

	w := crossWireTransports(comps, nil)
	if w.ipx == nil {
		t.Fatal("IPX mini-router was not built off the SMB consumer with zero ports")
	}

	port := &recordingIPXPort{}
	w.AttachPort(port)
	if port.cb == nil {
		t.Fatal("AttachPort did not attach the late IPX port (no delivery callback)")
	}
}

// TestTransportWiring_AttachPortNoRouters proves AttachPort is a safe no-op when no
// transport was wired (no NetBIOS/SMB consumer): the wiring holds nil mini-routers, so a
// late port is simply left alone rather than attached to a phantom router.
func TestTransportWiring_AttachPortNoRouters(t *testing.T) {
	w := crossWireTransports(map[string]component.Component{}, nil)
	if w.ipx != nil || w.netbeui != nil {
		t.Fatal("mini-routers built with no consumer to drive them")
	}
	// Must not panic and must not attach.
	port := &recordingNetBEUIPort{}
	w.AttachPort(port)
	if port.cb != nil {
		t.Fatal("AttachPort attached a port with no mini-router wired")
	}
	// A nil wiring is also safe (the pre-seam / no-transports build).
	var nilw *transportWiring
	nilw.AttachPort(port)
}

// TestSMBBrowseBridge_Forwards proves the browser→SMB BrowseProvider adapter copies
// the browse list field-for-field and forwards Available, so SMB's IPC$ NetServerEnum2
// answers from the live browser. A freshly built browser is a potential browser
// (Available false) and lists only itself — both observable through the bridge.
func TestSMBBrowseBridge_Forwards(t *testing.T) {
	br := browser.New(nil, nil, "CLASSICSTACK", "WORKGROUP")
	b := smbBrowseBridge{br: br}

	if b.Available() != br.Available() {
		t.Errorf("bridge Available()=%v, browser Available()=%v", b.Available(), br.Available())
	}
	got := b.ServerEntries()
	want := br.ServerEntries()
	if len(got) != len(want) {
		t.Fatalf("bridge ServerEntries len=%d, browser len=%d", len(got), len(want))
	}
	for i := range got {
		if got[i].Name != want[i].Name || got[i].Type != want[i].Type || got[i].Comment != want[i].Comment {
			t.Errorf("entry %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestCrossWireTransports_NoNetBIOS is a no-op when the NetBIOS service is absent:
// the transports have nothing to carry, so a NetBEUI port is left unattached rather
// than wired to a phantom router.
func TestCrossWireTransports_NoNetBIOS(t *testing.T) {
	port := &recordingNetBEUIPort{}
	comps := map[string]component.Component{"NetBEUI": port}

	crossWireTransports(comps, nil)

	if port.cb != nil {
		t.Fatal("cross-wire attached a NetBEUI port with no NetBIOS service to feed")
	}
}
