package browser

import (
	"slices"
	"sync"
	"testing"
	"time"

	proto "github.com/ObsoleteMadness/ClassicStack/core/protocol/browser"
	nbproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbios"
	"github.com/ObsoleteMadness/ClassicStack/core/service/netbios"
)

// recordingSink captures the datagrams the browser sends, so tests can assert its
// announcements / election / backup-list responses.
type recordingSink struct {
	mu   sync.Mutex
	sent []netbios.Datagram
}

func (r *recordingSink) SendDatagram(d netbios.Datagram) error {
	r.mu.Lock()
	r.sent = append(r.sent, d)
	r.mu.Unlock()
	return nil
}

func (r *recordingSink) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sent)
}

// lastBrowserOp decodes the most recent sent datagram and returns its browser
// opcode, or 0 if none/undecodable.
func (r *recordingSink) lastBrowserOp(t *testing.T) uint8 {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.sent) == 0 {
		return 0
	}
	tx, err := proto.UnmarshalMailslotTransaction(r.sent[len(r.sent)-1].Payload)
	if err != nil {
		t.Fatalf("decode last sent: %v", err)
	}
	op, _, ok := proto.UnwrapPayload(tx.Payload)
	if !ok {
		t.Fatal("last sent payload is not a browser frame")
	}
	return op
}

// hasBrowserOp reports whether any sent datagram carried the given opcode.
func (r *recordingSink) hasBrowserOp(t *testing.T, want uint8) bool {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range r.sent {
		tx, err := proto.UnmarshalMailslotTransaction(d.Payload)
		if err != nil {
			continue
		}
		if op, _, ok := proto.UnwrapPayload(tx.Payload); ok && op == want {
			return true
		}
	}
	return false
}

// announcementFrom builds an inbound host-announcement datagram from server name
// srv with type bits, wrapped in the mailslot envelope, sourced from srv.
func announcementFrom(srv string, serverType uint32) netbios.Datagram {
	payload := proto.Announcement{
		Op:         proto.OpHostAnnouncement,
		ServerName: srv,
		ServerType: serverType,
	}.Marshal()
	tx := proto.MailslotTransaction{MailslotName: proto.MailslotBrowse, Payload: payload}.Marshal()
	return netbios.Datagram{
		Source:      nbproto.NewName(srv, nbproto.NameTypeWorkstation),
		Destination: nbproto.NewName("WORKGROUP", proto.NameTypeMasterBrowser),
		Payload:     tx,
		Broadcast:   true,
	}
}

// electionFrom builds an inbound election datagram from server name srv with the
// given criteria/uptime.
func electionFrom(srv string, criteria, uptime uint32) netbios.Datagram {
	payload := proto.Election{Criteria: criteria, Uptime: uptime, ServerName: srv}.Marshal()
	tx := proto.MailslotTransaction{MailslotName: proto.MailslotBrowse, Payload: payload}.Marshal()
	return netbios.Datagram{
		Source:      nbproto.NewName(srv, nbproto.NameTypeWorkstation),
		Destination: nbproto.NewName("WORKGROUP", proto.NameTypeMasterBrowser),
		Payload:     tx,
		Broadcast:   true,
	}
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
	svc.HandleDatagram(announcementFrom("OTHERBOX", proto.ServerTypeServer))
	svc.HandleDatagram(announcementFrom("BACKUPBOX", proto.ServerTypeServer|proto.ServerTypeBackupBrowser))

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

// TestSelfSourcedDatagramDropped proves a datagram sourced from our own name (a
// looped-back broadcast) is ignored — no browse-list entry, no storm.
func TestSelfSourcedDatagramDropped(t *testing.T) {
	svc, _ := newBrowser(t)
	svc.HandleDatagram(announcementFrom("CLASSICSTACK", proto.ServerTypeServer))
	// BrowseList always includes self once; ensure no *duplicate* server record.
	svc.mu.Lock()
	n := len(svc.servers)
	svc.mu.Unlock()
	if n != 0 {
		t.Fatalf("self-sourced announcement recorded %d server(s), want 0", n)
	}
}

// TestElectionLost proves that an election from a stronger candidate (higher
// criteria) drops us to potential and we do NOT transmit an election frame.
func TestElectionLost(t *testing.T) {
	svc, sink := newBrowser(t)
	svc.role = RoleLocalMaster
	svc.HandleDatagram(electionFrom("STRONGER", 0xFFFFFFFF, 1))

	if svc.CurrentRole() != RolePotential {
		t.Fatalf("role = %d after losing, want potential", svc.CurrentRole())
	}
	if sink.hasBrowserOp(t, proto.OpRequestElection) {
		t.Error("transmitted an election frame after losing")
	}
}

// TestElectionWon proves that an election from a weaker candidate makes us
// transmit our own election frame and, after the uncontested transmit loop,
// declare local master with a local-master announcement.
func TestElectionWon(t *testing.T) {
	svc, sink := newBrowser(t)
	svc.electionDelay = func(Role) time.Duration { return time.Millisecond }
	svc.HandleDatagram(electionFrom("WEAKER", 0, 1))

	// Immediate: we transmit our candidacy.
	if !sink.hasBrowserOp(t, proto.OpRequestElection) {
		t.Fatal("did not transmit an election frame on a winnable election")
	}
	// Eventually: uncontested loop completes → local master + announcement.
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
	if !sink.hasBrowserOp(t, proto.OpLocalMasterAnnounce) {
		t.Error("did not emit a local-master announcement after winning")
	}
}

// TestGetBackupListAnsweredOnlyAsMaster proves GetBackupList is answered with a
// 0x0A response (echoing the token) only while we are the local master.
func TestGetBackupListAnsweredOnlyAsMaster(t *testing.T) {
	svc, sink := newBrowser(t)
	svc.HandleDatagram(announcementFrom("BACKUPBOX", proto.ServerTypeBackupBrowser))

	// As potential: no response.
	svc.HandleDatagram(backupListReqFrom("CLIENT", 0xABCD))
	if sink.count() != 0 {
		t.Fatalf("answered GetBackupList while not master (%d sent)", sink.count())
	}

	// As local master: a 0x0A response echoing the token.
	svc.mu.Lock()
	svc.role = RoleLocalMaster
	svc.mu.Unlock()
	svc.HandleDatagram(backupListReqFrom("CLIENT", 0xABCD))
	if sink.lastBrowserOp(t) != proto.OpGetBackupListResp {
		t.Fatalf("last op = %#x, want GetBackupListResp", sink.lastBrowserOp(t))
	}
	tx, _ := proto.UnmarshalMailslotTransaction(sink.sent[len(sink.sent)-1].Payload)
	resp, err := proto.UnmarshalGetBackupListResponse(tx.Payload)
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

func backupListReqFrom(srv string, token uint32) netbios.Datagram {
	payload := proto.GetBackupListRequest{RequestedCount: 4, Token: token}.Marshal()
	tx := proto.MailslotTransaction{MailslotName: proto.MailslotBrowse, Payload: payload}.Marshal()
	return netbios.Datagram{
		Source:      nbproto.NewName(srv, nbproto.NameTypeWorkstation),
		Destination: nbproto.NewName("WORKGROUP", proto.NameTypeMasterBrowser),
		Payload:     tx,
	}
}

func contains(ss []string, want string) bool { return slices.Contains(ss, want) }
