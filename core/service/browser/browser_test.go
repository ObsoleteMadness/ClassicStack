package browser

import (
	"slices"
	"sync"
	"testing"
	"time"

	proto "github.com/ObsoleteMadness/ClassicStack/core/protocol/browser"
	mswire "github.com/ObsoleteMadness/ClassicStack/core/protocol/mailslot"
	nbproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbios"
)

// sentMailslot is one captured outbound mailslot write: the mailslot name, the
// source/destination NetBIOS names, the bare browser-frame body, and broadcast flag.
type sentMailslot struct {
	name      string
	src, dest nbproto.Name
	body      []byte
	broadcast bool
}

// recordingSink captures the mailslot writes the browser sends, so tests assert its
// announcements / election / backup-list responses — at the mailslot seam, with no
// envelope (the browser never touches the SMB_COM_TRANSACTION wrapper).
type recordingSink struct {
	mu   sync.Mutex
	sent []sentMailslot
}

func (r *recordingSink) SendMailslot(name string, src, dest nbproto.Name, body []byte, broadcast bool) error {
	r.mu.Lock()
	r.sent = append(r.sent, sentMailslot{name, src, dest, append([]byte(nil), body...), broadcast})
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
// router would: the bare frame body on \MAILSLOT\BROWSE with the source name.
func deliver(svc *Service, srv string, body []byte) {
	svc.HandleMailslot(
		mswire.NameBrowse,
		nbproto.NewName(srv, nbproto.NameTypeWorkstation),
		nbproto.NewName("WORKGROUP", proto.NameTypeMasterBrowser),
		body,
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

func contains(ss []string, want string) bool { return slices.Contains(ss, want) }
