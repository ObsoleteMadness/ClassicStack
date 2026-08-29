package llap

import (
	"errors"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
)

// shortDDP builds a well-formed short-header LLAP frame carrying dataLen payload
// bytes, with the DDP length field set correctly.
func shortDDP(dst, src uint8, dataLen int) []byte {
	total := ShortDDPHeaderLen + dataLen
	f := []byte{dst, src, TypeShortDDP, byte(total>>8) & 0x03, byte(total), 0xFB, 0xEC, 0x03}
	return append(f, make([]byte, dataLen)...)
}

// longDDP builds a well-formed long-header LLAP frame carrying dataLen payload
// bytes. Byte 0 also holds the 4 hop bits, exercised separately below.
func longDDP(hops uint8, dataLen int) []byte {
	total := LongDDPHeaderLen + dataLen
	f := []byte{0xFF, 0x01, TypeLongDDP,
		(hops&0x0F)<<2 | byte(total>>8)&0x03, byte(total),
		0, 0, // checksum disabled
		0, 1, 0, 2, // dest net, src net
		0xFF, 0x01, // dest node, src node
		0xFB, 0xEC, 0x03,
	}
	return append(f, make([]byte, dataLen)...)
}

func TestValidateAcceptsWellFormed(t *testing.T) {
	cases := []struct {
		name  string
		frame []byte
	}{
		{"short DDP, no data", shortDDP(0xFE, 0x01, 0)},
		{"short DDP, data", shortDDP(0xFE, 0x01, 100)},
		{"short DDP, max data", shortDDP(0xFE, 0x01, ddp.MaxDataLength)},
		{"short DDP, broadcast", shortDDP(BroadcastNode, 0x01, 8)},
		{"long DDP, no data", longDDP(0, 0)},
		{"long DDP, hops set", longDDP(7, 40)},
		{"ENQ", EncodeControl(Enq(0xFE))},
		{"ACK", EncodeControl(Ack(0x01))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := Validate(tc.frame); err != nil {
				t.Fatalf("Validate(%x) = %v, want nil", tc.frame, err)
			}
		})
	}
}

func TestValidateRejects(t *testing.T) {
	// The stale-buffer case this check exists for: a real frame header whose
	// declared length is short of the bytes actually carried.
	staleTail := append(shortDDP(0xFE, 0x01, 4), 0xDE, 0xAD, 0xBE, 0xEF)
	// The mirror case: declared length longer than the datagram (truncation).
	truncated := shortDDP(0xFE, 0x01, 40)[:20]

	cases := []struct {
		name  string
		frame []byte
		want  error
	}{
		{"empty", nil, ErrShortLLAP},
		{"runt", []byte{0xFE, 0x01}, ErrShortLLAP},
		{"unknown type", []byte{0xFE, 0x01, 0x66, 0, 0, 0, 0, 0}, ErrBadType},
		{"RTS is not carried over UDP", []byte{0xFE, 0x01, 0x84}, ErrBadType},
		{"short DDP below header", []byte{0xFE, 0x01, TypeShortDDP, 0x00, 0x04}, ErrShortDDP},
		{"long DDP below header", []byte{0xFF, 0x01, TypeLongDDP, 0x00, 0x05, 0, 0}, ErrShortDDP},
		{"stale tail past declared length", staleTail, ErrBadLength},
		{"declared length past frame", truncated, ErrBadLength},
		{"ENQ with payload", []byte{0xFE, 0xFE, TypeENQ, 0x00}, ErrControlPayload},
		{"ENQ not self-addressed", []byte{0x81, 0x00, TypeENQ}, ErrControlAddress},
		{"ACK on broadcast node", []byte{0xFF, 0xFF, TypeACK}, ErrControlAddress},
		{"ENQ on unclaimed node", []byte{0x00, 0x00, TypeENQ}, ErrControlAddress},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.frame)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Validate(%x) = %v, want %v", tc.frame, err, tc.want)
			}
		})
	}
}

// TestValidateRejectsReservedBits pins that the reserved high bits differ between
// the two header forms: a short header reserves all six above the length, a long
// header reserves only the top two (the four between are the hop count).
func TestValidateRejectsReservedBits(t *testing.T) {
	s := shortDDP(0xFE, 0x01, 8)
	s[3] |= 0x04 // a hop bit — legal in a long header, reserved in a short one
	if !errors.Is(Validate(s), ErrReservedBits) {
		t.Fatalf("short header with hop bits = %v, want ErrReservedBits", Validate(s))
	}

	l := longDDP(0, 8)
	l[3] |= 0x80 // above the hop field: reserved in both forms
	if !errors.Is(Validate(l), ErrReservedBits) {
		t.Fatalf("long header with flag bits = %v, want ErrReservedBits", Validate(l))
	}
}

// TestValidatePassesCorruptedPayload documents the limit of this check: LLAP over
// UDP has no CRC, so a frame corrupted WITHIN its declared length is
// indistinguishable from a good one here and must be caught further up.
func TestValidatePassesCorruptedPayload(t *testing.T) {
	f := shortDDP(0xFE, 0x01, 16)
	for i := ShortDDPHeaderLen + HeaderLen; i < len(f); i++ {
		f[i] = 0xA5 // garbage payload, correct length
	}
	if err := Validate(f); err != nil {
		t.Fatalf("Validate = %v, want nil (payload corruption is out of scope here)", err)
	}
}

// TestMaxFrameLen ties the constant to the largest frame Validate accepts.
func TestMaxFrameLen(t *testing.T) {
	f := longDDP(0, ddp.MaxDataLength)
	if len(f) != MaxFrameLen {
		t.Fatalf("largest long-header frame is %d bytes, MaxFrameLen = %d", len(f), MaxFrameLen)
	}
	if err := Validate(f); err != nil {
		t.Fatalf("Validate(max frame) = %v, want nil", err)
	}
}
