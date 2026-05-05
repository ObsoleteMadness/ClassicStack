package ipx

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	ipxproto "github.com/ObsoleteMadness/ClassicStack/protocol/ipx"
	routeripx "github.com/ObsoleteMadness/ClassicStack/router/ipx"
)

func TestSAPRegisterFillsIdentityFromRouter(t *testing.T) {
	r, _ := setupRIPRouter(t) // reuses helpers from rip_test.go
	svc := NewSAPService(r)

	cancel := svc.Register(SAPEntry{
		ServiceType: SAPServiceTypeNetBIOS,
		Name:        "CLASSICSTACK",
		Socket:      [2]byte{0x04, 0x55},
	})
	defer cancel()

	got := svc.Entries()
	if len(got) != 1 {
		t.Fatalf("entries: got %d want 1", len(got))
	}
	if got[0].Network != ([4]byte{0xCA, 0xFE, 0xF0, 0x0D}) {
		t.Errorf("Network: got %x want CAFEF00D", got[0].Network)
	}
	if got[0].Node != ([6]byte{0x02, 0, 0, 0, 0, 0x42}) {
		t.Errorf("Node: got %x", got[0].Node)
	}
	if got[0].Hops != 1 {
		t.Errorf("Hops default: got %d want 1", got[0].Hops)
	}
}

func TestSAPRegisterRespectsExplicitFields(t *testing.T) {
	r, _ := setupRIPRouter(t)
	svc := NewSAPService(r)

	cancel := svc.Register(SAPEntry{
		ServiceType: SAPServiceTypeFileSrv,
		Name:        "REMOTE",
		Network:     [4]byte{0xAA, 0xBB, 0xCC, 0xDD},
		Node:        [6]byte{0x99, 0, 0, 0, 0, 0x99},
		Socket:      [2]byte{0x04, 0x51},
		Hops:        4,
	})
	defer cancel()

	got := svc.Entries()
	if got[0].Network != ([4]byte{0xAA, 0xBB, 0xCC, 0xDD}) {
		t.Errorf("explicit Network was overwritten: %x", got[0].Network)
	}
	if got[0].Hops != 4 {
		t.Errorf("explicit Hops was overwritten: %d", got[0].Hops)
	}
}

func TestSAPCancelRemovesEntry(t *testing.T) {
	r, _ := setupRIPRouter(t)
	svc := NewSAPService(r)
	cancel := svc.Register(SAPEntry{
		ServiceType: SAPServiceTypeNetBIOS, Name: "X", Socket: [2]byte{0x04, 0x55},
	})
	if got := svc.Entries(); len(got) != 1 {
		t.Fatalf("post-register count: %d", len(got))
	}
	cancel()
	if got := svc.Entries(); len(got) != 0 {
		t.Fatalf("post-cancel count: %d", len(got))
	}
}

func TestSAPGeneralQueryByType(t *testing.T) {
	r, port := setupRIPRouter(t)
	svc := NewSAPService(r)
	defer svc.Register(SAPEntry{
		ServiceType: SAPServiceTypeNetBIOS, Name: "CLASSICSTACK", Socket: [2]byte{0x04, 0x55},
	})()
	defer svc.Register(SAPEntry{
		ServiceType: SAPServiceTypeFileSrv, Name: "FILESRV", Socket: [2]byte{0x04, 0x51},
	})()

	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer svc.Stop()

	// Drain the startup broadcast.
	waitForSend(t, port, 1)

	// Query for NetBIOS specifically.
	body, _ := EncodeSAP(&SAPPacket{
		Operation: SAPGeneralQuery, QueryServiceType: SAPServiceTypeNetBIOS,
	})
	svc.HandleDatagram(&ipxproto.Datagram{
		SrcNet:  [4]byte{0xCA, 0xFE, 0xF0, 0x0D},
		SrcNode: [6]byte{0x02, 0, 0, 0, 0, 0x99},
		SrcSock: [2]byte{0x40, 0x00},
		Payload: body,
	})

	waitForSend(t, port, 2)

	port.mu.Lock()
	defer port.mu.Unlock()
	got := port.sent[1]
	if got.DstSock != ([2]byte{0x40, 0x00}) {
		t.Fatalf("response DstSock: got %x want 4000 (requester's source socket)", got.DstSock)
	}
	resp, err := DecodeSAP(got.Payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Operation != SAPGeneralResponse {
		t.Fatalf("operation: %d", resp.Operation)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("entries: got %d want 1", len(resp.Entries))
	}
	if resp.Entries[0].ServiceType != SAPServiceTypeNetBIOS {
		t.Errorf("type: %x", resp.Entries[0].ServiceType)
	}
	if resp.Entries[0].Name != "CLASSICSTACK" {
		t.Errorf("name: %q", resp.Entries[0].Name)
	}
}

func TestSAPWildcardQueryReturnsAll(t *testing.T) {
	r, port := setupRIPRouter(t)
	svc := NewSAPService(r)
	defer svc.Register(SAPEntry{
		ServiceType: SAPServiceTypeNetBIOS, Name: "X", Socket: [2]byte{0x04, 0x55},
	})()
	defer svc.Register(SAPEntry{
		ServiceType: SAPServiceTypeFileSrv, Name: "Y", Socket: [2]byte{0x04, 0x51},
	})()

	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer svc.Stop()

	waitForSend(t, port, 1)

	body, _ := EncodeSAP(&SAPPacket{
		Operation: SAPGeneralQuery, QueryServiceType: SAPServiceTypeWildcard,
	})
	svc.HandleDatagram(&ipxproto.Datagram{Payload: body})

	waitForSend(t, port, 2)

	port.mu.Lock()
	defer port.mu.Unlock()
	resp, _ := DecodeSAP(port.sent[1].Payload)
	if len(resp.Entries) != 2 {
		t.Fatalf("wildcard match count: got %d want 2", len(resp.Entries))
	}
}

func TestSAPNearestQueryReturnsOneEntry(t *testing.T) {
	r, port := setupRIPRouter(t)
	svc := NewSAPService(r)
	defer svc.Register(SAPEntry{
		ServiceType: SAPServiceTypeNetBIOS, Name: "FIRST", Socket: [2]byte{0x04, 0x55},
	})()
	defer svc.Register(SAPEntry{
		ServiceType: SAPServiceTypeNetBIOS, Name: "SECOND", Socket: [2]byte{0x04, 0x56},
	})()

	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer svc.Stop()

	waitForSend(t, port, 1)

	body, _ := EncodeSAP(&SAPPacket{
		Operation: SAPNearestQuery, QueryServiceType: SAPServiceTypeNetBIOS,
	})
	svc.HandleDatagram(&ipxproto.Datagram{Payload: body})

	waitForSend(t, port, 2)

	port.mu.Lock()
	defer port.mu.Unlock()
	resp, _ := DecodeSAP(port.sent[1].Payload)
	if resp.Operation != SAPNearestResponse {
		t.Fatalf("operation: got %d want %d", resp.Operation, SAPNearestResponse)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("nearest count: got %d want 1", len(resp.Entries))
	}
}

func TestSAPQueryWithNoMatchesIsSilent(t *testing.T) {
	r, port := setupRIPRouter(t)
	svc := NewSAPService(r)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer svc.Stop()

	// No registry entries means no startup broadcast either.
	body, _ := EncodeSAP(&SAPPacket{
		Operation: SAPGeneralQuery, QueryServiceType: SAPServiceTypeNetBIOS,
	})
	svc.HandleDatagram(&ipxproto.Datagram{Payload: body})

	time.Sleep(20 * time.Millisecond)
	port.mu.Lock()
	defer port.mu.Unlock()
	if len(port.sent) != 0 {
		t.Fatalf("expected no response, got %d", len(port.sent))
	}
}

func TestSAPPeriodicBroadcast(t *testing.T) {
	r, port := setupRIPRouter(t)
	svc := NewSAPService(r)
	defer svc.Register(SAPEntry{
		ServiceType: SAPServiceTypeNetBIOS, Name: "CLASSICSTACK", Socket: [2]byte{0x04, 0x55},
	})()

	tickCh := make(chan time.Time, 4)
	tickCount := atomic.Int32{}
	svc.sleep = func(d time.Duration) <-chan time.Time {
		tickCount.Add(1)
		return tickCh
	}

	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer svc.Stop()

	waitForSend(t, port, 1)        // startup
	tickCh <- time.Now()
	waitForSend(t, port, 2)        // tick 1
	tickCh <- time.Now()
	waitForSend(t, port, 3)        // tick 2

	port.mu.Lock()
	defer port.mu.Unlock()
	for i, sent := range port.sent {
		if sent.DstSock != SAPSocket {
			t.Errorf("send %d: DstSock %x", i, sent.DstSock)
		}
		if sent.DstNode != routeripx.BroadcastNode {
			t.Errorf("send %d: DstNode not broadcast", i)
		}
		resp, err := DecodeSAP(sent.Payload)
		if err != nil {
			t.Errorf("send %d: decode: %v", i, err)
			continue
		}
		if resp.Operation != SAPGeneralResponse || len(resp.Entries) != 1 {
			t.Errorf("send %d: %+v", i, resp)
		}
	}
}
