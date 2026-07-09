package browser

import (
	"context"
	"strings"
	"time"

	proto "github.com/ObsoleteMadness/ClassicStack/core/protocol/browser"
	mswire "github.com/ObsoleteMadness/ClassicStack/core/protocol/mailslot"
	nbproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbios"
	nbservice "github.com/ObsoleteMadness/ClassicStack/core/service/netbios"
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
func (s *Service) HandleMailslot(name string, src, dest nbproto.Name, body []byte, replyTo *nbservice.DatagramEndpoint) {
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
		s.replyHostAnnouncement(replyTo)
	case proto.OpGetBackupListReq:
		s.handleGetBackupList(frame, src, dest, replyTo)
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
	// A LocalMasterAnnounce (extraType carries the master bit) from any OTHER node
	// means the segment already has a master — record it so the startup
	// discoverMaster watcher does not force an election and fight the real master.
	if extraType&proto.ServerTypeMasterBrowser != 0 && name != s.server {
		s.masterSeen = true
	}
	s.servers[name] = serverRecord{
		serverType: a.ServerType | extraType,
		osMajor:    a.OSVersionMajor,
		osMinor:    a.OSVersionMinor,
		verMajor:   a.VersionMajor,
		verMinor:   a.VersionMinor,
		comment:    a.Comment,
		lastSeen:   s.now(),
	}
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
// echoes the request token and is directed straight back to the requester's node
// (replyTo, not a broadcast). requester is the request's source name (the reply's
// NetBIOS destination); dest is the name the client addressed the request TO — the
// reply source identity is chosen by backupListResponseSource so a client that asked
// WORKGROUP<1D>/<00> gets the <1D> master-browser identity it expects (per [MS-BRWS]
// §3.2.5.5); without it the client rejects the list and re-runs the election
// (captures/ipx.pcap frames 161–189).
func (s *Service) handleGetBackupList(frame []byte, requester, dest nbproto.Name, replyTo *nbservice.DatagramEndpoint) {
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
	_ = s.sink.SendMailslotTo(
		mswire.NameBrowse,
		s.backupListResponseSource(dest),
		requester,
		body,
		false,
		replyTo,
	)
}

// backupListResponseSource picks the NetBIOS name a GetBackupList response is
// sourced from. A Win9x client addresses the request to <workgroup><1D> or
// <workgroup><00>; in either case it expects the <workgroup><1D> master-browser
// identity in the reply. Mirror that whenever the request's destination names our
// workgroup; otherwise source from our own <20> file-server name. (Legacy
// service/smb backupListResponseSource.)
func (s *Service) backupListResponseSource(dest nbproto.Name) nbproto.Name {
	if strings.EqualFold(strings.TrimSpace(dest.String()), s.workgroup) {
		return nbproto.NewName(s.workgroup, proto.NameTypeMasterBrowser)
	}
	return nbproto.NewName(s.server, nbproto.NameTypeFileServer)
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
		s.masterSeen = true // a stronger candidate exists — do not self-elect
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

// replyHostAnnouncement answers an AnnouncementRequest with a host announcement
// directed back to the requester (replyTo), so a booting client that asked "who is
// out there?" learns of us immediately without waiting for the periodic broadcast. A
// nil replyTo falls back to a broadcast (the transport could not supply an endpoint).
func (s *Service) replyHostAnnouncement(replyTo *nbservice.DatagramEndpoint) {
	if s.sink == nil {
		return
	}
	body := s.announcementBody(proto.OpHostAnnouncement)
	_ = s.sink.SendMailslotTo(
		mswire.NameBrowse,
		nbproto.NewName(s.server, nbproto.NameTypeFileServer),
		nbproto.NewName(s.workgroup, proto.NameTypeMasterBrowser),
		body,
		true,
		replyTo,
	)
}

// emitAnnouncement broadcasts a host or local-master announcement for our identity
// to the workgroup master-browser group name.
func (s *Service) emitAnnouncement(op uint8) {
	if s.sink == nil {
		return
	}
	destType := proto.NameTypeMasterBrowser
	if op == proto.OpLocalMasterAnnounce {
		destType = nbproto.NameTypeGroup
	}
	_ = s.sendBrowseBroadcast(destType, s.announcementBody(op))
}

// announcementBody marshals a host (or local-master) announcement for our identity.
// A local-master announcement MUST advertise the Master Browser type bit in addition
// to our base workstation/server type, or the client does not accept us as the master
// browser and keeps re-running the election / never lists us (the legacy service set
// ServerType = Workstation|Master for the local-master frame; a plain workstation type
// on a 0x0F announcement was a refactor regression — captures/ipx.pcap frame 201).
func (s *Service) announcementBody(op uint8) []byte {
	serverType := proto.ServerTypeWorkstationSet
	if op == proto.OpLocalMasterAnnounce {
		serverType |= proto.ServerTypeMasterBrowser
	}
	return proto.Announcement{
		Op:             op,
		UpdateCount:    announceUpdateCount,
		PeriodicityMS:  uint32(hostAnnouncePeriod / time.Millisecond),
		ServerName:     s.server,
		OSVersionMajor: 4,
		ServerType:     serverType,
		VersionMajor:   proto.AnnounceVersionMajor,
		VersionMinor:   proto.AnnounceVersionMinor,
		Comment:        s.desc,
	}.Marshal()
}

// emitElection broadcasts an election frame for the given candidacy.
func (s *Service) emitElection(local proto.Election) error {
	if s.sink == nil {
		return nil
	}
	return s.sendBrowseBroadcast(nbproto.NameTypeGroup, local.Marshal())
}

// sendBrowseBroadcast writes body to \MAILSLOT\BROWSE, sourced from our file-server
// name (<20>) to the workgroup destination name type (e.g. <1D> or <1E>) as a broadcast.
func (s *Service) sendBrowseBroadcast(destType uint8, body []byte) error {
	return s.sink.SendMailslot(
		mswire.NameBrowse,
		nbproto.NewName(s.server, nbproto.NameTypeFileServer),
		nbproto.NewName(s.workgroup, destType),
		body,
		true,
	)
}
