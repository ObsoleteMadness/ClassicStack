package ipx

import (
	"context"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
	ipxproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx"
)

// raw8023Frame wraps an IPX datagram in a raw 802.3 frame (length-typed, IPX 0xFFFF magic
// as the first body bytes — no LLC header), the framing a NetWare 3.x server defaults to.
func raw8023Frame(dst, src [6]byte, ipxBytes []byte) []byte {
	frame := make([]byte, 0, ethHdrLen+len(ipxBytes))
	frame = append(frame, dst[:]...)
	frame = append(frame, src[:]...)
	frame = append(frame, byte(len(ipxBytes)>>8), byte(len(ipxBytes)))
	frame = append(frame, ipxBytes...)
	return frame
}

// TestReplyMirrorsReceivedFrameType asserts the port answers a unicast in the SAME frame
// type the peer's request arrived in: a request received in raw 802.3 draws a raw-802.3
// reply, not the Ethernet-II default. This is the multi-frame-type behaviour that lets a
// single server talk to clients bound on different framings at once.
func TestReplyMirrorsReceivedFrameType(t *testing.T) {
	fl := &fakeFrameLink{}
	srcMAC := [6]byte{0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0x01}
	c, err := New(enabledModel(t), fl, srcMAC, newTestLogger()) // default frame type = Ethernet II
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.Start(context.Background())
	defer c.Stop(context.Background())
	p := c.(*Port)

	// A peer speaks to us in raw 802.3.
	peer := [6]byte{0x02, 0x11, 0x22, 0x33, 0x44, 0x55}
	ipxBytes, _ := sampleDatagram().Encode(nil)
	p.onFrame(raw8023Frame(srcMAC, peer, ipxBytes))

	// The reply to that peer must go out in raw 802.3, not the Ethernet-II default.
	if err := p.Send(peer, sampleDatagram()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	frame := lastSent(t, fl)
	etherType := int(frame[12])<<8 | int(frame[13])
	if etherType > 0x05DC {
		t.Fatalf("reply etherType %#x is Ethernet II, want a length-typed 802.3 frame", etherType)
	}
	if frame[14] != 0xFF || frame[15] != 0xFF {
		t.Errorf("reply body[0:2] = % x, want ff ff (raw 802.3 IPX magic)", frame[14:16])
	}
}

// TestUnheardPeerUsesDefaultFrameType asserts a unicast to a peer we have NOT heard from
// falls back to the configured default frame type (Ethernet II here).
func TestUnheardPeerUsesDefaultFrameType(t *testing.T) {
	fl := &fakeFrameLink{}
	srcMAC := [6]byte{1, 2, 3, 4, 5, 6}
	c, _ := New(enabledModel(t), fl, srcMAC, newTestLogger())
	c.Start(context.Background())
	defer c.Stop(context.Background())

	dst := [6]byte{0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F}
	if err := c.(*Port).Send(dst, sampleDatagram()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	frame := lastSent(t, fl)
	if frame[12] != 0x81 || frame[13] != 0x37 {
		t.Errorf("unheard-peer reply etherType = % x, want 81 37 (Ethernet II default)", frame[12:14])
	}
}

// multiFrameModel builds an enabled IPX section advertising on several frame types.
func multiFrameModel(t *testing.T, types ...string) *config.Model {
	t.Helper()
	m := config.NewModel()
	m.Set(&port.Section{SKey: Name, Iface: "eth0", IsEnabled: true, IPXFrameTypes: types})
	return m
}

// TestBroadcastFansOutPerFrameType asserts a broadcast IPX datagram (e.g. a SAP advert) is
// emitted once per configured advertised frame type, so clients on any framing receive it.
func TestBroadcastFansOutPerFrameType(t *testing.T) {
	fl := &fakeFrameLink{}
	srcMAC := [6]byte{1, 2, 3, 4, 5, 6}
	c, err := New(multiFrameModel(t, "802.3", "802.2", "ethernet_ii"), fl, srcMAC, newTestLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.Start(context.Background())
	defer c.Stop(context.Background())
	p := c.(*Port)

	if got := len(p.FrameTypes()); got != 3 {
		t.Fatalf("FrameTypes len = %d, want 3", got)
	}

	if err := p.Send(broadcastMAC, sampleDatagram()); err != nil {
		t.Fatalf("Send broadcast: %v", err)
	}

	fl.mu.Lock()
	sent := append([][]byte(nil), fl.sent...)
	fl.mu.Unlock()
	if len(sent) != 3 {
		t.Fatalf("broadcast emitted %d frames, want 3 (one per frame type)", len(sent))
	}
	// Each emitted frame must decode back to the same datagram regardless of framing.
	for i, frame := range sent {
		payload, _, ok := Strip(frame)
		if !ok {
			t.Fatalf("frame %d not a recognised IPX encapsulation: % x", i, frame[12:16])
		}
		if _, err := ipxproto.Decode(payload); err != nil {
			t.Fatalf("frame %d decode: %v", i, err)
		}
	}
}
