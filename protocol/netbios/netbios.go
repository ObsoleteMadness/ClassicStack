package netbios

import (
	"errors"
	"strings"
)

var ErrNotImplemented = errors.New("not implemented")

// NameLength is the wire length of a NetBIOS name. The 16th byte is
// the *type* code (workstation, server, group, etc.) — not part of
// the human-visible name.
const NameLength = 16

// Standard NetBIOS name type bytes. The 16th byte of every name on
// the wire selects the resource type; clients form a "name + type"
// composite when claiming or resolving.
const (
	NameTypeWorkstation uint8 = 0x00
	NameTypeFileServer  uint8 = 0x20 // SMB / file-server
	NameTypeGroup       uint8 = 0x1E
)

// Name represents a 16-byte padded NetBIOS name. Bytes 0..14 carry
// the human-visible name (uppercase, space-padded); byte 15 is the
// type code (NameTypeWorkstation, NameTypeFileServer, ...).
type Name [NameLength]byte

// NewName builds a NetBIOS name from a human-facing string and a
// type byte. The name is uppercased, truncated to 15 bytes, and
// space-padded; the type goes in byte 15.
func NewName(name string, typ uint8) Name {
	var n Name
	upper := strings.ToUpper(strings.TrimSpace(name))
	if len(upper) > NameLength-1 {
		upper = upper[:NameLength-1]
	}
	for i := range NameLength - 1 {
		if i < len(upper) {
			n[i] = upper[i]
		} else {
			n[i] = ' '
		}
	}
	n[NameLength-1] = typ
	return n
}

// String renders the human-visible portion of the name with trailing
// spaces trimmed. The type byte is not included.
func (n Name) String() string {
	return strings.TrimRight(string(n[:NameLength-1]), " ")
}

// Type returns the type byte (byte 15).
func (n Name) Type() uint8 { return n[NameLength-1] }

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
