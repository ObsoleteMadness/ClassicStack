package netbios

// session.go defines the NetBIOS→upper-layer session-data seam. A NetBIOS
// transport reassembles whole session messages on a virtual circuit and hands
// them to a SessionConsumer (in practice the SMB command engine), writing the
// response back over the same circuit. The consumer holds no transport knowledge
// and the transport holds no SMB knowledge — the only contract between them is
// "here is one message on this circuit, give me the bytes to send back" (the
// §3-bis command-core / session-transport split).
//
// SMB's *smb.Service satisfies SessionConsumer through its ConsumerAdapter, so
// the netbios package depends on these two small interfaces rather than importing
// the SMB service. The NetBIOS service holds one SessionConsumer (set by compose
// via SetSessionConsumer) and routes every established circuit's traffic to it.

import (
	"context"
	"time"

	nbf "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbeui"
	protocol "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbios"
)

// frameType aliases the NBF frame the mini-router hands the engine, so the
// exported Engine method signatures match the core/router/netbeui handler
// interfaces exactly (which take *nbf.Frame) without restating the import path.
type frameType = nbf.Frame

// SessionConsumer opens one circuit per established NetBIOS session. The SMB
// service is the consumer; a future named-pipe or other session service could be
// another. NewConn is called once per session by the NBF session engine.
type SessionConsumer interface {
	// NewConn opens a virtual circuit for the transport remote-endpoint label
	// client (the requesting NetBIOS node's wire address; "" when unknown). The
	// engine serves each reassembled message through the returned SessionCircuit
	// and closes it on teardown.
	NewConn(client string) SessionCircuit
}

// nbfClientLabel formats an NBF (NetBEUI) circuit's source MAC as the
// "xx:xx:xx:xx:xx:xx" client label the SMB management view groups sessions under.
func nbfClientLabel(mac [6]byte) string { return hexColon(mac[:]) }

// nbipxClientLabel formats an NB-IPX circuit's remote node + socket as the
// "xx:xx:xx:xx:xx:xx.ssss" client label (socket suffix distinguishes it from the
// direct-hosted 0x0550 transport and from NBF).
func nbipxClientLabel(node [6]byte, sock [2]byte) string {
	const hexdigits = "0123456789abcdef"
	suffix := []byte{'.', hexdigits[sock[0]>>4], hexdigits[sock[0]&0x0f], hexdigits[sock[1]>>4], hexdigits[sock[1]&0x0f]}
	return hexColon(node[:]) + string(suffix)
}

// hexColon renders bytes as lower-case hex separated by colons (reflection-free,
// no fmt — this package is on the core stdlib-only ring).
func hexColon(b []byte) string {
	const hexdigits = "0123456789abcdef"
	out := make([]byte, 0, len(b)*3)
	for i, x := range b {
		if i > 0 {
			out = append(out, ':')
		}
		out = append(out, hexdigits[x>>4], hexdigits[x&0x0f])
	}
	return string(out)
}

// SessionCircuit is one open virtual circuit: serve a reassembled message
// (returning the reply bytes, or nil to send nothing), optionally accept a
// server-push writer for asynchronous server-initiated frames (SMB NOTIFY_CHANGE
// completion), and close on teardown. A transport that can retain per-circuit
// addressing installs a push writer after opening the circuit; one that cannot
// simply never calls it, and server-initiated frames are not delivered.
type SessionCircuit interface {
	ServeMessage(req []byte) []byte
	SetPushWriter(w func([]byte))
	Close()
}

// NetBIOSNamer is an optional SessionCircuit capability: recording the calling
// NetBIOS name a transport learned at session establishment (NBF's NAME_QUERY
// SourceName), for the management session view. SMB's *Conn implements it; a
// transport that has a calling name type-asserts its circuit against this
// interface after NewConn rather than the base SessionCircuit carrying it, so
// AFP/NCP's structurally-identical seams need no change.
type NetBIOSNamer interface {
	SetNetBIOSName(name string)
}

// DatagramEndpoint identifies the transport-level remote a directed NetBIOS
// datagram reply is sent back to. It is transport-tagged (Transport is one of the
// TransportNetBEUI/TransportIPX/TransportNBT family strings) so a reply is emitted
// only by the transport the request arrived on, and carries that transport's wire
// address: for NB-IPX the Network/Node/Socket tuple, for NBF the source MAC in the
// first 6 bytes of Node. The consumer treats it as an opaque token — it never reads
// the wire fields, only echoes the endpoint back on the reply Datagram — so the §3
// transport-agnostic contract holds.
type DatagramEndpoint struct {
	Transport string  // transport family (TransportIPX / TransportNetBEUI / …)
	Network   [4]byte // IPX network (NB-IPX)
	Node      [6]byte // IPX node (NB-IPX) or source MAC (NBF)
	Socket    [2]byte // IPX socket (NB-IPX)
}

// Datagram is one connectionless NetBIOS datagram delivered to a DatagramConsumer:
// the source and destination NetBIOS names and the application payload (a browser
// announcement / mailslot write). ReplyTo, when non-nil, is the transport endpoint
// the datagram arrived from: a consumer that wants to answer a specific requester
// (a browser GetBackupList / AnnouncementRequest) echoes it back on the reply
// Datagram so the reply is sent *directed* to that node rather than broadcast. It is
// nil for a broadcast the consumer only observes. The consumer stays
// transport-agnostic: it never inspects ReplyTo, only carries it.
type Datagram struct {
	Source      protocol.Name
	Destination protocol.Name
	Payload     []byte
	Broadcast   bool              // true for a group/broadcast datagram (DATAGRAM_BROADCAST)
	ReplyTo     *DatagramEndpoint // inbound: where it came from; outbound: send directed here (nil = broadcast)
}

// DatagramConsumer receives connectionless NetBIOS datagrams (mailslot / browser
// traffic) the transports deliver. A browser/mailslot service is the consumer; it
// is optional, so until one is installed datagrams drop cleanly after decode. The
// consumer holds no transport knowledge — the datagram is already decoded to
// names + payload (the §3-bis split, the datagram analogue of SessionConsumer).
type DatagramConsumer interface {
	HandleDatagram(d Datagram)
}

// SetDatagramConsumer installs the connectionless-datagram sink (a browser/mailslot
// service). Compose calls it during wiring; a nil consumer leaves datagrams
// dropped after decode.
func (s *Service) SetDatagramConsumer(c DatagramConsumer) {
	s.mu.Lock()
	s.dgramConsumer = c
	s.mu.Unlock()
}

// datagramConsumer returns the installed datagram consumer under the service lock.
// The transports read it through this accessor (passed as a callback) so a consumer
// attached after an engine is built is picked up live.
func (s *Service) datagramConsumer() DatagramConsumer {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dgramConsumer
}

// SetSessionConsumer installs the upper-layer session consumer (the SMB command
// engine) the NBF session engine routes established circuits to. Compose calls it
// during wiring; a nil consumer leaves session data undelivered (drops cleanly).
func (s *Service) SetSessionConsumer(c SessionConsumer) {
	s.mu.Lock()
	s.consumer = c
	s.mu.Unlock()
}

// sessionConsumer returns the installed consumer under the service lock. The NBF
// session engine reads it through this accessor (passed as a callback) so a
// consumer attached after the engine is built is picked up live.
func (s *Service) sessionConsumer() SessionConsumer {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.consumer
}

// localNames snapshots the local name set under the service lock, so the NBF
// engine answers NAME_QUERY for whatever names are currently claimed (including
// any registered after the engine was built).
func (s *Service) localNames() []protocol.Name {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]protocol.Name(nil), s.names...)
}

// SetWorkgroup records the configured workgroup, stamped into the NB-IPX
// NAME_RECOGNIZED reply prefix a Win98 NWLink client validates before opening a
// session. Compose sets it from Identity.Workgroup; safe before Start.
func (s *Service) SetWorkgroup(workgroup string) {
	s.mu.Lock()
	s.workgroup = workgroup
	s.mu.Unlock()
}

// workgroupName returns the configured workgroup under the service lock, read live by
// the NBIPX engine through a callback so a workgroup set after the engine is built is
// honoured.
func (s *Service) workgroupName() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.workgroup
}

// LocalNames is the exported snapshot of the local NetBIOS name set, for compose:
// when wiring the NBF engine onto the NetBEUI mini-router, the cross-wire registers
// the engine as the per-name NameHandler for each local name (the session-
// establishment CALL is a non-session frame addressed to one of our names). It is a
// point-in-time copy; names claimed later (RegisterName) are picked up live by the
// engine through localNames, but are NOT auto-registered on the router — compose
// registers the set known at wiring time, matching how the NBF engine test wires it.
func (s *Service) LocalNames() []protocol.Name { return s.localNames() }

// NewNBFEngine builds the NBF (NetBEUI) session state machine bound to sender
// (the core/router/netbeui mini-router, which it sends replies through). Compose
// registers the returned engine on the mini-router as both its NameHandler (for
// the session-establishment NAME_QUERY of each local name) and its SessionHandler
// (for the SESSION_* / DATA_* frames). The engine reads the live consumer and
// name set through the service, so SMB attaching late and names registered later
// are both honoured. The service tracks the engine so Stop tears down its
// circuits.
func (s *Service) NewNBFEngine(sender FrameSender) *Engine {
	eng := &Engine{e: newSessionEngine(s.logger, sender, s.sessionConsumer, s.datagramConsumer, s.localNames)}
	s.mu.Lock()
	s.closers = append(s.closers, eng)
	s.egresses = append(s.egresses, eng)
	s.mu.Unlock()
	return eng
}

// Engine is the exported handle to one transport's NBF session state machine. It
// satisfies the core/router/netbeui mini-router's NameHandler and SessionHandler
// (HandleFrame / HandleSessionFrame) so compose registers it directly, with no
// adapter shim. Its internals are the unexported sessionEngine.
type Engine struct{ e *sessionEngine }

// HandleFrame implements the netbeui mini-router NameHandler: a non-session NBF
// frame addressed to a registered local name (the session-establishment CALL).
func (g *Engine) HandleFrame(srcMAC, dstMAC [6]byte, frame *frameType) {
	g.e.HandleFrame(srcMAC, dstMAC, frame)
}

// HandleSessionFrame implements the netbeui mini-router SessionHandler: an NBF
// session-command frame (0x14–0x1F) driving the circuit lifecycle and data path.
func (g *Engine) HandleSessionFrame(srcMAC, dstMAC [6]byte, frame *frameType) {
	g.e.HandleSessionFrame(srcMAC, dstMAC, frame)
}

// closeCircuits tears down every open circuit (called from Stop).
func (g *Engine) closeCircuits() { g.e.closeAll() }

// emitDatagram implements datagramEgress: send a connectionless NetBIOS datagram
// (the browser's HostAnnounce / election / backup-list traffic) as an NBF UI frame.
func (g *Engine) emitDatagram(d Datagram) error { return g.e.emitDatagram(d) }

// transportFamily implements datagramEgress: this engine is the NetBEUI (NBF)
// transport, so a directed reply tagged TransportNetBEUI is emitted here.
func (g *Engine) transportFamily() string { return TransportNetBEUI }

// NewIPXEngine builds the NBIPX (NetBIOS-over-IPX) session state machine bound to
// sender (the core/router/ipx mini-router, which it sends replies through).
// Compose registers the returned engine on the mini-router as the SocketHandler
// for the NB-IPX session socket (0x0455), the NMPI name-query socket (0x0551), and
// the NB-IPX datagram socket (0x0553), so the engine carries SMB sessions, answers
// the client's name query for our server name (NMPI Query-name / NBIPX Find-name),
// AND delivers inbound browser mailslot datagrams (NMPI MailslotSend) to the
// datagram consumer. The engine reads the live session consumer, datagram consumer
// and name set through the service, so SMB and the browser attaching late and names
// registered later are all honoured. The service tracks the engine (as a
// circuitCloser) so Stop tears down its circuits.
func (s *Service) NewIPXEngine(sender DatagramSender) *IPXEngine {
	eng := &IPXEngine{e: newIPXSessionEngine(s.logger, sender, s.sessionConsumer, s.datagramConsumer, s.localNames, s.workgroupName)}
	s.mu.Lock()
	s.closers = append(s.closers, eng)
	s.egresses = append(s.egresses, eng)
	s.mu.Unlock()
	return eng
}

// IPXEngine is the exported handle to one IPX transport's NBIPX session state
// machine. It satisfies the core/router/ipx mini-router's SocketHandler
// (HandleDatagram) so compose registers it directly on socket 0x0455, with no
// adapter shim. Its internals are the unexported ipxSessionEngine.
type IPXEngine struct{ e *ipxSessionEngine }

// HandleDatagram implements the core/router/ipx mini-router SocketHandler: an IPX
// datagram delivered to the NB-IPX session socket, driving the circuit lifecycle
// and data path.
func (g *IPXEngine) HandleDatagram(d *ipxDatagramType) { g.e.HandleDatagram(d) }

// closeCircuits tears down every open circuit (called from Stop).
func (g *IPXEngine) closeCircuits() { g.e.closeAll() }

// ClaimName broadcasts a name-claim for name on the segment and reports whether it was
// uncontested (nil) or another node objected (error). self is our own IPX node so a
// looped-back self-broadcast is not mistaken for a conflict. Compose calls this on
// start, once per local name, to detect a conflict and (on success) gate the SAP
// advertisement — matching the legacy over_ipx claim-then-advertise ordering.
func (g *IPXEngine) ClaimName(ctx context.Context, self [6]byte, name protocol.Name, retries int, interval time.Duration) error {
	return g.e.ClaimName(ctx, self, name, retries, interval)
}

// emitDatagram implements datagramEgress: send a connectionless NetBIOS datagram
// (the browser's HostAnnounce / election / backup-list traffic) over NB-IPX as an
// NMPI mailslot send.
func (g *IPXEngine) emitDatagram(d Datagram) error { return g.e.emitDatagram(d) }

// transportFamily implements datagramEgress: this engine is the NB-IPX transport, so
// a directed reply tagged TransportIPX is emitted here.
func (g *IPXEngine) transportFamily() string { return TransportIPX }
