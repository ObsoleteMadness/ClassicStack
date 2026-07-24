package netbios

import (
	"testing"

	corelink "github.com/ObsoleteMadness/ClassicStack/core/link"
	browserproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/browser"
	mailslotproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/mailslot"
	messengerproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/messenger"
	nb "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbios"
)

// --- Target / protocol parsing ---

func TestParseTarget(t *testing.T) {
	t.Parallel()
	got, err := ParseTarget("SERVER,nbf", MessengerNameType)
	if err != nil {
		t.Fatalf("ParseTarget: %v", err)
	}
	if got.Name.String() != "SERVER" {
		t.Errorf("name = %q, want SERVER", got.Name.String())
	}
	if got.Name.Type() != MessengerNameType {
		t.Errorf("name type = %#x, want %#x", got.Name.Type(), MessengerNameType)
	}
	if got.Protocol != NBF {
		t.Errorf("protocol = %q, want %q", got.Protocol, NBF)
	}
}

func TestParseTargetErrors(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"SERVER", "SERVER,tcp", ",nbf", "SERVER,"} {
		if _, err := ParseTarget(s, NameTypeFileServer); err == nil {
			t.Errorf("ParseTarget(%q) = nil error, want an error", s)
		}
	}
}

func TestOpenerFor(t *testing.T) {
	t.Parallel()
	// pcap and tap are accepted (raw-Ethernet carriers); an empty type defaults to pcap.
	for _, kind := range []string{"pcap", "tap", "", "PCAP"} {
		o, err := OpenerFor(kind, "dev0", [6]byte{})
		if err != nil {
			t.Fatalf("OpenerFor(%q) = %v, want ok", kind, err)
		}
		if o.MAC == ([6]byte{}) {
			t.Errorf("OpenerFor(%q) left a zero MAC; want a synthesised station MAC", kind)
		}
	}
	// A pinned MAC is preserved; a datagram carrier over a DDP/TCP kind is rejected.
	pinned := [6]byte{0x02, 1, 2, 3, 4, 5}
	if o, err := OpenerFor("pcap", "dev0", pinned); err != nil || o.MAC != pinned {
		t.Fatalf("pinned MAC not preserved: mac=%v err=%v", o.MAC, err)
	}
	for _, bad := range []string{"ltoudp", "tashtalk", "tcp", "bogus"} {
		if _, err := OpenerFor(bad, "x", [6]byte{}); err == nil {
			t.Errorf("OpenerFor(%q) = nil error; a non-raw-Ethernet kind must be rejected", bad)
		}
	}
}

func TestParseProtocol(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]Protocol{"nbf": NBF, "NBIPX": NBIPX, " nbf ": NBF} {
		got, err := ParseProtocol(in)
		if err != nil || got != want {
			t.Errorf("ParseProtocol(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := ParseProtocol("ipx"); err == nil {
		t.Error("ParseProtocol(ipx) should fail — direct IPX carries no NetBIOS datagram")
	}
}

// --- Messenger payload round-trip (protocol-reuse proof) ---

// TestMessengerPayloadRoundTrips proves the payload SendMessage assembles (a messenger
// frame inside a \MAILSLOT\MESSNGR transaction) decodes back through the SAME core codecs
// the messenger service uses: what the client builds is exactly what the server parses.
func TestMessengerPayloadRoundTrips(t *testing.T) {
	t.Parallel()
	body := messengerproto.Message{From: "ALICE", To: "BOB", Text: "hello there"}.Marshal()
	payload := mailslotproto.Write{Name: mailslotproto.NameMessenger, Body: body}.Marshal()

	w, err := mailslotproto.Unmarshal(payload)
	if err != nil {
		t.Fatalf("mailslot Unmarshal: %v", err)
	}
	if w.Name != mailslotproto.NameMessenger {
		t.Errorf("mailslot name = %q, want %q", w.Name, mailslotproto.NameMessenger)
	}
	m, err := messengerproto.Unmarshal(w.Body)
	if err != nil {
		t.Fatalf("messenger Unmarshal: %v", err)
	}
	if m.From != "ALICE" || m.To != "BOB" || m.Text != "hello there" {
		t.Errorf("decoded message = %+v, want From=ALICE To=BOB Text=\"hello there\"", *m)
	}
}

// --- Browser announcement decode ---

// browseDatagramPayload wraps a bare browser frame in the \MAILSLOT\BROWSE transaction a
// host broadcasts.
func browseDatagramPayload(frame []byte) []byte {
	return mailslotproto.Write{Name: mailslotproto.NameBrowse, Body: frame}.Marshal()
}

func TestAnnouncementToHost_HostAnnouncement(t *testing.T) {
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

	h := announcementToHost(browseDatagramPayload(ann), NBF, "00:11:22:33:44:55")
	if h == nil {
		t.Fatal("announcementToHost returned nil for a valid host announcement")
	}
	if h.Name != "WIN95BOX" || h.Protocol != NBF || h.Address != "00:11:22:33:44:55" {
		t.Fatalf("identity mismatch: %+v", h)
	}
	if h.OSVersion != "4.0" || h.AppVersion != "3.10" {
		t.Fatalf("version mismatch: os=%q app=%q", h.OSVersion, h.AppVersion)
	}
	if h.Comment != "Bob's PC" || h.Role != "host" {
		t.Fatalf("comment/role mismatch: %+v", h)
	}
}

// TestDecodeNBFFrame_EndToEnd drives the full NBF receive path: build the exact frame
// sendNBF produces (LLC + NBF DATAGRAM_BROADCAST + browse mailslot) and decode it back.
func TestDecodeNBFFrame_EndToEnd(t *testing.T) {
	t.Parallel()
	ann := browserproto.Announcement{
		Op:             browserproto.OpLocalMasterAnnounce,
		ServerName:     "MASTER",
		OSVersionMajor: 5,
		OSVersionMinor: 0,
	}.Marshal()

	// Send-side encode, then receive-side decode — the two halves must round-trip.
	c := &Conn{proto: NBF, srcMAC: RandomMAC(), srcName: nb.NewName("CLIENT", NameTypeWorkstation)}
	captured := &captureLink{}
	c.fl = captured
	if err := c.SendMailslot(mailslotproto.NameBrowse, browseGroupName, ann, true); err != nil {
		t.Fatalf("SendMailslot: %v", err)
	}
	h := c.decodeFrame(captured.last)
	if h == nil {
		t.Fatal("decodeFrame returned nil for a self-sent NBF announcement")
	}
	if h.Name != "MASTER" || h.Role != "master" || h.OSVersion != "5.0" {
		t.Fatalf("host mismatch: %+v", h)
	}
}

// TestDecodeNBIPXFrame_EndToEnd drives the full NBIPX receive path: build the exact frame
// sendNBIPX produces (Ethernet II + IPX type-20 + NMPI MailslotSend + browse mailslot) and
// decode it back, confirming the IPX source net.node renders into the address.
func TestDecodeNBIPXFrame_EndToEnd(t *testing.T) {
	t.Parallel()
	ann := browserproto.Announcement{
		Op:             browserproto.OpHostAnnouncement,
		ServerName:     "NWBOX",
		OSVersionMajor: 6,
		OSVersionMinor: 22,
	}.Marshal()

	c := &Conn{proto: NBIPX, srcMAC: [6]byte{0x02, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE}, srcName: nb.NewName("CLIENT", NameTypeWorkstation)}
	captured := &captureLink{}
	c.fl = captured
	if err := c.SendMailslot(mailslotproto.NameBrowse, browseGroupName, ann, true); err != nil {
		t.Fatalf("SendMailslot: %v", err)
	}
	h := c.decodeFrame(captured.last)
	if h == nil {
		t.Fatal("decodeFrame returned nil for a self-sent NBIPX announcement")
	}
	if h.Name != "NWBOX" || h.Protocol != NBIPX || h.OSVersion != "6.22" {
		t.Fatalf("host mismatch: %+v", h)
	}
	// The address is the IPX source net.node; the source node is our station MAC.
	if want := "00000000.02:aa:bb:cc:dd:ee"; h.Address != want {
		t.Fatalf("address = %q, want %q", h.Address, want)
	}
}

func TestAnnouncementToHost_IgnoresNonBrowse(t *testing.T) {
	t.Parallel()
	other := mailslotproto.Write{Name: mailslotproto.NameMessenger, Body: []byte{0x01}}.Marshal()
	if h := announcementToHost(other, NBF, "x"); h != nil {
		t.Fatalf("expected nil for non-browse mailslot, got %+v", h)
	}
}

func TestMergeHost_KeepsRicherFields(t *testing.T) {
	t.Parallel()
	hosts := map[string]*Host{}
	mergeHost(hosts, &Host{Name: "PC", Protocol: NBF, Address: "m", OSVersion: "4.0", AppVersion: "3.10", Comment: "first", Role: "host"})
	// A later, sparser domain announcement must not wipe the version/comment.
	mergeHost(hosts, &Host{Name: "PC", Protocol: NBF, Address: "m", Role: "domain master"})
	got := hosts["PC"]
	if got.OSVersion != "4.0" || got.Comment != "first" {
		t.Fatalf("richer fields lost on merge: %+v", got)
	}
	if got.Role != "domain master" {
		t.Fatalf("role should upgrade to domain master: %+v", got)
	}
}

// captureLink is a core/link.FrameLink that records the last frame written, so a test can
// drive SendMailslot and inspect (or re-decode) the exact bytes put on the wire.
type captureLink struct{ last []byte }

func (l *captureLink) Write(frame corelink.Frame) error {
	l.last = append([]byte(nil), frame...)
	return nil
}
func (l *captureLink) Read() (corelink.Frame, error) { return nil, nil }
func (l *captureLink) Close() error                  { return nil }
