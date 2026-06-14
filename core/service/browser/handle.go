package browser

import (
	"context"
	"strings"
	"time"

	proto "github.com/ObsoleteMadness/ClassicStack/core/protocol/browser"
	mswire "github.com/ObsoleteMadness/ClassicStack/core/protocol/mailslot"
	nbproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbios"
)

// handle.go is the inbound mailslot dispatch + the announcement/election emitters.
// HandleMailslot is the mailslot.Consumer entry point (registered for
// \MAILSLOT\BROWSE): the mailslot layer has already unwrapped the SMB_COM_TRANSACTION
// envelope, so the browser receives the bare browser frame body. It decodes the
// browser opcode, updates the browse list, and (for elections / GetBackupList) emits
// a response through the MailslotSink. No mailslot-envelope code here.

// HandleMailslot implements mailslot.Consumer: one browser frame body delivered on
// \MAILSLOT\BROWSE, with the source/destination NetBIOS names. Browser frames are
// sent to group names the local stack also subscribes to, so our own broadcasts come
// back to us — drop self-sourced frames to avoid an election/announce storm (the
// loop observed in the legacy captures).
func (s *Service) HandleMailslot(name string, src, dest nbproto.Name, body []byte) {
	if s.isSelfSourced(src) {
		return
	}
	op, frame, ok := proto.UnwrapPayload(body)
	if !ok {
		return
	}
	switch op {
	case proto.OpHostAnnouncement:
		s.observeAnnouncement(frame, 0)
	case proto.OpLocalMasterAnnounce:
		s.observeAnnouncement(frame, proto.ServerTypeMasterBrowser)
	case proto.OpDomainAnnouncement:
		s.observeDomain(frame)
	case proto.OpAnnouncementRequest:
		s.sendHostAnnouncement()
	case proto.OpGetBackupListReq:
		s.handleGetBackupList(frame, src)
	case proto.OpRequestElection:
		s.handleElection(frame)
	}
}

// isSelfSourced reports whether a frame came from our own name or workgroup, so our
// looped-back broadcasts are ignored.
func (s *Service) isSelfSourced(src nbproto.Name) bool {
	name := strings.ToUpper(strings.TrimSpace(src.String()))
	if name == "" {
		return false
	}
	return name == s.server || name == s.workgroup
}

// observeAnnouncement records a host or local-master announcement in the browse
// list, ORing in extraType (the master bit for a local-master announcement).
func (s *Service) observeAnnouncement(frame []byte, extraType uint32) {
	a, err := proto.UnmarshalAnnouncement(frame)
	if err != nil {
		return
	}
	name := proto.NormalizeName(a.ServerName)
	if name == "" {
		return
	}
	s.mu.Lock()
	s.servers[name] = serverRecord{serverType: a.ServerType | extraType, lastSeen: s.now()}
	s.mu.Unlock()
}

// observeDomain records the local master a workgroup advertises.
func (s *Service) observeDomain(frame []byte) {
	da, err := proto.UnmarshalDomainAnnouncement(frame)
	if err != nil {
		return
	}
	group := proto.NormalizeName(da.MachineGroup)
	if group == "" {
		return
	}
	s.mu.Lock()
	s.machineGroups[group] = proto.NormalizeName(da.LocalMaster)
	s.mu.Unlock()
}

// handleGetBackupList answers a GetBackupList request, but only while we are the
// local master (only the master owns the authoritative backup list). The response
// echoes the request token, is sourced from our <1D> master-browser name, and is
// directed back to the requester (not a broadcast).
func (s *Service) handleGetBackupList(frame []byte, requester nbproto.Name) {
	s.mu.Lock()
	role := s.role
	s.mu.Unlock()
	if role != RoleLocalMaster {
		return
	}
	req, err := proto.UnmarshalGetBackupListRequest(frame)
	if err != nil {
		return
	}
	body := proto.GetBackupListResponse{
		Token:         req.Token,
		BackupServers: s.BackupList(),
	}.Marshal()
	_ = s.sink.SendMailslot(
		mswire.NameBrowse,
		nbproto.NewName(s.server, proto.NameTypeMasterBrowser),
		requester,
		body,
		false,
	)
}

// handleElection runs the election decision ([MS-BRWS] §3.3): compare the
// requester's criteria/uptime/name against ours. If we lose, drop to potential and
// stop transmitting. If we win, (re)start the election transmit loop that, after
// four uncontested transmissions, declares us local master.
func (s *Service) handleElection(frame []byte) {
	req, err := proto.UnmarshalElection(frame)
	if err != nil {
		return
	}
	local := s.localElectionFrame()
	cmp := proto.Compare(local, *req)
	if cmp < 0 {
		s.stopElection()
		s.mu.Lock()
		s.role = RolePotential
		s.mu.Unlock()
		s.logf("election lost")
		return
	}
	if cmp == 0 {
		return // tie — usually our own broadcast echoed back; stay silent
	}
	s.startElection()
	_ = s.emitElection(local)
}

// localElectionFrame builds our election candidacy frame from our identity and
// uptime.
func (s *Service) localElectionFrame() proto.Election {
	return proto.Election{
		Version:    proto.ElectionVersion,
		Criteria:   proto.ElectionCriteriaMaster,
		Uptime:     s.uptimeSecs(),
		ServerName: s.server,
	}
}

// uptimeSecs is our browser uptime in seconds (the election tie-breaker), never 0.
func (s *Service) uptimeSecs() uint32 {
	s.mu.Lock()
	started := s.started
	s.mu.Unlock()
	if started.IsZero() {
		return 1
	}
	secs := uint32(s.now().Sub(started) / time.Second)
	if secs == 0 {
		return 1
	}
	return secs
}

// startElection launches the election transmit loop if one is not already running.
func (s *Service) startElection() {
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return
	}
	delay := s.electionDelay(s.role)
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.electGen++
	gen := s.electGen
	s.mu.Unlock()
	go s.runElection(ctx, gen, delay)
}

// stopElection cancels any running election loop.
func (s *Service) stopElection() {
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// runElection retransmits the election frame up to three more times at the role
// backoff; if uncontested (not cancelled by a winning peer) it declares us local
// master and emits a local-master announcement.
func (s *Service) runElection(ctx context.Context, gen uint64, delay time.Duration) {
	for range 3 {
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		_ = s.emitElection(s.localElectionFrame())
	}

	s.mu.Lock()
	if s.electGen != gen { // a newer election superseded us
		s.mu.Unlock()
		return
	}
	s.cancel = nil
	s.role = RoleLocalMaster
	s.mu.Unlock()
	s.logf("election won — local master")
	s.sendLocalMasterAnnouncement()
}

// --- emitters ---

// sendHostAnnouncement broadcasts a host announcement to the workgroup<1D>.
func (s *Service) sendHostAnnouncement() {
	s.emitAnnouncement(proto.OpHostAnnouncement)
}

// sendLocalMasterAnnouncement broadcasts a local-master announcement.
func (s *Service) sendLocalMasterAnnouncement() {
	s.emitAnnouncement(proto.OpLocalMasterAnnounce)
}

// emitAnnouncement broadcasts a host or local-master announcement for our identity
// to the workgroup master-browser group name.
func (s *Service) emitAnnouncement(op uint8) {
	if s.sink == nil {
		return
	}
	body := proto.Announcement{
		Op:             op,
		UpdateCount:    0,
		PeriodicityMS:  uint32(hostAnnouncePeriod / time.Millisecond),
		ServerName:     s.server,
		OSVersionMajor: 4,
		ServerType:     proto.ServerTypeWorkstationSet,
		VersionMajor:   proto.AnnounceVersionMajor,
		VersionMinor:   proto.AnnounceVersionMinor,
	}.Marshal()
	_ = s.sendBrowseBroadcast(body)
}

// emitElection broadcasts an election frame for the given candidacy.
func (s *Service) emitElection(local proto.Election) error {
	if s.sink == nil {
		return nil
	}
	return s.sendBrowseBroadcast(local.Marshal())
}

// sendBrowseBroadcast writes body to \MAILSLOT\BROWSE, sourced from our workstation
// name to the workgroup<1D> master-browser group name, as a broadcast.
func (s *Service) sendBrowseBroadcast(body []byte) error {
	return s.sink.SendMailslot(
		mswire.NameBrowse,
		nbproto.NewName(s.server, nbproto.NameTypeWorkstation),
		nbproto.NewName(s.workgroup, proto.NameTypeMasterBrowser),
		body,
		true,
	)
}
