package ipx

import (
	"context"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/netlog"
	ipxproto "github.com/ObsoleteMadness/ClassicStack/protocol/ipx"
	routeripx "github.com/ObsoleteMadness/ClassicStack/router/ipx"
)

// SAPSocket is the well-known socket number for IPX SAP.
var SAPSocket = [2]byte{0x04, 0x52}

// DefaultSAPPeriod is the broadcast cadence used by NetWare-era SAP
// agents and matches what most clients expect.
const DefaultSAPPeriod = 60 * time.Second

// SAPService is an IPX Service Advertising Protocol agent. It
// maintains a local registry of services this node advertises, replies
// to inbound SAP queries, and periodically broadcasts the registry so
// clients pick us up without having to ask.
//
// Higher layers register their advertisements via Register; the
// returned cancel function removes the entry. NetBIOS-over-IPX, when
// it claims a name, registers a SAPServiceTypeNetBIOS entry pointing
// at our network/node/socket so SMB clients see the server in their
// browse list.
type SAPService struct {
	router routeripx.Router

	// Period is the broadcast cadence. Zero or negative uses
	// DefaultSAPPeriod.
	Period time.Duration

	// sleep is replaced in tests with a synthetic clock.
	sleep func(d time.Duration) <-chan time.Time

	mu      sync.Mutex
	entries []SAPEntry
	cancel  context.CancelFunc
	done    chan struct{}
}

// NewSAPService returns a SAP agent bound to r.
func NewSAPService(r routeripx.Router) *SAPService {
	return &SAPService{
		router: r,
		sleep: func(d time.Duration) <-chan time.Time {
			return time.After(d)
		},
	}
}

// Register adds an advertisement to the registry. The returned
// function removes it.
//
// Network, Node, and Socket are filled from the router's identity
// when the caller leaves them zero — most local advertisements want
// "this server, on this socket of mine" which is the registered
// identity by default. Callers re-advertising remote services (a
// future SAP-relay use case) can populate the fields explicitly.
func (s *SAPService) Register(entry SAPEntry) (cancel func()) {
	if isZero4(entry.Network) {
		entry.Network = s.router.Network()
	}
	if isZero6(entry.Node) {
		entry.Node = s.router.Node()
	}
	if entry.Hops == 0 {
		entry.Hops = 1
	}
	s.mu.Lock()
	s.entries = append(s.entries, entry)
	idx := len(s.entries) - 1
	id := entryID(entry)
	s.mu.Unlock()
	netlog.Info("[IPX][SAP] registered: type=%04x name=%q socket=%02x%02x",
		entry.ServiceType, entry.Name, entry.Socket[0], entry.Socket[1])
	_ = idx
	return func() { s.unregister(id) }
}

// entryID is a stable identifier for an advertisement so that
// unregister can locate it even after registry mutations.
type sapEntryID struct {
	ServiceType uint16
	Name        string
	Socket      [2]byte
}

func entryID(e SAPEntry) sapEntryID {
	return sapEntryID{ServiceType: e.ServiceType, Name: e.Name, Socket: e.Socket}
}

func (s *SAPService) unregister(id sapEntryID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, e := range s.entries {
		if entryID(e) == id {
			s.entries = append(s.entries[:i], s.entries[i+1:]...)
			netlog.Info("[IPX][SAP] unregistered: type=%04x name=%q",
				e.ServiceType, e.Name)
			return
		}
	}
}

// Entries returns a copy of the registry. Useful for tests and
// diagnostic logging.
func (s *SAPService) Entries() []SAPEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]SAPEntry, len(s.entries))
	copy(out, s.entries)
	return out
}

// Start registers the SAP socket and spawns the periodic broadcaster.
func (s *SAPService) Start(ctx context.Context) error {
	if err := s.router.RegisterSocket(SAPSocket, s); err != nil {
		return err
	}
	loopCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.cancel = cancel
	s.done = make(chan struct{})
	s.mu.Unlock()
	go s.broadcastLoop(loopCtx)
	return nil
}

// Stop cancels the broadcaster and waits for the goroutine to exit.
func (s *SAPService) Stop() error {
	s.mu.Lock()
	cancel := s.cancel
	done := s.done
	s.cancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
	return nil
}

// HandleDatagram implements router/ipx.SocketHandler.
func (s *SAPService) HandleDatagram(d *ipxproto.Datagram) {
	pkt, err := DecodeSAP(d.Payload)
	if err != nil {
		return
	}
	switch pkt.Operation {
	case SAPGeneralQuery, SAPNearestQuery:
		s.handleQuery(d, pkt)
	default:
		// Responses from other agents are ignored — we don't maintain
		// a remote-service table.
	}
}

// handleQuery answers a query with a unicast response naming all
// matching local advertisements. A wildcard service-type matches
// every entry; otherwise only entries whose service type matches.
func (s *SAPService) handleQuery(req *ipxproto.Datagram, q *SAPPacket) {
	matches := s.matching(q.QueryServiceType)
	if len(matches) == 0 {
		return
	}
	op := uint16(SAPGeneralResponse)
	if q.Operation == SAPNearestQuery {
		op = SAPNearestResponse
		// Nearest-service responses carry only one entry (the
		// "nearest" one). With a single registry we just return the
		// first match.
		matches = matches[:1]
	}
	resp := &SAPPacket{Operation: op, Entries: matches}
	body, err := EncodeSAP(resp)
	if err != nil {
		netlog.Warn("[IPX][SAP] encode response: %v", err)
		return
	}
	out := &ipxproto.Datagram{
		Type:    4, // Packet Exchange Packet (used for SAP)
		DstNet:  req.SrcNet,
		DstNode: req.SrcNode,
		DstSock: req.SrcSock,
		SrcSock: SAPSocket,
		Payload: body,
	}
	if err := s.router.Send(out); err != nil {
		netlog.Warn("[IPX][SAP] send response: %v", err)
	}
}

// matching returns the registry entries whose ServiceType matches t,
// or all entries when t is the wildcard 0xFFFF.
func (s *SAPService) matching(t uint16) []SAPEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]SAPEntry, 0, len(s.entries))
	for _, e := range s.entries {
		if t == SAPServiceTypeWildcard || e.ServiceType == t {
			out = append(out, e)
		}
	}
	return out
}

// broadcastLoop emits a periodic broadcast naming every registry
// entry. With ≤ 7 entries we fit in one packet; more would split
// across multiple packets. Since ClassicStack registers at most a
// handful (NetBIOS, file server) the single-packet path is fine.
func (s *SAPService) broadcastLoop(ctx context.Context) {
	defer func() {
		s.mu.Lock()
		done := s.done
		s.done = nil
		s.mu.Unlock()
		if done != nil {
			close(done)
		}
	}()

	period := s.Period
	if period <= 0 {
		period = DefaultSAPPeriod
	}

	s.broadcast()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.sleep(period):
			s.broadcast()
		}
	}
}

func (s *SAPService) broadcast() {
	entries := s.Entries()
	if len(entries) == 0 {
		return
	}
	// Chunk into packets of at most SAPMaxEntriesPerPacket.
	for off := 0; off < len(entries); off += SAPMaxEntriesPerPacket {
		end := min(off+SAPMaxEntriesPerPacket, len(entries))
		body, err := EncodeSAP(&SAPPacket{
			Operation: SAPGeneralResponse,
			Entries:   entries[off:end],
		})
		if err != nil {
			netlog.Warn("[IPX][SAP] encode broadcast: %v", err)
			return
		}
		out := &ipxproto.Datagram{
			Type:    4,
			DstNet:  s.router.Network(),
			DstNode: routeripx.BroadcastNode,
			DstSock: SAPSocket,
			SrcSock: SAPSocket,
			Payload: body,
		}
		if err := s.router.Send(out); err != nil {
			netlog.Debug("[IPX][SAP] broadcast: %v", err)
		}
	}
}

// helper duplicates of router/ipx unexported helpers to avoid an
// import dependency on internal symbols.
func isZero4(b [4]byte) bool { return b == [4]byte{} }
func isZero6(b [6]byte) bool { return b == [6]byte{} }
