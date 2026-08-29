package netbios

// nbipx_name_test.go covers the NB-IPX name-service, browser-mailslot and name-claim
// paths the engine answers alongside the session machine (nbipx_test.go covers the
// session path): the NMPI Query-name → Name-found resolution a WfW/Win9x client uses
// to locate CLASSICSTACK, the NMPI MailslotSend delivery to the browser (with its
// directed-reply endpoint), and the start-time ClaimName conflict detection.

import (
	"context"
	"testing"
	"time"

	ipxproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx"
	protocol "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbios"
	ipxrouter "github.com/ObsoleteMadness/ClassicStack/core/router/ipx"
)

// nmpiQuery builds an inbound NMPI Query-name (0xF3) datagram on the name-query
// socket (0x0551) addressed to the router, sourced from the test peer.
func nmpiQuery(requested protocol.Name) *ipxproto.Datagram {
	body := protocol.EncodeNMPIPacket(&protocol.NMPIPacket{
		Opcode:        protocol.NMPIOpNameQuery,
		NameType:      protocol.NMPINameTypeMachine,
		MessageID:     0x1234,
		RequestedName: requested,
		SourceName:    protocol.NewName("WIN98", protocol.NameTypeWorkstation),
	})
	return &ipxproto.Datagram{
		Type:    protocol.IPXTypeNetBIOS,
		DstNet:  ipxrouter.DefaultNetwork,
		DstNode: ipxrouter.BroadcastNode,
		DstSock: NBIPXNameQuerySocket,
		SrcNet:  testPeerNet,
		SrcNode: testPeerNode,
		SrcSock: NBIPXNameQuerySocket,
		Payload: body,
	}
}

// TestNBIPX_NameQueryAnswered proves an NMPI Query-name (0xF3) for our server name
// is answered with a Name-found (0xF4) reply unicast to the querier, echoing the
// message ID — how a WfW/Win9x client resolves CLASSICSTACK before opening a session.
func TestNBIPX_NameQueryAnswered(t *testing.T) {
	_, r, port, _ := newWiredIPXEngine(t)
	requested := protocol.NewName("CLASSICSTACK", protocol.NameTypeFileServer)
	r.Inbound(nmpiQuery(requested))

	if len(port.sent) != 1 {
		t.Fatalf("Query-name produced %d replies, want 1", len(port.sent))
	}
	reply := port.sent[0]
	if reply.Type != protocol.IPXTypePEP {
		t.Errorf("reply IPX type = %#x, want PEP(0x04)", reply.Type)
	}
	if reply.DstNode != testPeerNode {
		t.Errorf("reply dst node = % x, want the querier", reply.DstNode)
	}
	p, err := protocol.DecodeNMPIPacket(reply.Payload)
	if err != nil {
		t.Fatalf("DecodeNMPIPacket: %v", err)
	}
	if p.Opcode != protocol.NMPIOpNameFound {
		t.Errorf("reply opcode = %#x, want NameFound(0xF4)", p.Opcode)
	}
	if p.MessageID != 0x1234 {
		t.Errorf("reply MessageID = %#x, want the query's 0x1234", p.MessageID)
	}
	if p.RequestedName != requested {
		t.Errorf("reply RequestedName = %q, want CLASSICSTACK", p.RequestedName.String())
	}
}

// TestNBIPX_NameQueryForeignNameIgnored proves a Query-name for a name we do not own
// draws no reply (no negative response on the wire).
func TestNBIPX_NameQueryForeignNameIgnored(t *testing.T) {
	_, r, port, _ := newWiredIPXEngine(t)
	r.Inbound(nmpiQuery(protocol.NewName("SOMEONEELSE", protocol.NameTypeFileServer)))
	if len(port.sent) != 0 {
		t.Fatalf("Query-name for a foreign name produced %d replies, want 0", len(port.sent))
	}
}

// ipxFindName builds an inbound type-20 Find-name (0x01) name-service datagram on the
// session socket (0x0455) — the resolution path WIN98-2 uses in captures/ipx.pcap —
// broadcast to the router, sourced from the test peer.
func ipxFindName(requested protocol.Name) *ipxproto.Datagram {
	body := protocol.EncodeNameService(&protocol.NBIPXNameServicePacket{
		NameTypeFlag:   0x00,
		DataStreamType: protocol.NBIPXFindName,
		Name:           requested,
	})
	return &ipxproto.Datagram{
		Type:    protocol.IPXTypeNetBIOS,
		DstNet:  ipxrouter.DefaultNetwork,
		DstNode: ipxrouter.BroadcastNode,
		DstSock: NBIPXSessionSocket,
		SrcNet:  testPeerNet,
		SrcNode: testPeerNode,
		SrcSock: NBIPXSessionSocket,
		Payload: body,
	}
}

// TestNBIPX_FindNameAnswered proves a type-20 Find-name (0x01) for our server name is
// answered with a Name-recognized (0x02) reply unicast to the querier — the resolution
// path a WfW/Win9x client that broadcasts on 0x0455 (WIN98-2 in captures/ipx.pcap) uses,
// as opposed to the NMPI Query on 0x0551. The reply must (ERRATA captures/ipx.pcap) be an
// IPX type-4 (PEP) datagram carrying the self-identifying prefix — our own name + workgroup
// + the 0x44 (In-use|Registered) status flag — that the client validates before it follows
// up with SESSION_INITIALIZE. A zero-prefixed type-20 reply was ignored (no session opened).
func TestNBIPX_FindNameAnswered(t *testing.T) {
	svc, r, port, _ := newWiredIPXEngine(t)
	svc.SetWorkgroup("WORKGROUP")
	requested := protocol.NewName("CLASSICSTACK", protocol.NameTypeFileServer)
	r.Inbound(ipxFindName(requested))

	if len(port.sent) != 1 {
		t.Fatalf("Find-name produced %d replies, want 1", len(port.sent))
	}
	reply := port.sent[0]
	if reply.Type != protocol.IPXTypePEP {
		t.Errorf("reply IPX type = %#x, want PEP(0x04) — a type-20 reply is ignored by the client", reply.Type)
	}
	if reply.DstNode != testPeerNode {
		t.Errorf("reply dst node = % x, want the querier (unicast)", reply.DstNode)
	}
	// The trailing name field (offset 34) carries the queried name; DecodeNameService reads
	// exactly that tail, so it still identifies the resolved name.
	ns, err := protocol.DecodeNameService(reply.Payload)
	if err != nil {
		t.Fatalf("DecodeNameService: %v", err)
	}
	if ns.DataStreamType != protocol.NBIPXNameRecognized {
		t.Errorf("reply stream type = %#x, want NameRecognized(0x02)", ns.DataStreamType)
	}
	if ns.Name != requested {
		t.Errorf("reply name = %q, want CLASSICSTACK", ns.Name.String())
	}
	// The critical prefix a zero-fill got wrong: status flag 0x44 at [32], our own name at
	// [2], workgroup at [18].
	p := reply.Payload
	if len(p) != protocol.NBIPXNameServiceLen {
		t.Fatalf("reply len = %d, want %d", len(p), protocol.NBIPXNameServiceLen)
	}
	if p[protocol.NBIPXWANRouterBytes] != protocol.NBIPXNameRecogNameFlag {
		t.Errorf("status flag [32] = %#x, want 0x44 (In-use|Registered)", p[protocol.NBIPXWANRouterBytes])
	}
	own := protocol.NewName("CLASSICSTACK", protocol.NameTypeWorkstation)
	if got := p[2 : 2+protocol.NameLength]; string(got) != string(own[:]) {
		t.Errorf("reply own-name [2:18] = %q, want our workstation name CLASSICSTACK", got)
	}
	if wg := string(p[18:27]); wg != "WORKGROUP" {
		t.Errorf("reply workgroup [18:] = %q, want WORKGROUP", wg)
	}
}

// TestNBIPX_FindNameReplyMatchesCapture pins the reply bytes to the observed WIN98-1
// NAME_RECOGNIZED reply (captures/ipx.pcap frame 54, answering Find-name WIN98-1<20>): a
// byte-for-byte regression guard so the self-identifying prefix cannot silently drift back
// to the zero-fill the client ignores.
func TestNBIPX_FindNameReplyMatchesCapture(t *testing.T) {
	// Frame 54 IPX payload (50 bytes): [10][02][WIN98-1..00][WORKGROUP(14)][44][02][WIN98-1..20].
	want := []byte{
		0x10, 0x02,
		'W', 'I', 'N', '9', '8', '-', '1', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', 0x00,
		'W', 'O', 'R', 'K', 'G', 'R', 'O', 'U', 'P', ' ', ' ', ' ', ' ', ' ',
		0x44, 0x02,
		'W', 'I', 'N', '9', '8', '-', '1', ' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ', 0x20,
	}
	own := protocol.NewName("WIN98-1", protocol.NameTypeWorkstation)
	queried := protocol.NewName("WIN98-1", protocol.NameTypeFileServer)
	got := protocol.EncodeNameRecognized(own, "WORKGROUP", queried)
	if string(got) != string(want) {
		t.Errorf("EncodeNameRecognized mismatch\n got % x\nwant % x", got, want)
	}
}

// TestNBIPX_FindNameForeignNameIgnored proves a Find-name for a name we do not own draws
// no reply (no negative response on the wire).
func TestNBIPX_FindNameForeignNameIgnored(t *testing.T) {
	_, r, port, _ := newWiredIPXEngine(t)
	r.Inbound(ipxFindName(protocol.NewName("SOMEONEELSE", protocol.NameTypeFileServer)))
	if len(port.sent) != 0 {
		t.Fatalf("Find-name for a foreign name produced %d replies, want 0", len(port.sent))
	}
}

// TestNBIPX_MailslotDeliveredToConsumer proves an NMPI MailslotSend (0xFC) — the wire
// form of a browser HostAnnounce over NB-IPX — is decoded and routed to the datagram
// consumer with a ReplyTo endpoint tagged TransportIPX carrying the sender's address.
func TestNBIPX_MailslotDeliveredToConsumer(t *testing.T) {
	svc, r, _, _ := newWiredIPXEngine(t)
	dc := &recordingDatagramConsumer{}
	svc.SetDatagramConsumer(dc)

	payload := []byte{0xff, 'S', 'M', 'B', '-', 'a', 'n', 'n'}
	body := protocol.EncodeNMPIPacket(&protocol.NMPIPacket{
		Opcode:        protocol.NMPIOpMailslotSend,
		NameType:      protocol.NMPINameTypeWorkgroup,
		RequestedName: protocol.NewName("WORKGROUP", protocol.NameTypeGroup),
		SourceName:    protocol.NewName("WIN98", protocol.NameTypeWorkstation),
		Payload:       payload,
	})
	r.Inbound(&ipxproto.Datagram{
		Type:    protocol.IPXTypeNetBIOS,
		DstNet:  ipxrouter.DefaultNetwork,
		DstNode: ipxrouter.BroadcastNode,
		DstSock: NBIPXDatagramSocket,
		SrcNet:  testPeerNet,
		SrcNode: testPeerNode,
		SrcSock: NBIPXDatagramSocket,
		Payload: body,
	})

	if len(dc.got) != 1 {
		t.Fatalf("consumer got %d datagrams, want 1", len(dc.got))
	}
	d := dc.got[0]
	if d.Source.String() != "WIN98" || d.Destination.String() != "WORKGROUP" {
		t.Errorf("datagram names src=%q dst=%q", d.Source.String(), d.Destination.String())
	}
	if string(d.Payload) != string(payload) {
		t.Errorf("datagram payload = % x, want % x", d.Payload, payload)
	}
	if d.ReplyTo == nil || d.ReplyTo.Transport != TransportIPX || d.ReplyTo.Node != testPeerNode {
		t.Errorf("ReplyTo = %+v, want TransportIPX endpoint at the sender node", d.ReplyTo)
	}
}

// TestNBIPX_DirectedDatagramReplyUnicast proves a directed reply (Datagram.ReplyTo set
// to a TransportIPX endpoint) is emitted as a unicast NMPI MailslotSend to that node's
// IPX address, not a broadcast — the browser GetBackupList / AnnouncementRequest answer.
func TestNBIPX_DirectedDatagramReplyUnicast(t *testing.T) {
	svc, _, port, _ := newWiredIPXEngine(t)
	err := svc.SendDatagram(Datagram{
		Source:      protocol.NewName("CLASSICSTACK", protocol.NameTypeWorkstation),
		Destination: protocol.NewName("WIN98", protocol.NameTypeWorkstation),
		Payload:     []byte{0xff, 'S', 'M', 'B'},
		ReplyTo: &DatagramEndpoint{
			Transport: TransportIPX,
			Network:   testPeerNet,
			Node:      testPeerNode,
			Socket:    NBIPXDatagramSocket,
		},
	})
	if err != nil {
		t.Fatalf("SendDatagram: %v", err)
	}
	if len(port.sent) != 1 {
		t.Fatalf("directed reply emitted %d datagrams, want 1", len(port.sent))
	}
	if port.sent[0].DstNode != testPeerNode {
		t.Errorf("directed reply dst node = % x, want the requester (not broadcast)", port.sent[0].DstNode)
	}
}

// newWiredIPXEngineHandle is newWiredIPXEngine but returns the exported engine handle,
// for tests that drive ClaimName (which must run on the SAME engine registered on the
// router so an inbound conflict reaches its claim state).
func newWiredIPXEngineHandle(t *testing.T) (*ipxrouter.Router, *recordingIPXPort, *IPXEngine) {
	t.Helper()
	svc := NewService(nil, "CLASSICSTACK")
	r := ipxrouter.NewRouter(nil)
	r.SetIdentity(ipxrouter.DefaultNetwork, testRouterNode)
	port := &recordingIPXPort{}
	r.AddPort(port)
	eng := svc.NewIPXEngine(r)
	for _, sock := range [][2]byte{NBIPXSessionSocket, NBIPXNameQuerySocket, NBIPXDatagramSocket, NBIPXNameSocket} {
		if err := r.RegisterSocket(sock, eng); err != nil {
			t.Fatalf("RegisterSocket(%v): %v", sock, err)
		}
	}
	return r, port, eng
}

// TestNBIPX_ClaimNameUncontested proves ClaimName broadcasts the claim (a type-20
// Find-name plus an NMPI ClaimName) and, with no objection, returns nil so the caller
// may advertise the name via SAP.
func TestNBIPX_ClaimNameUncontested(t *testing.T) {
	_, port, eng := newWiredIPXEngineHandle(t)
	name := protocol.NewName("CLASSICSTACK", protocol.NameTypeFileServer)

	if err := eng.ClaimName(context.Background(), testRouterNode, name, 2, time.Millisecond); err != nil {
		t.Fatalf("ClaimName uncontested returned %v, want nil", err)
	}
	var sawFind, sawClaim bool
	for _, dg := range port.sent {
		if dg.Type != protocol.IPXTypeNetBIOS {
			continue
		}
		if ns, err := protocol.DecodeNameService(dg.Payload); err == nil && ns.DataStreamType == protocol.NBIPXFindName && ns.Name == name {
			sawFind = true
		}
		if p, err := protocol.DecodeNMPIPacket(dg.Payload); err == nil && p.Opcode == protocol.NMPIOpNameClaim && p.RequestedName == name {
			sawClaim = true
		}
	}
	if !sawFind {
		t.Error("claim did not broadcast a type-20 Find-name")
	}
	if !sawClaim {
		t.Error("claim did not broadcast an NMPI ClaimName")
	}
}

// TestNBIPX_ClaimNameContestedAborts proves a conflicting inbound name-service packet
// (another node owns the name) aborts the claim with ErrNameInUse, so the caller does
// NOT advertise a name that is in use.
func TestNBIPX_ClaimNameContestedAborts(t *testing.T) {
	r, _, eng := newWiredIPXEngineHandle(t)
	name := protocol.NewName("CLASSICSTACK", protocol.NameTypeFileServer)

	go func() {
		time.Sleep(2 * time.Millisecond)
		body := protocol.EncodeNameService(&protocol.NBIPXNameServicePacket{
			DataStreamType: protocol.NBIPXNameRecognized,
			Name:           name,
		})
		r.Inbound(&ipxproto.Datagram{
			Type:    protocol.IPXTypeNetBIOS,
			DstNet:  ipxrouter.DefaultNetwork,
			DstNode: ipxrouter.BroadcastNode,
			DstSock: NBIPXSessionSocket,
			SrcNet:  ipxrouter.DefaultNetwork,
			SrcNode: [6]byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF},
			SrcSock: NBIPXSessionSocket,
			Payload: body,
		})
	}()

	if err := eng.ClaimName(context.Background(), testRouterNode, name, 20, 20*time.Millisecond); err == nil {
		t.Fatal("ClaimName returned nil for a contested name, want ErrNameInUse")
	}
}
