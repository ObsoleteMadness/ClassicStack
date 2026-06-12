package ddp

import (
	"errors"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
)

// MaxDataLength is the maximum DDP payload, per the AppleTalk spec (and matching
// the legacy protocol/ddp implementation this codec mirrors on the wire).
const MaxDataLength = 586

// headerLen is the long-header DDP header size: 2 (flags+length) + 2 (checksum)
// + 2 (dest net) + 2 (src net) + 1 (dest node) + 1 (src node) + 1 (dest socket)
// + 1 (src socket) + 1 (DDP type) = 13 bytes.
const headerLen = 13

// Datagram is a decoded DDP packet (long-header form). Fields use fixed-width types; Data is
// the caller-owned payload slice. Keep it a value type to avoid per-packet heap allocation.
type Datagram struct {
	Hops        uint8
	DestNetwork uint16
	SrcNetwork  uint16
	DestNode    uint8
	SrcNode     uint8
	DestSocket  uint8
	SrcSocket   uint8
	DDPType     uint8
	Data        []byte
}

var (
	// ErrShort is returned by Decode when b is smaller than a long header.
	ErrShort = errors.New("ddp: datagram shorter than long header")
	// ErrBadHeader is returned by Decode when the long-header flag bits are set.
	ErrBadHeader = errors.New("ddp: invalid long DDP header")
	// ErrBadLength is returned when the encoded length field disagrees with the
	// buffer length or exceeds the maximum.
	ErrBadLength = errors.New("ddp: invalid long DDP length")
	// ErrTooLong is returned by Encode when Data exceeds MaxDataLength.
	ErrTooLong = errors.New("ddp: data exceeds MaxDataLength")
)

// checksum is the AppleTalk DDP checksum over the bytes following the checksum
// field. Kept here so Decode can validate a non-zero checksum and so callers can
// compute one; it mirrors the legacy protocol/ddp.Checksum exactly.
func checksum(data []byte) uint16 {
	var v uint16
	for _, b := range data {
		v += uint16(b)
		v = (v&0x7FFF)<<1 | (v>>15)&1
	}
	if v == 0 {
		return 0xFFFF
	}
	return v
}

// Encode appends the long-header wire form to dst and returns it (append-style → caller
// controls alloc). The checksum field is left zero (checksum disabled), matching the legacy
// AsLongHeaderBytes(false) behaviour; a zero checksum is valid on the wire and Decode skips
// verification for it.
func (d Datagram) Encode(dst []byte) ([]byte, error) {
	if len(d.Data) > MaxDataLength {
		return nil, ErrTooLong
	}
	length := headerLen + len(d.Data) // total datagram length, including the 4-byte prefix

	// byte 0: bits 7-6 = 0, bits 5-2 = hops (4 bits), bits 1-0 = high 2 bits of length.
	dst = append(dst,
		(d.Hops&0x0F)<<2|uint8((length&0x300)>>8),
		uint8(length&0xFF),
		0, 0, // checksum (disabled)
	)
	dst = bp.AppendBE16(dst, d.DestNetwork)
	dst = bp.AppendBE16(dst, d.SrcNetwork)
	dst = append(dst,
		d.DestNode,
		d.SrcNode,
		d.DestSocket,
		d.SrcSocket,
		d.DDPType,
	)
	dst = append(dst, d.Data...)
	return dst, nil
}

// Decode parses one long-header datagram from b. The returned Data ALIASES b (it is a
// sub-slice, not a copy); callers that retain it past b's lifetime must copy. A non-zero
// checksum is verified; a zero checksum is accepted as "no checksum".
func Decode(b []byte) (Datagram, error) {
	if len(b) < headerLen {
		return Datagram{}, ErrShort
	}
	first := b[0]
	if first&0xC0 != 0 {
		return Datagram{}, ErrBadHeader
	}
	hops := (first & 0x3C) >> 2
	length := int(first&0x03)<<8 | int(b[1])
	if length != len(b) || length > headerLen+MaxDataLength {
		return Datagram{}, ErrBadLength
	}
	if sum := bp.BE16(b[2:4]); sum != 0 {
		if got := checksum(b[4:]); got != sum {
			return Datagram{}, ErrBadLength
		}
	}
	return Datagram{
		Hops:        hops,
		DestNetwork: bp.BE16(b[4:6]),
		SrcNetwork:  bp.BE16(b[6:8]),
		DestNode:    b[8],
		SrcNode:     b[9],
		DestSocket:  b[10],
		SrcSocket:   b[11],
		DDPType:     b[12],
		Data:        b[13:],
	}, nil
}
