package ipx

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/capture"
	portipx "github.com/ObsoleteMadness/ClassicStack/port/ipx"
	ipxproto "github.com/ObsoleteMadness/ClassicStack/protocol/ipx"
	routeripx "github.com/ObsoleteMadness/ClassicStack/router/ipx"
)

// recordingPort is a minimal portipx.Port implementation that captures
// every Send and exposes the delivery callback the router installs.
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

func setupRIPRouter(t *testing.T) (routeripx.Router, *recordingPort) {
	t.Helper()
	r := routeripx.NewRouter()
	r.SetIdentity([4]byte{0xCA, 0xFE, 0xF0, 0x0D}, [6]byte{0x02, 0, 0, 0, 0, 0x42})
	port := &recordingPort{}
	r.AddPort(port)
	return r, port
}

func TestRIPRespondsToWildcardRequest(t *testing.T) {
	r, port := setupRIPRouter(t)
	svc := NewRIPService(r)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer svc.Stop()

	// Drain the immediate startup broadcast so the test only sees the
	// reply triggered by our request.
	waitForSend(t, port, 1)

	req := &RIPPacket{Operation: RIPRequest}
	body, _ := EncodeRIP(req)
	svc.HandleDatagram(&ipxproto.Datagram{
		SrcNet:  [4]byte{0xCA, 0xFE, 0xF0, 0x0D},
		SrcNode: [6]byte{0x02, 0, 0, 0, 0, 0x99},
		DstNet:  [4]byte{0xCA, 0xFE, 0xF0, 0x0D},
		DstNode: [6]byte{0x02, 0, 0, 0, 0, 0x42},
		DstSock: RIPSocket,
		Payload: body,
	})

	waitForSend(t, port, 2)

	got := port.sent[1]
	if got.DstSock != RIPSocket {
		t.Fatalf("response DstSock: got %x want %x", got.DstSock, RIPSocket)
	}
	if got.DstNode != [6]byte{0x02, 0, 0, 0, 0, 0x99} {
		t.Fatalf("response not unicast to requester: %x", got.DstNode)
	}
	resp, err := DecodeRIP(got.Payload)
	if err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Operation != RIPResponse {
		t.Fatalf("operation: got %d want %d", resp.Operation, RIPResponse)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("entries: got %d want 1", len(resp.Entries))
	}
	if resp.Entries[0].Network != ([4]byte{0xCA, 0xFE, 0xF0, 0x0D}) {
		t.Fatalf("advertised network: got %x", resp.Entries[0].Network)
	}
}

func TestRIPIgnoresUnknownNetworkRequest(t *testing.T) {
	r, port := setupRIPRouter(t)
	svc := NewRIPService(r)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer svc.Stop()

	waitForSend(t, port, 1) // startup broadcast

	body, _ := EncodeRIP(&RIPPacket{
		Operation: RIPRequest,
		Entries: []RIPEntry{
			{Network: [4]byte{0xAA, 0xBB, 0xCC, 0xDD}}, // not ours, not wildcard
		},
	})
	svc.HandleDatagram(&ipxproto.Datagram{
		SrcNode: [6]byte{0xDE, 0xAD, 0xBE, 0xEF, 0xCA, 0xFE},
		Payload: body,
	})

	// Give the responder a moment; we expect no extra send beyond the
	// startup broadcast.
	time.Sleep(20 * time.Millisecond)
	port.mu.Lock()
	defer port.mu.Unlock()
	if len(port.sent) != 1 {
		t.Fatalf("unexpected response to unknown-network request: sent=%d", len(port.sent))
	}
}

func TestRIPIgnoresResponses(t *testing.T) {
	r, port := setupRIPRouter(t)
	svc := NewRIPService(r)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer svc.Stop()

	waitForSend(t, port, 1)

	body, _ := EncodeRIP(&RIPPacket{
		Operation: RIPResponse,
		Entries: []RIPEntry{
			{Network: [4]byte{0xCA, 0xFE, 0xF0, 0x0D}, Hops: 1, Ticks: 1},
		},
	})
	svc.HandleDatagram(&ipxproto.Datagram{Payload: body})

	time.Sleep(20 * time.Millisecond)
	port.mu.Lock()
	defer port.mu.Unlock()
	if len(port.sent) != 1 {
		t.Fatal("RIP responder should ignore inbound RIP responses")
	}
}

func TestRIPPeriodicBroadcast(t *testing.T) {
	r, port := setupRIPRouter(t)
	svc := NewRIPService(r)

	// Drive the broadcast loop with a synthetic clock so we can assert
	// the cadence without sleeping in real time. Each "tick" closes a
	// channel that the loop is selecting on.
	tickCount := atomic.Int32{}
	tickCh := make(chan time.Time, 4)
	svc.sleep = func(d time.Duration) <-chan time.Time {
		tickCount.Add(1)
		return tickCh
	}

	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer svc.Stop()

	// Startup broadcast.
	waitForSend(t, port, 1)
	// Tick once: loop wakes, broadcasts again.
	tickCh <- time.Now()
	waitForSend(t, port, 2)
	// Tick again.
	tickCh <- time.Now()
	waitForSend(t, port, 3)

	// All three sends should be RIP-response broadcasts to the
	// broadcast node on socket 0x0453.
	port.mu.Lock()
	defer port.mu.Unlock()
	for i, sent := range port.sent {
		if sent.DstSock != RIPSocket {
			t.Errorf("send %d: DstSock %x", i, sent.DstSock)
		}
		if sent.DstNode != routeripx.BroadcastNode {
			t.Errorf("send %d: DstNode %x not broadcast", i, sent.DstNode)
		}
		resp, err := DecodeRIP(sent.Payload)
		if err != nil {
			t.Errorf("send %d: decode: %v", i, err)
			continue
		}
		if resp.Operation != RIPResponse || len(resp.Entries) != 1 {
			t.Errorf("send %d: unexpected packet %+v", i, resp)
		}
	}
}

// waitForSend blocks until the recorded sends reach n, or fails.
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
