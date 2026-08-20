// Package messenger is the wire codec for the Microsoft Messenger Service
// ([MS-MSRP]) datagram form delivered to the \MAILSLOT\MESSNGR mailslot: the
// "net send" / WinPopup pop-up message. It is NOT part of CIFS — like the
// browser, it merely borrows the SMB mailslot transport ([MS-CIFS] §1.7: "Although
// they are formatted as SMB messages, Messenger Service messages are not part of
// the CIFS protocol"). This package holds ONLY the messenger frame; the
// SMB_COM_TRANSACTION mailslot envelope that carries it is core/protocol/mailslot
// and the per-NetBIOS-transport wire framing is core/service/netbios (§3-quater).
//
// A Message is self-serialising (Marshal / Unmarshal, the DTO rule). The messenger
// service (core/service/messenger) registers for \MAILSLOT\MESSNGR on the mailslot
// router and exchanges these bare frames; it never touches the envelope.
//
// Wire format (the single-block message, [MS-MSRP] §2.2.2 "Send Single Block
// Message"): the mailslot body is one message-type byte followed by three
// NUL-terminated OEM strings —
//
//	+0   Type        1 byte   0x01 = single-block message ("net send" pop-up)
//	+1   FromName    NUL-terminated OEM string (the sender)
//	..   ToName      NUL-terminated OEM string (the recipient)
//	..   Text        NUL-terminated OEM string (the message body)
//
// The multi-block forms (0xD0 SMBsends … 0xD7, [MS-CIFS] §2.2.1.3) are the
// session/named-pipe variants and are out of scope for the mailslot datagram path.
// We have no live capture of net-send traffic (none in /captures), so per CLAUDE.md
// rule 6 this layout is documented from [MS-MSRP] and the long-stable WinPopup wire
// form; the parser is tolerant of a missing trailing NUL.
//
// Ring: CORE (stdlib only, reflection-free).
package messenger

import "errors"

// ERRATA — the single-block "net send" body carries NO message-type byte.
//
// This package used to prepend/require a TypeSingleBlock = 0x01 byte, citing
// [MS-MSRP] §2.2.2 — a document ClassicStack does not actually ship, so the value
// was never checked against anything. The wire disagrees: in
// spec/captures/nbipx-win98.pcap frames 228/229 (`net send` to the workgroup) and
// 241/242 (a directed one), the mailslot Data is exactly three NUL-terminated OEM
// strings and nothing else:
//
//	"WIN98USER\0" "WORKGROUP\0" "HELLO WORLD\0"   (Data Count 32, Data Offset 88)
//
// The 0x42 byte that sits between the Transaction Name and the data is SMB_COM_-
// TRANSACTION *padding* (Wireshark: "Padding: 42"), not a message type — reading it
// as one is what made this look like a type-byte protocol.
//
// Consequence of the old form: Unmarshal rejected every real Win98 pop-up at its
// first byte (ErrFrame), so HandleMailslot dropped it silently and no "net send"
// was ever logged or surfaced in the UI; and Marshal prepended a 0x01 that a real
// receiver would have read as the first character of the originator's name.

// ErrFrame indicates a buffer that is not a well-formed single-block messenger
// datagram (too short, wrong type, or missing the name terminators).
var ErrFrame = errors.New("messenger: invalid \\MAILSLOT\\MESSNGR datagram")

// maxField bounds each OEM string so a malformed datagram cannot make us scan an
// unbounded buffer; net-send names are NetBIOS-short and the text is a pop-up line.
const maxField = 512

// Message is one single-block messenger datagram: who it is from, who it is for,
// and the text. From/To are OEM (codepage) names as they appear on the wire; the
// service upper-cases for matching, the codec preserves them verbatim.
type Message struct {
	From string
	To   string
	Text string
}

// Marshal renders the single-block messenger datagram: From, To, and Text as
// NUL-terminated OEM strings, with no leading type byte (see the ERRATA above).
func (m Message) Marshal() []byte {
	out := make([]byte, 0, len(m.From)+len(m.To)+len(m.Text)+3)
	out = append(out, m.From...)
	out = append(out, 0)
	out = append(out, m.To...)
	out = append(out, 0)
	out = append(out, m.Text...)
	out = append(out, 0)
	return out
}

// Unmarshal parses a single-block messenger datagram: From, To, Text as
// NUL-terminated OEM strings, with no leading type byte (see the ERRATA above). A
// buffer whose From/To fields are not NUL-terminated returns ErrFrame. A missing
// terminator on the final Text field is tolerated (some senders omit it), the
// remainder being taken as the text.
func Unmarshal(b []byte) (*Message, error) {
	if len(b) < 1 {
		return nil, ErrFrame
	}
	from, rest, ok := takeCString(b)
	if !ok {
		return nil, ErrFrame
	}
	to, rest, ok := takeCString(rest)
	if !ok {
		return nil, ErrFrame
	}
	// Text may or may not carry a trailing NUL; take up to it if present.
	text, _, _ := takeCString(rest)
	return &Message{From: from, To: to, Text: text}, nil
}

// takeCString reads a NUL-terminated string (bounded by maxField) from the front of
// b, returning the string, the remainder after the terminator, and whether a
// terminator was found within bounds. When no terminator is found it returns the
// bounded remainder with ok=false so the caller can decide (Text tolerates this).
func takeCString(b []byte) (s string, rest []byte, ok bool) {
	limit := min(len(b), maxField)
	for i := range limit {
		if b[i] == 0 {
			return string(b[:i]), b[i+1:], true
		}
	}
	return string(b[:limit]), nil, false
}
