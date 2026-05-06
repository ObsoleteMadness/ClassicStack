package over_ipx

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/capture"
	portipx "github.com/ObsoleteMadness/ClassicStack/port/ipx"
	ipxproto "github.com/ObsoleteMadness/ClassicStack/protocol/ipx"
	netbiosproto "github.com/ObsoleteMadness/ClassicStack/protocol/netbios"
	routeripx "github.com/ObsoleteMadness/ClassicStack/router/ipx"
	ipxsvc "github.com/ObsoleteMadness/ClassicStack/service/ipx"
	"github.com/ObsoleteMadness/ClassicStack/service/netbios"
)

// recordingPort captures every Send and exposes the delivery
// callback the router installs.
type recordingPort struct {
	mu   sync.Mutex
	sent []*ipxproto.Datagram
	cb   portipx.DeliveryCallback
}

func (p *recordingPort) Start() error { return nil }
func (p *recordingPort) Stop() error  { return nil }
func (p *recordingPort) Send(d *ipxproto.Datagram) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	cp := *d
	p.sent = append(p.sent, &cp)
	return nil
}
func (p *recordingPort) SetDeliveryCallback(cb portipx.DeliveryCallback) {
	p.mu.Lock()
	p.cb = cb
	p.mu.Unlock()
}
func (p *recordingPort) SetCaptureSink(_ capture.Sink) {}

// fakeSAPRegistrar tracks registrations and cancellations.
type fakeSAPRegistrar struct {
	mu       sync.Mutex
	entries  []ipxsvc.SAPEntry
	canceled atomic.Int32
}

type fakeCommandHandler struct {
	mu        sync.Mutex
	datagrams []*netbiosproto.Datagram
	contexts  []netbios.DatagramContext
}

func (h *fakeCommandHandler) HandleSession(_ *netbiosproto.SessionPacket) error { return nil }
func (h *fakeCommandHandler) HandleDatagram(d *netbiosproto.Datagram) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.datagrams = append(h.datagrams, d)
	return nil
}

func (h *fakeCommandHandler) HandleDatagramContext(d *netbiosproto.Datagram, ctx netbios.DatagramContext) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.datagrams = append(h.datagrams, d)
	h.contexts = append(h.contexts, ctx)
	return nil
}

func (s *fakeSAPRegistrar) Register(entry ipxsvc.SAPEntry) func() {
	s.mu.Lock()
	s.entries = append(s.entries, entry)
	s.mu.Unlock()
	return func() { s.canceled.Add(1) }
}

func (s *fakeSAPRegistrar) Entries() []ipxsvc.SAPEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ipxsvc.SAPEntry, len(s.entries))
	copy(out, s.entries)
	return out
}

func setupTransport(t *testing.T) (routeripx.Router, *recordingPort, *fakeSAPRegistrar) {
	t.Helper()
	r := routeripx.NewRouter()
	r.SetIdentity([4]byte{0xCA, 0xFE, 0xF0, 0x0D}, [6]byte{0x02, 0, 0, 0, 0, 0x42})
	port := &recordingPort{}
	r.AddPort(port)
	return r, port, &fakeSAPRegistrar{}
}

func waitForSend(t *testing.T, port *recordingPort, n int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		port.mu.Lock()
		got := len(port.sent)
		port.mu.Unlock()
		if got >= n {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	port.mu.Lock()
	defer port.mu.Unlock()
	t.Fatalf("waited for %d sends, only got %d", n, len(port.sent))
}

func TestUncontestedNameClaimRegistersWithSAP(t *testing.T) {
	r, port, sap := setupTransport(t)
	name := netbiosproto.NewName("CLASSICSTACK", netbiosproto.NameTypeFileServer)
	tr := NewTransport(r, sap, name).(*transport)
	tr.claimRetries = 3
	ticks := make(chan time.Time, 8)
	tr.sleep = func(d time.Duration) <-chan time.Time { return ticks }

	if err := tr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tr.Stop()

	// First broadcast goes out before any tick; advance the synthetic
	// clock to drive the next two retries to completion.
	waitForSend(t, port, 2)
	ticks <- time.Now()
	waitForSend(t, port, 4)
	ticks <- time.Now()
	waitForSend(t, port, 6)
	ticks <- time.Now() // unblocks loop exit and triggers SAP.Register

	// Wait for the goroutine to publish via SAP.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(sap.Entries()) > 0 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	got := sap.Entries()
	if len(got) != 1 {
		t.Fatalf("SAP entries: got %d want 1", len(got))
	}
	if got[0].ServiceType != ipxsvc.SAPServiceTypeNetBIOS {
		t.Errorf("ServiceType: got %x want %x", got[0].ServiceType, ipxsvc.SAPServiceTypeNetBIOS)
	}
	if got[0].Name != "CLASSICSTACK" {
		t.Errorf("Name: got %q", got[0].Name)
	}
	if got[0].Socket != NBIPXSessionSocket {
		t.Errorf("Socket: got %x want %x", got[0].Socket, NBIPXSessionSocket)
	}

	// Every retry emits two type-20 broadcasts: NBIPX FindName on
	// socket 0x0455 and NMPI ClaimName on socket 0x0551.
	port.mu.Lock()
	defer port.mu.Unlock()
	findCount := 0
	claimCount := 0
	for i, sent := range port.sent {
		if sent.Type != netbiosproto.IPXTypeNetBIOS {
			t.Errorf("send %d: IPX type %d want %d", i, sent.Type, netbiosproto.IPXTypeNetBIOS)
		}
		if sent.DstNode != routeripx.BroadcastNode {
			t.Errorf("send %d: DstNode not broadcast", i)
		}
		switch sent.DstSock {
		case NBIPXSessionSocket:
			findCount++
			pkt, err := netbiosproto.DecodeNameService(sent.Payload)
			if err != nil {
				t.Errorf("send %d: decode payload: %v", i, err)
				continue
			}
			if pkt.DataStreamType != netbiosproto.NBIPXFindName {
				t.Errorf("send %d: stream type %#x want %#x", i, pkt.DataStreamType, netbiosproto.NBIPXFindName)
			}
			if pkt.Name != name {
				t.Errorf("send %d: name %q want %q", i, pkt.Name.String(), name.String())
			}
		case NBIPXNameQuerySocket:
			claimCount++
			if sent.SrcSock != NBIPXServerSocket {
				t.Errorf("send %d: claim src socket %x want %x", i, sent.SrcSock, NBIPXServerSocket)
			}
			p, err := netbiosproto.DecodeNMPIPacket(sent.Payload)
			if err != nil {
				t.Errorf("send %d: decode NMPI payload: %v", i, err)
				continue
			}
			if p.Opcode != netbiosproto.NMPIOpNameClaim {
				t.Errorf("send %d: NMPI opcode %#x want %#x", i, p.Opcode, netbiosproto.NMPIOpNameClaim)
			}
			if p.RequestedName != name || p.SourceName != name {
				t.Errorf("send %d: claim name mismatch", i)
			}
		default:
			t.Errorf("send %d: unexpected destination socket %x", i, sent.DstSock)
		}
	}
	if findCount != 3 {
		t.Fatalf("find-name count: got %d want 3", findCount)
	}
	if claimCount != 3 {
		t.Fatalf("claim-name count: got %d want 3", claimCount)
	}
}

func TestContestedNameClaimAborts(t *testing.T) {
	r, port, sap := setupTransport(t)
	name := netbiosproto.NewName("CLASSICSTACK", netbiosproto.NameTypeFileServer)
	tr := NewTransport(r, sap, name).(*transport)
	tr.claimRetries = 6
	ticks := make(chan time.Time, 8)
	tr.sleep = func(d time.Duration) <-chan time.Time { return ticks }

	if err := tr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tr.Stop()

	// One broadcast goes out; deliver an inbound objection from
	// another node carrying our name.
	waitForSend(t, port, 2)
	body := netbiosproto.EncodeNameService(&netbiosproto.NBIPXNameServicePacket{Name: name})
	tr.HandleDatagram(&ipxproto.Datagram{
		Type:    netbiosproto.IPXTypeNetBIOS,
		SrcNet:  [4]byte{0xCA, 0xFE, 0xF0, 0x0D},
		SrcNode: [6]byte{0xDE, 0xAD, 0xBE, 0xEF, 0xCA, 0xFE}, // not us
		Payload: body,
	})

	// Allow the goroutine to observe the objection and exit. SAP must
	// not have been called.
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if len(sap.Entries()) > 0 {
			t.Fatal("contested claim should not register with SAP")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestSelfBroadcastNotTreatedAsObjection(t *testing.T) {
	r, port, sap := setupTransport(t)
	name := netbiosproto.NewName("CLASSICSTACK", netbiosproto.NameTypeFileServer)
	tr := NewTransport(r, sap, name).(*transport)
	tr.claimRetries = 2
	ticks := make(chan time.Time, 4)
	tr.sleep = func(d time.Duration) <-chan time.Time { return ticks }

	if err := tr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tr.Stop()

	waitForSend(t, port, 2)
	// Loop back our own broadcast: same source net+node as the
	// router's identity. Must be ignored.
	body := netbiosproto.EncodeNameService(&netbiosproto.NBIPXNameServicePacket{Name: name})
	tr.HandleDatagram(&ipxproto.Datagram{
		Type:    netbiosproto.IPXTypeNetBIOS,
		SrcNet:  r.Network(),
		SrcNode: r.Node(),
		Payload: body,
	})
	ticks <- time.Now()
	waitForSend(t, port, 4)
	ticks <- time.Now() // exit loop, register

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(sap.Entries()) == 1 {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("self-loopback aborted the claim; SAP entries=%d", len(sap.Entries()))
}

func TestStopCancelsSAPEntry(t *testing.T) {
	r, port, sap := setupTransport(t)
	name := netbiosproto.NewName("CLASSICSTACK", netbiosproto.NameTypeFileServer)
	tr := NewTransport(r, sap, name).(*transport)
	tr.claimRetries = 1
	ticks := make(chan time.Time, 2)
	tr.sleep = func(d time.Duration) <-chan time.Time { return ticks }

	if err := tr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForSend(t, port, 1)
	ticks <- time.Now() // exit + register

	// Wait for the SAP register to land before Stop.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(sap.Entries()) == 1 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if len(sap.Entries()) != 1 {
		t.Fatal("setup precondition: SAP entry not registered")
	}

	if err := tr.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if sap.canceled.Load() != 1 {
		t.Fatalf("SAP cancel not called: got %d", sap.canceled.Load())
	}
}

func TestEmptyNameSkipsClaim(t *testing.T) {
	r, port, sap := setupTransport(t)
	tr := NewTransport(r, sap, netbiosproto.Name{}).(*transport) // empty name
	if err := tr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tr.Stop()

	time.Sleep(50 * time.Millisecond)
	port.mu.Lock()
	if len(port.sent) != 0 {
		t.Errorf("empty-name transport sent %d packets, want 0", len(port.sent))
	}
	port.mu.Unlock()
	if len(sap.Entries()) != 0 {
		t.Errorf("empty-name transport registered SAP")
	}
}

func TestSendDatagramEncodesDirectedDatagramPEP(t *testing.T) {
	r, port, _ := setupTransport(t)
	tr := NewTransport(r, nil, netbiosproto.Name{}).(*transport)

	dg := &netbiosproto.Datagram{
		Destination: netbiosproto.NewName("WORKGROUP", netbiosproto.NameTypeGroup),
		Source:      netbiosproto.NewName("CLASSICSTACK", netbiosproto.NameTypeFileServer),
		Payload:     []byte("browse"),
	}
	if err := tr.SendDatagram(dg); err != nil {
		t.Fatalf("SendDatagram: %v", err)
	}

	port.mu.Lock()
	defer port.mu.Unlock()
	if len(port.sent) != 1 {
		t.Fatalf("sent count: got %d want 1", len(port.sent))
	}
	sent := port.sent[0]
	if sent.Type != netbiosproto.IPXTypeNetBIOS {
		t.Fatalf("IPX type: got %d want %d", sent.Type, netbiosproto.IPXTypeNetBIOS)
	}
	if sent.DstSock != NBIPXDatagramSocket {
		t.Fatalf("DstSock: got %x want %x", sent.DstSock, NBIPXDatagramSocket)
	}
	if len(sent.Payload) < netbiosproto.NMPIFixedHeaderLen {
		t.Fatalf("payload too short: got %d want >= %d", len(sent.Payload), netbiosproto.NMPIFixedHeaderLen)
	}
	if sent.Payload[32] != netbiosproto.NMPIOpMailslotSend {
		t.Fatalf("opcode: got %#x want %#x", sent.Payload[32], netbiosproto.NMPIOpMailslotSend)
	}
	if sent.Payload[33] != netbiosproto.NMPINameTypeWorkgroup {
		t.Fatalf("name type: got %#x want %#x", sent.Payload[33], netbiosproto.NMPINameTypeWorkgroup)
	}
}

func TestSendDirectedDatagramEncodesUnicastReply(t *testing.T) {
	r, port, _ := setupTransport(t)
	tr := NewTransport(r, nil, netbiosproto.Name{}).(*transport)

	dg := &netbiosproto.Datagram{
		Destination: netbiosproto.NewName("W98CLIENT", netbiosproto.NameTypeWorkstation),
		Source:      netbiosproto.NewName("CLASSICSTACK", netbiosproto.NameTypeFileServer),
		Payload:     []byte("browse"),
	}
	remote := netbios.DatagramEndpoint{
		Network: [4]byte{0, 0, 0, 0},
		Node:    [6]byte{0x08, 0x00, 0x27, 0x14, 0x74, 0x6D},
		Socket:  [2]byte{0x05, 0x53},
	}
	if err := tr.SendDirectedDatagram(dg, remote); err != nil {
		t.Fatalf("SendDirectedDatagram: %v", err)
	}

	port.mu.Lock()
	defer port.mu.Unlock()
	if len(port.sent) != 1 {
		t.Fatalf("sent count: got %d want 1", len(port.sent))
	}
	sent := port.sent[0]
	if sent.DstNet != remote.Network || sent.DstNode != remote.Node || sent.DstSock != remote.Socket {
		t.Fatalf("directed IPX destination mismatch")
	}
	if sent.Payload[33] != netbiosproto.NMPINameTypeMachine {
		t.Fatalf("name type: got %#x want %#x", sent.Payload[33], netbiosproto.NMPINameTypeMachine)
	}
}

func TestHandleDirectedDatagramCallsHandler(t *testing.T) {
	r, _, _ := setupTransport(t)
	tr := NewTransport(r, nil, netbiosproto.Name{}).(*transport)
	h := &fakeCommandHandler{}
	tr.SetCommandHandler(h)

	dg := &netbiosproto.Datagram{
		Destination: netbiosproto.NewName("WORKGROUP", netbiosproto.NameTypeGroup),
		Source:      netbiosproto.NewName("CLASSICSTACK", netbiosproto.NameTypeFileServer),
		Payload:     []byte("host-announcement"),
	}
	body, err := dg.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	tr.HandleDatagram(&ipxproto.Datagram{
		Type:    netbiosproto.IPXTypePEP,
		DstSock: NBIPXDatagramSocket,
		Payload: append([]byte{0x00, netbiosproto.NBIPXDirectedDatagram}, body...),
	})

	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.datagrams) != 1 {
		t.Fatalf("datagrams delivered: got %d want 1", len(h.datagrams))
	}
	if h.datagrams[0].Source != dg.Source || h.datagrams[0].Destination != dg.Destination {
		t.Fatalf("delivered datagram names mismatch")
	}
}

func TestHandleNMPINameQueryRepliesNameFound(t *testing.T) {
	r, port, _ := setupTransport(t)
	name := netbiosproto.NewName("CLASSICSTACK", netbiosproto.NameTypeFileServer)
	tr := NewTransport(r, nil, name).(*transport)

	query := netbiosproto.EncodeNMPIPacket(&netbiosproto.NMPIPacket{
		Opcode:        netbiosproto.NMPIOpNameQuery,
		NameType:      netbiosproto.NMPINameTypeMachine,
		MessageID:     0x0042,
		RequestedName: name,
		SourceName:    netbiosproto.NewName("W98CLIENT", netbiosproto.NameTypeWorkstation),
	})
	tr.HandleDatagram(&ipxproto.Datagram{
		Type:    netbiosproto.IPXTypePEP,
		SrcNet:  [4]byte{0, 0, 0, 0},
		SrcNode: [6]byte{0x08, 0x00, 0x27, 0x14, 0x74, 0x6D},
		SrcSock: [2]byte{0x05, 0x52},
		DstSock: NBIPXNameQuerySocket,
		Payload: query,
	})

	port.mu.Lock()
	defer port.mu.Unlock()
	if len(port.sent) != 1 {
		t.Fatalf("sent count: got %d want 1", len(port.sent))
	}
	resp := port.sent[0]
	if resp.DstSock != [2]byte{0x05, 0x52} {
		t.Fatalf("response dst socket: got %x want 0552", resp.DstSock)
	}
	if resp.SrcSock != NBIPXNameQuerySocket {
		t.Fatalf("response src socket: got %x want %x", resp.SrcSock, NBIPXNameQuerySocket)
	}
	p, err := netbiosproto.DecodeNMPIPacket(resp.Payload)
	if err != nil {
		t.Fatalf("Decode response: %v", err)
	}
	if p.Opcode != netbiosproto.NMPIOpNameFound {
		t.Fatalf("opcode: got %#x want %#x", p.Opcode, netbiosproto.NMPIOpNameFound)
	}
	if p.MessageID != 0x0042 {
		t.Fatalf("message id: got %#x want 0x0042", p.MessageID)
	}
}

func TestHandleNMPIMailslotSendCallsHandler(t *testing.T) {
	r, _, _ := setupTransport(t)
	tr := NewTransport(r, nil, netbiosproto.Name{}).(*transport)
	h := &fakeCommandHandler{}
	tr.SetCommandHandler(h)

	src := netbiosproto.NewName("W98CLIENT", netbiosproto.NameTypeWorkstation)
	dst := netbiosproto.NewName("WORKGROUP", netbiosproto.NameTypeGroup)
	msg := []byte("browser")
	tr.HandleDatagram(&ipxproto.Datagram{
		Type:    netbiosproto.IPXTypeNetBIOS,
		DstSock: NBIPXDatagramSocket,
		Payload: netbiosproto.EncodeNMPIPacket(&netbiosproto.NMPIPacket{
			Opcode:        netbiosproto.NMPIOpMailslotSend,
			NameType:      netbiosproto.NMPINameTypeWorkgroup,
			RequestedName: dst,
			SourceName:    src,
			Payload:       msg,
		}),
	})

	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.datagrams) != 1 {
		t.Fatalf("datagrams delivered: got %d want 1", len(h.datagrams))
	}
	if h.datagrams[0].Source != src || h.datagrams[0].Destination != dst {
		t.Fatalf("delivered datagram names mismatch")
	}
	if string(h.datagrams[0].Payload) != string(msg) {
		t.Fatalf("payload mismatch: got %q want %q", string(h.datagrams[0].Payload), string(msg))
	}
	if len(h.contexts) != 1 {
		t.Fatalf("contexts delivered: got %d want 1", len(h.contexts))
	}
	if h.contexts[0].Remote.Socket != [2]byte{0x00, 0x00} {
		// This synthetic test does not populate IPX source fields.
		t.Fatalf("unexpected remote socket: got %x want 0000", h.contexts[0].Remote.Socket)
	}
}

func TestHandleNMPISelfLoopbackIgnored(t *testing.T) {
	r, _, _ := setupTransport(t)
	tr := NewTransport(r, nil, netbiosproto.Name{}).(*transport)
	h := &fakeCommandHandler{}
	tr.SetCommandHandler(h)

	src := netbiosproto.NewName("CLASSICSTACK", netbiosproto.NameTypeFileServer)
	dst := netbiosproto.NewName("WORKGROUP", netbiosproto.NameTypeGroup)
	tr.HandleDatagram(&ipxproto.Datagram{
		Type:    netbiosproto.IPXTypeNetBIOS,
		SrcNet:  r.Network(),
		SrcNode: r.Node(),
		SrcSock: NBIPXDatagramSocket,
		DstNet:  r.Network(),
		DstNode: routeripx.BroadcastNode,
		DstSock: NBIPXDatagramSocket,
		Payload: netbiosproto.EncodeNMPIPacket(&netbiosproto.NMPIPacket{
			Opcode:        netbiosproto.NMPIOpMailslotSend,
			NameType:      netbiosproto.NMPINameTypeWorkgroup,
			RequestedName: dst,
			SourceName:    src,
			Payload:       []byte("election"),
		}),
	})

	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.datagrams) != 0 {
		t.Fatalf("self-looped datagram should be ignored; got %d delivered", len(h.datagrams))
	}
}
