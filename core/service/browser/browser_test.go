package browser

import (
	"slices"
	"sync"
	"testing"
	"time"

	proto "github.com/ObsoleteMadness/ClassicStack/core/protocol/browser"
	mswire "github.com/ObsoleteMadness/ClassicStack/core/protocol/mailslot"
	nbproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbios"
	nbservice "github.com/ObsoleteMadness/ClassicStack/core/service/netbios"
)

// sentMailslot is one captured outbound mailslot write: the mailslot name, the
// source/destination NetBIOS names, the bare browser-frame body, the broadcast flag,
// and the directed reply endpoint (nil for a broadcast).
type sentMailslot struct {
	name      string
	src, dest nbproto.Name
	body      []byte
	broadcast bool
	replyTo   *nbservice.DatagramEndpoint
}

// recordingSink captures the mailslot writes the browser sends, so tests assert its
// announcements / election / backup-list responses — at the mailslot seam, with no
// envelope (the browser never touches the SMB_COM_TRANSACTION wrapper).
type recordingSink struct {
	mu   sync.Mutex
	sent []sentMailslot
}

func (r *recordingSink) SendMailslot(name string, src, dest nbproto.Name, body []byte, broadcast bool) error {
	return r.SendMailslotTo(name, src, dest, body, broadcast, nil)
}

func (r *recordingSink) SendMailslotTo(name string, src, dest nbproto.Name, body []byte, broadcast bool, replyTo *nbservice.DatagramEndpoint) error {
	r.mu.Lock()
	r.sent = append(r.sent, sentMailslot{name, src, dest, append([]byte(nil), body...), broadcast, replyTo})
	r.mu.Unlock()
	return nil
}

func (r *recordingSink) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sent)
}

// lastBrowserOp decodes the most recent sent body and returns its browser opcode.
func (r *recordingSink) lastBrowserOp(t *testing.T) uint8 {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.sent) == 0 {
		return 0
	}
	op, _, ok := proto.UnwrapPayload(r.sent[len(r.sent)-1].body)
	if !ok {
		t.Fatal("last sent body is not a browser frame")
	}
	return op
}

// hasBrowserOp reports whether any sent body carried the given opcode.
func (r *recordingSink) hasBrowserOp(want uint8) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.sent {
		if op, _, ok := proto.UnwrapPayload(s.body); ok && op == want {
			return true
		}
	}
	return false
}

// deliver drives an inbound browser frame to the service exactly as the mailslot
// router would: the bare frame body on \MAILSLOT\BROWSE with the source name and no
// reply endpoint (a broadcast the service only observes).
func deliver(svc *Service, srv string, body []byte) {
	deliverFrom(svc, srv, body, nil)
}

// deliverFrom is deliver with an explicit reply endpoint, for directed-reply tests.
func deliverFrom(svc *Service, srv string, body []byte, replyTo *nbservice.DatagramEndpoint) {
	svc.HandleMailslot(
		mswire.NameBrowse,
		nbproto.NewName(srv, nbproto.NameTypeWorkstation),
		nbproto.NewName("WORKGROUP", proto.NameTypeMasterBrowser),
		body,
		replyTo,
	)
}

func announcementBody(srv string, serverType uint32) []byte {
	return proto.Announcement{Op: proto.OpHostAnnouncement, ServerName: srv, ServerType: serverType}.Marshal()
}

func electionBody(srv string, criteria, uptime uint32) []byte {
	return proto.Election{Criteria: criteria, Uptime: uptime, ServerName: srv}.Marshal()
}

func backupListReqBody(token uint32) []byte {
	return proto.GetBackupListRequest{RequestedCount: 4, Token: token}.Marshal()
}

func newBrowser(t *testing.T) (*Service, *recordingSink) {
	t.Helper()
	sink := &recordingSink{}
	svc := New(nil, sink, "CLASSICSTACK", "WORKGROUP")
	svc.started = time.Now().Add(-time.Hour) // a real uptime for elections
	return svc, sink
}

// TestObserveAnnouncementBuildsBrowseList proves an observed host announcement
// lands in the browse list and a backup-typed one lands in the backup list.
func TestObserveAnnouncementBuildsBrowseList(t *testing.T) {
	svc, _ := newBrowser(t)
	deliver(svc, "OTHERBOX", announcementBody("OTHERBOX", proto.ServerTypeServer))
	deliver(svc, "BACKUPBOX", announcementBody("BACKUPBOX", proto.ServerTypeServer|proto.ServerTypeBackupBrowser))

	list := svc.BrowseList()
	if !contains(list, "CLASSICSTACK") || !contains(list, "OTHERBOX") || !contains(list, "BACKUPBOX") {
		t.Fatalf("browse list = %v, want self + both observed", list)
	}
	backups := svc.BackupList()
	if !contains(backups, "CLASSICSTACK") || !contains(backups, "BACKUPBOX") {
		t.Fatalf("backup list = %v, want self + BACKUPBOX", backups)
	}
	if contains(backups, "OTHERBOX") {
		t.Errorf("backup list contains a non-backup server: %v", backups)
	}
}

// TestObserveAnnouncementRetainsVersionAndComment proves the enriched browse listing
// (the csnetview "net view" surface): an observed HostAnnouncement's OS/app versions
// and comment are retained on the ServerEntries row, not just the type bits.
func TestObserveAnnouncementRetainsVersionAndComment(t *testing.T) {
	svc, _ := newBrowser(t)
	body := proto.Announcement{
		Op:             proto.OpHostAnnouncement,
		ServerName:     "WIN95BOX",
		ServerType:     proto.ServerTypeServer,
		OSVersionMajor: 4,
		OSVersionMinor: 0,
		VersionMajor:   3,
		VersionMinor:   10,
		Comment:        "Bob's PC",
	}.Marshal()
	deliver(svc, "WIN95BOX", body)

	var got *ServerEntry
	for _, e := range svc.ServerEntries() {
		if e.Name == "WIN95BOX" {
			e := e
			got = &e
		}
	}
	if got == nil {
		t.Fatal("WIN95BOX not in ServerEntries")
	}
	if got.OSMajor != 4 || got.OSMinor != 0 || got.VerMajor != 3 || got.VerMinor != 10 {
		t.Fatalf("version fields = %d.%d / %d.%d, want 4.0 / 3.10", got.OSMajor, got.OSMinor, got.VerMajor, got.VerMinor)
	}
	if got.Comment != "Bob's PC" {
		t.Fatalf("comment = %q, want 'Bob's PC'", got.Comment)
	}
}

// TestServerEntriesCarriesSelfDescription proves the §4-bis server description set via
// SetDescription rides the browser's own ServerEntries row (the comment a Windows
// browse list shows next to our name).
func TestServerEntriesCarriesSelfDescription(t *testing.T) {
	svc, _ := newBrowser(t)
	svc.SetDescription("ClassicStack file server")
	entries := svc.ServerEntries()
	if len(entries) == 0 {
		t.Fatal("ServerEntries returned no rows")
	}
	if entries[0].Name != "CLASSICSTACK" || entries[0].Comment != "ClassicStack file server" {
		t.Fatalf("self entry = %+v, want name CLASSICSTACK comment 'ClassicStack file server'", entries[0])
	}
}

// TestSelfSourcedDatagramDropped proves a frame sourced from our own name (a
// looped-back broadcast) is ignored — no browse-list entry, no storm.
func TestSelfSourcedDatagramDropped(t *testing.T) {
	svc, _ := newBrowser(t)
	deliver(svc, "CLASSICSTACK", announcementBody("CLASSICSTACK", proto.ServerTypeServer))
	svc.mu.Lock()
	n := len(svc.servers)
	svc.mu.Unlock()
	if n != 0 {
		t.Fatalf("self-sourced announcement recorded %d server(s), want 0", n)
	}
}

// TestElectionLost proves an election from a stronger candidate drops us to
// potential and we do NOT transmit an election frame.
func TestElectionLost(t *testing.T) {
	svc, sink := newBrowser(t)
	svc.role = RoleLocalMaster
	deliver(svc, "STRONGER", electionBody("STRONGER", 0xFFFFFFFF, 1))

	if svc.CurrentRole() != RolePotential {
		t.Fatalf("role = %d after losing, want potential", svc.CurrentRole())
	}
	if sink.hasBrowserOp(proto.OpRequestElection) {
		t.Error("transmitted an election frame after losing")
	}
}

// TestElectionWon proves an election from a weaker candidate makes us transmit our
// own election frame and, after the uncontested transmit loop, declare local master
// with a local-master announcement.
func TestElectionWon(t *testing.T) {
	svc, sink := newBrowser(t)
	svc.electionDelay = func(Role) time.Duration { return time.Millisecond }
	deliver(svc, "WEAKER", electionBody("WEAKER", 0, 1))

	if !sink.hasBrowserOp(proto.OpRequestElection) {
		t.Fatal("did not transmit an election frame on a winnable election")
	}
	for _, s := range sink.sent {
		if op, _, ok := proto.UnwrapPayload(s.body); ok && op == proto.OpRequestElection {
			if s.dest.Type() != nbproto.NameTypeGroup {
				t.Errorf("election request dest name type = %#02x, want NameTypeGroup (0x1E)", s.dest.Type())
			}
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if svc.CurrentRole() == RoleLocalMaster {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if svc.CurrentRole() != RoleLocalMaster {
		t.Fatal("did not become local master after an uncontested election")
	}
	if !sink.hasBrowserOp(proto.OpLocalMasterAnnounce) {
		t.Error("did not emit a local-master announcement after winning")
	}
}

// TestLocalMasterAnnouncementCarriesMasterBit proves a local-master announcement
// (opcode 0x0F) advertises the Master Browser type bit on top of our base
// workstation/server type. A plain workstation type on the 0x0F frame — the refactor
// regression seen in captures/ipx.pcap frame 201 — makes clients reject us as master
// and never list \\CLASSICSTACK.
func TestLocalMasterAnnouncementCarriesMasterBit(t *testing.T) {
	svc, sink := newBrowser(t)
	svc.sendLocalMasterAnnouncement()

	last := sink.sent[len(sink.sent)-1]
	if last.dest.Type() != nbproto.NameTypeGroup {
		t.Errorf("local-master dest name type = %#02x, want NameTypeGroup (0x1E)", last.dest.Type())
	}
	op, frame, ok := proto.UnwrapPayload(last.body)
	if !ok || op != proto.OpLocalMasterAnnounce {
		t.Fatalf("last op = %#x ok=%v, want LocalMasterAnnounce", op, ok)
	}
	a, err := proto.UnmarshalAnnouncement(frame)
	if err != nil {
		t.Fatalf("decode local-master announcement: %v", err)
	}
	if a.ServerType&proto.ServerTypeMasterBrowser == 0 {
		t.Errorf("local-master ServerType = %#08x, missing Master Browser bit %#08x", a.ServerType, proto.ServerTypeMasterBrowser)
	}
	if a.ServerType&proto.ServerTypeWorkstationSet != proto.ServerTypeWorkstationSet {
		t.Errorf("local-master ServerType = %#08x, missing base workstation set %#08x", a.ServerType, proto.ServerTypeWorkstationSet)
	}

	// The plain host announcement must NOT carry the Master bit.
	sink.mu.Lock()
	sink.sent = nil
	sink.mu.Unlock()
	svc.sendHostAnnouncement()
	hlast := sink.sent[len(sink.sent)-1]
	if hlast.dest.Type() != proto.NameTypeMasterBrowser {
		t.Errorf("host announcement dest name type = %#02x, want NameTypeMasterBrowser (0x1D)", hlast.dest.Type())
	}
	_, hframe, _ := proto.UnwrapPayload(hlast.body)
	ha, _ := proto.UnmarshalAnnouncement(hframe)
	if ha.ServerType&proto.ServerTypeMasterBrowser != 0 {
		t.Errorf("host announcement ServerType = %#08x, must NOT set Master Browser bit", ha.ServerType)
	}
}

// TestGetBackupListAnsweredOnlyAsMaster proves GetBackupList is answered with a
// 0x0A response (echoing the token) only while we are the local master, and the
// response is directed (not broadcast) back to the requester.
func TestGetBackupListAnsweredOnlyAsMaster(t *testing.T) {
	svc, sink := newBrowser(t)
	deliver(svc, "BACKUPBOX", announcementBody("BACKUPBOX", proto.ServerTypeBackupBrowser))

	// As potential: no response.
	deliver(svc, "CLIENT", backupListReqBody(0xABCD))
	if sink.count() != 0 {
		t.Fatalf("answered GetBackupList while not master (%d sent)", sink.count())
	}

	// As local master: a directed 0x0A response echoing the token.
	svc.mu.Lock()
	svc.role = RoleLocalMaster
	svc.mu.Unlock()
	deliver(svc, "CLIENT", backupListReqBody(0xABCD))
	if sink.lastBrowserOp(t) != proto.OpGetBackupListResp {
		t.Fatalf("last op = %#x, want GetBackupListResp", sink.lastBrowserOp(t))
	}
	last := sink.sent[len(sink.sent)-1]
	if last.broadcast {
		t.Error("GetBackupList response was broadcast, want directed")
	}
	if last.dest.String() != "CLIENT" {
		t.Errorf("response dest = %q, want CLIENT", last.dest.String())
	}
	resp, err := proto.UnmarshalGetBackupListResponse(last.body)
	if err != nil {
		t.Fatalf("decode backup-list response: %v", err)
	}
	if resp.Token != 0xABCD {
		t.Errorf("response token = %#x, want 0xABCD", resp.Token)
	}
	if !contains(resp.BackupServers, "CLASSICSTACK") || !contains(resp.BackupServers, "BACKUPBOX") {
		t.Errorf("backup servers = %v", resp.BackupServers)
	}
}

// TestGetBackupListDirectedToRequester proves a GetBackupList answer is sent
// *directed* back to the requester's transport endpoint (replyTo echoed) and sourced
// from the <workgroup><1D> master-browser identity the client expects, so the client
// accepts the list instead of re-running the election.
func TestGetBackupListDirectedToRequester(t *testing.T) {
	svc, sink := newBrowser(t)
	svc.mu.Lock()
	svc.role = RoleLocalMaster
	svc.mu.Unlock()

	ep := &nbservice.DatagramEndpoint{
		Transport: nbservice.TransportIPX,
		Network:   [4]byte{0, 0, 0, 1},
		Node:      [6]byte{0x00, 0x86, 0xB0, 0xAE, 0x29, 0x6F},
		Socket:    [2]byte{0x05, 0x52},
	}
	deliverFrom(svc, "CLIENT", backupListReqBody(0x1234), ep)

	last := sink.sent[len(sink.sent)-1]
	if last.replyTo != ep {
		t.Fatalf("reply endpoint = %+v, want the requester's %+v", last.replyTo, ep)
	}
	// Source identity must be the <1D> master browser of our workgroup.
	if last.src.String() != "WORKGROUP" || last.src.Type() != proto.NameTypeMasterBrowser {
		t.Errorf("reply source = %q<%#x>, want WORKGROUP<1D>", last.src.String(), last.src.Type())
	}
	if last.dest.String() != "CLIENT" {
		t.Errorf("reply dest = %q, want CLIENT", last.dest.String())
	}
}

// TestAnnouncementRequestAnsweredDirected proves an AnnouncementRequest is answered
// with a HostAnnouncement directed back to the requester (replyTo echoed), so a
// booting client learns of us at once.
func TestAnnouncementRequestAnsweredDirected(t *testing.T) {
	svc, sink := newBrowser(t)
	ep := &nbservice.DatagramEndpoint{Transport: nbservice.TransportNetBEUI, Node: [6]byte{1, 2, 3, 4, 5, 6}}
	// AnnouncementRequest frame: opcode + reserved (no response name).
	deliverFrom(svc, "CLIENT", []byte{proto.OpAnnouncementRequest, 0x00}, ep)

	if sink.lastBrowserOp(t) != proto.OpHostAnnouncement {
		t.Fatalf("last op = %#x, want HostAnnouncement", sink.lastBrowserOp(t))
	}
	if last := sink.sent[len(sink.sent)-1]; last.replyTo != ep {
		t.Errorf("reply endpoint = %+v, want requester's", last.replyTo)
	}
}

// localMasterBody builds a LocalMasterAnnouncement (0x0F) frame for srv.
func localMasterBody(srv string) []byte {
	return proto.Announcement{
		Op:         proto.OpLocalMasterAnnounce,
		ServerName: srv,
		ServerType: proto.ServerTypeWorkstationSet | proto.ServerTypeMasterBrowser,
	}.Marshal()
}

// waitForRole polls until the service reaches want or the deadline elapses.
func waitForRole(svc *Service, want Role, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if svc.CurrentRole() == want {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return svc.CurrentRole() == want
}

// TestStartSelfElectsOnMasterlessSegment proves that on a segment where no master
// browser announces within the discovery window, Start forces our own election and
// we become local master — the fix for ClassicStack being invisible in "net view"
// over IPX/NBIPX, where no client ever sends a RequestElection (captures/ipx.pcap:
// only Host Announcements, no 0x08, no 0x0f from us; every NetServerEnum2 went
// client-to-client).
func TestStartSelfElectsOnMasterlessSegment(t *testing.T) {
	svc, sink := newBrowser(t)
	svc.discoveryDelay = 5 * time.Millisecond
	svc.electionDelay = func(Role) time.Duration { return time.Millisecond }

	if err := svc.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = svc.Stop(t.Context()) })

	if !waitForRole(svc, RoleLocalMaster, 2*time.Second) {
		t.Fatalf("role = %d, want local master after self-election", svc.CurrentRole())
	}
	if !sink.hasBrowserOp(proto.OpRequestElection) {
		t.Error("did not transmit a RequestElection frame")
	}
	if !sink.hasBrowserOp(proto.OpLocalMasterAnnounce) {
		t.Error("did not emit a local-master announcement after self-electing")
	}
}

// TestStartDoesNotSelfElectWhenMasterExists proves that an observed LocalMaster
// announcement from another node suppresses the startup self-election, so
// ClassicStack never fights a real Windows master browser for the role.
func TestStartDoesNotSelfElectWhenMasterExists(t *testing.T) {
	svc, sink := newBrowser(t)
	svc.discoveryDelay = 20 * time.Millisecond
	svc.electionDelay = func(Role) time.Duration { return time.Millisecond }

	if err := svc.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = svc.Stop(t.Context()) })

	// A real master announces before the discovery window elapses.
	deliver(svc, "REALMASTER", localMasterBody("REALMASTER"))

	// Give the watcher time to fire (it must not).
	time.Sleep(60 * time.Millisecond)
	if svc.CurrentRole() != RolePotential {
		t.Fatalf("role = %d, want potential (a master exists)", svc.CurrentRole())
	}
	if sink.hasBrowserOp(proto.OpRequestElection) {
		t.Error("forced an election despite an existing master browser")
	}
}

func contains(ss []string, want string) bool { return slices.Contains(ss, want) }
