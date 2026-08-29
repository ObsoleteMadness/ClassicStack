package afp

import (
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/atp"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
	"github.com/ObsoleteMadness/ClassicStack/core/router"
)

// atpRequest is one inbound ATP TReq, decoded just enough for the ASP layer: the
// transaction id, the requester's response-packet bitmap, the 4-byte UserData
// (which carries the ASP function/session/seq), and the payload after the ATP
// header (the ASP/AFP command block). The originating datagram and port are kept
// so replies route back to exactly the address the client sent to (so the reply
// SrcSocket is the SLS for GetStatus/OpenSession and the session socket for
// commands — see router.Reply).
type atpRequest struct {
	d        ddp.Datagram
	from     router.RoutedPort
	control  uint8
	bitmap   uint8
	transID  uint16
	userData uint32
	payload  []byte
}

// parseATPRequest decodes the ATP header from a DDP type-3 datagram and returns
// the request if it is a TReq. Non-TReq packets (TResp/TRel) return ok=false;
// TResp packets carrying two-phase-write data are handled via parseATPResponse.
func parseATPRequest(d ddp.Datagram, from router.RoutedPort) (atpRequest, bool) {
	h, err := atp.Decode(d.Data)
	if err != nil {
		return atpRequest{}, false
	}
	if h.FuncCode() != atp.FuncTReq {
		return atpRequest{}, false
	}
	return atpRequest{
		d:        d,
		from:     from,
		control:  h.Control,
		bitmap:   h.Bitmap,
		transID:  h.TransID,
		userData: h.UserData,
		payload:  d.Data[atp.HeaderSize:],
	}, true
}

// atpResponse is one inbound ATP TResp packet, decoded for the two-phase-write
// data path: the transaction id correlates it back to the aspDataWrite TReq the
// server sent (the workstation echoes that id), seq is the response packet's
// sequence number, eom marks the final packet of the message, and payload is the
// write data the packet carries.
type atpResponse struct {
	transID uint16
	seq     uint8
	eom     bool
	payload []byte
}

// parseATPResponse decodes a DDP type-3 datagram as an ATP TResp. Non-TResp
// packets (TReq/TRel) return ok=false.
func parseATPResponse(d ddp.Datagram) (atpResponse, bool) {
	h, err := atp.Decode(d.Data)
	if err != nil {
		return atpResponse{}, false
	}
	if h.FuncCode() != atp.FuncTResp {
		return atpResponse{}, false
	}
	return atpResponse{
		transID: h.TransID,
		seq:     h.Bitmap, // sequence number in a TResp
		eom:     h.EOM(),
		payload: d.Data[atp.HeaderSize:],
	}, true
}

// respond sends an ATP transaction response back to the requester, splitting
// data into up to atp.MaxResponsePackets packets of atp.MaxATPData bytes each.
// The same userData is carried in every packet's ATP header (ASP echoes the
// function/session/seq); EOM is set on the final packet. Each packet is sent via
// router.Reply, which addresses it back to the originator and sets the reply
// SrcSocket to the socket the client sent to.
//
// The requester's bitmap (h.Bitmap on the TReq) names which response packets it
// is prepared to receive; we honour it by only sending packets whose sequence
// bit is set, exactly as ATP requires for retransmission/partial-ack. A zero
// bitmap is treated as "packet 0 only" so a malformed request still gets a
// single reply rather than silence.
func (r *atpRequest) respond(rtr router.ServiceRouter, userData uint32, data []byte) {
	mask := r.bitmap
	if mask == 0 {
		mask = 0x01
	}

	// Number of packets the data spans (at least one, even for an empty reply).
	nPackets := (len(data) + atp.MaxATPData - 1) / atp.MaxATPData
	if nPackets == 0 {
		nPackets = 1
	}
	if nPackets > atp.MaxResponsePackets {
		nPackets = atp.MaxResponsePackets
	}

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
		h := atp.Header{
			Control:  control,
			Bitmap:   uint8(seq), // sequence number in a TResp
			TransID:  r.transID,
			UserData: userData,
		}
		frame := h.Encode(make([]byte, 0, atp.HeaderSize+len(chunk)))
		frame = append(frame, chunk...)
		rtr.Reply(r.d, r.from, atp.DDPType, frame)
	}
}
