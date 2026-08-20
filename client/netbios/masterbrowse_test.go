package netbios

import (
	"testing"
	"time"

	corelink "github.com/ObsoleteMadness/ClassicStack/core/link"
	browserproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/browser"
	mailslotproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/mailslot"
	nb "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbios"
)

// scriptLink is a FrameLink that replays a fixed inbound frame on every Read, so it is
// available in both of FindMaster's time-bounded read halves (announce-listen, then
// backup-list) without a real NIC. Writes are discarded (the solicits FindMaster emits).
type scriptLink struct{ frame []byte }

func (l *scriptLink) Write(corelink.Frame) error    { return nil }
func (l *scriptLink) Read() (corelink.Frame, error) { return corelink.Frame(l.frame), nil }
func (l *scriptLink) Close() error                  { return nil }

// TestMSBrowseName checks the special __MSBROWSE__ segment-master group name is built with
// the exact [MS-BRWS] framing bytes: 0x01 0x02 "__MSBROWSE__" 0x02, suffix <01>. nb.NewName
// would upper-case/space-pad and corrupt the leading 0x01/0x02, which is why it is a raw
// literal.
func TestMSBrowseName(t *testing.T) {
	t.Parallel()
	want := [16]byte{0x01, 0x02, '_', '_', 'M', 'S', 'B', 'R', 'O', 'W', 'S', 'E', '_', '_', 0x02, 0x01}
	if nb.Name(want) != msBrowseName {
		t.Fatalf("__MSBROWSE__ name = % x, want % x", msBrowseName, want)
	}
}

// TestDecodeBackupList_RoundTrip drives the full GetBackupList response receive path over
// NBF: encode the exact frame the master would send (LLC + NBF DATAGRAM + browse mailslot +
// GetBackupListResponse with our token), then decode it back to the backup-browser list.
func TestDecodeBackupList_RoundTrip(t *testing.T) {
	t.Parallel()
	resp := browserproto.GetBackupListResponse{
		Token:         getBackupListToken,
		BackupServers: []string{"BACKUP1", "BACKUP2"},
	}.Marshal()

	c := &Conn{proto: NBF, srcMAC: RandomMAC(), srcName: nb.NewName("CLIENT", NameTypeWorkstation)}
	captured := &captureLink{}
	c.fl = captured
	if err := c.SendMailslot(mailslotproto.NameBrowse, browseGroupName, resp, true); err != nil {
		t.Fatalf("SendMailslot: %v", err)
	}
	servers, ok := c.decodeBackupList(captured.last)
	if !ok {
		t.Fatal("decodeBackupList returned ok=false for a self-sent GetBackupList response")
	}
	if len(servers) != 2 || servers[0] != "BACKUP1" || servers[1] != "BACKUP2" {
		t.Fatalf("backup servers = %v, want [BACKUP1 BACKUP2]", servers)
	}
}

// TestFindMaster_LearnsMasterFromBackupList proves the capture-verified path: when NO
// LocalMasterAnnouncement is heard, FindMaster still identifies the master from the
// GetBackupList RESPONSE alone (its first named server is the master itself). In
// captures/win98nbf-win31nbf.pcapng WIN311 never receives a 0x0F announcement — it learns
// WIN98-NBF is the master purely from frame 26's backup-list answer, then mounts it.
func TestFindMaster_LearnsMasterFromBackupList(t *testing.T) {
	t.Parallel()
	// Build the exact backup-list response frame the master puts on the wire (self-encode
	// the same way sendNBF does), so the scripted link replays a realistic inbound frame.
	respBody := browserproto.GetBackupListResponse{
		Token:         getBackupListToken,
		BackupServers: []string{"WIN98-NBF"},
	}.Marshal()
	enc := &Conn{proto: NBF, srcMAC: [6]byte{0x00, 0x86, 0xB0, 0xA4, 0xB8, 0x81}, srcName: nb.NewName("WIN98-NBF", NameTypeFileServer)}
	sink := &captureLink{}
	enc.fl = sink
	if err := enc.SendMailslot(mailslotproto.NameBrowse, nb.NewName("WIN311-NBF", NameTypeWorkstation), respBody, false); err != nil {
		t.Fatalf("encode response: %v", err)
	}

	c := &Conn{proto: NBF, srcMAC: RandomMAC(), srcName: nb.NewName("WIN311-NBF", NameTypeWorkstation)}
	// Replay the response in both read halves: FindMaster reads announcements first (where
	// this frame is a non-announcement no-op), then the backup-list half (where it is
	// decoded and promotes the master), so make it available in both.
	c.fl = &scriptLink{frame: sink.last}
	info, err := c.FindMaster("WORKGROUP", 20*time.Millisecond)
	if err != nil {
		t.Fatalf("FindMaster: %v", err)
	}
	if info.MasterName != "WIN98-NBF" {
		t.Fatalf("MasterName = %q, want WIN98-NBF (learned from the GetBackupList response)", info.MasterName)
	}
}

// TestDecodeBackupList_WrongToken confirms a GetBackupList response bearing a different
// token is ignored — a stale reply to someone else's request must not pollute our list.
func TestDecodeBackupList_WrongToken(t *testing.T) {
	t.Parallel()
	resp := browserproto.GetBackupListResponse{
		Token:         getBackupListToken ^ 0xFFFFFFFF,
		BackupServers: []string{"OTHER"},
	}.Marshal()

	c := &Conn{proto: NBF, srcMAC: RandomMAC(), srcName: nb.NewName("CLIENT", NameTypeWorkstation)}
	captured := &captureLink{}
	c.fl = captured
	if err := c.SendMailslot(mailslotproto.NameBrowse, browseGroupName, resp, true); err != nil {
		t.Fatalf("SendMailslot: %v", err)
	}
	if servers, ok := c.decodeBackupList(captured.last); ok {
		t.Fatalf("decodeBackupList accepted a foreign token: %v", servers)
	}
}

// TestRequestBackupList_Emits confirms requestBackupList puts a GetBackupList request on the
// wire that decodes back to our requested count + token (the exact bytes a master receives).
func TestRequestBackupList_Emits(t *testing.T) {
	t.Parallel()
	c := &Conn{proto: NBF, srcMAC: RandomMAC(), srcName: nb.NewName("CLIENT", NameTypeWorkstation)}
	captured := &captureLink{}
	c.fl = captured
	if err := c.requestBackupList(""); err != nil {
		t.Fatalf("requestBackupList: %v", err)
	}
	payload := c.browserPayload(captured.last)
	if payload == nil {
		t.Fatal("no browser datagram was emitted")
	}
	w, err := mailslotproto.Unmarshal(payload)
	if err != nil || w.Name != mailslotproto.NameBrowse {
		t.Fatalf("mailslot = %q err=%v, want %s", w.Name, err, mailslotproto.NameBrowse)
	}
	op, frame, ok := browserproto.UnwrapPayload(w.Body)
	if !ok || op != browserproto.OpGetBackupListReq {
		t.Fatalf("op = %#x ok=%t, want GetBackupListReq %#x", op, ok, browserproto.OpGetBackupListReq)
	}
	req, err := browserproto.UnmarshalGetBackupListRequest(frame)
	if err != nil {
		t.Fatalf("UnmarshalGetBackupListRequest: %v", err)
	}
	if req.Token != getBackupListToken || req.RequestedCount != getBackupListRequestedCount {
		t.Fatalf("request = %+v, want token %#x count %d", *req, getBackupListToken, getBackupListRequestedCount)
	}
}

// TestSolicitCarriesResponseName guards the fix for the "malformed AnnouncementRequest"
// wire bug: a browser AnnouncementRequest with no NUL-terminated ResponseName is rejected
// by real Win98/NT browsers (Wireshark flags it "Malformed Packet: BROWSER") and never
// draws a re-announce. Our solicit MUST populate ResponseName with the station's computer
// name — the same shape a real host sends (verified against captures/nt-98-nbf.pcap frame 19).
func TestSolicitCarriesResponseName(t *testing.T) {
	t.Parallel()
	station := nb.NewName("CS-TEST", NameTypeWorkstation)
	c := &Conn{proto: NBF, srcMAC: RandomMAC(), srcName: station}
	captured := &captureLink{}
	c.fl = captured
	if err := c.solicit(""); err != nil {
		t.Fatalf("solicit: %v", err)
	}
	w, err := mailslotproto.Unmarshal(c.browserPayload(captured.last))
	if err != nil {
		t.Fatalf("mailslot Unmarshal: %v", err)
	}
	op, frame, ok := browserproto.UnwrapPayload(w.Body)
	if !ok || op != browserproto.OpAnnouncementRequest {
		t.Fatalf("op = %#x ok=%t, want AnnouncementRequest %#x", op, ok, browserproto.OpAnnouncementRequest)
	}
	req, err := browserproto.UnmarshalAnnouncementRequest(frame)
	if err != nil {
		t.Fatalf("UnmarshalAnnouncementRequest: %v", err)
	}
	if req.ResponseName != "CS-TEST" {
		t.Fatalf("ResponseName = %q, want CS-TEST (an empty name is a malformed request real browsers drop)", req.ResponseName)
	}
}
