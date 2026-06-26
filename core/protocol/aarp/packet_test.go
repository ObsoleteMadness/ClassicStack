package aarp

import (
	"bytes"
	"testing"
)

func mac(b ...byte) [6]byte {
	var m [6]byte
	copy(m[:], b)
	return m
}

// TestPacketRoundTrip proves every opcode encodes and decodes back to the same Packet,
// and that the encoded length is the fixed EtherTalk AARP packet size.
func TestPacketRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		pkt  Packet
	}{
		{
			"probe",
			Probe(mac(0x00, 0x11, 0x22, 0x33, 0x44, 0x55), ProtoAddr{Network: 0xFE01, Node: 0x42}),
		},
		{
			"request",
			Request(mac(0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF),
				ProtoAddr{Network: 0x0001, Node: 0x10}, ProtoAddr{Network: 0x0001, Node: 0x20}),
		},
		{
			"reply",
			Reply(mac(1, 2, 3, 4, 5, 6), ProtoAddr{Network: 0x0001, Node: 0x10},
				mac(9, 8, 7, 6, 5, 4), ProtoAddr{Network: 0x0001, Node: 0x20}),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			wire := c.pkt.Encode(nil)
			if len(wire) != packetLen {
				t.Fatalf("encoded len = %d, want %d", len(wire), packetLen)
			}
			got, err := Decode(wire)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if got != c.pkt {
				t.Fatalf("round-trip mismatch:\n got %+v\nwant %+v", got, c.pkt)
			}
		})
	}
}

// TestEncodeWireLayout pins the exact bytes of a probe so a wire regression is caught:
// the 8-byte fixed header (hwType=1, protoType=0x809B, hwLen=6, protoLen=4, op=3) then
// senderHW · senderProto · targetHW(zero) · targetProto.
func TestEncodeWireLayout(t *testing.T) {
	p := Probe(mac(0x00, 0x11, 0x22, 0x33, 0x44, 0x55), ProtoAddr{Network: 0xFE01, Node: 0x42})
	want := []byte{
		0x00, 0x01, // hardware type = Ethernet
		0x80, 0x9B, // protocol type = AppleTalk
		0x06, 0x04, // hw len 6, proto len 4
		0x00, 0x03, // opcode = probe
		0x00, 0x11, 0x22, 0x33, 0x44, 0x55, // sender HW
		0x00, 0xFE, 0x01, 0x42, // sender proto: pad, net hi, net lo, node
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // target HW (zero for a probe)
		0x00, 0xFE, 0x01, 0x42, // target proto = tentative (repeated)
	}
	if got := p.Encode(nil); !bytes.Equal(got, want) {
		t.Fatalf("probe wire:\n got %x\nwant %x", got, want)
	}
}

// TestDecodeRejectsNonEtherTalk proves Decode rejects short packets and packets whose
// fixed header is not the EtherTalk AARP form.
func TestDecodeRejectsNonEtherTalk(t *testing.T) {
	good := Probe(mac(1, 2, 3, 4, 5, 6), ProtoAddr{Network: 1, Node: 2}).Encode(nil)

	if _, err := Decode(good[:packetLen-1]); err != ErrShortAARP {
		t.Fatalf("short err = %v, want ErrShortAARP", err)
	}

	badHW := append([]byte(nil), good...)
	badHW[1] = 0x06 // hardware type != Ethernet(1)
	if _, err := Decode(badHW); err != ErrBadAARP {
		t.Fatalf("bad-hwtype err = %v, want ErrBadAARP", err)
	}

	badLen := append([]byte(nil), good...)
	badLen[4] = 0x08 // hardware addr len != 6
	if _, err := Decode(badLen); err != ErrBadAARP {
		t.Fatalf("bad-hwlen err = %v, want ErrBadAARP", err)
	}
}
