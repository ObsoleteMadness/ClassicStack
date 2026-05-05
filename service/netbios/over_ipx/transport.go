// Package over_ipx adapts the IPX router to the netbios.Transport
// contract. NetBIOS over IPX (NWLink) uses three sockets:
//
//	0x0455 — NetBIOS-over-IPX (session + name service)
//	0x0553 — NetBIOS datagram
//	0x0554 — NetBIOS name service (alternative path used by some clients)
//
// On Start the transport runs a name-claim broadcast against the
// segment, six 500ms retries (~3s total). If any node replies with
// our name owning it, the claim fails. If silence, we register with
// SAP under SAPServiceTypeNetBIOS so other nodes browsing SAP find us.
package over_ipx

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/netlog"
	ipxproto "github.com/ObsoleteMadness/ClassicStack/protocol/ipx"
	protocol "github.com/ObsoleteMadness/ClassicStack/protocol/netbios"
	"github.com/ObsoleteMadness/ClassicStack/router/ipx"
	ipxsvc "github.com/ObsoleteMadness/ClassicStack/service/ipx"
	"github.com/ObsoleteMadness/ClassicStack/service/netbios"
)

// Sockets is the ordered list of IPX socket numbers NetBIOS-over-IPX
// claims. Exposed for documentation and tests.
var Sockets = [3][2]byte{
	{0x04, 0x55}, // session + most name-service traffic
	{0x05, 0x53}, // datagram
	{0x05, 0x54}, // name service (alternative)
}

// NB-IPX socket numbers as constants for readability inside this
// package. The wire bytes are identical to Sockets[*] but the names
// document intent at call sites.
var (
	NBIPXSessionSocket  = [2]byte{0x04, 0x55}
	NBIPXDatagramSocket = [2]byte{0x05, 0x53}
	NBIPXNameSocket     = [2]byte{0x05, 0x54}
)

// Default name-claim retry parameters. NWLink and Win9x clients use
// the same 500ms × 6 cadence (≈3s total) before considering a name
// uncontested.
const (
	DefaultNameClaimRetries  = 6
	DefaultNameClaimInterval = 500 * time.Millisecond
)

// ErrNameInUse is returned when a name claim is contested by another
// node holding the same name.
var ErrNameInUse = errors.New("netbios/over_ipx: name already in use on segment")

// SAPRegistrar is the slice of *ipxsvc.SAPService this package needs.
// Carrying it as an interface keeps tests independent of the full
// SAP machinery — a fake registrar with a single method satisfies it.
type SAPRegistrar interface {
	Register(entry ipxsvc.SAPEntry) (cancel func())
}

type transport struct {
	router ipx.Router
	sap    SAPRegistrar
	name   protocol.Name

	// Tunable claim parameters; tests override these to drive the
	// name-claim machinery without sleeping in real time.
	claimRetries  int
	claimInterval time.Duration
	sleep         func(d time.Duration) <-chan time.Time

	mu          sync.RWMutex
	handler     netbios.CommandHandler
	objection   chan struct{}
	sapCancel   func()
	stopOnce    sync.Once
	stopped     chan struct{}
}

// NewTransport returns a netbios.Transport that registers on the
// IPX NetBIOS sockets, claims name on the segment, and (on success)
// publishes itself via SAP. Pass an empty name to skip the name
// claim — useful for tests that want only the socket-level transport.
func NewTransport(r ipx.Router, sap SAPRegistrar, name protocol.Name) netbios.Transport {
	return &transport{
		router:        r,
		sap:           sap,
		name:          name,
		claimRetries:  DefaultNameClaimRetries,
		claimInterval: DefaultNameClaimInterval,
		sleep: func(d time.Duration) <-chan time.Time {
			return time.After(d)
		},
		objection: make(chan struct{}, 1),
		stopped:   make(chan struct{}),
	}
}

// Start registers our IPX sockets and runs the name claim. Returns
// nil even if the claim fails — the transport stays alive as a
// receiver for sessions destined to whatever node we already are,
// but no SAP advertisement appears. Errors here would prevent the
// rest of NetBIOS from starting; we'd rather log and continue.
func (t *transport) Start(ctx context.Context) error {
	for _, sock := range Sockets {
		if err := t.router.RegisterSocket(sock, t); err != nil {
			return err
		}
	}

	if t.shouldClaimName() {
		go t.claimAndAdvertise(ctx)
	}
	return nil
}

// shouldClaimName returns true when both the SAP service and a
// non-empty name are available. A zero name means the operator did
// not configure one (unit-test transports do this).
func (t *transport) shouldClaimName() bool {
	if t.sap == nil {
		return false
	}
	var zero protocol.Name
	return t.name != zero
}

// claimAndAdvertise broadcasts FindName retries until either an
// objection arrives or all retries lapse. On success it registers
// the name with SAP under SAPServiceTypeNetBIOS.
func (t *transport) claimAndAdvertise(ctx context.Context) {
	netlog.Info("[NetBIOS][IPX] claiming name %q (%d retries × %v)",
		t.name.String(), t.claimRetries, t.claimInterval)

	for i := range t.claimRetries {
		if err := t.broadcastFindName(); err != nil {
			netlog.Warn("[NetBIOS][IPX] FindName broadcast %d: %v", i+1, err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.objection:
			netlog.Warn("[NetBIOS][IPX] name %q is already in use; aborting claim", t.name.String())
			return
		case <-t.sleep(t.claimInterval):
			// Continue to the next retry.
		}
	}

	// Name uncontested — publish via SAP.
	cancel := t.sap.Register(ipxsvc.SAPEntry{
		ServiceType: ipxsvc.SAPServiceTypeNetBIOS,
		Name:        t.name.String(),
		Socket:      NBIPXSessionSocket,
	})
	t.mu.Lock()
	t.sapCancel = cancel
	t.mu.Unlock()
	netlog.Info("[NetBIOS][IPX] name %q claimed; advertised via SAP type 0x%04x",
		t.name.String(), ipxsvc.SAPServiceTypeNetBIOS)
}

// broadcastFindName emits one type-20 IPX broadcast carrying our name
// to socket 0x0455 on every node of the segment.
func (t *transport) broadcastFindName() error {
	body := protocol.EncodeNameService(&protocol.NBIPXNameServicePacket{Name: t.name})
	out := &ipxproto.Datagram{
		Type:    protocol.IPXTypeNetBIOS,
		DstNet:  t.router.Network(),
		DstNode: ipx.BroadcastNode,
		DstSock: NBIPXSessionSocket,
		SrcSock: NBIPXSessionSocket,
		Payload: body,
	}
	return t.router.Send(out)
}

// Stop unregisters the SAP advertisement (if any) and stops further
// inbound dispatch.
func (t *transport) Stop() error {
	t.stopOnce.Do(func() {
		close(t.stopped)
		t.mu.Lock()
		cancel := t.sapCancel
		t.sapCancel = nil
		t.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	})
	return nil
}

func (t *transport) SendName(_ protocol.Name) error              { return netbios.ErrNotImplemented }
func (t *transport) SendDatagram(_ *protocol.Datagram) error     { return netbios.ErrNotImplemented }
func (t *transport) SendSession(_ *protocol.SessionPacket) error { return netbios.ErrNotImplemented }

func (t *transport) SetCommandHandler(h netbios.CommandHandler) {
	t.mu.Lock()
	t.handler = h
	t.mu.Unlock()
}

// HandleDatagram implements router/ipx.SocketHandler. It dispatches by
// the IPX packet-type field:
//
//   - Type 20 (NetBIOS broadcast/forwarding): name service. During a
//     pending claim, this is how we learn another node owns our name.
//   - Type 4 (Packet Exchange): session-layer traffic. Forwarded to
//     the session machine when that lands in Phase 5C; for now we
//     log and drop.
func (t *transport) HandleDatagram(d *ipxproto.Datagram) {
	switch d.Type {
	case protocol.IPXTypeNetBIOS:
		t.handleNameService(d)
	case protocol.IPXTypePEP:
		// Session traffic — Phase 5C wires this into the session
		// machine. Until then, drop.
	}
}

// handleNameService examines an inbound type-20 packet during a
// pending claim. If the packet's name matches ours and the source
// is some other node, we have a conflict.
func (t *transport) handleNameService(d *ipxproto.Datagram) {
	pkt, err := protocol.DecodeNameService(d.Payload)
	if err != nil {
		return
	}
	if pkt.Name != t.name {
		return
	}
	// Ignore our own broadcast looping back. The router's accept
	// filter already rejects packets whose destination isn't us or
	// a broadcast, but pcap can deliver our own broadcasts back to
	// us depending on the OS / driver, so we filter by source node.
	if d.SrcNet == t.router.Network() && d.SrcNode == t.router.Node() {
		return
	}
	// Real conflict. Signal the claim goroutine.
	select {
	case t.objection <- struct{}{}:
	default:
		// Channel already armed; one signal is enough.
	}
}
