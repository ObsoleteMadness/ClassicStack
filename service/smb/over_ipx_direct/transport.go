package over_ipx_direct

import (
	"context"
	"encoding/binary"

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
)

type sessionHandler interface {
	HandleSessionContext(packet *netbiosproto.SessionPacket, ctx netbios.SessionContext) (*netbiosproto.SessionPacket, error)
}

type Transport struct {
	router  ipx.Router
	handler sessionHandler
}

func New(router ipx.Router, handler sessionHandler) *Transport {
	return &Transport{router: router, handler: handler}
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
		_ = t.router.Send(&ipxproto.Datagram{
			Type:    netbiosproto.IPXTypePEP,
			DstNet:  d.SrcNet,
			DstNode: d.SrcNode,
			DstSock: d.SrcSock,
			SrcSock: d.DstSock,
			Payload: append([]byte(nil), resp.Payload...),
		})
		return
	}

	for seq := uint16(1); seq <= echoCount; seq++ {
		payload := append([]byte(nil), resp.Payload...)
		// ECHO response Words contains only SequenceNumber at SMB+33..34.
		binary.LittleEndian.PutUint16(payload[smbHeaderLen+1:smbHeaderLen+3], seq)
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
