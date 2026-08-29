// Package pap holds the Printer Access Protocol (PAP) codec. PAP is a
// connection-oriented protocol layered on ATP that carries a byte stream
// between a workstation and a printer (or print server).
//
// There is no legacy ClassicStack PAP implementation to migrate and no current
// service consumes PAP; this codec is written fresh from the published spec so
// the M2 protocol set is complete and a future print service can build on it.
// The wire layout below is from Inside AppleTalk, 2nd ed., Chapter 10 ("Printer
// Access Protocol"). Per CLAUDE.md it is spec-derived (not capture-observed);
// any client deviation found later should be recorded in spec/errata.md.
//
// Ring: CORE (stdlib only, reflection-free).
package pap

import "errors"

// PAP rides on ATP: every PAP packet is an ATP request or response whose 4-byte
// ATP UserData field carries the PAP header below. The DDP type is ATP's.
//
// ATP UserData layout for PAP (big-endian, MSB first):
//
//	[0] Connection ID
//	[1] Function (PAP function code)
//	[2:3] Function-dependent (e.g. flow quantum, or 0)
//
// The connection-request/response packets additionally carry a responding
// socket and flow quantum in the ATP data area; this codec handles the
// UserData header, which every PAP packet shares.

// Function codes carried in UserData byte 1 (Inside AppleTalk, 2nd ed., Ch. 10,
// Table "PAP packet types").
const (
	FuncOpenConn       uint8 = 0x01 // Open-Connection request
	FuncOpenConnReply  uint8 = 0x02 // Open-Connection reply
	FuncSendData       uint8 = 0x03 // Send-Data request
	FuncData           uint8 = 0x04 // Data response
	FuncTickle         uint8 = 0x05 // Tickle (keep-alive)
	FuncCloseConn      uint8 = 0x06 // Close-Connection request
	FuncCloseConnReply uint8 = 0x07 // Close-Connection reply
	FuncSendStatus     uint8 = 0x08 // Send-Status request
	FuncStatus         uint8 = 0x09 // Status reply
)

// EOFFlag is the end-of-file flag carried in the high bit of the function-
// dependent field of a Data response (FuncData): set on the final data packet
// of a job.
const EOFFlag uint16 = 0x8000

// DefaultFlowQuantum is the standard PAP flow quantum (number of ATP response
// buffers a receiver advertises): 8 on a standard AppleTalk network.
const DefaultFlowQuantum uint8 = 8

// ErrBadFunction is returned by ParseHeader for an unrecognised function code.
var ErrBadFunction = errors.New("pap: unrecognised function code")

// Header is the PAP header carried in the 4-byte ATP UserData field.
type Header struct {
	ConnID   uint8  // PAP connection identifier
	Function uint8  // one of the Func* codes
	FuncData uint16 // function-dependent (flow quantum, EOF flag, or 0)
}

// Encode packs the header into the 4-byte ATP UserData value (big-endian):
// [0] ConnID [1] Function [2:3] FuncData.
func (h Header) Encode() uint32 {
	return uint32(h.ConnID)<<24 |
		uint32(h.Function)<<16 |
		uint32(h.FuncData)
}

// ParseHeader unpacks a PAP header from an ATP UserData value. It returns
// ErrBadFunction if the function code is outside the known range; callers that
// want to tolerate unknown codes can read the fields directly via the returned
// Header (which is always populated) and ignore the error.
func ParseHeader(userData uint32) (Header, error) {
	h := Header{
		ConnID:   uint8(userData >> 24),
		Function: uint8(userData >> 16),
		FuncData: uint16(userData),
	}
	if h.Function < FuncOpenConn || h.Function > FuncStatus {
		return h, ErrBadFunction
	}
	return h, nil
}

// IsEOF reports whether the EOF flag is set in a Data response's FuncData field.
func (h Header) IsEOF() bool { return h.FuncData&EOFFlag != 0 }
