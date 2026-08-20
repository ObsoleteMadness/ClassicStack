package ncp

import (
	"fmt"
	"strings"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/link"
	ipxport "github.com/ObsoleteMadness/ClassicStack/core/port/ipx"
	ipxproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx"
	ncpproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ncp"
)

// resolve.go turns a NetWare server NAME into a routable IPX address via SAP, the step a
// real NETx/VLM shell performs before it can attach. It matters because a NetWare 3.x/4.x
// server offers its NCP file service on its INTERNAL network (a distinct IPX net, node 1)
// that is reachable only through a router hop — NOT on the local cable. A CreateConnection
// broadcast to net 0 stays on the cable and never reaches the internal net, so the server
// never answers (observed against a real NW 4.1 server: three broadcast CreateConnections,
// zero replies). Resolving the server's real IPX net/node from SAP — and learning the
// Ethernet MAC + frame type the SAP reply arrived in as the L2 next hop — lets the transport
// address the attach straight at the service. (Reference: Novell SAP, Get Nearest Server.)

// sapResolveWait bounds how long resolveServer collects SAP responses before giving up.
const sapResolveWait = 2 * time.Second

// sapResolveFrameTypes are the framings the SAP query is broadcast in when none is pinned:
// all three legacy encapsulations, since a real server is often bound only on raw-802.3 /
// 802.2 and each frame type is a distinct logical IPX network on the wire.
var sapResolveFrameTypes = []ipxport.FrameType{ipxport.FrameEthernetII, ipxport.FrameRaw8023, ipxport.FrameLLC8022}

// resolveServer broadcasts a SAP query for the File Server service and returns the address
// of the server whose name matches serverName (case-insensitive). It queries in every
// requested frame type (all three when frameTypes is nil) so a server on any binding
// answers, and records the frame type + Ethernet source MAC of the matching reply as the
// L2 next hop the transport should use. It returns an error if no matching server answers
// within sapResolveWait — the same "server not found" condition a shell reports.
func resolveServer(fl link.FrameLink, srcMAC [6]byte, serverName string, frameTypes []ipxport.FrameType) (ServerAddr, error) {
	if len(frameTypes) == 0 {
		frameTypes = sapResolveFrameTypes
	}
	want := strings.ToUpper(strings.TrimSpace(serverName))

	// Broadcast a General Query for the File Server type in each frame type.
	query := ncpproto.MarshalQuery(ncpproto.SAPGeneralQuery, ncpproto.SAPServerTypeFileServer, nil)
	d := &ipxproto.Datagram{
		Type:    ipxproto.TypePEP, // SAP rides IPX type 4 (PEP); the server accepts type 0/4
		DstNode: ipxproto.BroadcastNode,
		DstSock: ncpproto.SAPSocket,
		SrcNode: srcMAC,
		SrcSock: ncpproto.SAPSocket,
		Payload: query,
	}
	ipxBytes, err := d.Encode(nil)
	if err != nil {
		return ServerAddr{}, err
	}
	for _, ft := range frameTypes {
		if err := fl.Write(ft.Encapsulate(d.DstNode, srcMAC, ipxBytes)); err != nil {
			return ServerAddr{}, fmt.Errorf("ncp: send SAP query: %w", err)
		}
	}

	deadline := time.Now().Add(sapResolveWait)
	for time.Now().Before(deadline) {
		frame, err := fl.Read()
		if err != nil {
			if err == link.ErrTimeout {
				continue
			}
			return ServerAddr{}, fmt.Errorf("ncp: SAP resolve read: %w", err)
		}
		payload, ft, ok := ipxport.Strip(frame)
		if !ok {
			continue
		}
		dd, derr := ipxproto.Decode(payload)
		if derr != nil || (dd.DstSock != ncpproto.SAPSocket && dd.SrcSock != ncpproto.SAPSocket) {
			continue
		}
		op, entries, perr := ncpproto.ParseSAPResponse(dd.Payload)
		if perr != nil || (op != ncpproto.SAPGeneralResponse && op != ncpproto.SAPNearestResponse) {
			continue
		}
		for _, e := range entries {
			if e.Type != ncpproto.SAPServerTypeFileServer {
				continue
			}
			if strings.ToUpper(strings.TrimRight(e.Name, "\x00 ")) != want {
				continue
			}
			var mac [6]byte
			copy(mac[:], frame[6:12]) // Ethernet source of the reply = L2 next hop
			return ServerAddr{Net: e.Network, Node: e.Node, MAC: mac, FrameType: ft}, nil
		}
	}
	return ServerAddr{}, fmt.Errorf("ncp: server %q not found via SAP", serverName)
}
