package main

import (
	"testing"

	browserproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/browser"
	mailslotproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/mailslot"
	nbf "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbeui"
)

// browseDatagramPayload builds the NBF datagram payload a browser host broadcasts:
// the SMB mailslot \MAILSLOT\BROWSE transaction wrapping a bare browser frame body.
func browseDatagramPayload(frame []byte) []byte {
	return mailslotproto.Write{Name: mailslotproto.NameBrowse, Body: frame}.Marshal()
}

func TestAnnouncementToHost_NetBEUIHostAnnouncement(t *testing.T) {
	t.Parallel()
	ann := browserproto.Announcement{
		Op:             browserproto.OpHostAnnouncement,
		ServerName:     "WIN95BOX",
		ServerType:     browserproto.ServerTypeServer,
		OSVersionMajor: 4,
		OSVersionMinor: 0,
		VersionMajor:   3,
		VersionMinor:   10,
		Comment:        "Bob's PC",
	}.Marshal()

	h := announcementToHost(browseDatagramPayload(ann), "NetBEUI", "00:11:22:33:44:55")
	if h == nil {
		t.Fatal("announcementToHost returned nil for a valid host announcement")
	}
	if h.name != "WIN95BOX" || h.transport != "NetBEUI" || h.addr != "00:11:22:33:44:55" {
		t.Fatalf("identity mismatch: %+v", h)
	}
	if h.osVer != "4.0" || h.appVer != "3.10" {
		t.Fatalf("version mismatch: os=%q app=%q", h.osVer, h.appVer)
	}
	if h.comment != "Bob's PC" || h.role != "host" {
		t.Fatalf("comment/role mismatch: %+v", h)
	}
}

// TestDecodeNetBEUI_EndToEnd drives the full NBF decode path: an NBF
// datagram-broadcast frame whose payload is the browse mailslot transaction.
func TestDecodeNetBEUI_EndToEnd(t *testing.T) {
	t.Parallel()
	ann := browserproto.Announcement{
		Op:             browserproto.OpLocalMasterAnnounce,
		ServerName:     "MASTER",
		OSVersionMajor: 5,
		OSVersionMinor: 0,
	}.Marshal()
	f := &nbf.Frame{Command: nbf.CmdDatagramBroadcast, Payload: browseDatagramPayload(ann)}
	body, err := f.Encode()
	if err != nil {
		t.Fatalf("encode NBF: %v", err)
	}
	h := decodeNetBEUI(body, "aa:bb:cc:dd:ee:ff")
	if h == nil {
		t.Fatal("decodeNetBEUI returned nil")
	}
	if h.name != "MASTER" || h.role != "master" || h.osVer != "5.0" {
		t.Fatalf("host mismatch: %+v", h)
	}
}

func TestAnnouncementToHost_IgnoresNonBrowse(t *testing.T) {
	t.Parallel()
	// A mailslot write to a non-browse mailslot must be ignored.
	other := mailslotproto.Write{Name: "\\MAILSLOT\\MESSNGR", Body: []byte{0x01}}.Marshal()
	if h := announcementToHost(other, "NetBEUI", "x"); h != nil {
		t.Fatalf("expected nil for non-browse mailslot, got %+v", h)
	}
}

func TestMerge_KeepsRicherFields(t *testing.T) {
	t.Parallel()
	hosts := map[string]*host{}
	merge(hosts, &host{name: "PC", transport: "NetBEUI", addr: "m", osVer: "4.0", appVer: "3.10", comment: "first", role: "host"})
	// A later, sparser domain announcement must not wipe the version/comment.
	merge(hosts, &host{name: "PC", transport: "NetBEUI", addr: "m", role: "domain master"})
	got := hosts["PC"]
	if got.osVer != "4.0" || got.comment != "first" {
		t.Fatalf("richer fields lost on merge: %+v", got)
	}
	if got.role != "domain master" {
		t.Fatalf("role should upgrade to domain master: %+v", got)
	}
}
