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
	// NewConn opens a virtual circuit. The engine serves each reassembled
	// message through the returned SessionCircuit and closes it on teardown.
	NewConn() SessionCircuit
}

// SessionCircuit is one open virtual circuit: serve a reassembled message
// (returning the reply bytes, or nil to send nothing), and close on teardown.
type SessionCircuit interface {
	ServeMessage(req []byte) []byte
	Close()
}

// Datagram is one connectionless NetBIOS datagram delivered to a DatagramConsumer:
// the source and destination NetBIOS names and the application payload (a browser
// announcement / mailslot write). It carries no transport addressing — the
// consumer is transport-agnostic, exactly like SessionCircuit.
type Datagram struct {
	Source      protocol.Name
	Destination protocol.Name
	Payload     []byte
	Broadcast   bool // true for a group/broadcast datagram (DATAGRAM_BROADCAST)
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

// NewIPXEngine builds the NBIPX (NetBIOS-over-IPX) session state machine bound to
// sender (the core/router/ipx mini-router, which it sends replies through).
// Compose registers the returned engine on the mini-router as the SocketHandler
// for the NB-IPX session socket (0x0455). The engine reads the live consumer
// through the service, so SMB attaching late is honoured. The service tracks the
// engine (as a circuitCloser) so Stop tears down its circuits. NB-IPX answers
// NAME_QUERY at the name layer, so unlike NewNBFEngine this engine needs no name
// set — it is purely the session-data path.
func (s *Service) NewIPXEngine(sender DatagramSender) *IPXEngine {
	eng := &IPXEngine{e: newIPXSessionEngine(s.logger, sender, s.sessionConsumer)}
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

// emitDatagram implements datagramEgress: send a connectionless NetBIOS datagram
// (the browser's HostAnnounce / election / backup-list traffic) over NB-IPX as an
// NMPI mailslot send.
func (g *IPXEngine) emitDatagram(d Datagram) error { return g.e.emitDatagram(d) }
