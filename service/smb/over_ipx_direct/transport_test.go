package over_ipx_direct

import (
	"context"
	"encoding/binary"
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
func (p *recordingPort) SetCaptureSink(_ capture.Sink)                   {}

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

type echoHandler struct {
	seen int
}

func (h *echoHandler) HandleSessionContext(packet *netbiosproto.SessionPacket, _ netbios.SessionContext) (*netbiosproto.SessionPacket, error) {
	h.seen++
	if packet == nil || len(packet.Payload) < 37 {
		return nil, nil
	}
	bc := int(binary.LittleEndian.Uint16(packet.Payload[35:37]))
	if 37+bc > len(packet.Payload) {
		return nil, nil
	}
	data := packet.Payload[37 : 37+bc]
	out := make([]byte, 37+len(data))
	copy(out[:32], packet.Payload[:32])
	out[4] = 0x2b
	binary.LittleEndian.PutUint32(out[5:9], 0)
	out[9] |= 0x80
	out[32] = 1
	binary.LittleEndian.PutUint16(out[33:35], 1)
	binary.LittleEndian.PutUint16(out[35:37], uint16(len(data)))
	copy(out[37:], data)
	return &netbiosproto.SessionPacket{Type: netbiosproto.SessionMessage, Payload: out}, nil
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

	req := &ipxproto.Datagram{
		Type:    netbiosproto.IPXTypePEP,
		SrcNet:  [4]byte{0, 0, 0, 0},
		SrcNode: [6]byte{0x52, 0x54, 0x00, 0x52, 0x0b, 0x12},
		SrcSock: [2]byte{0x05, 0x52},
		DstNet:  [4]byte{0, 0, 0, 0},
		DstNode: [6]byte{0x84, 0xa9, 0x38, 0x4a, 0xfa, 0x3b},
		DstSock: directSMBSocket,
		Payload: []byte{0xff, 'S', 'M', 'B', 0x72, 0x00},
	}
	tr.HandleDatagram(req)

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

func TestDirectIPXTransportEchoSendsEchoCountResponses(t *testing.T) {
	r := routeripx.NewRouter()
	r.SetIdentity([4]byte{0, 0, 0, 0}, [6]byte{0x84, 0xa9, 0x38, 0x4a, 0xfa, 0x3b})
	p := &recordingPort{}
	r.AddPort(p)

	h := &echoHandler{}
	tr := New(r, h)
	if err := tr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	req := buildEchoRequestDatagram(3, []byte("ping"))
	tr.HandleDatagram(req)

	if h.seen != 1 {
		t.Fatalf("handler calls: got %d want 1", h.seen)
	}
	if len(p.sent) != 3 {
		t.Fatalf("sent count: got %d want 3", len(p.sent))
	}
	for i := 0; i < 3; i++ {
		seq := binary.LittleEndian.Uint16(p.sent[i].Payload[33:35])
		want := uint16(i + 1)
		if seq != want {
			t.Fatalf("response[%d] sequence: got %d want %d", i, seq, want)
		}
	}
}

func TestDirectIPXTransportEchoErrorSendsSingleResponse(t *testing.T) {
	r := routeripx.NewRouter()
	r.SetIdentity([4]byte{0, 0, 0, 0}, [6]byte{0x84, 0xa9, 0x38, 0x4a, 0xfa, 0x3b})
	p := &recordingPort{}
	r.AddPort(p)

	h := &fakeHandler{seen: 0}
	tr := New(r, h)
	if err := tr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Handler returns non-ECHO payload, so transport must not multiply responses.
	req := buildEchoRequestDatagram(5, []byte("x"))
	tr.HandleDatagram(req)

	if len(p.sent) != 1 {
		t.Fatalf("sent count: got %d want 1", len(p.sent))
	}
}

func buildEchoRequestDatagram(echoCount uint16, data []byte) *ipxproto.Datagram {
	payload := make([]byte, 37+len(data))
	copy(payload[0:4], []byte{0xff, 'S', 'M', 'B'})
	payload[4] = 0x2b
	payload[32] = 1
	binary.LittleEndian.PutUint16(payload[33:35], echoCount)
	binary.LittleEndian.PutUint16(payload[35:37], uint16(len(data)))
	copy(payload[37:], data)

	return &ipxproto.Datagram{
		Type:    netbiosproto.IPXTypePEP,
		SrcNet:  [4]byte{0, 0, 0, 0},
		SrcNode: [6]byte{0x52, 0x54, 0x00, 0x52, 0x0b, 0x12},
		SrcSock: [2]byte{0x05, 0x52},
		DstNet:  [4]byte{0, 0, 0, 0},
		DstNode: [6]byte{0x84, 0xa9, 0x38, 0x4a, 0xfa, 0x3b},
		DstSock: directSMBSocket,
		Payload: payload,
	}
}
