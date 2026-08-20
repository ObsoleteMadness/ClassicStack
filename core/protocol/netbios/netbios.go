// Package netbios holds the NetBIOS name/session codec plus NetBIOS-over-IPX
// (NBIPX) packet encoding. Wire-format only: no I/O, and the session table /
// state machine lives in the service ring.
//
// Ring: CORE (stdlib only, reflection-free). Multi-byte integer codecs come from
// core/binaryprimitives, because encoding/binary transitively imports reflect.
package netbios

import (
	"errors"
	"strings"
)

var (
	// ErrShortDatagram is returned when a datagram is shorter than two names.
	ErrShortDatagram = errors.New("netbios: datagram too short")
	// ErrShortSession is returned when a session packet lacks its 4-byte header.
	ErrShortSession = errors.New("netbios: session packet too short")
	// ErrTruncated is returned when a declared length runs past the buffer.
	ErrTruncated = errors.New("netbios: packet truncated")
	// ErrTooLarge is returned when a payload exceeds the encodable maximum.
	ErrTooLarge = errors.New("netbios: payload too large")
)

// NameLength is the wire length of a NetBIOS name. The 16th byte is the type
// code (workstation, server, group, ...) — not part of the visible name.
const NameLength = 16

// Standard NetBIOS name type bytes. The 16th byte of every name on the wire
// selects the resource type; clients form a "name + type" composite when
// claiming or resolving.
const (
	NameTypeWorkstation uint8 = 0x00
	NameTypeMessenger   uint8 = 0x03 // Messenger Service (net send / WinPopup) recipient
	NameTypeFileServer  uint8 = 0x20 // SMB / file-server
	NameTypeGroup       uint8 = 0x1E
)

// Name is a 16-byte padded NetBIOS name: bytes 0..14 carry the visible name
// (uppercase, space-padded); byte 15 is the type code.
type Name [NameLength]byte

// NewName builds a NetBIOS name from a string and a type byte. The name is
// uppercased, truncated to 15 bytes, and space-padded; the type goes in byte 15.
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

// String renders the visible portion of the name with trailing spaces trimmed.
// The type byte is not included.
func (n Name) String() string {
	return strings.TrimRight(string(n[:NameLength-1]), " ")
}

// Type returns the type byte (byte 15).
func (n Name) Type() uint8 { return n[NameLength-1] }

// Datagram represents a NetBIOS datagram: a destination name, a source name,
// and an opaque payload.
type Datagram struct {
	Destination Name
	Source      Name
	Payload     []byte
}

// Encode serialises the datagram (dest name, source name, payload).
func (d *Datagram) Encode() ([]byte, error) {
	out := make([]byte, 2*NameLength+len(d.Payload))
	copy(out[0:NameLength], d.Destination[:])
	copy(out[NameLength:2*NameLength], d.Source[:])
	copy(out[2*NameLength:], d.Payload)
	return out, nil
}

// DecodeDatagram parses a NetBIOS datagram. Payload is COPIED.
func DecodeDatagram(b []byte) (*Datagram, error) {
	if len(b) < 2*NameLength {
		return nil, ErrShortDatagram
	}
	var d Datagram
	copy(d.Destination[:], b[0:NameLength])
	copy(d.Source[:], b[NameLength:2*NameLength])
	d.Payload = make([]byte, len(b)-2*NameLength)
	copy(d.Payload, b[2*NameLength:])
	return &d, nil
}

// SessionPacketType is the 1-byte type of an RFC 1002 / SMB-Direct-TCP session
// packet.
type SessionPacketType uint8

const (
	SessionMessage          SessionPacketType = 0x00
	SessionRequest          SessionPacketType = 0x81
	PositiveSessionResponse SessionPacketType = 0x82
	NegativeSessionResponse SessionPacketType = 0x83
	RetargetSessionResponse SessionPacketType = 0x84
	SessionKeepAlive        SessionPacketType = 0x85
)

// MaxSessionPayload is the largest payload encodable in the 24-bit length field
// of an RFC 1002 / SMB-Direct session packet.
const MaxSessionPayload = 0xFFFFFF

// SessionHeaderLen is the fixed 4-byte RFC 1002 session-packet header: a 1-byte
// message type then a 3-byte (24-bit) big-endian length.
const SessionHeaderLen = 4

// PutSessionHeader writes the 4-byte session header (type + 24-bit big-endian
// length) into dst, which must be at least SessionHeaderLen long. It is the
// streaming form of SessionPacket.Encode, for the TCP framers on both sides
// (adapter/smbtcp and client/smb) which write the header and the payload as
// separate writes and must not copy the message to frame it.
func PutSessionHeader(dst []byte, typ SessionPacketType, length int) {
	if len(dst) < SessionHeaderLen {
		return
	}
	dst[0] = byte(typ)
	dst[1] = byte(length >> 16)
	dst[2] = byte(length >> 8)
	dst[3] = byte(length)
}

// ParseSessionHeader reads a 4-byte session header, returning the message type and
// the 24-bit payload length that follows it. It is the streaming counterpart of
// DecodeSessionPacket: a reader consumes SessionHeaderLen bytes, then reads exactly
// length payload bytes. Returns ErrShortSession when b is too short.
func ParseSessionHeader(b []byte) (SessionPacketType, int, error) {
	if len(b) < SessionHeaderLen {
		return 0, 0, ErrShortSession
	}
	return SessionPacketType(b[0]), int(b[1])<<16 | int(b[2])<<8 | int(b[3]), nil
}

// SessionPacket represents an RFC 1002 / MS-SMB2 Direct TCP session packet: a
// 1-byte type, a 3-byte (24-bit, big-endian) length, then the payload.
type SessionPacket struct {
	Type    SessionPacketType
	Payload []byte
}

// Encode serialises the session packet.
func (s *SessionPacket) Encode() ([]byte, error) {
	l := len(s.Payload)
	if l > MaxSessionPayload {
		return nil, ErrTooLarge
	}
	b := make([]byte, 4+l)
	b[0] = byte(s.Type)
	b[1] = byte(l >> 16)
	b[2] = byte(l >> 8)
	b[3] = byte(l)
	copy(b[4:], s.Payload)
	return b, nil
}

// DecodeSessionPacket parses a session packet. Payload is COPIED so the caller
// does not pin b.
func DecodeSessionPacket(b []byte) (*SessionPacket, error) {
	if len(b) < 4 {
		return nil, ErrShortSession
	}
	l := int(b[1])<<16 | int(b[2])<<8 | int(b[3])
	if len(b) < 4+l {
		return nil, ErrTruncated
	}
	payload := make([]byte, l)
	copy(payload, b[4:4+l])
	return &SessionPacket{Type: SessionPacketType(b[0]), Payload: payload}, nil
}
