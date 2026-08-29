// Package dsi holds the Data Stream Interface (DSI) codec: the session-layer framing
// that carries AFP over TCP/IP. DSI is ASP's TCP analogue — where ASP frames an AFP
// command as an ATP TReq/TResp UserData+data pair, DSI frames it as a fixed 16-byte
// header plus a variable-length data block on a TCP byte stream. This package is
// wire-format only — no I/O, no goroutines, no state; the server transport
// (adapter/dsi) and client session (client/dsi) both build on it.
//
// Ring: CORE (stdlib only, reflection-free; uses core/binaryprimitives, not
// encoding/binary, per the archtest forbidden-import gate).
//
// References:
//   - Apple's "AFP over TCP" / DSI specification (AppleShare IP era; the DSI header
//     shape below is unchanged through AFP 3.x).
//   - Cross-checked against Netatalk's libatalk/dsi (dsi.h struct DSI, dsi_stream.c) —
//     the long-lived open-source DSI implementation other AFP clients/servers
//     interoperate with, used here as the "golden" reference in the absence of a local
//     packet capture (no DSI capture exists yet under spec/captures; see
//     spec/21-dsi.md).
package dsi

import (
	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
)

// Command codes — the DSI header's Command byte.
const (
	CloseSession = 1 // either direction: end the session
	Command      = 2 // workstation → server: run an AFP command block
	GetStatus    = 3 // workstation → server: FPGetSrvrInfo, no session needed
	OpenSession  = 4 // workstation → server: establish the session
	Tickle       = 5 // either direction: keep-alive, no reply expected
	Write        = 6 // workstation → server: run an AFP write command (header+data)
	WriteReply   = 7 // reserved (WriteContinue on ASP has no DSI analogue in practice)
	Attention    = 8 // server → workstation: unsolicited notification (e.g. message waiting)
)

// Flags — the DSI header's Flags byte.
const (
	Request = 0x00
	Reply   = 0x01
)

// HeaderSize is the fixed DSI header length.
const HeaderSize = 16

// Header is one DSI header (16 bytes, all fields big-endian):
//
//	 0               1               2               3
//	 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|     Flags     |    Command    |           Request ID          |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                    ErrorCode / DataOffset                     |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                         Total Data Length                     |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                           Reserved                            |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//
// The third field is dual-purpose, distinguished by Flags: on a Reply it is the
// signed AFP/DSI result code (ErrorCode — a Command/Write/OpenSession/GetStatus reply
// carries its result HERE, not in the data payload); on a Write REQUEST it is the byte
// offset within the payload where the raw write data begins (DataOffset) — always
// equal to the fixed AFP write-header length (12 for FPWrite, 20 for FPAddIcon) for a
// well-formed request, so a correctly-framed request can be forwarded to the AFP
// command core (header+data concatenated) without consulting this field. It is unused
// (0) on every other request.
type Header struct {
	Flags       uint8
	Command     uint8
	RequestID   uint16
	ErrorOffset uint32 // ErrorCode on a reply; DataOffset on a Write request; else 0
	DataLen     uint32 // length of the data block following the header
	Reserved    uint32
}

// Marshal encodes the header into a fresh 16-byte slice.
func (h *Header) Marshal() []byte {
	b := make([]byte, HeaderSize)
	b[0] = h.Flags
	b[1] = h.Command
	bp.PutBE16(b[2:4], h.RequestID)
	bp.PutBE32(b[4:8], h.ErrorOffset)
	bp.PutBE32(b[8:12], h.DataLen)
	bp.PutBE32(b[12:16], h.Reserved)
	return b
}

// Unmarshal decodes the header from b, which must be at least HeaderSize bytes.
func (h *Header) Unmarshal(b []byte) bool {
	if len(b) < HeaderSize {
		return false
	}
	h.Flags = b[0]
	h.Command = b[1]
	h.RequestID = bp.BE16(b[2:4])
	h.ErrorOffset = bp.BE32(b[4:8])
	h.DataLen = bp.BE32(b[8:12])
	h.Reserved = bp.BE32(b[12:16])
	return true
}
