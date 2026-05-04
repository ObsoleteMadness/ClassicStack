// Package netbeui implements the NetBIOS Frames Protocol (NBF) frame
// format. NBF rides on 802.2 LLC directly over Ethernet (DSAP/SSAP
// both 0xF0); this package handles only the NBF body that follows the
// 3-byte LLC header — link-layer framing is the port's job.
//
// The NBF body format used here is the IBM/Microsoft documented one:
//
//	+0  uint16  length (little-endian, including the length field)
//	+2  uint16  delimiter (0xEFFF, "NBF")
//	+4  uint8   command
//	+5  uint8   data1 / option flags
//	+6  uint16  data2 / response correlator
//	+8  [16]    destination NetBIOS name (or addressing fields)
//	+24 [16]    source NetBIOS name
//	+40         user data
//
// This stub handles the fixed-shape session/datagram frames with a
// 16/16 byte name pair. The variable header layout used by some
// commands (e.g. ADD_GROUP_NAME_QUERY) lands when the real NBF
// state machine does.
package netbeui

import (
	"encoding/binary"
	"errors"
)

// NBFDelimiter is the constant 0xEFFF "NBF" delimiter that follows
// the length field in every NBF body.
const NBFDelimiter uint16 = 0xEFFF

// HeaderLength is the fixed-portion size of an NBF body before the
// user-data trailer.
const HeaderLength = 40

// ErrNotImplemented is returned by call sites that have not been
// filled in.
var ErrNotImplemented = errors.New("netbeui: not implemented")

// ErrShortFrame is returned by Decode when the input cannot contain
// even a fixed-shape NBF header.
var ErrShortFrame = errors.New("netbeui: short frame")

// ErrBadDelimiter is returned by Decode when the 0xEFFF delimiter is
// missing — a strong signal the input is not an NBF body.
var ErrBadDelimiter = errors.New("netbeui: bad delimiter")

// Frame represents an NBF frame.
type Frame struct {
	Command            uint8
	Data1              uint8
	ResponseCorrelator uint16
	DestinationName    [16]byte
	SourceName         [16]byte
	Payload            []byte
}

// Encode serializes the NBF frame to bytes. The result starts at the
// length field; callers prepend the 3-byte 802.2 LLC header at the
// link layer.
func (f *Frame) Encode() ([]byte, error) {
	total := HeaderLength + len(f.Payload)
	if total > 0xFFFF {
		return nil, errors.New("netbeui: frame too large")
	}
	b := make([]byte, total)
	binary.LittleEndian.PutUint16(b[0:2], uint16(total))
	binary.LittleEndian.PutUint16(b[2:4], NBFDelimiter)
	b[4] = f.Command
	b[5] = f.Data1
	binary.LittleEndian.PutUint16(b[6:8], f.ResponseCorrelator)
	copy(b[8:24], f.DestinationName[:])
	copy(b[24:40], f.SourceName[:])
	copy(b[40:], f.Payload)
	return b, nil
}

// Decode parses an NBF body (without the leading LLC header).
func Decode(b []byte) (*Frame, error) {
	if len(b) < HeaderLength {
		return nil, ErrShortFrame
	}
	if binary.LittleEndian.Uint16(b[2:4]) != NBFDelimiter {
		return nil, ErrBadDelimiter
	}
	f := &Frame{
		Command:            b[4],
		Data1:              b[5],
		ResponseCorrelator: binary.LittleEndian.Uint16(b[6:8]),
	}
	copy(f.DestinationName[:], b[8:24])
	copy(f.SourceName[:], b[24:40])
	if len(b) > HeaderLength {
		f.Payload = make([]byte, len(b)-HeaderLength)
		copy(f.Payload, b[HeaderLength:])
	}
	return f, nil
}
