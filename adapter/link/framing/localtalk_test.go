package framing

import (
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/adapter/link/inmem"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
)

// TestLocalTalk_LongHeaderRoundTrip: an inter-network datagram (Dest != Src
// network) round-trips through the LLAP long header (type 0x02), preserving the
// full DDP header including network numbers.
func TestLocalTalk_LongHeaderRoundTrip(t *testing.T) {
	fl := inmem.Loopback(4)
	defer fl.Close()

	framer := &LocalTalk{Addr: NewStaticAddr(0x00CC, 0x42)}
	dl, err := framer.Framing(fl)
	if err != nil {
		t.Fatalf("Framing: %v", err)
	}

	in := ddp.Datagram{
		DestNetwork: 0x1234, SrcNetwork: 0x5678, // differ → long header
		DestNode: 0x10, SrcNode: 0x20,
		DestSocket: 253, SrcSocket: 254, DDPType: 2,
		Data: []byte("over-llap"),
	}
	if err := dl.WriteDatagram(in); err != nil {
		t.Fatalf("WriteDatagram: %v", err)
	}
	out, err := dl.ReadDatagram()
	if err != nil {
		t.Fatalf("ReadDatagram: %v", err)
	}
	assertDatagramEqual(t, in, out)
}

// TestLocalTalk_ShortHeaderRoundTrip: an intra-network datagram (Dest == Src
// network, a concrete shared number) uses the LLAP short header (type 0x01). On
// the wire the network is omitted; the receiving framer reconstructs it from its
// own claimed network — which must equal the sender's for the round-trip to hold.
func TestLocalTalk_ShortHeaderRoundTrip(t *testing.T) {
	fl := inmem.Loopback(4)
	defer fl.Close()

	const net = 0x00CC
	framer := &LocalTalk{Addr: NewStaticAddr(net, 0x42)}
	dl, err := framer.Framing(fl)
	if err != nil {
		t.Fatalf("Framing: %v", err)
	}

	in := ddp.Datagram{
		DestNetwork: net, SrcNetwork: net, // same → short header
		DestNode: 0x10, SrcNode: 0x42,
		DestSocket: 123, SrcSocket: 200, DDPType: 3,
		Data: []byte("intra"),
	}
	if err := dl.WriteDatagram(in); err != nil {
		t.Fatalf("WriteDatagram: %v", err)
	}
	out, err := dl.ReadDatagram()
	if err != nil {
		t.Fatalf("ReadDatagram: %v", err)
	}
	assertDatagramEqual(t, in, out)
}

// TestLocalTalk_HeaderChoiceFollowsRouterDecision proves the framer picks the
// header form from the datagram's OWN network fields — i.e. the routing decision
// the AppleTalk router already made — not from the port's claimed network. With a
// port claiming network 0x00CC, an inter-network datagram still encodes long even
// though the port "is on" a network, and an intra-network datagram (net 0)
// encodes short even though it does not match the port's number.
func TestLocalTalk_HeaderChoiceFollowsRouterDecision(t *testing.T) {
	framer := &LocalTalk{Addr: NewStaticAddr(0x00CC, 0x42)}
	dl := framer.mustLink(t)

	cases := []struct {
		name      string
		dg        ddp.Datagram
		wantShort bool
	}{
		{"inter-network → long", ddp.Datagram{DestNetwork: 1, SrcNetwork: 2, SrcNode: 1, DestSocket: 1, SrcSocket: 1, DDPType: 1}, false},
		{"intra net 0 → short", ddp.Datagram{DestNetwork: 0, SrcNetwork: 0, SrcNode: 1, DestSocket: 1, SrcSocket: 1, DDPType: 1}, true},
		{"intra concrete → short", ddp.Datagram{DestNetwork: 9, SrcNetwork: 9, SrcNode: 1, DestSocket: 1, SrcSocket: 1, DDPType: 1}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			frame, err := dl.encode(tc.dg)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			gotType := frame[2]
			wantType := uint8(llapLongDDP)
			if tc.wantShort {
				wantType = llapShortDDP
			}
			if gotType != wantType {
				t.Fatalf("LLAP type = 0x%02X, want 0x%02X", gotType, wantType)
			}
		})
	}
}

// TestLocalTalk_StampsSourceNodeAndBroadcast: the LLAP source node is the port's
// claimed node, and a DDP datagram with no dest node (0) is sent to the LLAP
// broadcast node 0xFF.
func TestLocalTalk_StampsSourceNodeAndBroadcast(t *testing.T) {
	framer := &LocalTalk{Addr: NewStaticAddr(9, 0x42)}
	dl := framer.mustLink(t)

	frame, err := dl.encode(ddp.Datagram{DestNetwork: 9, SrcNetwork: 9, DestNode: 0, SrcNode: 0x42, DestSocket: 1, SrcSocket: 1, DDPType: 1})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if frame[0] != llapBroadcastNode {
		t.Errorf("dest node = 0x%02X, want broadcast 0x%02X", frame[0], llapBroadcastNode)
	}
	if frame[1] != 0x42 {
		t.Errorf("src node = 0x%02X, want claimed 0x42", frame[1])
	}
}

// TestLocalTalk_SkipsControlFrames proves an LLAP ENQ/ACK frame is skipped by
// ReadDatagram (not mis-parsed as DDP): a control frame followed by a real DDP
// frame yields the DDP datagram, with the control frame silently consumed.
func TestLocalTalk_SkipsControlFrames(t *testing.T) {
	fl := inmem.Loopback(8)
	defer fl.Close()

	// Inject a raw ENQ control frame, then a real long-header DDP frame.
	enq := []byte{0xFE, 0xFE, llapENQ}
	if err := fl.Write(enq); err != nil {
		t.Fatalf("write ENQ: %v", err)
	}
	framer := &LocalTalk{Addr: NewStaticAddr(0, 0x42)}
	dl, err := framer.Framing(fl)
	if err != nil {
		t.Fatalf("Framing: %v", err)
	}
	in := ddp.Datagram{DestNetwork: 1, SrcNetwork: 2, SrcNode: 1, DestSocket: 5, SrcSocket: 6, DDPType: 1, Data: []byte("x")}
	if err := dl.WriteDatagram(in); err != nil {
		t.Fatalf("WriteDatagram: %v", err)
	}
	out, err := dl.ReadDatagram()
	if err != nil {
		t.Fatalf("ReadDatagram: %v", err)
	}
	assertDatagramEqual(t, in, out)
}

// TestLocalTalk_ShortFrameRejected: a frame below the 3-byte LLAP header is not a
// datagram; decode reports ErrShortLLAP (skipped by the read loop).
func TestLocalTalk_ShortFrameRejected(t *testing.T) {
	framer := &LocalTalk{}
	dl := framer.mustLink(t)
	if _, err := dl.decode([]byte{0x01, 0x02}); err != ErrShortLLAP {
		t.Fatalf("decode of 2-byte frame = %v, want ErrShortLLAP", err)
	}
}

// TestLiveAddr proves the late-bound LiveAddr reports the unclaimed state until
// Set, then tracks its source — the seam the compose factory uses to point the
// framer at the port after the port is constructed.
func TestLiveAddr(t *testing.T) {
	var live LiveAddr
	if live.Network() != 0 || live.Node() != 0 {
		t.Fatalf("unbound LiveAddr = net %d node %d, want 0/0", live.Network(), live.Node())
	}
	live.Set(NewStaticAddr(0x00CC, 0x42))
	if live.Network() != 0x00CC || live.Node() != 0x42 {
		t.Fatalf("bound LiveAddr = net 0x%X node 0x%X, want 0x00CC/0x42", live.Network(), live.Node())
	}
	// A framer built around the LiveAddr stamps the bound node on outbound frames.
	framer := &LocalTalk{Addr: &live}
	dl := framer.mustLink(t)
	frame, err := dl.encode(ddp.Datagram{DestNetwork: 9, SrcNetwork: 9, DestNode: 0x10, DestSocket: 1, SrcSocket: 1, DDPType: 1})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if frame[1] != 0x42 {
		t.Fatalf("LLAP src node = 0x%02X, want bound 0x42", frame[1])
	}
	// Reverting to nil source returns to unclaimed.
	live.Set(nil)
	if live.Node() != 0 {
		t.Fatalf("LiveAddr after Set(nil) node = 0x%X, want 0", live.Node())
	}
}

// mustLink builds the datagram link over a throwaway loopback for encode/decode
// unit tests that don't drive the FrameLink.
func (e *LocalTalk) mustLink(t *testing.T) *ltDatagramLink {
	t.Helper()
	dl, err := e.Framing(inmem.Loopback(1))
	if err != nil {
		t.Fatalf("Framing: %v", err)
	}
	return dl.(*ltDatagramLink)
}
