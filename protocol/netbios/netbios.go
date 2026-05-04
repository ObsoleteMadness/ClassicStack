package netbios

import "errors"

var ErrNotImplemented = errors.New("not implemented")

// Name represents a 16-byte padded NetBIOS name.
type Name [16]byte

// Datagram represents a NetBIOS datagram.
type Datagram struct {
	Destination Name
	Source      Name
	Payload     []byte
}

func (d *Datagram) Encode() ([]byte, error)      { return nil, ErrNotImplemented }
func DecodeDatagram(b []byte) (*Datagram, error) { return nil, ErrNotImplemented }

type SessionPacketType uint8

const (
	SessionMessage          SessionPacketType = 0x00
	SessionRequest          SessionPacketType = 0x81
	PositiveSessionResponse SessionPacketType = 0x82
	NegativeSessionResponse SessionPacketType = 0x83
	RetargetSessionResponse SessionPacketType = 0x84
	SessionKeepAlive        SessionPacketType = 0x85
)

// SessionPacket represents an RFC 1002 / MS-SMB2 Direct TCP session packet.
type SessionPacket struct {
	Type    SessionPacketType
	Payload []byte
}

func (s *SessionPacket) Encode() ([]byte, error) {
	l := len(s.Payload)
	if l > 16777215 { // MaxDirectTcpPacketLength
		return nil, errors.New("payload too large")
	}

	b := make([]byte, 4+l)
	b[0] = byte(s.Type)
	b[1] = byte(l >> 16)
	b[2] = byte(l >> 8)
	b[3] = byte(l)
	copy(b[4:], s.Payload)
	return b, nil
}

func DecodeSessionPacket(b []byte) (*SessionPacket, error) {
	if len(b) < 4 {
		return nil, errors.New("packet too short")
	}
	l := (int(b[1]) << 16) | (int(b[2]) << 8) | int(b[3])
	if len(b) < 4+l {
		return nil, errors.New("packet truncated")
	}
	// Copy the payload so the caller doesn't pin the underlying buffer.
	payload := make([]byte, l)
	copy(payload, b[4:4+l])
	return &SessionPacket{
		Type:    SessionPacketType(b[0]),
		Payload: payload,
	}, nil
}
