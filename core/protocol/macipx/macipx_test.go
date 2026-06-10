package macipx

import (
	"bytes"
	"testing"
)

func TestDecodeFrameOpcodeSplit(t *testing.T) {
	op, rest, err := DecodeFrame([]byte{byte(OpcodeListen), 0xAA, 0xBB})
	if err != nil {
		t.Fatalf("DecodeFrame: %v", err)
	}
	if op != OpcodeListen {
		t.Errorf("opcode = 0x%02x, want 0x%02x", byte(op), byte(OpcodeListen))
	}
	if !bytes.Equal(rest, []byte{0xAA, 0xBB}) {
		t.Errorf("rest = %x, want aabb", rest)
	}
	if _, _, err := DecodeFrame(nil); err != ErrEmptyFrame {
		t.Errorf("empty frame err = %v, want ErrEmptyFrame", err)
	}
}

func TestEncodeDataPrefixesOpcode(t *testing.T) {
	got := EncodeData([]byte{0x01, 0x02, 0x03})
	want := []byte{byte(OpcodeData), 0x01, 0x02, 0x03}
	if !bytes.Equal(got, want) {
		t.Errorf("EncodeData = %x, want %x", got, want)
	}
}

// TestRegisterRoundTrip exercises the spec example: request blob "00 02 00 00 00
// 01" assigning node 7a:00:00:00:01:01 → wire 23 00 02 00 00 00 01 00 01 01.
func TestRegisterRoundTrip(t *testing.T) {
	req := [6]byte{0x00, 0x02, 0x00, 0x00, 0x00, 0x01}
	node := AssignedNodeForDDP(1, 1)
	if node != [6]byte{0x7A, 0x00, 0x00, 0x00, 0x01, 0x01} {
		t.Fatalf("AssignedNodeForDDP(1,1) = %x, want 7a000000010 1", node)
	}
	frame := EncodeRegisterReply(req, node)
	want := []byte{0x23, 0x00, 0x02, 0x00, 0x00, 0x00, 0x01, 0x00, 0x01, 0x01}
	if !bytes.Equal(frame, want) {
		t.Fatalf("reply = %x, want %x", frame, want)
	}

	// Decode the request blob and the assigned node back out (skipping the opcode).
	gotReq, err := DecodeRegisterRequest(frame[1:])
	if err != nil {
		t.Fatalf("DecodeRegisterRequest: %v", err)
	}
	if gotReq != req {
		t.Errorf("decoded request = %x, want %x", gotReq, req)
	}
	gotNode, err := DecodeRegisterReply(frame[1:])
	if err != nil {
		t.Fatalf("DecodeRegisterReply: %v", err)
	}
	if gotNode != node {
		t.Errorf("decoded node = %x, want %x", gotNode, node)
	}
}

func TestDecodeListenMultiEntry(t *testing.T) {
	// Two 8-byte (node, socket) pairs: broadcast node + sockets 0x0456 and 0xDEAD.
	bcast := []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	payload := append(append([]byte{}, bcast...), 0x04, 0x56)
	payload = append(payload, bcast...)
	payload = append(payload, 0xDE, 0xAD)

	entries, err := DecodeListen(payload)
	if err != nil {
		t.Fatalf("DecodeListen: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	if entries[0].Socket != [2]byte{0x04, 0x56} || entries[1].Socket != [2]byte{0xDE, 0xAD} {
		t.Errorf("sockets = %x %x, want 0456 dead", entries[0].Socket, entries[1].Socket)
	}
	if _, err := DecodeListen([]byte{0x01, 0x02, 0x03}); err != ErrListenAlign {
		t.Errorf("misaligned listen err = %v, want ErrListenAlign", err)
	}
}
