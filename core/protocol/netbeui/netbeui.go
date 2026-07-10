// Package netbeui holds the NetBIOS Frames Protocol (NBF) frame codec. NBF
// rides on 802.2 LLC directly over Ethernet (DSAP/SSAP both 0xF0); this package
// handles only the NBF body that follows the 3-byte LLC header — link-layer
// framing is the port's job.
//
// NBF defines two header shapes on the wire (IBM SC30-3587 §5.5.3):
//
//  1. Non-session frames (commands 0x00–0x13, DLC UI): 44 bytes — a 12-byte
//     common prefix + 16-byte dest name + 16-byte source name, optionally
//     followed by user data (e.g. STATUS_RESPONSE).
//  2. Session frames (commands 0x14–0x1F, DLC I-format LPDU): 14 bytes — the
//     12-byte common prefix + 1-byte dest session number + 1-byte source
//     session number, followed by user data.
//
// Common prefix layout (both shapes, all multi-byte fields little-endian):
//
//	+0  uint16  LENGTH          (header length only: X'000E' session,
//	                             X'002C' non-session — user data NOT counted)
//	+2  uint16  DELIMITER       (0xEFFF)
//	+4  uint8   COMMAND
//	+5  uint8   DATA1           (option flags / reserved)
//	+6  uint16  DATA2           (per-command)
//	+8  uint16  XMIT CORRELATOR
//	+10 uint16  RSP CORRELATOR
//
// Ring: CORE (stdlib only, reflection-free). Little-endian helpers are
// hand-rolled because encoding/binary transitively imports reflect.
package netbeui

import (
	"errors"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
)

// NBFDelimiter is the constant 0xEFFF "NBF" delimiter that follows the length
// field in every NBF body.
const NBFDelimiter uint16 = 0xEFFF

// commonPrefixLen is the 12-byte prefix shared by both header shapes.
const commonPrefixLen = 12

var (
	// ErrShortFrame is returned by Decode when the input cannot contain a
	// common prefix (or the full header for the detected command).
	ErrShortFrame = errors.New("netbeui: short frame")
	// ErrBadDelimiter is returned by Decode when the 0xEFFF delimiter is
	// missing — a strong signal the input is not an NBF body.
	ErrBadDelimiter = errors.New("netbeui: bad delimiter")
	// ErrFrameTooLarge is returned by Encode when header+payload exceeds
	// 64 KiB — far beyond any link MTU, so a caller bug.
	ErrFrameTooLarge = errors.New("netbeui: frame too large")
)

// Frame represents a decoded NBF frame. The Command field selects the header
// shape: for non-session commands (0x00–0x13) DestinationName/SourceName are
// populated; for session commands (0x14–0x1F) DestNumber/SourceNumber are.
// Use IsSessionCommand(f.Command) to discriminate.
type Frame struct {
	// Common prefix fields (both shapes).
	Command        uint8
	Data1          uint8
	Data2          uint16
	XmitCorrelator uint16
	RspCorrelator  uint16

	// Non-session header fields (commands 0x00–0x13).
	DestinationName [16]byte
	SourceName      [16]byte

	// Session header fields (commands 0x14–0x1F).
	DestNumber   uint8
	SourceNumber uint8

	// Payload follows the header (may be empty).
	Payload []byte
}

// Encode serialises the NBF frame. The result starts at the length field;
// callers prepend the 3-byte 802.2 LLC header at the link layer.
func (f *Frame) Encode() ([]byte, error) {
	hdrLen := NonSessionHeaderLength
	if IsSessionCommand(f.Command) {
		hdrLen = SessionHeaderLength
	}

	total := hdrLen + len(f.Payload)
	if total > 0xFFFF {
		return nil, ErrFrameTooLarge
	}

	b := make([]byte, total)

	// Common prefix. LENGTH is the header length only (X'000E' / X'002C',
	// [IBM SC30-3587] Table 5-25 etc.) — never header+payload. NT 3.51's
	// netbeui.sys silently discards session frames whose LENGTH differs
	// (without even acknowledging them at the LLC level), while Win9x does
	// not validate the field; see spec/errata.md.
	bp.PutLE16(b[0:2], uint16(hdrLen))
	bp.PutLE16(b[2:4], NBFDelimiter)
	b[4] = f.Command
	b[5] = f.Data1
	bp.PutLE16(b[6:8], f.Data2)
	bp.PutLE16(b[8:10], f.XmitCorrelator)
	bp.PutLE16(b[10:12], f.RspCorrelator)

	if IsSessionCommand(f.Command) {
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

// Decode parses an NBF body (without the leading LLC header). The command byte
// determines which header shape is expected. Any trailing user data is COPIED
// into Payload so the caller does not pin b.
func Decode(b []byte) (*Frame, error) {
	if len(b) < commonPrefixLen {
		return nil, ErrShortFrame
	}
	if bp.LE16(b[2:4]) != NBFDelimiter {
		return nil, ErrBadDelimiter
	}

	cmd := b[4]
	hdrLen := NonSessionHeaderLength
	if IsSessionCommand(cmd) {
		hdrLen = SessionHeaderLength
	}
	if len(b) < hdrLen {
		return nil, ErrShortFrame
	}

	f := &Frame{
		Command:        cmd,
		Data1:          b[5],
		Data2:          bp.LE16(b[6:8]),
		XmitCorrelator: bp.LE16(b[8:10]),
		RspCorrelator:  bp.LE16(b[10:12]),
	}
	if IsSessionCommand(cmd) {
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
