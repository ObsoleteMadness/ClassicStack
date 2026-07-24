package atalk

import (
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/atp"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
)

// responder.go is the small ATP RESPONDER surface a workstation needs even though it
// is mostly a requester: an AFP server drives a two-phase FPWrite by sending the
// workstation a server-initiated aspDataWrite TReq (asking it to send the write data),
// and keeps the session alive with server-initiated Tickle/Attention/CloseSession
// TReqs. The client session (client/asp) binds its session socket and dispatches those
// inbound TReqs here.

// InboundTReq is a server-initiated ATP TReq received on a bound socket, decoded for
// the client session: the transaction id (to echo in the TResp), the requester bitmap,
// the UserData (ASP function/session/seq), the data payload, and the datagram it came
// on (so a TResp routes back to the sender).
type InboundTReq struct {
	Datagram ddp.Datagram
	Control  uint8
	Bitmap   uint8
	TransID  uint16
	UserData uint32
	Payload  []byte
}

// XO reports whether the TReq requested exactly-once delivery (the aspDataWrite is XO).
func (r InboundTReq) XO() bool { return r.Control&atp.XO != 0 }

// DecodeTReq decodes a datagram as an ATP TReq. ok is false when it is not a type-3 ATP
// TReq.
func DecodeTReq(d ddp.Datagram) (InboundTReq, bool) {
	if d.DDPType != atp.DDPType {
		return InboundTReq{}, false
	}
	h, err := atp.Decode(d.Data)
	if err != nil || h.FuncCode() != atp.FuncTReq {
		return InboundTReq{}, false
	}
	return InboundTReq{
		Datagram: d,
		Control:  h.Control,
		Bitmap:   h.Bitmap,
		TransID:  h.TransID,
		UserData: h.UserData,
		Payload:  append([]byte(nil), d.Data[atp.HeaderSize:]...),
	}, true
}

// RespondTReq sends the TResp set answering req, splitting data into up to 8 packets of
// atp.MaxATPData bytes and honouring the requester's bitmap. userData is carried in
// every packet's header; EOM marks the last. The reply is sourced from the socket the
// TReq was addressed to (req.Datagram.DestSocket) and sent back to the requester.
func (e *Endpoint) RespondTReq(req InboundTReq, userData uint32, data []byte) error {
	mask := req.Bitmap
	if mask == 0 {
		mask = 0x01
	}
	nPackets := (len(data) + atp.MaxATPData - 1) / atp.MaxATPData
	if nPackets == 0 {
		nPackets = 1
	}
	if nPackets > atp.MaxResponsePackets {
		nPackets = atp.MaxResponsePackets
	}

	dst := Addr{Network: req.Datagram.SrcNetwork, Node: req.Datagram.SrcNode, Socket: req.Datagram.SrcSocket}
	srcSocket := req.Datagram.DestSocket

	for seq := 0; seq < nPackets; seq++ {
		if mask&(1<<uint(seq)) == 0 {
			continue
		}
		start := seq * atp.MaxATPData
		end := min(start+atp.MaxATPData, len(data))
		var chunk []byte
		if start < len(data) {
			chunk = data[start:end]
		}
		control := uint8(atp.TRESP)
		if seq == nPackets-1 {
			control |= atp.EOM
		}
		h := atp.Header{Control: control, Bitmap: uint8(seq), TransID: req.TransID, UserData: userData}
		frame := h.Encode(make([]byte, 0, atp.HeaderSize+len(chunk)))
		frame = append(frame, chunk...)
		if err := e.Send(dst, srcSocket, atp.DDPType, frame); err != nil {
			return err
		}
	}
	return nil
}
