// Package snefru implements the Snefru-128 variant the classic Mac netboot ROM
// uses to authenticate downloaded boot images.
//
// This is NOT textbook Snefru: Apple's generate_hash (SuperMario
// os/netboot/Hash/Hash.c) seeds the third whitening word with the input BIT
// length and post-increments it per 512-bit block and per fold. Port of Elliot
// Nunn's snefru_hash.py (NetBoot project), which is validated against the ROM;
// the S-boxes are Ralph C. Merkle's / Xerox's (see sboxes.go).
//
// Ring: CORE (stdlib only, reflection-free).
//
// Reference: spec/19-netboot.md ("Snefru-128 self-authentication").
package snefru

import (
	"errors"
	"math/bits"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
)

// Size is the hash output length in bytes.
const Size = 16

// BlockSize is the input granularity: Sum input must be a multiple of it.
const BlockSize = 64

// TrailerSize is the self-authentication trailer a netboot payload carries:
// 48 zero bytes + the Size-byte hash of everything before the trailer.
const TrailerSize = 64

// ErrInputSize is returned by Sum for input not a multiple of BlockSize.
var ErrInputSize = errors.New("snefru: input length must be a multiple of 64 bytes")

// hash512 is one Snefru compression: 16 input words are whitened with p0/p1/p2,
// stirred through four S-box passes (lookup shifts 0,16,24,8 with the looked-up
// word rotated left by the same shift — the pre-rotated boxes of the reference),
// and folded back onto the first four input words.
func hash512(in *[16]uint32, p0, p1, p2 uint32) [4]uint32 {
	edit := *in
	edit[0] ^= p0
	edit[1] ^= p1
	edit[2] ^= p2

	for _, shift := range [4]int{0, 16, 24, 8} {
		for idx := range 16 {
			b := (edit[idx] >> uint(shift)) & 0xFF
			var v uint32
			if idx%4 < 2 {
				v = sbox0[b]
			} else {
				v = sbox1[b]
			}
			v = bits.RotateLeft32(v, shift)
			edit[(idx+1)%16] ^= v
			edit[(idx+15)%16] ^= v
		}
	}

	edit[14] ^= p0
	edit[13] ^= p1
	edit[12] ^= p2

	return [4]uint32{
		in[0] ^ edit[15],
		in[1] ^ edit[14],
		in[2] ^ edit[13],
		in[3] ^ edit[12],
	}
}

// Sum computes the netboot Snefru-128 digest of in, whose length must be a
// multiple of BlockSize. Mirrors snefru_hash.py's snefru() exactly, including
// the p2 = bit-length seed and its per-block/per-fold increments.
func Sum(in []byte) ([Size]byte, error) {
	var out [Size]byte
	if len(in)%BlockSize != 0 {
		return out, ErrInputSize
	}

	var p0, p1 uint32
	p2 := uint32(len(in) * 8)

	var temp [16]uint32
	loc := 0
	for off := 0; off < len(in); off += BlockSize {
		var grist [16]uint32
		for i := range 16 {
			grist[i] = bp.BE32(in[off+4*i : off+4*i+4])
		}
		h := hash512(&grist, p0, p1, p2)
		copy(temp[loc:loc+4], h[:])
		p2++
		loc += 4

		if loc >= 16 {
			h = hash512(&temp, p0, p1, p2)
			copy(temp[0:4], h[:])
			loc = 4
			p2++
		}
	}

	final := hash512(&temp, p0, p1, p2)
	var buf []byte
	for _, w := range final {
		buf = bp.AppendBE32(buf, w)
	}
	copy(out[:], buf)
	return out, nil
}

// AppendTrailer pads payload with zeros so that, after the trailer, its length
// is a multiple of align and at least 2*align (1-block payloads crash the
// client), then appends the 64-byte self-authentication trailer: 48 zero bytes
// + the hash of everything before the trailer. align is the ABP block size the
// payload will be served with and must be a multiple of BlockSize.
// Mirrors snefru_hash.py's CLI (--align) plus append_snefru.
func AppendTrailer(payload []byte, align int) ([]byte, error) {
	if align <= 0 || align%BlockSize != 0 {
		return nil, ErrInputSize
	}
	out := append([]byte(nil), payload...)
	for len(out)%align != align-TrailerSize || len(out)+TrailerSize < 2*align {
		out = append(out, 0)
	}
	sum, err := Sum(out)
	if err != nil {
		return nil, err
	}
	out = append(out, make([]byte, TrailerSize-Size)...)
	out = append(out, sum[:]...)
	return out, nil
}

// HasValidTrailer reports whether payload already ends in a valid
// self-authentication trailer (hash of payload[:len-64] in the last 16 bytes).
// Used to serve pre-hashed payloads (e.g. built by the NetBoot repo's
// Makefile) untouched.
func HasValidTrailer(payload []byte) bool {
	if len(payload) < TrailerSize+BlockSize || (len(payload)-TrailerSize)%BlockSize != 0 {
		return false
	}
	body := payload[:len(payload)-TrailerSize]
	sum, err := Sum(body)
	if err != nil {
		return false
	}
	tail := payload[len(payload)-Size:]
	for i := range sum {
		if sum[i] != tail[i] {
			return false
		}
	}
	return true
}
