// Package atp holds the AppleTalk Transaction Protocol (ATP) codec.
//
// ATP provides reliable, request-response transactions over DDP, with both
// at-least-once (ALO) and exactly-once (XO) delivery models. This package is
// wire-format only — no I/O, no goroutines, no session state.
//
// Ring: CORE (stdlib only, reflection-free). Big-endian integer codecs come from
// core/binaryprimitives, because encoding/binary transitively imports reflect.
//
// Reference: Inside Macintosh: Networking, Chapter 6.
// https://dev.os9.ca/techpubs/mac/Networking/Networking-143.html#HEADING143-0
package atp

import (
	"errors"
	"time"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
)

// ATP control bit masks.
// Refer: https://dev.os9.ca/techpubs/mac/Networking/Networking-145.html#HEADING145-10
const (
	TREQ  = 0x40 // Transaction Request
	TRESP = 0x80 // Transaction Response
	TREL  = 0xC0 // Transaction Release
	XO    = 0x20 // Exactly Once
	EOM   = 0x10 // End of Message
	STS   = 0x08 // Send Transaction Status

	FuncMask = 0xC0 // Mask for the 2-bit function code
)

// FuncCode is the 2-bit function code in the ATP control byte.
type FuncCode uint8

const (
	FuncTReq  FuncCode = TREQ
	FuncTResp FuncCode = TRESP
	FuncTRel  FuncCode = TREL
)

// FuncCode returns the function code (TReq, TResp, or TRel) from the header.
func (h Header) FuncCode() FuncCode { return FuncCode(h.Control & FuncMask) }

// XO reports whether the XO (exactly-once) bit is set.
func (h Header) XO() bool { return h.Control&XO != 0 }

// EOM reports whether the EOM (end-of-message) bit is set.
func (h Header) EOM() bool { return h.Control&EOM != 0 }

// STS reports whether the STS (send-transaction-status) bit is set.
func (h Header) STS() bool { return h.Control&STS != 0 }

// TRelTimeout encodes the 3-bit TRel timeout indicator carried in the low bits
// of the control byte for XO TReq packets.
type TRelTimeout uint8

const (
	TRel30s TRelTimeout = 0
	TRel1m  TRelTimeout = 1
	TRel2m  TRelTimeout = 2
	TRel4m  TRelTimeout = 3
	TRel8m  TRelTimeout = 4
)

// Duration converts a TRelTimeout indicator to its wall-clock value.
func (t TRelTimeout) Duration() time.Duration {
	switch t {
	case TRel30s:
		return 30 * time.Second
	case TRel1m:
		return 1 * time.Minute
	case TRel2m:
		return 2 * time.Minute
	case TRel4m:
		return 4 * time.Minute
	case TRel8m:
		return 8 * time.Minute
	default:
		return 30 * time.Second
	}
}

// GetTRelTimeout extracts the TRel timeout indicator from the control byte.
func (h Header) GetTRelTimeout() TRelTimeout { return TRelTimeout(h.Control & 0x07) }

// SetTRelTimeout encodes the TRel timeout indicator into the control byte.
func (h *Header) SetTRelTimeout(t TRelTimeout) {
	h.Control = (h.Control &^ 0x07) | (uint8(t) & 0x07)
}

// Protocol limits per Inside AppleTalk Ch. 9.
const (
	// MaxResponsePackets is the maximum number of packets in a TResp message.
	MaxResponsePackets = 8
	// MaxATPData is the maximum data payload of a single ATP packet (DDP max
	// payload 586 - 8-byte ATP header).
	MaxATPData = 578
)

// MaxRespForPayload returns how many ATP response packets (1..8) are needed to
// carry bytes of payload. The workstation puts this count in the TReq bitmap so
// a System 7 responder that omits EOM still completes when the requested slots
// arrive (classicstack-web bitmapForPayload). Asking for more slots than the
// server will send stalls until ATP retry.
func MaxRespForPayload(bytes int) int {
	if bytes < 1 {
		bytes = 1
	}
	n := (bytes + MaxATPData - 1) / MaxATPData
	if n < 1 {
		n = 1
	}
	if n > MaxResponsePackets {
		n = MaxResponsePackets
	}
	return n
}

// DDPType is the DDP protocol type for ATP packets.
const DDPType = 3

// HeaderSize is the fixed ATP header length in bytes.
const HeaderSize = 8

// ErrShort is returned by Decode when the buffer is shorter than a header.
var ErrShort = errors.New("atp: buffer shorter than ATP header")

// Header represents an ATP packet header.
// Refer: https://dev.os9.ca/techpubs/mac/Networking/Networking-145.html#HEADING145-0
//
//	 0               1               2               3
//	 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|Control|  Bitmap/Seq   |       Transaction ID          |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                         User Data                             |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
type Header struct {
	Control  uint8
	Bitmap   uint8 // sequence number for TRESP, bitmap for TREQ
	TransID  uint16
	UserData uint32
}

// Encode appends the 8-byte ATP header to dst and returns it (append-style →
// caller controls allocation).
func (h Header) Encode(dst []byte) []byte {
	dst = append(dst, h.Control, h.Bitmap)
	dst = bp.AppendBE16(dst, h.TransID)
	dst = bp.AppendBE32(dst, h.UserData)
	return dst
}

// Decode parses an ATP header from the front of b. It returns ErrShort if b is
// shorter than HeaderSize. Any payload following the header is b[HeaderSize:].
func Decode(b []byte) (Header, error) {
	if len(b) < HeaderSize {
		return Header{}, ErrShort
	}
	return Header{
		Control:  b[0],
		Bitmap:   b[1],
		TransID:  bp.BE16(b[2:4]),
		UserData: bp.BE32(b[4:8]),
	}, nil
}
