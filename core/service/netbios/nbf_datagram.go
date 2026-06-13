package netbios

// nbf_datagram.go carries the two connectionless NBF paths the responder answers
// alongside the session machine: the node-status query (STATUS_QUERY →
// STATUS_RESPONSE, how nbtstat -A and browser elections probe a node) and the
// directed/broadcast datagram (mailslot / browser traffic, routed to the optional
// DatagramConsumer). Neither path touches the virtual-circuit state — a node-status
// reply is built from the engine's own name set, and a datagram is decoded to
// names + payload and handed up. Both reach the wire only through the FrameSender
// seam, exactly like the session path (§3-bis).

import (
	proto "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbeui"
	nbf "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbios"
)

// statusEntryLen is the wire length of one name entry in a NODE_STATUS payload:
// the 16-byte name, a name-number byte, and a flags byte ([IBM SC30-3587] §5,
// the ADAPTER.STATUS name table).
const statusEntryLen = 18

// status flag bits in a node-status name entry.
const (
	statusFlagGroup uint8 = 0x80 // the name is a group name
)

// data2 length-field bits in a STATUS_RESPONSE: the low 14 bits carry the payload
// length, the top two bits signal truncation (the requester's buffer was too small
// to carry the whole table).
const (
	statusLenMask    uint16 = 0x3FFF
	statusFlagMore   uint16 = 0x8000 // more data than returned (table longer than buffer)
	statusFlagTooBig uint16 = 0x4000 // requester buffer too small for even one entry
)

// handleStatusQuery answers a STATUS_QUERY (NODE.STATUS) for one of our names with
// a STATUS_RESPONSE carrying the local name table, truncated to the buffer length
// the requester advertised in Data2. A query for a name we do not own is ignored
// (not addressed to us). Mirrors the legacy over_netbeui handleStatusQuery.
func (e *sessionEngine) handleStatusQuery(srcMAC [6]byte, frame *proto.Frame) {
	queried := nbf.Name(frame.DestinationName)
	if !e.ownsName(queried) {
		return
	}
	payload, more, tooBig := e.buildStatusPayload(frame.Data2)
	data2 := uint16(len(payload)) & statusLenMask
	if more {
		data2 |= statusFlagMore
	}
	if tooBig {
		data2 |= statusFlagTooBig
	}

	resp := &proto.Frame{
		Command:        proto.CmdStatusResponse,
		XmitCorrelator: frame.RspCorrelator,
		Data2:          data2,
		Payload:        payload,
	}
	copy(resp.DestinationName[:], frame.SourceName[:])
	copy(resp.SourceName[:], queried[:])
	e.send(srcMAC, resp, "status-response")
}

// buildStatusPayload renders the local name table as 18-byte entries, truncated to
// the requester's advertised buffer length (Data2). It returns the payload and two
// truncation flags: more (the table was longer than the buffer) and tooBig (the
// buffer could not hold even one whole entry). A zero buffer length is treated as
// "tell me the size" — no payload, both flags set when any names exist.
func (e *sessionEngine) buildStatusPayload(requestedBufLen uint16) (payload []byte, more, tooBig bool) {
	names := e.names()
	if len(names) == 0 {
		return nil, false, false
	}
	full := make([]byte, 0, len(names)*statusEntryLen)
	for _, n := range names {
		entry := make([]byte, statusEntryLen)
		copy(entry[0:nbf.NameLength], n[:])
		entry[16] = n.Type() // name-number byte = the NetBIOS suffix
		if n.Type() == nbf.NameTypeGroup {
			entry[17] = statusFlagGroup
		}
		full = append(full, entry...)
	}

	maxLen := int(requestedBufLen)
	if maxLen <= 0 {
		return nil, true, true // size probe — report truncation so the client re-asks
	}
	if len(full) <= maxLen {
		return full, false, false
	}
	if maxLen < statusEntryLen {
		return nil, true, true
	}
	truncLen := (maxLen / statusEntryLen) * statusEntryLen
	return full[:truncLen], true, true
}

// handleDatagram decodes a directed or broadcast NBF datagram to its source and
// destination names plus payload and hands it to the installed DatagramConsumer (a
// browser / mailslot service). With no consumer wired the datagram is dropped after
// decode — the listening file server has no use for it, but the path is complete so
// a browser service can plug in without touching the transport.
func (e *sessionEngine) handleDatagram(frame *proto.Frame, broadcast bool) {
	consumer := e.dgram()
	if consumer == nil {
		return
	}
	consumer.HandleDatagram(Datagram{
		Source:      nbf.Name(frame.SourceName),
		Destination: nbf.Name(frame.DestinationName),
		Payload:     append([]byte(nil), frame.Payload...),
		Broadcast:   broadcast,
	})
}
