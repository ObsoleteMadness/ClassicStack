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

// Message-type byte for the single-block messenger datagram ([MS-MSRP] §2.2.2).
const (
	// TypeSingleBlock is the one-datagram "net send" / WinPopup message: a sender,
	// a recipient, and the text, all in one mailslot write.
	TypeSingleBlock uint8 = 0x01
)

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

// Marshal renders the single-block messenger datagram: the type byte followed by
// From, To, and Text as NUL-terminated OEM strings.
func (m Message) Marshal() []byte {
	out := make([]byte, 0, 1+len(m.From)+len(m.To)+len(m.Text)+3)
	out = append(out, TypeSingleBlock)
	out = append(out, m.From...)
	out = append(out, 0)
	out = append(out, m.To...)
	out = append(out, 0)
	out = append(out, m.Text...)
	out = append(out, 0)
	return out
}

// Unmarshal parses a single-block messenger datagram. A buffer that is not a
// single-block message (wrong/absent type byte) or whose name fields are not
// NUL-terminated returns ErrFrame. A missing terminator on the final Text field is
// tolerated (some senders omit it), the remainder being taken as the text.
func Unmarshal(b []byte) (*Message, error) {
	if len(b) < 1 || b[0] != TypeSingleBlock {
		return nil, ErrFrame
	}
	rest := b[1:]
	from, rest, ok := takeCString(rest)
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
