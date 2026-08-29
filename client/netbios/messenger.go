package netbios

import (
	mailslotproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/mailslot"
	messengerproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/messenger"
	nb "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbios"
)

// messenger.go is the SDK's "net send" primitive: build the single-block Messenger
// datagram (\MAILSLOT\MESSNGR) and transmit it over a datagram carrier. This is the send
// half csnetsend was missing — the tool now parses flags and calls SendMessage.

// MessengerNameType is the NetBIOS name-type suffix (<03>) a Messenger-service recipient
// registers under; a "net send" target is addressed at this type. Exposed so a tool's
// target parse (ParseTarget) stamps the right suffix.
const MessengerNameType = nb.NameTypeMessenger

// Message is one pop-up message: who it is from, who it is for, and the text. It mirrors
// core/protocol/messenger.Message but is the SDK-facing form so a consumer need not
// import the core codec.
type Message struct {
	From string
	To   string
	Text string
}

// SendMessage delivers msg as a single-block Messenger datagram to dst over this carrier.
// It builds the [MS-MSRP] single-block frame (From/To/Text), which SendMailslot wraps in
// the \MAILSLOT\MESSNGR envelope and emits — directed to the one recipient (a net send is
// not a broadcast). dst.Name should carry the Messenger name-type (MessengerNameType); a
// Target parsed with ParseTarget(..., MessengerNameType) already does.
//
// Delivery is connectionless and unacknowledged (the Messenger datagram has no reply at
// this layer — the recipient pops up the message or drops it), so a nil return means the
// datagram was transmitted, not that it was received.
func (c *Conn) SendMessage(dst nb.Name, msg Message) error {
	body := messengerproto.Message{From: msg.From, To: msg.To, Text: msg.Text}.Marshal()
	return c.SendMailslot(mailslotproto.NameMessenger, dst, body, false)
}
