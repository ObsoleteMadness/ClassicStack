package ipx

import (
	"context"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
	ipxproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx"
)

func TestParseFrameType(t *testing.T) {
	cases := []struct {
		in   string
		want FrameType
		ok   bool
	}{
		{"", FrameEthernetII, true}, // empty defaults to Ethernet II (MacIPX)
		{"ethernet_ii", FrameEthernetII, true},
		{"Ethernet II", FrameEthernetII, true},
		{"DIX", FrameEthernetII, true},
		{"802.3", FrameRaw8023, true},
		{"raw", FrameRaw8023, true},
		{"Ethernet_802.3", FrameRaw8023, true},
		{"802.2", FrameLLC8022, true},
		{"LLC", FrameLLC8022, true},
		{"  802.2  ", FrameLLC8022, true},
		{"snap", 0, false},
		{"garbage", 0, false},
	}
	for _, c := range cases {
		got, err := ParseFrameType(c.in)
		if c.ok && err != nil {
			t.Errorf("ParseFrameType(%q) unexpected error: %v", c.in, err)
			continue
		}
		if !c.ok {
			if err == nil {
				t.Errorf("ParseFrameType(%q) = %v, want error", c.in, got)
			}
			continue
		}
		if got != c.want {
			t.Errorf("ParseFrameType(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// frameTypeModel builds an enabled IPX section with an explicit ipx_frame_type.
func frameTypeModel(t *testing.T, ft string) *config.Model {
	t.Helper()
	m := config.NewModel()
	m.Set(&port.Section{SKey: Name, Iface: "eth0", IsEnabled: true, IPXFrameType: ft})
	return m
}

func TestSendDefaultsToEthernetII(t *testing.T) {
	fl := &fakeFrameLink{}
	src := [6]byte{0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0x01}
	c, _ := New(enabledModel(t), fl, src, newTestLogger()) // no ipx_frame_type set
	c.Start(context.Background())
	defer c.Stop(context.Background())

	dst := [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	if err := c.(*Port).Send(dst, sampleDatagram()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	frame := lastSent(t, fl)
	if frame[12] != 0x81 || frame[13] != 0x37 {
		t.Errorf("default ethertype = % x, want 81 37 (Ethernet II)", frame[12:14])
	}
}

func TestSendRaw8023(t *testing.T) {
	fl := &fakeFrameLink{}
	src := [6]byte{1, 2, 3, 4, 5, 6}
	c, err := New(frameTypeModel(t, "802.3"), fl, src, newTestLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.Start(context.Background())
	defer c.Stop(context.Background())

	dst := [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	if err := c.(*Port).Send(dst, sampleDatagram()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	frame := lastSent(t, fl)

	// 802.3 length-typed: the type field is the IPX body length, and the body
	// begins with the IPX datagram itself (0xFFFF "no checksum" magic).
	ipxBytes, _ := sampleDatagram().Encode(nil)
	gotLen := int(frame[12])<<8 | int(frame[13])
	if gotLen != len(ipxBytes) {
		t.Errorf("802.3 length field = %d, want %d", gotLen, len(ipxBytes))
	}
	if gotLen > 0x05DC {
		t.Errorf("802.3 length %d exceeds 0x05DC (would look like an EtherType)", gotLen)
	}
	if frame[14] != 0xFF || frame[15] != 0xFF {
		t.Errorf("802.3 body[0:2] = % x, want ff ff (IPX magic)", frame[14:16])
	}

	// The datagram must decode back identically off the wire.
	assertRoundTrips(t, c.(*Port), fl, frame)
}

func TestSendLLC8022(t *testing.T) {
	fl := &fakeFrameLink{}
	src := [6]byte{1, 2, 3, 4, 5, 6}
	c, err := New(frameTypeModel(t, "802.2"), fl, src, newTestLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.Start(context.Background())
	defer c.Stop(context.Background())

	dst := [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	if err := c.(*Port).Send(dst, sampleDatagram()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	frame := lastSent(t, fl)

	// 802.2 LLC: length-typed, then the LLC UI header (E0 E0 03), then IPX.
	ipxBytes, _ := sampleDatagram().Encode(nil)
	wantLen := len(llcIPX) + len(ipxBytes)
	gotLen := int(frame[12])<<8 | int(frame[13])
	if gotLen != wantLen {
		t.Errorf("802.2 length field = %d, want %d", gotLen, wantLen)
	}
	if frame[14] != 0xE0 || frame[15] != 0xE0 || frame[16] != 0x03 {
		t.Errorf("802.2 LLC header = % x, want e0 e0 03", frame[14:17])
	}

	assertRoundTrips(t, c.(*Port), fl, frame)
}

func TestBadFrameTypeRejected(t *testing.T) {
	_, err := New(frameTypeModel(t, "snap"), &fakeFrameLink{}, [6]byte{}, newTestLogger())
	if err == nil {
		t.Fatal("New with bad ipx_frame_type must error")
	}
}

// lastSent returns the most recently written frame, failing if none was sent.
func lastSent(t *testing.T, fl *fakeFrameLink) []byte {
	t.Helper()
	fl.mu.Lock()
	defer fl.mu.Unlock()
	if len(fl.sent) == 0 {
		t.Fatal("no frame sent")
	}
	return fl.sent[len(fl.sent)-1]
}

// assertRoundTrips strips the encapsulation off a frame the port just sent and
// checks the IPX datagram decodes — every framing this port emits must also be
// accepted by its own inbound path.
func assertRoundTrips(t *testing.T, _ *Port, _ *fakeFrameLink, frame []byte) {
	t.Helper()
	payload, ok := stripEncapsulation(frame)
	if !ok {
		t.Fatalf("stripEncapsulation rejected our own % x frame", frame[12:16])
	}
	if _, err := ipxproto.Decode(payload); err != nil {
		t.Fatalf("decode of stripped payload failed: %v", err)
	}
}
