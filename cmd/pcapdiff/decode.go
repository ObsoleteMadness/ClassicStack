package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/google/gopacket/pcapgo"

	patp "github.com/ObsoleteMadness/ClassicStack/protocol/atp"
	"github.com/ObsoleteMadness/ClassicStack/protocol/ddp"
	"github.com/ObsoleteMadness/ClassicStack/protocol/llap"
)

// Event is one decoded packet from a pcap file. Layers below DDP that
// fail to decode produce an Event with Note set; partial decode is
// preserved up to the layer that failed so divergence is still visible.
type Event struct {
	Index     int       `json:"i"`
	Timestamp time.Time `json:"ts"`
	WireLen   int       `json:"wire_len"`
	Source    string    `json:"src"` // e.g. "1.123" (network.node) or "-" if pre-DDP
	Dest      string    `json:"dst"`
	DDPType   uint8     `json:"ddp_type"`
	ATPFunc   string    `json:"atp_func,omitempty"` // TReq/TResp/TRel
	ATPTID    uint16    `json:"atp_tid,omitempty"`
	ATPBitSeq uint8     `json:"atp_bitseq,omitempty"`
	UserData  uint32    `json:"atp_user,omitempty"`
	PayloadSz int       `json:"payload"`
	Note      string    `json:"note,omitempty"`
}

// decodePcap reads a pcap file, decodes each packet to Event, and
// returns the slice. The pcap link type is auto-detected.
func decodePcap(path string) ([]Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	r, err := pcapgo.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	lt := r.LinkType()
	var events []Event
	for i := 0; ; i++ {
		data, ci, err := r.ReadPacketData()
		if err == io.EOF {
			break
		}
		if err != nil {
			return events, fmt.Errorf("packet %d: %w", i, err)
		}
		ev := Event{Index: i, Timestamp: ci.Timestamp, WireLen: ci.Length}
		switch uint(lt) {
		case 114: // DLT_LTALK
			decodeLLAP(&ev, data)
		case 1: // DLT_EN10MB
			decodeEthernet(&ev, data)
		default:
			ev.Note = fmt.Sprintf("unsupported linktype %d", lt)
		}
		events = append(events, ev)
	}
	return events, nil
}

func decodeLLAP(ev *Event, data []byte) {
	frame, err := llap.FrameFromBytes(data)
	if err != nil {
		ev.Note = "llap: " + err.Error()
		return
	}
	switch frame.Type {
	case llap.TypeAppleTalkShortHeader:
		d, err := ddp.DatagramFromShortHeaderBytes(frame.DestinationNode, frame.SourceNode, frame.Payload)
		if err != nil {
			ev.Note = "ddp-short: " + err.Error()
			return
		}
		fillDDP(ev, d)
	case llap.TypeAppleTalkLongHeader:
		d, err := ddp.DatagramFromLongHeaderBytes(frame.Payload, false)
		if err != nil {
			ev.Note = "ddp-long: " + err.Error()
			return
		}
		fillDDP(ev, d)
	default:
		ev.Note = fmt.Sprintf("llap-control 0x%02x", frame.Type)
	}
}

func decodeEthernet(ev *Event, data []byte) {
	if len(data) < 14 {
		ev.Note = "eth: short"
		return
	}
	ethType := uint16(data[12])<<8 | uint16(data[13])
	payload := data[14:]
	if ethType <= 1500 {
		// 802.3 length + LLC/SNAP. Need at least 8 bytes of LLC/SNAP
		// (DSAP, SSAP, CTL, OUI[3], PID[2]).
		if len(payload) < 8 {
			ev.Note = "snap: short"
			return
		}
		// AppleTalk DDP: OUI 08:00:07, PID 80:9b
		// AARP:          OUI 00:00:00, PID 80:f3
		oui := payload[3:6]
		pid := uint16(payload[6])<<8 | uint16(payload[7])
		body := payload[8:]
		switch {
		case oui[0] == 0x08 && oui[1] == 0x00 && oui[2] == 0x07 && pid == 0x809b:
			d, err := ddp.DatagramFromLongHeaderBytes(body, false)
			if err != nil {
				ev.Note = "ddp-eth: " + err.Error()
				return
			}
			fillDDP(ev, d)
		case pid == 0x80f3:
			ev.Note = "aarp"
		default:
			ev.Note = fmt.Sprintf("snap pid 0x%04x", pid)
		}
		return
	}
	// EtherType II — uncommon for AppleTalk on this stack but handle.
	switch ethType {
	case 0x809b:
		d, err := ddp.DatagramFromLongHeaderBytes(payload, false)
		if err != nil {
			ev.Note = "ddp-eth2: " + err.Error()
			return
		}
		fillDDP(ev, d)
	case 0x80f3:
		ev.Note = "aarp-eth2"
	default:
		ev.Note = fmt.Sprintf("ethertype 0x%04x", ethType)
	}
}

func fillDDP(ev *Event, d ddp.Datagram) {
	ev.Source = fmt.Sprintf("%d.%d", d.SourceNetwork, d.SourceNode)
	ev.Dest = fmt.Sprintf("%d.%d", d.DestinationNetwork, d.DestinationNode)
	ev.DDPType = d.DDPType
	ev.PayloadSz = len(d.Data)
	if d.DDPType == patp.DDPType {
		var h patp.Header
		if err := h.Unmarshal(d.Data); err == nil {
			ev.ATPFunc = atpFuncName(h.FuncCode())
			ev.ATPTID = h.TransID
			ev.ATPBitSeq = h.Bitmap
			ev.UserData = h.UserData
		}
	}
}

func atpFuncName(fc patp.FuncCode) string {
	switch fc {
	case patp.FuncTReq:
		return "TReq"
	case patp.FuncTResp:
		return "TResp"
	case patp.FuncTRel:
		return "TRel"
	default:
		return fmt.Sprintf("0x%02x", uint8(fc))
	}
}
