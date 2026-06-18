// Package browser is the NetBIOS browser service (datagram-layer, §3-ter): the
// master-browser-election + host/domain announcement + browse-list machine that
// SMB clients use to populate Network Neighborhood. It is NOT part of SMB — it is
// a connectionless DATAGRAM service common to every NetBIOS transport (NetBEUI,
// IPX, NBT). It plugs into the NetBIOS service as its DatagramConsumer (the inbound
// seam) and sends its own announcements/elections out through the NetBIOS
// SendDatagram egress (the outbound seam); it imports core/service/netbios only for
// those two seam types, and core/protocol/browser for the wire frames.
//
// The one place the browser meets the SESSION layer is the RAP/LANMAN
// NetServerEnum2 "get server list" call, which arrives over the SMB IPC$ pipe:
// SMB asks the browser for the current list via the read-only BrowseList() /
// BackupList() query API here; SMB holds no browser logic and the browser holds no
// SMB logic.
//
// Ring: CORE (stdlib only, reflection-free). Timers are injectable so the election
// machine is unit-testable without real-time sleeps.
package browser

import (
	"context"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	proto "github.com/ObsoleteMadness/ClassicStack/core/protocol/browser"
	nbproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbios"
	"github.com/ObsoleteMadness/ClassicStack/core/service/mailslot"
)

// Name is the component name for the browser service.
const Name = "Browser"

// hostAnnouncePeriod is how often the service re-announces itself.
const hostAnnouncePeriod = 2 * time.Minute

// Role is the browser's current standing in the workgroup ([MS-BRWS]).
type Role uint8

const (
	RolePotential   Role = iota // not (yet) a browser
	RoleBackup                  // a backup browser
	RoleLocalMaster             // won the election, owns the browse list
)

// MailslotSink is the outbound seam the browser sends through: write a body to a
// named mailslot, sourced from src to dest. The mailslot router's SendMailslot
// satisfies it structurally — the browser holds NO mailslot-envelope and NO
// transport code; the router wraps the SMB_COM_TRANSACTION envelope and the NetBIOS
// transports do the per-protocol wire framing.
type MailslotSink interface {
	SendMailslot(name string, src, dest nbproto.Name, body []byte, broadcast bool) error
}

// serverRecord is one observed browser/server: its advertised type bits, the OS and
// app/browser-protocol versions and comment it announced, and when it was last seen
// (for ageing, future). The version/comment fields come straight off the
// HostAnnouncement frame ([MS-BRWS] §2.2.1) and feed the enriched browse listing
// (ServerEntries → the csnetview "net view" tool); they are zero/empty for a server
// known only from a domain announcement or backup-list mention.
type serverRecord struct {
	serverType uint32
	osMajor    uint8
	osMinor    uint8
	verMajor   uint8
	verMinor   uint8
	comment    string
	lastSeen   time.Time
}

// Service is the browser command core. It records the servers it has observed
// (browse list), maintains its election role, and answers GetBackupList. It is a
// mailslot.Consumer (HandleMailslot, registered for \MAILSLOT\BROWSE) and sends
// through the MailslotSink; compose registers it on the mailslot router and hands
// it the sink. It holds no mailslot-envelope and no transport knowledge.
type Service struct {
	logger    log.Logger
	sink      MailslotSink
	server    string // our server name (the identity, §4-bis)
	desc      string // our server comment (the identity description, §4-bis); optional
	workgroup string

	mu            sync.Mutex
	running       bool
	role          Role
	started       time.Time
	servers       map[string]serverRecord // browse list, keyed by normalised name
	machineGroups map[string]string       // workgroup → local master name

	// election timing, injectable for tests.
	electionDelay func(Role) time.Duration
	now           func() time.Time

	cancel    context.CancelFunc
	electGen  uint64
	announceC chan struct{}
}

// New builds a browser service for the given server identity + workgroup, sending
// through sink. server/workgroup come from the shared config.Identity (§4-bis); an
// empty server defaults to CLASSICSTACK, empty workgroup to WORKGROUP.
func New(logger log.Logger, sink MailslotSink, server, workgroup string) *Service {
	if server == "" {
		server = "CLASSICSTACK"
	}
	if workgroup == "" {
		workgroup = "WORKGROUP"
	}
	return &Service{
		logger:        logger,
		sink:          sink,
		server:        proto.NormalizeName(server),
		workgroup:     proto.NormalizeName(workgroup),
		role:          RolePotential,
		servers:       make(map[string]serverRecord),
		machineGroups: make(map[string]string),
		electionDelay: defaultElectionDelay,
		now:           time.Now,
	}
}

// Name returns the component name.
func (s *Service) Name() string { return Name }

// SetDescription sets the server comment the browser advertises for itself (its self
// entry in ServerEntries / the comment a Windows browse list shows). It comes from
// the shared config.Identity.Description (§4-bis); the compose registry hands it the
// one value. Empty = no comment. Idempotent; safe before Start.
func (s *Service) SetDescription(desc string) {
	s.mu.Lock()
	s.desc = desc
	s.mu.Unlock()
}

// SetSink installs the outbound mailslot seam late, for compose: the browser
// factory builds the service before the mailslot router exists (the router needs
// the NetBIOS service), so the cross-wire injects the sink afterwards — mirroring
// how AFP's SetRouter binds the shared router post-construction. A nil sink leaves
// the browser receive-only (it records announcements but emits none). Set before
// Start so the first host announcement has a sink. Idempotent.
func (s *Service) SetSink(sink MailslotSink) {
	s.mu.Lock()
	s.sink = sink
	s.mu.Unlock()
}

// Start brings the browser up: record the start time (election uptime) and emit a
// first host announcement, then a periodic announce loop. Idempotent (§3).
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = true
	s.started = s.now()
	s.announceC = make(chan struct{})
	announceC := s.announceC
	s.mu.Unlock()

	s.sendHostAnnouncement()
	go s.announceLoop(ctx, announceC)
	s.logf("browser started")
	return nil
}

// Stop brings the browser down, cancelling any election loop and the announce
// loop. Safe after a partial Start (§3).
func (s *Service) Stop(ctx context.Context) error {
	_ = ctx
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	cancel := s.cancel
	s.cancel = nil
	if s.announceC != nil {
		close(s.announceC)
		s.announceC = nil
	}
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	s.logf("browser stopped")
	return nil
}

// announceLoop re-emits a host announcement every hostAnnouncePeriod until Stop.
func (s *Service) announceLoop(ctx context.Context, done chan struct{}) {
	t := time.NewTicker(hostAnnouncePeriod)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-t.C:
			s.sendHostAnnouncement()
		}
	}
}

// --- query API (the read-only seam SMB's IPC$ \PIPE\LANMAN NetServerEnum2 uses) ---

// ServerEntry is one row of the browse list: a server name, its advertised
// SV_TYPE_* bits, an optional comment, and the OS/app versions it announced. SMB's
// NetServerEnum2 packs the Name/Type/Comment into a SERVER_INFO_1 record (it does not
// carry the versions); the OS/app versions feed the enriched csnetview listing.
type ServerEntry struct {
	Name     string
	Type     uint32
	Comment  string
	OSMajor  uint8
	OSMinor  uint8
	VerMajor uint8
	VerMinor uint8
}

// ServerEntries returns the full browse list (ourselves first, then every observed
// server) as typed rows, for SMB's RAP NetServerEnum2 over IPC$. Self advertises
// the workstation type ClassicStack announces.
func (s *Service) ServerEntries() []ServerEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ServerEntry, 0, len(s.servers)+1)
	out = append(out, ServerEntry{Name: s.server, Type: proto.ServerTypeWorkstationSet, Comment: s.desc})
	for name, rec := range s.servers {
		if name == s.server {
			continue
		}
		out = append(out, ServerEntry{
			Name:     name,
			Type:     rec.serverType,
			Comment:  rec.comment,
			OSMajor:  rec.osMajor,
			OSMinor:  rec.osMinor,
			VerMajor: rec.verMajor,
			VerMinor: rec.verMinor,
		})
	}
	return out
}

// Available reports whether the browser can serve a server list — false while it is
// only a potential browser (NetServerEnum2 then returns ERROR_REQ_NOT_ACCEP per
// [MS-BRWS] §3.3.5.6), true once it is a backup or local master.
func (s *Service) Available() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.role != RolePotential
}

// BrowseList returns the names of every server the browser has observed (plus
// ourselves). Kept as a convenience alongside the typed ServerEntries.
func (s *Service) BrowseList() []string {
	entries := s.ServerEntries()
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Name
	}
	return out
}

// BackupList returns ourselves plus every observed backup browser, for a
// GetBackupList response. Self is always first (a master browser is its own first
// backup, matching the legacy/Windows behaviour).
func (s *Service) BackupList() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []string{s.server}
	for name, rec := range s.servers {
		if name == s.server {
			continue
		}
		if rec.serverType&proto.ServerTypeBackupBrowser != 0 {
			out = append(out, name)
		}
	}
	return out
}

// CurrentRole reports the browser's election standing, for diagnostics/tests.
func (s *Service) CurrentRole() Role {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.role
}

// logf emits one info line through the logger if configured.
func (s *Service) logf(msg string) {
	if s.logger == nil || !s.logger.Enabled(log.Info) {
		return
	}
	s.logger.Log1(log.Info, msg, log.Str("scope", Name))
}

// defaultElectionDelay is the per-role backoff before (re)transmitting an election
// frame ([MS-BRWS] §3.3): a current master responds fastest, a potential browser
// slowest, so the rightful winner usually transmits first.
func defaultElectionDelay(role Role) time.Duration {
	switch role {
	case RoleLocalMaster:
		return 100 * time.Millisecond
	case RoleBackup:
		return 200 * time.Millisecond
	default:
		return 400 * time.Millisecond
	}
}

// compile-time assertions: the service is a Component and a mailslot Consumer (it
// registers for \MAILSLOT\BROWSE on the mailslot router).
var (
	_ component.Component = (*Service)(nil)
	_ mailslot.Consumer   = (*Service)(nil)
)
