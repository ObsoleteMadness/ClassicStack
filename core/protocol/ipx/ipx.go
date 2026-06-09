// Package ipx holds the IPX datagram codec (Novell NetWare / RFC 1132 framing
// of the IPX header). Wire-format only: no I/O, no routing state.
//
// Ring: CORE (stdlib only, reflection-free). All multi-byte header fields are
// big-endian; the codec works on fixed-width byte arrays, so no endian helper
// is needed beyond the length field.
package ipx

import "errors"

// HeaderLen is the fixed IPX header length in bytes (checksum .. src socket).
const HeaderLen = 30

// MaxLength is the largest value encodable in the 16-bit IPX length field.
const MaxLength = 0xFFFF

var (
	// ErrTooLarge is returned by Encode when the total datagram exceeds the
	// 16-bit length field.
	ErrTooLarge = errors.New("ipx: datagram exceeds 65535 bytes")
	// ErrShort is returned by Decode when the buffer is shorter than a header.
	ErrShort = errors.New("ipx: buffer shorter than IPX header")
	// ErrBadLength is returned by Decode when the length field is invalid or
	// runs past the buffer.
	ErrBadLength = errors.New("ipx: invalid or truncated length")
)

// Datagram represents an IPX packet header and payload. Address fields use
// fixed-width arrays (network 4, node 6, socket 2) so the codec is copy-only.
type Datagram struct {
	Checksum [2]byte
	Length   uint16
	Hops     uint8
	Type     uint8
	DstNet   [4]byte
	DstNode  [6]byte
	DstSock  [2]byte
	SrcNet   [4]byte
	SrcNode  [6]byte
	SrcSock  [2]byte
	Payload  []byte
}

// Encode appends the wire form to dst and returns it (append-style → caller
// controls allocation). A zero Checksum is emitted as 0xFFFF, matching NetWare
// "checksum disabled". The Length field on the wire is always the computed
// total, overriding d.Length.
func (d *Datagram) Encode(dst []byte) ([]byte, error) {
	total := HeaderLen + len(d.Payload)
	if total > MaxLength {
		return nil, ErrTooLarge
	}

	// Checksum: 0xFFFF means "no checksum" on the wire.
	if d.Checksum[0] == 0 && d.Checksum[1] == 0 {
		dst = append(dst, 0xFF, 0xFF)
	} else {
		dst = append(dst, d.Checksum[0], d.Checksum[1])
	}
	dst = append(dst, byte(total>>8), byte(total))
	dst = append(dst, d.Hops, d.Type)
	dst = append(dst, d.DstNet[:]...)
	dst = append(dst, d.DstNode[:]...)
	dst = append(dst, d.DstSock[:]...)
	dst = append(dst, d.SrcNet[:]...)
	dst = append(dst, d.SrcNode[:]...)
	dst = append(dst, d.SrcSock[:]...)
	dst = append(dst, d.Payload...)
	return dst, nil
}

// Decode parses one IPX datagram from b. The returned Payload is COPIED so the
// caller does not pin b.
func Decode(b []byte) (*Datagram, error) {
	if len(b) < HeaderLen {
		return nil, ErrShort
	}
	total := int(b[2])<<8 | int(b[3])
	if total < HeaderLen || len(b) < total {
		return nil, ErrBadLength
	}
	d := &Datagram{
		Length: uint16(total),
		Hops:   b[4],
		Type:   b[5],
	}
	copy(d.Checksum[:], b[0:2])
	copy(d.DstNet[:], b[6:10])
	copy(d.DstNode[:], b[10:16])
	copy(d.DstSock[:], b[16:18])
	copy(d.SrcNet[:], b[18:22])
	copy(d.SrcNode[:], b[22:28])
	copy(d.SrcSock[:], b[28:30])

	d.Payload = make([]byte, total-HeaderLen)
	copy(d.Payload, b[HeaderLen:total])
	return d, nil
}
