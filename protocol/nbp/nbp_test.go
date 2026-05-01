package nbp

import (
	"bytes"
	"testing"
)

func TestParsePacketLkUp(t *testing.T) {
	t.Parallel()
	// LkUp for "Foo:AFPServer@Eng" with reply addr 1.2.3.42 sock 4 enum 5
	obj, typ, zone := []byte("Foo"), []byte("AFPServer"), []byte("Eng")
	data := []byte{
		(CtrlLkUp << 4) | 1, // function | tuple count
		0x77,                // NBPID
		0x00, 0x01,          // network 1
		0x02,                // node
		0x03,                // socket
		0x04,                // enumerator
		byte(len(obj)),
	}
	data = append(data, obj...)
	data = append(data, byte(len(typ)))
	data = append(data, typ...)
	data = append(data, byte(len(zone)))
	data = append(data, zone...)

	pkt, err := ParsePacket(data)
	if err != nil {
		t.Fatalf("ParsePacket: %v", err)
	}
	if pkt.Function != CtrlLkUp || pkt.TupleCount != 1 || pkt.NBPID != 0x77 {
		t.Fatalf("header mismatch: %+v", pkt)
	}
	if pkt.Tuple.Network != 1 || pkt.Tuple.Node != 2 || pkt.Tuple.Socket != 3 || pkt.Tuple.Enumerator != 4 {
		t.Fatalf("tuple addr mismatch: %+v", pkt.Tuple)
	}
	if !bytes.Equal(pkt.Tuple.Object, obj) || !bytes.Equal(pkt.Tuple.Type, typ) || !bytes.Equal(pkt.Tuple.Zone, zone) {
		t.Fatalf("tuple name mismatch: %+v", pkt.Tuple)
	}
}

func TestParsePacketEmptyZoneBecomesWildcard(t *testing.T) {
	t.Parallel()
	obj, typ := []byte("X"), []byte("Y")
	data := []byte{(CtrlBrRq << 4) | 1, 0, 0, 0, 0, 0, 0, byte(len(obj))}
	data = append(data, obj...)
	data = append(data, byte(len(typ)))
	data = append(data, typ...)
	data = append(data, 0) // zoneLen = 0
	pkt, err := ParsePacket(data)
	if err != nil {
		t.Fatalf("ParsePacket: %v", err)
	}
	if string(pkt.Tuple.Zone) != "*" {
		t.Fatalf("expected zone wildcard, got %q", pkt.Tuple.Zone)
	}
}

func TestParsePacketMalformed(t *testing.T) {
	t.Parallel()
	cases := [][]byte{
		nil,
		{0x10, 0, 0, 0, 0, 0, 0}, // <8 bytes
		{(CtrlLkUp << 4) | 1, 0, 0, 0, 0, 0, 0, 0}, // objLen=0
	}
	for i, c := range cases {
		if _, err := ParsePacket(c); err == nil {
			t.Fatalf("case %d: expected error", i)
		}
	}
}

func TestBuildLkUpRplyRoundTrip(t *testing.T) {
	t.Parallel()
	obj, typ, zone := []byte("Server"), []byte("AFPServer"), []byte("Mktg")
	out := BuildLkUpRply(0x42, 0x1234, 0x55, 0x66, obj, typ, zone)
	pkt, err := ParsePacket(out)
	if err != nil {
		t.Fatalf("ParsePacket: %v", err)
	}
	if pkt.Function != CtrlLkUpRply || pkt.NBPID != 0x42 {
		t.Fatalf("header: %+v", pkt)
	}
	if pkt.Tuple.Network != 0x1234 || pkt.Tuple.Node != 0x55 || pkt.Tuple.Socket != 0x66 {
		t.Fatalf("addr: %+v", pkt.Tuple)
	}
	if !bytes.Equal(pkt.Tuple.Object, obj) || !bytes.Equal(pkt.Tuple.Type, typ) || !bytes.Equal(pkt.Tuple.Zone, zone) {
		t.Fatalf("name: %+v", pkt.Tuple)
	}
}

func TestNameMatch(t *testing.T) {
	t.Parallel()
	if !NameMatch([]byte{NameWildcard}, []byte("anything")) {
		t.Fatal("= should match anything")
	}
	if !NameMatch([]byte("Foo"), []byte("foo")) {
		t.Fatal("name match should be case-insensitive")
	}
	if NameMatch([]byte("Foo"), []byte("Bar")) {
		t.Fatal("name mismatch should fail")
	}
}

func TestZoneMatch(t *testing.T) {
	t.Parallel()
	if !ZoneMatch([]byte{ZoneWildcard}, []byte("anything")) {
		t.Fatal("* should match anything")
	}
	if !ZoneMatch([]byte("Eng"), []byte("eng")) {
		t.Fatal("zone match should be case-insensitive")
	}
}
