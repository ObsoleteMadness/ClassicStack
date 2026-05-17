package macipx

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/protocol/ipx"
)

// hexBytes is a tiny helper so each test reads like the pcap payload column.
func hexBytes(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("invalid hex %q: %v", s, err)
	}
	return b
}

// TestDecodeFrame_RegisterRequest exercises the 7-byte client →
// gateway address-assignment request: opcode 0x20 followed by the
// 6-byte request blob.
func TestDecodeFrame_RegisterRequest(t *testing.T) {
	payload := hexBytes(t, "20000200000001")
	op, rest, err := DecodeFrame(payload)
	if err != nil {
		t.Fatalf("DecodeFrame: %v", err)
	}
	if op != OpcodeRegisterReq {
		t.Fatalf("opcode = 0x%02x, want 0x20", op)
	}
	want := hexBytes(t, "000200000001")
	if !bytes.Equal(rest, want) {
		t.Fatalf("rest = %x, want %x", rest, want)
	}
}

// TestDecodeFrame_RegisterReply exercises the gateway → client register
// reply: opcode 0x23, the 6-byte request blob echoed back, then the
// low 3 bytes of the assigned IPX node. The full node is the
// MacIPXNodePrefix followed by those 3 bytes.
func TestDecodeFrame_RegisterReply(t *testing.T) {
	payload := hexBytes(t, "23000200000001000101")
	op, rest, err := DecodeFrame(payload)
	if err != nil {
		t.Fatalf("DecodeFrame: %v", err)
	}
	if op != OpcodeRegisterRsp {
		t.Fatalf("opcode = 0x%02x, want 0x23", op)
	}
	node, err := DecodeRegisterReply(rest)
	if err != nil {
		t.Fatalf("DecodeRegisterReply: %v", err)
	}
	want := [6]byte{0x7A, 0x00, 0x00, 0x00, 0x01, 0x01}
	if node != want {
		t.Fatalf("assigned node = %x, want %x", node, want)
	}
}

// TestEncodeRegisterReply round-trips a known-good reply.
func TestEncodeRegisterReply(t *testing.T) {
	req := [6]byte{0x00, 0x02, 0x00, 0x00, 0x00, 0x01}
	node := [6]byte{0x7A, 0x00, 0x00, 0x00, 0x01, 0x01}
	got := EncodeRegisterReply(req, node)
	want := hexBytes(t, "23000200000001000101")
	if !bytes.Equal(got, want) {
		t.Fatalf("EncodeRegisterReply = %x, want %x", got, want)
	}
}

// TestAssignedNodeForDDP confirms the deterministic encoding the
// gateway uses to map a DDP source address to an IPX node:
// 7A:00:00:00:<AT_net_low>:<AT_node>.
func TestAssignedNodeForDDP(t *testing.T) {
	cases := []struct {
		name string
		net  uint16
		node uint8
		want [6]byte
	}{
		{"AT 1.1", 1, 1, [6]byte{0x7A, 0, 0, 0, 0x01, 0x01}},
		{"AT 3.62", 3, 0x3E, [6]byte{0x7A, 0, 0, 0, 0x03, 0x3E}},
	}
	for _, tc := range cases {
		got := AssignedNodeForDDP(tc.net, tc.node)
		if got != tc.want {
			t.Errorf("%s: got %x, want %x", tc.name, got, tc.want)
		}
	}
}

// TestDecodeFrame_EncapsulatedIPX exercises an opcode-0x00 frame
// carrying a Mac-to-NetWare PEP packet (IPX type 4) from a registered
// client (src node 7a:00:00:00:01:01). The remainder of the frame must
// parse cleanly via protocol/ipx.
func TestDecodeFrame_EncapsulatedIPX(t *testing.T) {
	payload := hexBytes(t, "00ffff002e000400000000ffffffffffff869b000000007a0000000101869bffffffff000000000001000200000000")
	op, rest, err := DecodeFrame(payload)
	if err != nil {
		t.Fatalf("DecodeFrame: %v", err)
	}
	if op != OpcodeData {
		t.Fatalf("opcode = 0x%02x, want 0x00", op)
	}
	dg, err := ipx.Decode(rest)
	if err != nil {
		t.Fatalf("ipx.Decode: %v", err)
	}
	if dg.Length != 46 {
		t.Fatalf("ipx length = %d, want 46", dg.Length)
	}
	if dg.Type != 4 {
		t.Fatalf("ipx type = %d, want 4 (PEP)", dg.Type)
	}
	wantSrcNode := [6]byte{0x7A, 0, 0, 0, 0x01, 0x01}
	if dg.SrcNode != wantSrcNode {
		t.Fatalf("ipx src node = %x, want %x", dg.SrcNode, wantSrcNode)
	}
	wantSrcSock := [2]byte{0x86, 0x9B}
	if dg.SrcSock != wantSrcSock {
		t.Fatalf("ipx src sock = %x, want 869b", dg.SrcSock)
	}
}

// TestDecodeFrame_Listen exercises an opcode-0x10 frame registering a
// single (broadcast-node, IPX-socket) pair.
func TestDecodeFrame_Listen(t *testing.T) {
	payload := hexBytes(t, "10ffffffffffff0456")
	op, rest, err := DecodeFrame(payload)
	if err != nil {
		t.Fatalf("DecodeFrame: %v", err)
	}
	if op != OpcodeListen {
		t.Fatalf("opcode = 0x%02x, want 0x10", op)
	}
	want := hexBytes(t, "ffffffffffff0456")
	if !bytes.Equal(rest, want) {
		t.Fatalf("rest = %x, want %x", rest, want)
	}
}

// TestDecodeListen_MultiplePairs exercises a 0x10 frame that registers
// two (broadcast-node, IPX-socket) pairs in a single frame — the
// NetWare diagnostic socket (0x0456) plus a Duke3D-style game socket
// (0xDEAD).
func TestDecodeListen_MultiplePairs(t *testing.T) {
	payload := hexBytes(t, "10ffffffffffff0456ffffffffffffdead")
	op, rest, err := DecodeFrame(payload)
	if err != nil {
		t.Fatalf("DecodeFrame: %v", err)
	}
	if op != OpcodeListen {
		t.Fatalf("opcode = 0x%02x, want 0x10", op)
	}
	entries, err := DecodeListen(rest)
	if err != nil {
		t.Fatalf("DecodeListen: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	if entries[0].Socket != [2]byte{0x04, 0x56} {
		t.Errorf("entries[0].Socket = %x, want 0456", entries[0].Socket)
	}
	if entries[1].Socket != [2]byte{0xDE, 0xAD} {
		t.Errorf("entries[1].Socket = %x, want dead", entries[1].Socket)
	}
}

func TestDecodeFrame_Empty(t *testing.T) {
	if _, _, err := DecodeFrame(nil); err == nil {
		t.Fatal("DecodeFrame(nil) = nil error, want ErrEmptyFrame")
	}
}

func TestEncodeData_RoundTrip(t *testing.T) {
	ipxBytes := hexBytes(t, "ffff0028000100000000ffffffffffff04530000000000000000010140000001ffffffffffffffff")
	frame := EncodeData(ipxBytes)
	if frame[0] != byte(OpcodeData) {
		t.Fatalf("frame[0] = 0x%02x, want 0x00", frame[0])
	}
	if !bytes.Equal(frame[1:], ipxBytes) {
		t.Fatalf("frame[1:] differs from input")
	}
}
