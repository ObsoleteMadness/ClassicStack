package ipxdiag

import (
	"testing"

	ipxproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx/diag"
)

type recordingSender struct{ sent []*ipxproto.Datagram }

func (s *recordingSender) Send(d *ipxproto.Datagram) error {
	s.sent = append(s.sent, d)
	return nil
}

// reqDatagram builds an inbound diagnostic request datagram from a remote endpoint.
func reqDatagram(t *testing.T, srcNode [6]byte, srcSock [2]byte, req diag.Request) *ipxproto.Datagram {
	t.Helper()
	payload, err := req.Marshal()
	if err != nil {
		t.Fatalf("Marshal request: %v", err)
	}
	return &ipxproto.Datagram{
		Type:    ipxPEPType,
		SrcNode: srcNode,
		SrcSock: srcSock,
		DstSock: diag.Socket,
		Payload: payload,
	}
}

func TestResponder_RepliesToPing(t *testing.T) {
	t.Parallel()
	snd := &recordingSender{}
	r := New(nil, snd, [6]byte{0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA})

	src := [6]byte{0x00, 0x50, 0x56, 0xC0, 0x00, 0x01}
	clientSock := [2]byte{0x40, 0x00}
	r.HandleDatagram(reqDatagram(t, src, clientSock, diag.Request{}))

	if len(snd.sent) != 1 {
		t.Fatalf("want 1 reply, got %d", len(snd.sent))
	}
	out := snd.sent[0]
	if out.DstNode != src || out.DstSock != clientSock {
		t.Fatalf("reply not addressed back to requester: %+v", out)
	}
	if out.SrcSock != diag.Socket {
		t.Fatalf("reply source socket = %v, want diag.Socket", out.SrcSock)
	}
	resp, err := diag.UnmarshalResponse(out.Payload)
	if err != nil || len(resp.Components) != 1 || resp.Components[0].Type != diag.CompIPX {
		t.Fatalf("reply payload = %+v err=%v", resp, err)
	}
}

func TestResponder_SilentWhenSelfExcluded(t *testing.T) {
	t.Parallel()
	snd := &recordingSender{}
	self := [6]byte{0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA}
	r := New(nil, snd, self)

	// A broadcast request that already lists our node must not be answered.
	req := diag.Request{Exclusions: [][6]byte{self}}
	r.HandleDatagram(reqDatagram(t, [6]byte{1, 2, 3, 4, 5, 6}, [2]byte{0x40, 0x00}, req))

	if len(snd.sent) != 0 {
		t.Fatalf("want silence when self-excluded, got %d replies", len(snd.sent))
	}
}

func TestResponder_IgnoresNil(t *testing.T) {
	t.Parallel()
	snd := &recordingSender{}
	r := New(nil, snd, [6]byte{})
	r.HandleDatagram(nil)
	if len(snd.sent) != 0 {
		t.Fatalf("nil datagram should produce no reply")
	}
}
