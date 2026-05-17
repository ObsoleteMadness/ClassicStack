// Package over_ipx_direct implements SMB-over-IPX direct hosting transport.
package over_ipx_direct

import (
	"context"
	"encoding/binary"
	"sync"

	ipxproto "github.com/ObsoleteMadness/ClassicStack/protocol/ipx"
	netbiosproto "github.com/ObsoleteMadness/ClassicStack/protocol/netbios"
	"github.com/ObsoleteMadness/ClassicStack/router/ipx"
	"github.com/ObsoleteMadness/ClassicStack/service/netbios"
)

var directSMBSocket = [2]byte{0x05, 0x50}

const (
	smbHeaderLen    = 32
	smbCommandOff   = 4
	smbStatusOff    = 5
	smbWordCountOff = smbHeaderLen
	echoCommand     = 0x2b
	negotiateCmd    = 0x72

	// Over Direct IPX, SMB header SecurityFeatures[8] (bytes 14..21) holds:
	//   bytes 14..17: Key (ULONG)
	//   bytes 18..19: CID (USHORT) — Connection ID, server-generated
	//   bytes 20..21: SequenceNumber (USHORT) — echoed back in responses
	// See [MS-CIFS] 2.2.3.1 and 2.2.1.6.4.
	smbOffCID            = 18
	smbOffSequenceNumber = 20
)

type sessionHandler interface {
	HandleSessionContext(packet *netbiosproto.SessionPacket, ctx netbios.SessionContext) (*netbiosproto.SessionPacket, error)
}

type Transport struct {
	router  ipx.Router
	handler sessionHandler

	cidMu     sync.Mutex
	cids      map[[10]byte]uint16 // remote endpoint (network+node) → CID
	nextCID   uint16
}

func New(router ipx.Router, handler sessionHandler) *Transport {
	return &Transport{
		router:  router,
		handler: handler,
		cids:    make(map[[10]byte]uint16),
		nextCID: 1, // 0x0000 and 0xFFFF are reserved per [MS-CIFS] 2.2.1.6.4.
	}
}

// cidFor returns the CID assigned to the given remote endpoint, allocating
// one on the first call. The CID space wraps over 0xFFFE valid values
// (0x0000 and 0xFFFF reserved). Practical client counts are far below that.
func (t *Transport) cidFor(network [4]byte, node [6]byte, allocate bool) uint16 {
	var key [10]byte
	copy(key[0:4], network[:])
	copy(key[4:10], node[:])

	t.cidMu.Lock()
	defer t.cidMu.Unlock()
	if cid, ok := t.cids[key]; ok {
		return cid
	}
	if !allocate {
		return 0
	}
	cid := t.nextCID
	t.nextCID++
	if t.nextCID == 0xFFFF {
		t.nextCID = 1
	}
	t.cids[key] = cid
	return cid
}

func (t *Transport) Start(_ context.Context) error {
	if t == nil || t.router == nil {
		return nil
	}
	return t.router.RegisterSocket(directSMBSocket, t)
}

func (t *Transport) Stop() error { return nil }

func (t *Transport) HandleDatagram(d *ipxproto.Datagram) {
	if t == nil || d == nil || t.handler == nil {
		return
	}
	if d.Type != netbiosproto.IPXTypePEP {
		return
	}
	if len(d.Payload) < 4 || string(d.Payload[:4]) != "\xffSMB" {
		return
	}
	// Ignore SMB responses on ingress; only requests should be dispatched.
	if len(d.Payload) > 9 && (d.Payload[9]&0x80) != 0 {
		return
	}
	// Allocate a CID on NEGOTIATE; on later commands, look up the CID
	// previously assigned to this remote. Per [MS-CIFS] 2.2.1.6.4 the
	// server generates the CID and embeds it in the NEGOTIATE response;
	// the client then carries it on every subsequent message and we
	// echo it back. 0x0000/0xFFFF are reserved as CID values.
	allocate := len(d.Payload) > smbCommandOff && d.Payload[smbCommandOff] == negotiateCmd
	cid := t.cidFor(d.SrcNet, d.SrcNode, allocate)

	resp, err := t.handler.HandleSessionContext(&netbiosproto.SessionPacket{
		Type:    netbiosproto.SessionMessage,
		Payload: append([]byte(nil), d.Payload...),
	}, netbios.SessionContext{
		Local:  netbios.DatagramEndpoint{Network: d.DstNet, Node: d.DstNode, Socket: d.DstSock},
		Remote: netbios.DatagramEndpoint{Network: d.SrcNet, Node: d.SrcNode, Socket: d.SrcSock},
	})
	if err != nil || resp == nil || len(resp.Payload) == 0 {
		return
	}
	echoCount := echoResponseCount(d.Payload, resp.Payload)
	if echoCount <= 1 {
		payload := append([]byte(nil), resp.Payload...)
		stampConnectionlessHeader(payload, d.Payload, cid)
		_ = t.router.Send(&ipxproto.Datagram{
			Type:    netbiosproto.IPXTypePEP,
			DstNet:  d.SrcNet,
			DstNode: d.SrcNode,
			DstSock: d.SrcSock,
			SrcSock: d.DstSock,
			Payload: payload,
		})
		return
	}

	for seq := uint16(1); seq <= echoCount; seq++ {
		payload := append([]byte(nil), resp.Payload...)
		// ECHO response Words contains only SequenceNumber at SMB+33..34.
		binary.LittleEndian.PutUint16(payload[smbHeaderLen+1:smbHeaderLen+3], seq)
		stampConnectionlessHeader(payload, d.Payload, cid)
		_ = t.router.Send(&ipxproto.Datagram{
			Type:    netbiosproto.IPXTypePEP,
			DstNet:  d.SrcNet,
			DstNode: d.SrcNode,
			DstSock: d.SrcSock,
			SrcSock: d.DstSock,
			Payload: payload,
		})
	}
}

// stampConnectionlessHeader writes the CID and SequenceNumber into the
// SMB header SecurityFeatures field, as required for connectionless
// transports per [MS-CIFS] 2.2.3.1. SequenceNumber is mirrored from the
// client's request so the redirector can match request to response;
// the Key field (bytes 14..17) is left zero since we do not negotiate
// connection-level signing over IPX.
func stampConnectionlessHeader(resp, req []byte, cid uint16) {
	if len(resp) < smbHeaderLen || len(req) < smbHeaderLen {
		return
	}
	if reqCID := binary.LittleEndian.Uint16(req[smbOffCID : smbOffCID+2]); reqCID != 0 && reqCID != 0xFFFF {
		cid = reqCID
	}
	binary.LittleEndian.PutUint16(resp[smbOffCID:smbOffCID+2], cid)
	copy(resp[smbOffSequenceNumber:smbOffSequenceNumber+2],
		req[smbOffSequenceNumber:smbOffSequenceNumber+2])
}

func echoResponseCount(reqPayload, respPayload []byte) uint16 {
	if len(reqPayload) < smbHeaderLen+5 || len(respPayload) < smbHeaderLen+5 {
		return 1
	}
	if reqPayload[smbCommandOff] != echoCommand || respPayload[smbCommandOff] != echoCommand {
		return 1
	}
	// Multi-response applies only to successful SMB_COM_ECHO responses.
	if binary.LittleEndian.Uint32(respPayload[smbStatusOff:smbStatusOff+4]) != 0 {
		return 1
	}
	if reqPayload[smbWordCountOff] != 1 || respPayload[smbWordCountOff] != 1 {
		return 1
	}
	c := binary.LittleEndian.Uint16(reqPayload[smbHeaderLen+1 : smbHeaderLen+3])
	if c == 0 {
		return 1
	}
	return c
}
