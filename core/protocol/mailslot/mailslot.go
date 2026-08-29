// Package mailslot is the wire codec for the Microsoft mailslot transport: the
// SMB_COM_TRANSACTION "mailslot write" that carries a connectionless, unreliable,
// one-way datagram to a named mailslot (\MAILSLOT\*). It is a GENERAL NetBIOS
// second-class datagram-delivery mechanism, owned by no single consumer — the
// browser (\MAILSLOT\BROWSE), the RAP datagram form (\MAILSLOT\LANMAN), the
// messenger (\MAILSLOT\MESSNGR, net send / WinPopup), and future consumers all
// ride it (§3-quater). This package holds ONLY the envelope codec; the consumers'
// own frames are their own packages (e.g. core/protocol/browser).
//
// A Write is self-serialising (Marshal / Unmarshal, the DTO rule): the dispatch
// layer (core/service/mailslot) wraps a consumer's body into a Write to send and
// unwraps an inbound Write to route by mailslot name. Neither the consumers nor the
// NetBIOS transports touch this codec directly except through that layer.
//
// Ring: CORE (stdlib only, reflection-free). Fixed-width fields use
// core/binaryprimitives; multi-byte fields are little-endian (SMB wire order).
package mailslot

import (
	"errors"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
)

// Well-known mailslot names (the dispatch layer routes inbound writes by these).
const (
	NameBrowse    = "\\MAILSLOT\\BROWSE"  // browser host/domain announcements + elections
	NameLANMAN    = "\\MAILSLOT\\LANMAN"  // RAP datagram form (older browse traffic)
	NameMessenger = "\\MAILSLOT\\MESSNGR" // messenger: net send / WinPopup
)

// ErrEnvelope indicates a buffer that is not a well-formed SMB_COM_TRANSACTION
// mailslot write.
var ErrEnvelope = errors.New("mailslot: invalid SMB_COM_TRANSACTION envelope")

// SMB_COM_TRANSACTION mailslot-write wire constants. The data block follows the
// byte area (mailslot name); its offset is computed from the name length rather
// than fixed, so a longer mailslot name (e.g. \MAILSLOT\MESSNGR) does not overrun.
const (
	smbHeaderLen       = 32
	txWordCount        = 17
	txWordsLen         = 34
	txByteCountOffset  = smbHeaderLen + 1 + txWordsLen // 67; ByteCount, then the name field
	commandTransaction = 0x25                          // SMB_COM_TRANSACTION
)

// Write is one mailslot write: the destination mailslot name and the consumer's
// body (the inner frame — a browser frame, a messenger message, …). Timeout/
// Priority/Class are the SMB_COM_TRANSACTION fields; the defaults match observed
// Windows traffic and need not be set by callers.
type Write struct {
	Name      string
	Body      []byte
	TimeoutMS uint32
	Priority  uint16
	Class     uint16
}

// Marshal renders the SMB_COM_TRANSACTION mailslot-write envelope: the mailslot
// Name in the byte area and the Body as the transaction data. The Name MUST be set
// (a mailslot write has no default destination at this layer — the consumer names
// it).
func (w Write) Marshal() []byte {
	nameField := append([]byte(w.Name), 0)
	timeout := w.TimeoutMS
	if timeout == 0 {
		timeout = 1000
	}
	class := w.Class
	if class == 0 {
		class = 2
	}

	// The data block starts after the byte area (ByteCount(2) + the name field),
	// so its offset tracks the name length.
	dataOffset := txByteCountOffset + 2 + len(nameField)

	out := make([]byte, dataOffset+len(w.Body))
	copy(out[0:4], "\xffSMB")
	out[4] = commandTransaction
	out[smbHeaderLen] = txWordCount
	words := out[smbHeaderLen+1 : smbHeaderLen+1+txWordsLen]
	bp.PutLE16(words[2:4], uint16(len(w.Body)))   // TotalDataCount
	bp.PutLE32(words[12:16], timeout)             // Timeout (ULONG)
	bp.PutLE16(words[22:24], uint16(len(w.Body))) // DataCount
	bp.PutLE16(words[24:26], uint16(dataOffset))  // DataOffset
	words[26] = 3                                 // SetupCount
	bp.PutLE16(words[28:30], 1)                   // Setup word (legacy fixed)
	bp.PutLE16(words[30:32], w.Priority)
	bp.PutLE16(words[32:34], class)

	bp.PutLE16(out[txByteCountOffset:txByteCountOffset+2], uint16(len(nameField)+len(w.Body)))
	copy(out[txByteCountOffset+2:], nameField)
	copy(out[dataOffset:], w.Body)
	return out
}

// Unmarshal parses an SMB_COM_TRANSACTION mailslot write, extracting the mailslot
// name and the body data window. A buffer that is not such a write returns
// ErrEnvelope.
func Unmarshal(b []byte) (*Write, error) {
	if len(b) < txByteCountOffset+2 || string(b[0:4]) != "\xffSMB" {
		return nil, ErrEnvelope
	}
	if b[4] != commandTransaction || b[smbHeaderLen] != txWordCount {
		return nil, ErrEnvelope
	}
	words := b[smbHeaderLen+1 : smbHeaderLen+1+txWordsLen]
	dataCount := int(bp.LE16(words[22:24]))
	dataOffset := int(bp.LE16(words[24:26]))
	if dataCount == 0 || dataOffset < txByteCountOffset+2 || dataOffset > len(b) || dataOffset+dataCount > len(b) {
		return nil, ErrEnvelope
	}
	byteStart := txByteCountOffset + 2
	nameEnd := indexByte(b[byteStart:dataOffset], 0)
	if nameEnd < 0 {
		return nil, ErrEnvelope
	}
	return &Write{
		Name:      string(b[byteStart : byteStart+nameEnd]),
		Body:      append([]byte(nil), b[dataOffset:dataOffset+dataCount]...),
		TimeoutMS: bp.LE32(words[12:16]),
		Priority:  bp.LE16(words[30:32]),
		Class:     bp.LE16(words[32:34]),
	}, nil
}

func indexByte(b []byte, c byte) int {
	for i, x := range b {
		if x == c {
			return i
		}
	}
	return -1
}
