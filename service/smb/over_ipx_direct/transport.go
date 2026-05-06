package over_ipx_direct

import (
	"context"

	ipxproto "github.com/ObsoleteMadness/ClassicStack/protocol/ipx"
	netbiosproto "github.com/ObsoleteMadness/ClassicStack/protocol/netbios"
	"github.com/ObsoleteMadness/ClassicStack/router/ipx"
	"github.com/ObsoleteMadness/ClassicStack/service/netbios"
)

var directSMBSocket = [2]byte{0x05, 0x50}

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
	resp, err := t.handler.HandleSessionContext(&netbiosproto.SessionPacket{
		Type:    netbiosproto.SessionMessage,
		Payload: append([]byte(nil), d.Payload...),
	}, netbios.SessionContext{
		Local: netbios.DatagramEndpoint{Network: d.DstNet, Node: d.DstNode, Socket: d.DstSock},
		Remote: netbios.DatagramEndpoint{Network: d.SrcNet, Node: d.SrcNode, Socket: d.SrcSock},
	})
	if err != nil || resp == nil || len(resp.Payload) == 0 {
		return
	}
	_ = t.router.Send(&ipxproto.Datagram{
		Type:    netbiosproto.IPXTypePEP,
		DstNet:  d.SrcNet,
		DstNode: d.SrcNode,
		DstSock: d.SrcSock,
		SrcSock: d.DstSock,
		Payload: append([]byte(nil), resp.Payload...),
	})
}
