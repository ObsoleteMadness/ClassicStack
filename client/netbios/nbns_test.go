package netbios

import (
	"encoding/binary"
	"net"
	"testing"

	browserproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/browser"
	nb "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbios"
)

func TestEncodeDecodeNBNSName(t *testing.T) {
	name := nb.NewName("WORKGROUP", browserproto.NameTypeMasterBrowser)
	enc := encodeNBNSName(name)
	if enc[0] != 32 || enc[33] != 0 {
		t.Fatalf("label framing = %d ... %d, want 32 ... 0", enc[0], enc[33])
	}
	pkt := append([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, enc...)
	got := decodeNBNSName(pkt, 12)
	if got != "WORKGROUP" {
		t.Fatalf("decode = %q, want WORKGROUP", got)
	}
}

func TestMarshalParseNBNSQueryResponse(t *testing.T) {
	name := nb.NewName("WORKGROUP", browserproto.NameTypeMasterBrowser)
	id := uint16(0x4353)
	q := marshalNBNSQuery(id, name)
	if binary.BigEndian.Uint16(q[0:2]) != id {
		t.Fatalf("id = %x", q[:2])
	}

	// Build a minimal positive response: copy the question name, one NB answer
	// pointing at 192.168.0.10.
	resp := make([]byte, 0, len(q)+nbnsHeaderLen+nbnsEncodedName+16)
	hdr := make([]byte, nbnsHeaderLen)
	binary.BigEndian.PutUint16(hdr[0:2], id)
	binary.BigEndian.PutUint16(hdr[2:4], 0x8400) // response, authoritative
	binary.BigEndian.PutUint16(hdr[6:8], 1)      // ANCOUNT
	resp = append(resp, hdr...)
	resp = append(resp, encodeNBNSName(name)...)
	rr := make([]byte, 10+nbnsRDataLen)
	binary.BigEndian.PutUint16(rr[0:2], nbnsTypeNB)
	binary.BigEndian.PutUint16(rr[2:4], nbnsClassIN)
	binary.BigEndian.PutUint16(rr[8:10], nbnsRDataLen)
	copy(rr[12:16], net.IPv4(192, 168, 0, 10).To4())
	resp = append(resp, rr...)

	ans := parseNBNSAnswers(resp, id)
	if len(ans) != 1 || ans[0].Name != "WORKGROUP" || !ans[0].IP.Equal(net.IPv4(192, 168, 0, 10)) {
		t.Fatalf("answers = %+v", ans)
	}
}
