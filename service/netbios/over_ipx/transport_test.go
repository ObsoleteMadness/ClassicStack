package over_ipx

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	portipx "github.com/ObsoleteMadness/ClassicStack/port/ipx"
	ipxproto "github.com/ObsoleteMadness/ClassicStack/protocol/ipx"
	netbiosproto "github.com/ObsoleteMadness/ClassicStack/protocol/netbios"
	routeripx "github.com/ObsoleteMadness/ClassicStack/router/ipx"
	ipxsvc "github.com/ObsoleteMadness/ClassicStack/service/ipx"
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

// fakeSAPRegistrar tracks registrations and cancellations.
type fakeSAPRegistrar struct {
	mu       sync.Mutex
	entries  []ipxsvc.SAPEntry
	canceled atomic.Int32
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
	waitForSend(t, port, 1)
	ticks <- time.Now()
	waitForSend(t, port, 2)
	ticks <- time.Now()
	waitForSend(t, port, 3)
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

	// Every broadcast was a type-20 packet to the broadcast node on
	// socket 0x0455.
	port.mu.Lock()
	defer port.mu.Unlock()
	for i, sent := range port.sent {
		if sent.Type != netbiosproto.IPXTypeNetBIOS {
			t.Errorf("send %d: IPX type %d want %d", i, sent.Type, netbiosproto.IPXTypeNetBIOS)
		}
		if sent.DstNode != routeripx.BroadcastNode {
			t.Errorf("send %d: DstNode not broadcast", i)
		}
		if sent.DstSock != NBIPXSessionSocket {
			t.Errorf("send %d: DstSock %x", i, sent.DstSock)
		}
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
	waitForSend(t, port, 1)
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

	waitForSend(t, port, 1)
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
	waitForSend(t, port, 2)
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
