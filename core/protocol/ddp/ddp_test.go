package ddp

import (
	"bytes"
	"errors"
	"testing"
)

func sample() Datagram {
	return Datagram{
		Hops:        2,
		DestNetwork: 0x0102,
		SrcNetwork:  0x0304,
		DestNode:    0x80,
		SrcNode:     0x81,
		DestSocket:  0xFB, // 251 (NBP-ish)
		SrcSocket:   0xFE,
		DDPType:     0x02,
		Data:        []byte{0xDE, 0xAD, 0xBE, 0xEF},
	}
}

func TestEncodeGolden(t *testing.T) {
	d := sample()
	got, err := d.Encode(nil)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// Hand-built long-header wire form (checksum disabled = 0x0000).
	// length = 13 header + 4 data = 17 = 0x11; high 2 bits = 0, low byte = 0x11.
	// byte0 = hops<<2 | lengthHigh = (2<<2)|0 = 0x08.
	want := []byte{
		0x08, 0x11, // flags/hops + length
		0x00, 0x00, // checksum (disabled)
		0x01, 0x02, // dest network
		0x03, 0x04, // src network
		0x80,                   // dest node
		0x81,                   // src node
		0xFB,                   // dest socket
		0xFE,                   // src socket
		0x02,                   // DDP type
		0xDE, 0xAD, 0xBE, 0xEF, // data
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("Encode mismatch\n got: % X\nwant: % X", got, want)
	}
}

func TestRoundTrip(t *testing.T) {
	d := sample()
	enc, err := d.Encode(nil)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := Decode(enc)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if got.Hops != d.Hops || got.DestNetwork != d.DestNetwork || got.SrcNetwork != d.SrcNetwork ||
		got.DestNode != d.DestNode || got.SrcNode != d.SrcNode || got.DestSocket != d.DestSocket ||
		got.SrcSocket != d.SrcSocket || got.DDPType != d.DDPType {
		t.Fatalf("header round-trip mismatch:\n got %+v\nwant %+v", got, d)
	}
	if !bytes.Equal(got.Data, d.Data) {
		t.Fatalf("data round-trip mismatch: got % X want % X", got.Data, d.Data)
	}
}

func TestEncodeAppendsToDst(t *testing.T) {
	prefix := []byte{0xAA, 0xBB}
	out, err := sample().Encode(prefix)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.HasPrefix(out, prefix) {
		t.Fatalf("Encode must append to dst, keeping the prefix; got % X", out)
	}
}

func TestDecodeErrors(t *testing.T) {
	if _, err := Decode([]byte{0x00}); !errors.Is(err, ErrShort) {
		t.Fatalf("short buffer: want ErrShort, got %v", err)
	}

	enc, _ := sample().Encode(nil)

	bad := append([]byte(nil), enc...)
	bad[0] |= 0xC0 // set the reserved high bits → invalid long header
	if _, err := Decode(bad); !errors.Is(err, ErrBadHeader) {
		t.Fatalf("bad header bits: want ErrBadHeader, got %v", err)
	}

	short := enc[:len(enc)-1] // length field now disagrees with buffer length
	if _, err := Decode(short); !errors.Is(err, ErrBadLength) {
		t.Fatalf("length mismatch: want ErrBadLength, got %v", err)
	}
}

func TestEncodeTooLong(t *testing.T) {
	d := sample()
	d.Data = make([]byte, MaxDataLength+1)
	if _, err := d.Encode(nil); !errors.Is(err, ErrTooLong) {
		t.Fatalf("oversized data: want ErrTooLong, got %v", err)
	}
}

// TestChecksumVerified asserts a corrupted payload under a set checksum is
// rejected, and that a correct checksum is accepted.
func TestChecksumVerified(t *testing.T) {
	enc, _ := sample().Encode(nil)
	// Manually set a valid checksum over the bytes following the checksum field.
	sum := checksum(enc[4:])
	enc[2] = byte(sum >> 8)
	enc[3] = byte(sum)
	if _, err := Decode(enc); err != nil {
		t.Fatalf("valid checksum should decode: %v", err)
	}
	enc[len(enc)-1] ^= 0xFF // corrupt the payload
	if _, err := Decode(enc); !errors.Is(err, ErrBadLength) {
		t.Fatalf("corrupt payload under checksum: want ErrBadLength, got %v", err)
	}
}
