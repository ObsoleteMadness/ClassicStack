package browser

import (
	"testing"
)

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

// TestUnwrapPayload proves the opcode/pad detection: a bare frame, a frame behind the
// two-byte Win9x pad, and a non-browser payload.
//
// Detection is opcode AND minimum length — an opcode byte alone is not enough. Real
// Win98 leaves whatever two bytes its buffer last held ahead of the data block, and
// some of those values (0x0F LocalMasterAnnouncement, 0x0C DomainAnnouncement) are
// themselves valid opcodes; without the length test a 7-byte GetBackupList behind a
// `0f 07` pad decoded as a truncated announcement and was dropped. So the fixtures
// here are full-length frames, not two-byte stubs.
func TestUnwrapPayload(t *testing.T) {
	// A bare, correctly-sized frame at offset 0.
	host := make([]byte, AnnouncementMinLen)
	host[0] = OpHostAnnouncement
	if op, frame, ok := UnwrapPayload(host); !ok || op != OpHostAnnouncement || frame[0] != OpHostAnnouncement {
		t.Error("bare full-length frame not detected")
	}

	// The same frame behind a two-byte pad whose first byte is ITSELF a valid opcode
	// (0x0F) — the case that regressed. Length must decide, so the pad loses.
	padded := append([]byte{OpLocalMasterAnnounce, 0x07}, make([]byte, ElectionMinLen)...)
	padded[win9xPadLen] = OpRequestElection
	if op, frame, ok := UnwrapPayload(padded); !ok || op != OpRequestElection || frame[0] != OpRequestElection {
		t.Errorf("padded frame not skipped to opcode: op=%#x ok=%v", op, ok)
	}

	// An opcode byte with nothing behind it is NOT a frame.
	if _, _, ok := UnwrapPayload([]byte{OpHostAnnouncement, 0x00}); ok {
		t.Error("undersized payload accepted as an announcement")
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

// TestElectionCriteriaPacking proves ElectionCriteria packs the four criteria bytes
// in the [MS-BRWS] §2.2.17 precedence order (OS, minor, major, desire) by decoding
// the value a real Win98 station put on the wire, and proves ClassicStack's own
// advertised criteria outranks it — the regression that let the two flap between
// Local Master (captures/ipx.pcap 2026-08-19).
func TestElectionCriteriaPacking(t *testing.T) {
	// captures/ipx.pcap frame 163: WIN98-1 advertises OS 0x01 (WfW), browser
	// protocol 0x15.0x04, desire 0x00.
	const win98 uint32 = 0x01041500
	if got := ElectionCriteria(ElectionOSWfW, AnnounceVersionMajor, AnnounceVersionMinor, 0x00); got != win98 {
		t.Fatalf("ElectionCriteria packing = 0x%08X, want the observed 0x%08X", got, win98)
	}
	// Our own candidacy: same OS and version, plus the Master desire bit.
	if ElectionCriteriaMaster <= win98 {
		t.Errorf("ElectionCriteriaMaster = 0x%08X does not outrank a Win98 peer (0x%08X)",
			ElectionCriteriaMaster, win98)
	}
	// The OS byte must dominate the comparison: an NT Server with no desire bits
	// still beats a WfW master.
	nt := ElectionCriteria(ElectionOSNTServer, 0, 0, 0)
	if Compare(Election{Criteria: nt}, Election{Criteria: ElectionCriteriaMaster}) <= 0 {
		t.Error("Election OS should outrank desire")
	}
}

// TestCaptureReplay_ElectionFrame round-trips the exact RequestElection bytes a real
// Win98 put on the wire (spec/captures/nbf-win98.pcap frame 33, mailslot DataCount 26)
// and re-marshals them byte-identically. It pins the 14-byte fixed header — opcode,
// version, criteria(4), uptime(4), MustBeZero(4) — plus the NUL-terminated candidate
// name, which is what makes ElectionMinLen 15 (14 + at least the terminator).
//
// The uptime here is the one that proved the field is MILLISECONDS: 0x000051df =
// 20959, and the frame is at t=16.2s with the box booted ~20.9s earlier.
func TestCaptureReplay_ElectionFrame(t *testing.T) {
	golden := []byte{
		0x08,                   // RequestElection
		0x01,                   // Election Version
		0x00, 0x15, 0x04, 0x01, // Criteria 0x01041500 (OS WfW, 21.4, desire 0)
		0xdf, 0x51, 0x00, 0x00, // Uptime 20959 ms
		0x00, 0x00, 0x00, 0x00, // MustBeZero
		'W', 'I', 'N', '9', '8', '-', 'N', 'B', 'F', '-', '1', 0x00,
	}
	if len(golden) != 26 {
		t.Fatalf("fixture is %d bytes, want the captured 26", len(golden))
	}
	if len(golden) < ElectionMinLen {
		t.Fatalf("fixture shorter than ElectionMinLen %d", ElectionMinLen)
	}

	got, err := UnmarshalElection(golden)
	if err != nil {
		t.Fatalf("UnmarshalElection of a real Win98 frame: %v", err)
	}
	if got.Version != ElectionVersion {
		t.Errorf("Version = %#x, want %#x", got.Version, ElectionVersion)
	}
	if got.Criteria != 0x01041500 {
		t.Errorf("Criteria = %#08x, want 0x01041500", got.Criteria)
	}
	if got.Uptime != 20959 {
		t.Errorf("Uptime = %d, want 20959 (milliseconds, not seconds)", got.Uptime)
	}
	if got.Reserved != 0 {
		t.Errorf("MustBeZero = %#x, want 0", got.Reserved)
	}
	if got.ServerName != "WIN98-NBF-1" {
		t.Errorf("ServerName = %q, want WIN98-NBF-1", got.ServerName)
	}
	if round := got.Marshal(); string(round) != string(golden) {
		t.Errorf("re-marshal drifted:\n got % x\nwant % x", round, golden)
	}
}
