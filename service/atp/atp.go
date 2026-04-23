/*
Package atp provides AppleTalk Transaction Protocol (ATP) header types and constants.

ATP provides reliable, request-response transactions. It supports both at-least-once (ALO)
and exactly-once (XO) delivery models.

Inside Macintosh: Networking, Chapter 6.
https://dev.os9.ca/techpubs/mac/Networking/Networking-143.html#HEADING143-0
*/
package atp

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/pgodw/omnitalk/protocol"
)

// ATP Control bit masks.
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
func (h *ATPHeader) FuncCode() FuncCode { return FuncCode(h.Control & FuncMask) }

// XO returns true if the XO bit is set.
func (h *ATPHeader) XO() bool { return h.Control&XO != 0 }

// EOM returns true if the EOM bit is set.
func (h *ATPHeader) EOM() bool { return h.Control&EOM != 0 }

// STS returns true if the STS bit is set.
func (h *ATPHeader) STS() bool { return h.Control&STS != 0 }

// TRelTimeout encodes the 3-bit TRel timeout indicator carried in the low
// bits of the control byte for XO TReq packets.
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
func (h *ATPHeader) GetTRelTimeout() TRelTimeout {
	return TRelTimeout(h.Control & 0x07)
}

// SetTRelTimeout encodes the TRel timeout indicator into the control byte.
func (h *ATPHeader) SetTRelTimeout(t TRelTimeout) {
	h.Control = (h.Control &^ 0x07) | (uint8(t) & 0x07)
}

// Protocol limits per Inside AppleTalk Ch. 9.
const (
	// MaxResponsePackets is the maximum number of packets in a TResp message.
	MaxResponsePackets = 8
	// MaxATPData is the maximum data payload of a single ATP packet (DDP max
	// payload 586 - 8 byte ATP header).
	MaxATPData = 578
)

// DDPTypeATP is the DDP type for ATP packets.
const DDPTypeATP = 3

// ATPHeader represents an ATP packet header.
// Refer: https://dev.os9.ca/techpubs/mac/Networking/Networking-145.html#HEADING145-0
//
//	 0               1               2               3
//	 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|Control|  Res  | Bitmap/Seq    |       Transaction ID          |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                         User Data                             |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
type ATPHeader struct {
	Control  uint8
	Bitmap   uint8 // Sequence number for TRESP, bitmap for TREQ
	TransID  uint16
	UserData uint32
}

// ATPHeaderSize is the size of an ATP header in bytes.
const ATPHeaderSize = 8

// Marshal binary-encodes the ATP header.
func (h *ATPHeader) Marshal() []byte {
	b := make([]byte, ATPHeaderSize)
	b[0] = h.Control
	b[1] = h.Bitmap
	binary.BigEndian.PutUint16(b[2:4], h.TransID)
	binary.BigEndian.PutUint32(b[4:8], h.UserData)
	return b
}

// Unmarshal binary-decodes the ATP header.
func (h *ATPHeader) Unmarshal(b []byte) error {
	if len(b) < ATPHeaderSize {
		return errors.New("packet too short for ATP header")
	}
	h.Control = b[0]
	h.Bitmap = b[1]
	h.TransID = binary.BigEndian.Uint16(b[2:4])
	h.UserData = binary.BigEndian.Uint32(b[4:8])
	return nil
}

func (h *ATPHeader) String() string {
	return fmt.Sprintf("ATPHeader{Control:0x%02x Bitmap:0x%02x TransID:%d UserData:0x%08x}", h.Control, h.Bitmap, h.TransID, h.UserData)
}

var _ protocol.Packet = (*ATPHeader)(nil)
