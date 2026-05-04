package ipx

import "errors"

var ErrNotImplemented = errors.New("not implemented")

// Datagram represents an IPX packet header and payload.
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

// Encode serializes the Datagram to bytes.
func (d *Datagram) Encode() ([]byte, error) {
	totalLen := 30 + len(d.Payload)
	if totalLen > 65535 {
		return nil, errors.New("ipx: payload too large")
	}

	b := make([]byte, totalLen)
	
	// Default checksum to 0xFFFF if not set
	if d.Checksum[0] == 0 && d.Checksum[1] == 0 {
		b[0] = 0xFF
		b[1] = 0xFF
	} else {
		b[0] = d.Checksum[0]
		b[1] = d.Checksum[1]
	}

	b[2] = byte(totalLen >> 8)
	b[3] = byte(totalLen)
	b[4] = d.Hops
	b[5] = d.Type
	copy(b[6:10], d.DstNet[:])
	copy(b[10:16], d.DstNode[:])
	copy(b[16:18], d.DstSock[:])
	copy(b[18:22], d.SrcNet[:])
	copy(b[22:28], d.SrcNode[:])
	copy(b[28:30], d.SrcSock[:])
	copy(b[30:], d.Payload)

	return b, nil
}

// Decode deserializes bytes into an IPX Datagram.
func Decode(b []byte) (*Datagram, error) {
	if len(b) < 30 {
		return nil, errors.New("ipx: packet too short")
	}

	totalLen := (int(b[2]) << 8) | int(b[3])
	if totalLen < 30 {
		return nil, errors.New("ipx: invalid length")
	}
	if len(b) < totalLen {
		return nil, errors.New("ipx: packet truncated")
	}

	d := &Datagram{
		Length: uint16(totalLen),
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

	payloadLen := totalLen - 30
	d.Payload = make([]byte, payloadLen)
	copy(d.Payload, b[30:30+payloadLen])

	return d, nil
}
