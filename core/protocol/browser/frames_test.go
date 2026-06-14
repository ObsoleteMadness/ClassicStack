package browser

import (
	"bytes"
	"testing"
)

// TestMailslotTransactionRoundTrip proves a browser payload survives wrapping in
// the SMB_COM_TRANSACTION mailslot envelope and unwrapping back, with the mailslot
// name preserved.
func TestMailslotTransactionRoundTrip(t *testing.T) {
	payload := []byte{OpHostAnnouncement, 0x01, 0x02, 0x03}
	tx := MailslotTransaction{MailslotName: MailslotBrowse, Payload: payload}
	wire := tx.Marshal()

	got, err := UnmarshalMailslotTransaction(wire)
	if err != nil {
		t.Fatalf("UnmarshalMailslotTransaction: %v", err)
	}
	if got.MailslotName != MailslotBrowse {
		t.Errorf("mailslot name = %q, want %q", got.MailslotName, MailslotBrowse)
	}
	if !bytes.Equal(got.Payload, payload) {
		t.Errorf("payload = % x, want % x", got.Payload, payload)
	}
}

// TestAnnouncementRoundTrip proves a host announcement Marshals and Unmarshals with
// every field preserved (the server name normalised/upper-cased).
func TestAnnouncementRoundTrip(t *testing.T) {
	a := Announcement{
		Op:             OpHostAnnouncement,
		UpdateCount:    3,
		PeriodicityMS:  120000,
		ServerName:     "CLASSICSTACK",
		OSVersionMajor: 4,
		ServerType:     ServerTypeWorkstationSet,
		VersionMajor:   AnnounceVersionMajor,
		VersionMinor:   AnnounceVersionMinor,
		Comment:        "test box",
	}
	got, err := UnmarshalAnnouncement(a.Marshal())
	if err != nil {
		t.Fatalf("UnmarshalAnnouncement: %v", err)
	}
	if got.ServerName != "CLASSICSTACK" || got.ServerType != ServerTypeWorkstationSet {
		t.Errorf("server=%q type=%#x", got.ServerName, got.ServerType)
	}
	if got.PeriodicityMS != 120000 || got.Comment != "test box" {
		t.Errorf("periodicity=%d comment=%q", got.PeriodicityMS, got.Comment)
	}
	if got.Op != OpHostAnnouncement {
		t.Errorf("op = %#x, want host announcement", got.Op)
	}
}

// TestElectionRoundTripAndCompare proves the election frame round-trips and that
// Compare implements the [MS-BRWS] ordering: higher criteria wins, then higher
// uptime, then lexically lower name; an identical frame ties.
func TestElectionRoundTripAndCompare(t *testing.T) {
	e := Election{Version: ElectionVersion, Criteria: ElectionCriteriaMaster, Uptime: 5000, ServerName: "CLASSICSTACK"}
	got, err := UnmarshalElection(e.Marshal())
	if err != nil {
		t.Fatalf("UnmarshalElection: %v", err)
	}
	if got.Criteria != ElectionCriteriaMaster || got.Uptime != 5000 || got.ServerName != "CLASSICSTACK" {
		t.Fatalf("decoded = %+v", *got)
	}

	// Higher criteria wins.
	hi := Election{Criteria: ElectionCriteriaMaster, Uptime: 1}
	lo := Election{Criteria: 0, Uptime: 9999}
	if Compare(hi, lo) <= 0 {
		t.Error("higher criteria should win regardless of uptime")
	}
	// Equal criteria → higher uptime wins.
	old := Election{Criteria: 1, Uptime: 9999, ServerName: "ZED"}
	young := Election{Criteria: 1, Uptime: 1, ServerName: "AAA"}
	if Compare(old, young) <= 0 {
		t.Error("higher uptime should win on equal criteria")
	}
	// Equal criteria + uptime → lexically lower name wins.
	if Compare(Election{Criteria: 1, Uptime: 1, ServerName: "AAA"}, Election{Criteria: 1, Uptime: 1, ServerName: "ZED"}) <= 0 {
		t.Error("lower name should win on equal criteria+uptime")
	}
	// Identical → tie.
	if Compare(e, e) != 0 {
		t.Error("identical election frames should tie")
	}
}

// TestGetBackupListRoundTrip proves the request and response round-trip, including
// the variable server-name list and the echoed token.
func TestGetBackupListRoundTrip(t *testing.T) {
	req := GetBackupListRequest{RequestedCount: 4, Token: 0xdeadbeef}
	gotReq, err := UnmarshalGetBackupListRequest(req.Marshal())
	if err != nil || gotReq.Token != 0xdeadbeef || gotReq.RequestedCount != 4 {
		t.Fatalf("request round-trip: %v %+v", err, gotReq)
	}

	resp := GetBackupListResponse{Token: 0xdeadbeef, BackupServers: []string{"CLASSICSTACK", "OTHERBOX"}}
	gotResp, err := UnmarshalGetBackupListResponse(resp.Marshal())
	if err != nil {
		t.Fatalf("UnmarshalGetBackupListResponse: %v", err)
	}
	if gotResp.Token != 0xdeadbeef {
		t.Errorf("token = %#x", gotResp.Token)
	}
	if len(gotResp.BackupServers) != 2 || gotResp.BackupServers[0] != "CLASSICSTACK" || gotResp.BackupServers[1] != "OTHERBOX" {
		t.Errorf("servers = %v", gotResp.BackupServers)
	}
}

// TestUnwrapPayload proves the opcode/preamble detection: a bare opcode, a Win9x
// preamble (01 03), and a non-browser payload.
func TestUnwrapPayload(t *testing.T) {
	if op, _, ok := UnwrapPayload([]byte{OpHostAnnouncement, 0x00}); !ok || op != OpHostAnnouncement {
		t.Error("bare opcode not detected")
	}
	if op, frame, ok := UnwrapPayload([]byte{0x01, 0x03, OpRequestElection, 0xff}); !ok || op != OpRequestElection || frame[0] != OpRequestElection {
		t.Error("Win9x preamble not skipped to opcode")
	}
	if _, _, ok := UnwrapPayload([]byte{0x99, 0x98, 0x97}); ok {
		t.Error("non-browser payload accepted")
	}
}

// TestUnmarshalRejectsWrongOpcode proves a frame Unmarshalled as the wrong type is
// rejected (ErrBadOp), not mis-decoded.
func TestUnmarshalRejectsWrongOpcode(t *testing.T) {
	host := Announcement{Op: OpHostAnnouncement, ServerName: "X"}.Marshal()
	if _, err := UnmarshalElection(host); err == nil {
		t.Error("election Unmarshal accepted a host announcement")
	}
}
