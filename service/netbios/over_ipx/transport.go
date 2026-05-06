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
var Sockets = [4][2]byte{
	{0x04, 0x55}, // session + most name-service traffic
	{0x05, 0x51}, // NMPI name-query
	{0x05, 0x53}, // datagram
	{0x05, 0x54}, // name service (alternative)
}

// NB-IPX socket numbers as constants for readability inside this
// package. The wire bytes are identical to Sockets[*] but the names
// document intent at call sites.
var (
	NBIPXSessionSocket   = [2]byte{0x04, 0x55}
	NBIPXServerSocket    = [2]byte{0x05, 0x50}
	NBIPXNameQuerySocket = [2]byte{0x05, 0x51}
	NBIPXDatagramSocket  = [2]byte{0x05, 0x53}
	NBIPXNameSocket      = [2]byte{0x05, 0x54}
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

	mu        sync.RWMutex
	handler   netbios.CommandHandler
	objection chan struct{}
	sapCancel func()
	stopOnce  sync.Once
	stopped   chan struct{}
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
		if err := t.broadcastNMPIClaim(); err != nil {
			netlog.Warn("[NetBIOS][IPX] NMPI ClaimName broadcast %d: %v", i+1, err)
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
	body := protocol.EncodeNameService(&protocol.NBIPXNameServicePacket{
		NameTypeFlag:   0x00,
		DataStreamType: protocol.NBIPXFindName,
		Name:           t.name,
	})
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

func (t *transport) broadcastNMPIClaim() error {
	body := protocol.EncodeNMPIPacket(&protocol.NMPIPacket{
		Opcode:        protocol.NMPIOpNameClaim,
		NameType:      protocol.NMPINameTypeMachine,
		MessageID:     0,
		RequestedName: t.name,
		SourceName:    t.name,
	})
	out := &ipxproto.Datagram{
		Type:    protocol.IPXTypeNetBIOS,
		DstNet:  t.router.Network(),
		DstNode: ipx.BroadcastNode,
		DstSock: NBIPXNameQuerySocket,
		SrcSock: NBIPXServerSocket,
		Payload: body,
	}
	netlog.Debug("[NetBIOS][IPX] tx NMPI claim name=%q", t.name.String())
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

func (t *transport) SendName(_ protocol.Name) error { return netbios.ErrNotImplemented }

func (t *transport) SendDatagram(dg *protocol.Datagram) error {
	if dg == nil {
		return nil
	}
	netlog.Debug("[NetBIOS][IPX] tx mailslot send src=%q dst=%q payload=%d",
		dg.Source.String(), dg.Destination.String(), len(dg.Payload))
	return t.sendNMPIDatagram(dg, netbios.DatagramEndpoint{
		Network: t.router.Network(),
		Node:    ipx.BroadcastNode,
		Socket:  NBIPXDatagramSocket,
	})
}

func (t *transport) SendDirectedDatagram(dg *protocol.Datagram, remote netbios.DatagramEndpoint) error {
	if dg == nil {
		return nil
	}
	if remote.Socket == ([2]byte{}) {
		remote.Socket = NBIPXDatagramSocket
	}
	netlog.Debug("[NetBIOS][IPX] tx directed mailslot send src=%q dst=%q ipx=%x.%x:%02x%02x payload=%d",
		dg.Source.String(), dg.Destination.String(),
		remote.Network, remote.Node, remote.Socket[0], remote.Socket[1], len(dg.Payload))
	return t.sendNMPIDatagram(dg, remote)
}

func (t *transport) sendNMPIDatagram(dg *protocol.Datagram, remote netbios.DatagramEndpoint) error {
	payload := protocol.EncodeNMPIPacket(&protocol.NMPIPacket{
		Opcode:        protocol.NMPIOpMailslotSend,
		NameType:      nmpiNameType(dg.Destination),
		MessageID:     0,
		RequestedName: dg.Destination,
		SourceName:    dg.Source,
		Payload:       dg.Payload,
	})
	out := &ipxproto.Datagram{
		Type:    protocol.IPXTypeNetBIOS,
		DstNet:  remote.Network,
		DstNode: remote.Node,
		DstSock: remote.Socket,
		SrcSock: NBIPXDatagramSocket,
		Payload: payload,
	}
	return t.router.Send(out)
}

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
	if d == nil {
		return
	}
	if d.SrcNet == t.router.Network() && d.SrcNode == t.router.Node() {
		netlog.Debug("[NetBIOS][IPX] drop self-looped datagram type=0x%02x srcSock=%02x%02x dstSock=%02x%02x",
			d.Type, d.SrcSock[0], d.SrcSock[1], d.DstSock[0], d.DstSock[1])
		return
	}
	netlog.Debug("[NetBIOS][IPX] rx ipx type=0x%02x srcSock=%02x%02x dstSock=%02x%02x payload=%d",
		d.Type, d.SrcSock[0], d.SrcSock[1], d.DstSock[0], d.DstSock[1], len(d.Payload))
	switch d.Type {
	case protocol.IPXTypeNetBIOS:
		if t.handleNMPIPayload(d) {
			return
		}
		t.handleNameService(d)
	case protocol.IPXTypePEP:
		t.handlePEP(d)
	}
}

func (t *transport) handleNMPIPayload(d *ipxproto.Datagram) bool {
	if d == nil || len(d.Payload) < 2 {
		return false
	}
	if d.DstSock != NBIPXNameQuerySocket && d.DstSock != NBIPXDatagramSocket {
		return false
	}
	p, err := protocol.DecodeNMPIPacket(d.Payload)
	if err != nil {
		return false
	}
	netlog.Debug("[NetBIOS][IPX] rx NMPI opcode=0x%02x nameType=0x%02x src=%q dst=%q payload=%d",
		p.Opcode, p.NameType, p.SourceName.String(), p.RequestedName.String(), len(p.Payload))
	t.handleNMPI(d, p)
	return true
}

func (t *transport) handlePEP(d *ipxproto.Datagram) {
	if d == nil || len(d.Payload) < 2 {
		return
	}
	if t.handleNMPIPayload(d) {
		return
	}
	if d.DstSock == NBIPXDatagramSocket {
		if d.Payload[1] != protocol.NBIPXDirectedDatagram {
			return
		}
		dg, err := protocol.DecodeDatagram(d.Payload[2:])
		if err != nil {
			return
		}
		netlog.Debug("[NetBIOS][IPX] rx directed datagram src=%q dst=%q payload=%d",
			dg.Source.String(), dg.Destination.String(), len(dg.Payload))
		t.mu.RLock()
		h := t.handler
		t.mu.RUnlock()
		if h != nil {
			if ch, ok := h.(netbios.ContextualDatagramHandler); ok {
				_ = ch.HandleDatagramContext(dg, netbios.DatagramContext{
					Local: netbios.DatagramEndpoint{
						Network: d.DstNet,
						Node:    d.DstNode,
						Socket:  d.DstSock,
					},
					Remote: netbios.DatagramEndpoint{
						Network: d.SrcNet,
						Node:    d.SrcNode,
						Socket:  d.SrcSock,
					},
				})
				return
			}
			_ = h.HandleDatagram(dg)
		}
		return
	}

	if d.DstSock != NBIPXSessionSocket {
		return
	}
	hdr, err := protocol.DecodeSessionHeader(d.Payload)
	if err != nil {
		return
	}
	if len(d.Payload) < protocol.NBIPXSessionHeaderLen+int(hdr.DataLen) {
		return
	}
	body := append([]byte(nil), d.Payload[protocol.NBIPXSessionHeaderLen:protocol.NBIPXSessionHeaderLen+int(hdr.DataLen)]...)

	if hdr.DataStreamType == protocol.NBIPXSessionInit {
		_ = t.sendPEPSessionControl(d, hdr, protocol.NBIPXSessionConfirm)
		return
	}
	if hdr.DataStreamType == protocol.NBIPXSessionEnd {
		_ = t.sendPEPSessionControl(d, hdr, protocol.NBIPXSessionEndAck)
		return
	}
	if hdr.DataStreamType != protocol.NBIPXDataOnlyLast && hdr.DataStreamType != protocol.NBIPXDataFirstMiddle {
		return
	}

	netlog.Debug("[NetBIOS][IPX] rx session data srcConn=%04x dstConn=%04x seq=%d bytes=%d",
		hdr.SourceConnID, hdr.DestConnID, hdr.SendSeq, len(body))
	t.mu.RLock()
	h := t.handler
	t.mu.RUnlock()
	if h == nil {
		return
	}
	sp := &protocol.SessionPacket{Type: protocol.SessionMessage, Payload: body}
	if sh, ok := h.(netbios.ContextualSessionHandler); ok {
		resp, err := sh.HandleSessionContext(sp, netbios.SessionContext{
			Local: netbios.DatagramEndpoint{
				Network: d.DstNet,
				Node:    d.DstNode,
				Socket:  d.DstSock,
			},
			Remote: netbios.DatagramEndpoint{
				Network: d.SrcNet,
				Node:    d.SrcNode,
				Socket:  d.SrcSock,
			},
			SourceConnID:  hdr.SourceConnID,
			DestConnID:    hdr.DestConnID,
			Sequence:      hdr.SendSeq,
			ConnectionCtl: hdr.ConnCtrlByte,
		})
		if err == nil && resp != nil && len(resp.Payload) > 0 {
			_ = t.sendPEPSessionData(d, hdr, resp.Payload)
		}
		return
	}
	_ = h.HandleSession(sp)
}
func (t *transport) sendPEPSessionControl(in *ipxproto.Datagram, inHdr *protocol.NBIPXSessionHeader, streamType uint8) error {
	if in == nil || inHdr == nil {
		return nil
	}
	h := &protocol.NBIPXSessionHeader{
		ConnCtrlFlag:   protocol.NBIPXConnFlagSYS,
		DataStreamType: streamType,
		SourceConnID:   inHdr.DestConnID,
		DestConnID:     inHdr.SourceConnID,
		SendSeq:        inHdr.SendSeq,
		TotalDataLen:   0,
		Offset:         0,
		DataLen:        0,
		ConnCtrlByte:   inHdr.ConnCtrlByte,
	}
	body := protocol.EncodeSessionHeader(h)
	out := &ipxproto.Datagram{
		Type:    protocol.IPXTypePEP,
		DstNet:  in.SrcNet,
		DstNode: in.SrcNode,
		DstSock: in.SrcSock,
		SrcSock: in.DstSock,
		Payload: body,
	}
	return t.router.Send(out)
}

func (t *transport) sendPEPSessionData(in *ipxproto.Datagram, inHdr *protocol.NBIPXSessionHeader, payload []byte) error {
	if in == nil || inHdr == nil {
		return nil
	}
	h := &protocol.NBIPXSessionHeader{
		ConnCtrlFlag:   protocol.NBIPXConnFlagEOM,
		DataStreamType: protocol.NBIPXDataOnlyLast,
		SourceConnID:   inHdr.DestConnID,
		DestConnID:     inHdr.SourceConnID,
		SendSeq:        inHdr.SendSeq,
		TotalDataLen:   uint16(len(payload)),
		Offset:         0,
		DataLen:        uint16(len(payload)),
		ConnCtrlByte:   inHdr.ConnCtrlByte,
	}
	body := append(protocol.EncodeSessionHeader(h), payload...)
	out := &ipxproto.Datagram{
		Type:    protocol.IPXTypePEP,
		DstNet:  in.SrcNet,
		DstNode: in.SrcNode,
		DstSock: in.SrcSock,
		SrcSock: in.DstSock,
		Payload: body,
	}
	return t.router.Send(out)
}

func (t *transport) handleNMPI(d *ipxproto.Datagram, p *protocol.NMPIPacket) {
	if p == nil {
		return
	}
	if p.Opcode == protocol.NMPIOpMailslotSend {
		netlog.Debug("[NetBIOS][IPX] request mailslot send src=%q dst=%q payload=%d",
			p.SourceName.String(), p.RequestedName.String(), len(p.Payload))
		t.mu.RLock()
		h := t.handler
		t.mu.RUnlock()
		if h != nil {
			dg := &protocol.Datagram{
				Destination: p.RequestedName,
				Source:      p.SourceName,
				Payload:     append([]byte(nil), p.Payload...),
			}
			if ch, ok := h.(netbios.ContextualDatagramHandler); ok {
				_ = ch.HandleDatagramContext(dg, netbios.DatagramContext{
					Local: netbios.DatagramEndpoint{
						Network: d.DstNet,
						Node:    d.DstNode,
						Socket:  d.DstSock,
					},
					Remote: netbios.DatagramEndpoint{
						Network: d.SrcNet,
						Node:    d.SrcNode,
						Socket:  d.SrcSock,
					},
				})
				return
			}
			_ = h.HandleDatagram(dg)
		}
		return
	}
	if p.Opcode != protocol.NMPIOpNameQuery {
		return
	}
	netlog.Debug("[NetBIOS][IPX] request name query msg=0x%04x src=%q dst=%q",
		p.MessageID, p.SourceName.String(), p.RequestedName.String())
	if p.RequestedName != t.name {
		return
	}
	resp := protocol.EncodeNMPIPacket(&protocol.NMPIPacket{
		Opcode:        protocol.NMPIOpNameFound,
		NameType:      p.NameType,
		MessageID:     p.MessageID,
		RequestedName: p.RequestedName,
		SourceName:    t.name,
	})
	out := &ipxproto.Datagram{
		Type:    protocol.IPXTypePEP,
		DstNet:  d.SrcNet,
		DstNode: d.SrcNode,
		DstSock: d.SrcSock,
		SrcSock: d.DstSock,
		Payload: resp,
	}
	netlog.Debug("[NetBIOS][IPX] response name found msg=0x%04x src=%q dst=%q",
		p.MessageID, t.name.String(), p.SourceName.String())
	if err := t.router.Send(out); err != nil {
		netlog.Warn("[NetBIOS][IPX] NMPI NameFound send failed: %v", err)
	}
}

func nmpiNameType(name protocol.Name) uint8 {
	if name.Type() == protocol.NameTypeGroup {
		return protocol.NMPINameTypeWorkgroup
	}
	return protocol.NMPINameTypeMachine
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
	// Real conflict. Signal the claim goroutine.
	select {
	case t.objection <- struct{}{}:
	default:
		// Channel already armed; one signal is enough.
	}
}
