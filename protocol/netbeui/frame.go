// Package netbeui implements the NetBIOS Frames Protocol (NBF) frame
// format. NBF rides on 802.2 LLC directly over Ethernet (DSAP/SSAP
// both 0xF0); this package handles only the NBF body that follows the
// 3-byte LLC header — link-layer framing is the port's job.
//
// NBF defines two distinct header shapes on the wire (IBM SC30-3587
// §5.5.3):
//
//  1. Non-session frames (commands 0x00–0x13, DLC UI):
//     44 bytes total — 12-byte common prefix + 16-byte dest name +
//     16-byte source name, optionally followed by user data
//     (STATUS_RESPONSE).
//
//  2. Session frames (commands 0x14–0x1F, DLC I-format LPDU):
//     14 bytes total — 12-byte common prefix + 1-byte dest session
//     number + 1-byte source session number, followed by user data.
//
// Common prefix layout (both shapes):
//
//	+0  uint16  LENGTH          (little-endian, includes this field)
//	+2  uint16  DELIMITER       (0xEFFF)
//	+4  uint8   COMMAND
//	+5  uint8   DATA1           (option flags / reserved)
//	+6  uint16  DATA2           (per-command, LE)
//	+8  uint16  XMIT CORRELATOR (LE)
//	+10 uint16  RSP CORRELATOR  (LE)
//
// This package provides a unified Frame type that carries the decoded
// header fields for both shapes, discriminated by IsSessionCommand().
package netbeui

import (
	"encoding/binary"
	"errors"
)

// NBFDelimiter is the constant 0xEFFF "NBF" delimiter that follows
// the length field in every NBF body.
const NBFDelimiter uint16 = 0xEFFF

// HeaderLength is kept as an alias for NonSessionHeaderLength for
// backward compatibility with callers that reference it.
const HeaderLength = NonSessionHeaderLength

// commonPrefixLen is the 12-byte prefix shared by both header shapes.
const commonPrefixLen = 12

// --- Errors ---

// ErrNotImplemented is returned by call sites that have not been
// filled in.
var ErrNotImplemented = errors.New("netbeui: not implemented")

// ErrShortFrame is returned by Decode when the input cannot contain
// even a common prefix.
var ErrShortFrame = errors.New("netbeui: short frame")

// ErrBadDelimiter is returned by Decode when the 0xEFFF delimiter is
// missing — a strong signal the input is not an NBF body.
var ErrBadDelimiter = errors.New("netbeui: bad delimiter")

// ErrFrameTooLarge is returned by Encode when the frame exceeds the
// maximum length encodable in the 16-bit length field.
var ErrFrameTooLarge = errors.New("netbeui: frame too large")

// --- Frame ---

// Frame represents a decoded NBF frame. The Command field determines
// which header shape was on the wire:
//
//   - Commands 0x00–0x13 (non-session): DestinationName and SourceName
//     are populated; DestNumber and SourceNumber are zero.
//   - Commands 0x14–0x1F (session): DestNumber and SourceNumber are
//     populated; DestinationName and SourceName are zero.
//
// Use IsSessionCommand(f.Command) to discriminate.
type Frame struct {
	// Common prefix fields (both shapes)
	Command        uint8
	Data1          uint8
	Data2          uint16
	XmitCorrelator uint16
	RspCorrelator  uint16

	// Non-session header fields (commands 0x00–0x13)
	DestinationName [16]byte
	SourceName      [16]byte

	// Session header fields (commands 0x14–0x1F)
	DestNumber uint8
	SourceNumber uint8

	// Payload follows the header (may be empty).
	Payload []byte

	// --- Deprecated aliases for backward compatibility ---

	// ResponseCorrelator is an alias for RspCorrelator.
	//
	// Deprecated: use RspCorrelator.
	ResponseCorrelator uint16
}

// Encode serializes the NBF frame to bytes. The result starts at the
// length field; callers prepend the 3-byte 802.2 LLC header at the
// link layer.
func (f *Frame) Encode() ([]byte, error) {
	// Resolve deprecated alias: if the caller set ResponseCorrelator
	// but not RspCorrelator, honour the deprecated field.
	rspCorr := f.RspCorrelator
	if rspCorr == 0 && f.ResponseCorrelator != 0 {
		rspCorr = f.ResponseCorrelator
	}

	session := IsSessionCommand(f.Command)

	var hdrLen int
	if session {
		hdrLen = SessionHeaderLength
	} else {
		hdrLen = NonSessionHeaderLength
	}

	total := hdrLen + len(f.Payload)
	if total > 0xFFFF {
		return nil, ErrFrameTooLarge
	}

	b := make([]byte, total)

	// Common prefix
	binary.LittleEndian.PutUint16(b[0:2], uint16(total))
	binary.LittleEndian.PutUint16(b[2:4], NBFDelimiter)
	b[4] = f.Command
	b[5] = f.Data1
	binary.LittleEndian.PutUint16(b[6:8], f.Data2)
	binary.LittleEndian.PutUint16(b[8:10], f.XmitCorrelator)
	binary.LittleEndian.PutUint16(b[10:12], rspCorr)

	if session {
		b[12] = f.DestNumber
		b[13] = f.SourceNumber
	} else {
		copy(b[12:28], f.DestinationName[:])
		copy(b[28:44], f.SourceName[:])
	}

	if len(f.Payload) > 0 {
		copy(b[hdrLen:], f.Payload)
	}
	return b, nil
}

// Decode parses an NBF body (without the leading LLC header). The
// command byte determines which header shape is expected.
func Decode(b []byte) (*Frame, error) {
	if len(b) < commonPrefixLen {
		return nil, ErrShortFrame
	}
	if binary.LittleEndian.Uint16(b[2:4]) != NBFDelimiter {
		return nil, ErrBadDelimiter
	}

	cmd := b[4]
	session := IsSessionCommand(cmd)

	var hdrLen int
	if session {
		hdrLen = SessionHeaderLength
	} else {
		hdrLen = NonSessionHeaderLength
	}

	if len(b) < hdrLen {
		return nil, ErrShortFrame
	}

	f := &Frame{
		Command:        cmd,
		Data1:          b[5],
		Data2:          binary.LittleEndian.Uint16(b[6:8]),
		XmitCorrelator: binary.LittleEndian.Uint16(b[8:10]),
		RspCorrelator:  binary.LittleEndian.Uint16(b[10:12]),
	}
	// Populate deprecated alias
	f.ResponseCorrelator = f.RspCorrelator

	if session {
		f.DestNumber = b[12]
		f.SourceNumber = b[13]
	} else {
		copy(f.DestinationName[:], b[12:28])
		copy(f.SourceName[:], b[28:44])
	}

	if len(b) > hdrLen {
		f.Payload = make([]byte, len(b)-hdrLen)
		copy(f.Payload, b[hdrLen:])
	}
	return f, nil
}
