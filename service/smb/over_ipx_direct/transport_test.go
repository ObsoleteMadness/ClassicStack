package over_ipx_direct

import (
	"context"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/capture"
	portipx "github.com/ObsoleteMadness/ClassicStack/port/ipx"
	ipxproto "github.com/ObsoleteMadness/ClassicStack/protocol/ipx"
	netbiosproto "github.com/ObsoleteMadness/ClassicStack/protocol/netbios"
	routeripx "github.com/ObsoleteMadness/ClassicStack/router/ipx"
	"github.com/ObsoleteMadness/ClassicStack/service/netbios"
)

type recordingPort struct {
	sent []*ipxproto.Datagram
	cb   portipx.DeliveryCallback
}

func (p *recordingPort) Start() error { return nil }
func (p *recordingPort) Stop() error  { return nil }
func (p *recordingPort) Send(d *ipxproto.Datagram) error {
	cp := *d
	p.sent = append(p.sent, &cp)
	return nil
}
func (p *recordingPort) SetDeliveryCallback(cb portipx.DeliveryCallback) { p.cb = cb }
func (p *recordingPort) SetCaptureSink(_ capture.Sink)                    {}

type fakeHandler struct {
	seen int
}

func (h *fakeHandler) HandleSessionContext(packet *netbiosproto.SessionPacket, _ netbios.SessionContext) (*netbiosproto.SessionPacket, error) {
	h.seen++
	if packet == nil || len(packet.Payload) < 4 {
		return nil, nil
	}
	return &netbiosproto.SessionPacket{Type: netbiosproto.SessionMessage, Payload: []byte{0xff, 'S', 'M', 'B', 0x72}}, nil
}

func TestDirectIPXTransportHandlesRawSMB(t *testing.T) {
	r := routeripx.NewRouter()
	r.SetIdentity([4]byte{0, 0, 0, 0}, [6]byte{0x84, 0xa9, 0x38, 0x4a, 0xfa, 0x3b})
	p := &recordingPort{}
	r.AddPort(p)

	h := &fakeHandler{}
	tr := New(r, h)
	if err := tr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	tr.HandleDatagram(&ipxproto.Datagram{
		Type:    netbiosproto.IPXTypePEP,
		SrcNet:  [4]byte{0, 0, 0, 0},
		SrcNode: [6]byte{0x52, 0x54, 0x00, 0x52, 0x0b, 0x12},
		SrcSock: [2]byte{0x05, 0x52},
		DstNet:  [4]byte{0, 0, 0, 0},
		DstNode: [6]byte{0x84, 0xa9, 0x38, 0x4a, 0xfa, 0x3b},
		DstSock: directSMBSocket,
		Payload: []byte{0xff, 'S', 'M', 'B', 0x72, 0x00},
	})

	if h.seen != 1 {
		t.Fatalf("handler calls: got %d want 1", h.seen)
	}
	if len(p.sent) != 1 {
		t.Fatalf("sent count: got %d want 1", len(p.sent))
	}
	if p.sent[0].DstSock != [2]byte{0x05, 0x52} {
		t.Fatalf("response dst socket: got %x want 0552", p.sent[0].DstSock)
	}
	if p.sent[0].SrcSock != directSMBSocket {
		t.Fatalf("response src socket: got %x want %x", p.sent[0].SrcSock, directSMBSocket)
	}
}
