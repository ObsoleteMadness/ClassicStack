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

func TestDirectIPXTransportStampsCIDOnNegotiateAndReusesIt(t *testing.T) {
	r := routeripx.NewRouter()
	r.SetIdentity([4]byte{0, 0, 0, 0}, [6]byte{0x84, 0xa9, 0x38, 0x4a, 0xfa, 0x3b})
	p := &recordingPort{}
	r.AddPort(p)

	h := &headerEchoHandler{}
	tr := New(r, h)
	if err := tr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// NEGOTIATE from client A; the request carries SequenceNumber=7 in
	// SecurityFeatures so we can verify it is mirrored back.
	clientA := [6]byte{0x52, 0x54, 0x00, 0x52, 0x0b, 0x12}
	negA := buildSMBRequestDatagram(0x72, clientA, 7)
	tr.HandleDatagram(negA)

	if len(p.sent) != 1 {
		t.Fatalf("sent count after NEGOTIATE: got %d want 1", len(p.sent))
	}
	cidA := binary.LittleEndian.Uint16(p.sent[0].Payload[smbOffCID : smbOffCID+2])
	if cidA == 0 || cidA == 0xFFFF {
		t.Fatalf("CID assignment: got %#x; 0x0000 and 0xFFFF are reserved", cidA)
	}
	if seq := binary.LittleEndian.Uint16(p.sent[0].Payload[smbOffSequenceNumber : smbOffSequenceNumber+2]); seq != 7 {
		t.Fatalf("SequenceNumber mirror: got %d want 7", seq)
	}

	// A second command from the same client must reuse the CID.
	echo := buildSMBRequestDatagram(0x2b, clientA, 9)
	tr.HandleDatagram(echo)
	if len(p.sent) != 2 {
		t.Fatalf("sent count after ECHO: got %d want 2", len(p.sent))
	}
	cidA2 := binary.LittleEndian.Uint16(p.sent[1].Payload[smbOffCID : smbOffCID+2])
	if cidA2 != cidA {
		t.Fatalf("CID reuse: got %#x want %#x", cidA2, cidA)
	}
	if seq := binary.LittleEndian.Uint16(p.sent[1].Payload[smbOffSequenceNumber : smbOffSequenceNumber+2]); seq != 9 {
		t.Fatalf("SequenceNumber mirror on second response: got %d want 9", seq)
	}

	// A different client gets a different CID via NEGOTIATE.
	clientB := [6]byte{0x52, 0x54, 0x00, 0x52, 0x0b, 0x99}
	negB := buildSMBRequestDatagram(0x72, clientB, 1)
	tr.HandleDatagram(negB)
	if len(p.sent) != 3 {
		t.Fatalf("sent count after second NEGOTIATE: got %d want 3", len(p.sent))
	}
	cidB := binary.LittleEndian.Uint16(p.sent[2].Payload[smbOffCID : smbOffCID+2])
	if cidB == cidA {
		t.Fatalf("distinct clients share CID: %#x", cidB)
	}
}

func TestDirectIPXTransportMirrorsRequestCIDOnResponse(t *testing.T) {
	r := routeripx.NewRouter()
	r.SetIdentity([4]byte{0, 0, 0, 0}, [6]byte{0x84, 0xa9, 0x38, 0x4a, 0xfa, 0x3b})
	p := &recordingPort{}
	r.AddPort(p)

	h := &headerEchoHandler{}
	tr := New(r, h)
	if err := tr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	req := buildSMBRequestDatagram(0x2b, [6]byte{0x52, 0x54, 0x00, 0x52, 0x0b, 0x12}, 4)
	binary.LittleEndian.PutUint16(req.Payload[smbOffCID:smbOffCID+2], 0x0042)
	tr.HandleDatagram(req)

	if len(p.sent) != 1 {
		t.Fatalf("sent count: got %d want 1", len(p.sent))
	}
	if got := binary.LittleEndian.Uint16(p.sent[0].Payload[smbOffCID : smbOffCID+2]); got != 0x0042 {
		t.Fatalf("CID mirror mismatch: got %#x want %#x", got, uint16(0x0042))
	}
}

// headerEchoHandler returns a 32-byte SMB header that mirrors the request
// header, simulating a real command response builder (which always copies
// the request header). It lets the test inspect SecurityFeatures stamping.
type headerEchoHandler struct{}

func (h *headerEchoHandler) HandleSessionContext(packet *netbiosproto.SessionPacket, _ netbios.SessionContext) (*netbiosproto.SessionPacket, error) {
	if packet == nil || len(packet.Payload) < 32 {
		return nil, nil
	}
	out := make([]byte, 32)
	copy(out, packet.Payload[:32])
	out[9] |= 0x80
	return &netbiosproto.SessionPacket{Type: netbiosproto.SessionMessage, Payload: out}, nil
}

func buildSMBRequestDatagram(cmd byte, srcNode [6]byte, sequence uint16) *ipxproto.Datagram {
	payload := make([]byte, 32)
	copy(payload[0:4], []byte{0xff, 'S', 'M', 'B'})
	payload[smbCommandOff] = cmd
	binary.LittleEndian.PutUint16(payload[smbOffSequenceNumber:smbOffSequenceNumber+2], sequence)
	return &ipxproto.Datagram{
		Type:    netbiosproto.IPXTypePEP,
		SrcNet:  [4]byte{0, 0, 0, 0},
		SrcNode: srcNode,
		SrcSock: [2]byte{0x05, 0x52},
		DstNet:  [4]byte{0, 0, 0, 0},
		DstNode: [6]byte{0x84, 0xa9, 0x38, 0x4a, 0xfa, 0x3b},
		DstSock: directSMBSocket,
		Payload: payload,
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
