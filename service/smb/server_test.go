package smb

import (
	"context"
	"encoding/binary"
	"sync"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/pkg/vfs"
	netbiosproto "github.com/ObsoleteMadness/ClassicStack/protocol/netbios"
	"github.com/ObsoleteMadness/ClassicStack/service/netbios"
)

type fakeDatagramSender struct {
	mu        sync.Mutex
	datagrams []*netbiosproto.Datagram
	directed  []directedDatagram
}

type directedDatagram struct {
	remote   netbios.DatagramEndpoint
	datagram *netbiosproto.Datagram
}

func (f *fakeDatagramSender) SendDatagram(d *netbiosproto.Datagram) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.datagrams = append(f.datagrams, d)
	return nil
}

func (f *fakeDatagramSender) SendDirectedDatagram(d *netbiosproto.Datagram, remote netbios.DatagramEndpoint) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.directed = append(f.directed, directedDatagram{remote: remote, datagram: d})
	return nil
}

func TestServiceLifecycleSubscribesAndUnsubscribes(t *testing.T) {
	bus := vfs.NewBus(vfs.BusOptions{})

	svc := NewService(ServerOptions{Bus: bus}, nil, []ShareConfig{
		{Name: "Public", Path: "/tmp/pub", FSType: "local_fs"},
	})

	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Publishing should not panic and should reach our subscriber.
	bus.Publish(vfs.Event{Op: vfs.OpRename, HostPath: "/tmp/pub/a", OldPath: "/tmp/pub/b", Origin: "afp"})

	if err := svc.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Calling Stop again must be idempotent.
	if err := svc.Stop(); err != nil {
		t.Fatalf("Stop (second): %v", err)
	}
}

func TestServiceShortnameOptional(t *testing.T) {
	svc := NewService(ServerOptions{}, nil, nil)
	if svc.opts.Shortname != nil {
		t.Fatal("Shortname should be nil by default")
	}
}

func TestServiceStartSendsHostAnnouncement(t *testing.T) {
	sender := &fakeDatagramSender{}
	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, nil)
	svc.SetDatagramSender(sender)

	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer svc.Stop()

	sender.mu.Lock()
	defer sender.mu.Unlock()
	if len(sender.datagrams) != 1 {
		t.Fatalf("datagram count: got %d want 1", len(sender.datagrams))
	}
	if len(sender.directed) != 0 {
		t.Fatalf("directed datagram count: got %d want 0", len(sender.directed))
	}
	var hostFrame *hostAnnouncementFrame
	for _, got := range sender.datagrams {
		if got.Source.String() != "CLASSICSTACK" {
			t.Fatalf("source name: got %q want %q", got.Source.String(), "CLASSICSTACK")
		}
		if got.Destination.String() != "WORKGROUP" || got.Destination.Type() != browserNameTypeMasterBrowser {
			t.Fatalf("destination mismatch: got %q<%#x> want WORKGROUP<%#x>", got.Destination.String(), got.Destination.Type(), browserNameTypeMasterBrowser)
		}
		tx, err := unmarshalBrowserMailslotTransaction(got.Payload)
		if err != nil {
			t.Fatalf("unmarshalBrowserMailslotTransaction: %v", err)
		}
		if tx.MailslotName != browserMailslotBrowse {
			t.Fatalf("mailslot: got %q want %q", tx.MailslotName, browserMailslotBrowse)
		}
		if len(tx.BrowserPayload) == 0 {
			continue
		}
		switch tx.BrowserPayload[0] {
		case browserCommandHostAnnouncement:
			hostFrame, err = unmarshalHostAnnouncementFrame(tx.BrowserPayload)
			if err != nil {
				t.Fatalf("unmarshalHostAnnouncementFrame: %v", err)
			}
		}
	}
	if hostFrame == nil {
		t.Fatalf("missing host announcement frame")
	}
	frame := hostFrame
	if frame.ServerName != "CLASSICSTACK" {
		t.Fatalf("host name mismatch: got %q want CLASSICSTACK", frame.ServerName)
	}
	if frame.UpdateCount != 0x03 {
		t.Fatalf("update count: got %#x want 0x03", frame.UpdateCount)
	}
	if frame.PeriodicityMS != uint32(hostAnnouncementPeriod.Milliseconds()) {
		t.Fatalf("periodicity: got %d want %d", frame.PeriodicityMS, hostAnnouncementPeriod.Milliseconds())
	}
	if frame.BrowserVersionMajor != hostAnnouncementVersionMajor || frame.BrowserVersionMinor != hostAnnouncementVersionMinor {
		t.Fatalf("browser version: got %d.%d want %d.%d", frame.BrowserVersionMajor, frame.BrowserVersionMinor, hostAnnouncementVersionMajor, hostAnnouncementVersionMinor)
	}
}

func TestHandleDatagramObservedBackupIncludedInBackupList(t *testing.T) {
	sender := &fakeDatagramSender{}
	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, nil)
	svc.SetDatagramSender(sender)
	svc.browserRole = browserRoleLocalMaster

	host := hostAnnouncementFrame{
		UpdateCount:         0x03,
		PeriodicityMS:       uint32(hostAnnouncementPeriod / time.Millisecond),
		ServerName:          "BACKUP1",
		OSVersionMajor:      0x04,
		OSVersionMinor:      0x00,
		ServerType:          browserServerTypeWorkstationMask | browserServerTypeBackupMask,
		BrowserVersionMajor: hostAnnouncementVersionMajor,
		BrowserVersionMinor: hostAnnouncementVersionMinor,
		Signature:           browserSignature,
	}.MarshalBinary()
	announce := &netbiosproto.Datagram{
		Destination: netbiosproto.NewName("WORKGROUP", netbiosproto.NameTypeGroup),
		Source:      netbiosproto.NewName("BACKUP1", netbiosproto.NameTypeFileServer),
		Payload: browserMailslotTransaction{
			MailslotName:   browserMailslotBrowse,
			BrowserPayload: host,
			TimeoutMS:      1000,
			Priority:       0,
			Class:          2,
		}.MarshalBinary(),
	}
	if err := svc.HandleDatagram(announce); err != nil {
		t.Fatalf("HandleDatagram host announcement: %v", err)
	}

	request := &netbiosproto.Datagram{
		Destination: netbiosproto.NewName("WORKGROUP", netbiosproto.NameTypeGroup),
		Source:      netbiosproto.NewName("W98CLIENT", netbiosproto.NameTypeWorkstation),
		Payload:     makeBrowseRequestPayload(0x11223344),
	}
	if err := svc.HandleDatagram(request); err != nil {
		t.Fatalf("HandleDatagram backup list request: %v", err)
	}

	sender.mu.Lock()
	defer sender.mu.Unlock()
	if len(sender.datagrams) != 1 {
		t.Fatalf("datagram count: got %d want 1", len(sender.datagrams))
	}
	tx, err := unmarshalBrowserMailslotTransaction(sender.datagrams[0].Payload)
	if err != nil {
		t.Fatalf("unmarshalBrowserMailslotTransaction: %v", err)
	}
	resp, err := unmarshalGetBackupListResponseFrame(tx.BrowserPayload)
	if err != nil {
		t.Fatalf("unmarshalGetBackupListResponseFrame: %v", err)
	}
	if len(resp.BackupServers) != 2 {
		t.Fatalf("backup server count: got %d want 2", len(resp.BackupServers))
	}
	if resp.BackupServers[0] != "CLASSICSTACK" || resp.BackupServers[1] != "BACKUP1" {
		t.Fatalf("backup list mismatch: got %v want [CLASSICSTACK BACKUP1]", resp.BackupServers)
	}
}

func TestHandleDatagramGetBackupListRequestSendsResponse(t *testing.T) {
	sender := &fakeDatagramSender{}
	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, nil)
	svc.SetDatagramSender(sender)
	svc.browserRole = browserRoleLocalMaster

	in := &netbiosproto.Datagram{
		Destination: netbiosproto.NewName("WORKGROUP", netbiosproto.NameTypeGroup),
		Source:      netbiosproto.NewName("W98CLIENT", netbiosproto.NameTypeWorkstation),
		Payload:     makeBrowseRequestPayload(0x11223344),
	}
	if err := svc.HandleDatagram(in); err != nil {
		t.Fatalf("HandleDatagram: %v", err)
	}

	sender.mu.Lock()
	defer sender.mu.Unlock()
	if len(sender.datagrams) != 1 {
		t.Fatalf("datagram count: got %d want 1", len(sender.datagrams))
	}
	if len(sender.directed) != 0 {
		t.Fatalf("directed datagram count: got %d want 0", len(sender.directed))
	}
	got := sender.datagrams[0]
	if got.Destination != in.Source {
		t.Fatalf("destination mismatch")
	}
	if got.Source.String() != "CLASSICSTACK" || got.Source.Type() != netbiosproto.NameTypeFileServer {
		t.Fatalf("source name/type: got %q<%#x> want CLASSICSTACK<%#x>", got.Source.String(), got.Source.Type(), netbiosproto.NameTypeFileServer)
	}
	tx, err := unmarshalBrowserMailslotTransaction(got.Payload)
	if err != nil {
		t.Fatalf("unmarshalBrowserMailslotTransaction: %v", err)
	}
	frame, err := unmarshalGetBackupListResponseFrame(tx.BrowserPayload)
	if err != nil {
		t.Fatalf("unmarshalGetBackupListResponseFrame: %v", err)
	}
	if len(frame.BackupServers) != 1 {
		t.Fatalf("backup server count: got %d want 1", len(frame.BackupServers))
	}
	if frame.Token != 0x11223344 {
		t.Fatalf("token mismatch: got %#x want %#x", frame.Token, uint32(0x11223344))
	}
	if frame.BackupServers[0] != "CLASSICSTACK" {
		t.Fatalf("backup server mismatch: got %q want CLASSICSTACK", frame.BackupServers[0])
	}
}

func TestHandleDatagramGetBackupListRequestToMasterBrowserUsesMasterSource(t *testing.T) {
	sender := &fakeDatagramSender{}
	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, nil)
	svc.SetDatagramSender(sender)
	svc.browserRole = browserRoleLocalMaster

	in := &netbiosproto.Datagram{
		Destination: netbiosproto.NewName("WORKGROUP", browserNameTypeMasterBrowser),
		Source:      netbiosproto.NewName("W98CLIENT", netbiosproto.NameTypeWorkstation),
		Payload:     makeBrowseRequestPayload(0x55667788),
	}
	if err := svc.HandleDatagram(in); err != nil {
		t.Fatalf("HandleDatagram: %v", err)
	}

	sender.mu.Lock()
	defer sender.mu.Unlock()
	if len(sender.datagrams) != 1 {
		t.Fatalf("datagram count: got %d want 1", len(sender.datagrams))
	}
	got := sender.datagrams[0]
	if got.Source.String() != "WORKGROUP" || got.Source.Type() != browserNameTypeMasterBrowser {
		t.Fatalf("source name/type: got %q<%#x> want WORKGROUP<%#x>", got.Source.String(), got.Source.Type(), browserNameTypeMasterBrowser)
	}
	tx, err := unmarshalBrowserMailslotTransaction(got.Payload)
	if err != nil {
		t.Fatalf("unmarshalBrowserMailslotTransaction: %v", err)
	}
	frame, err := unmarshalGetBackupListResponseFrame(tx.BrowserPayload)
	if err != nil {
		t.Fatalf("unmarshalGetBackupListResponseFrame: %v", err)
	}
	if frame.Token != 0x55667788 {
		t.Fatalf("token mismatch: got %#x want %#x", frame.Token, uint32(0x55667788))
	}
}

func TestHandleDatagramElectionRequestSendsParticipation(t *testing.T) {
	sender := &fakeDatagramSender{}
	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, nil)
	svc.SetDatagramSender(sender)
	svc.electionDelay = func(browserRole) time.Duration { return 20 * time.Millisecond }
	defer svc.Stop()

	in := &netbiosproto.Datagram{
		Destination: netbiosproto.NewName("WORKGROUP", netbiosproto.NameTypeGroup),
		Source:      netbiosproto.NewName("W98CLIENT", netbiosproto.NameTypeWorkstation),
		Payload:     makeElectionRequestPayload(),
	}
	if err := svc.HandleDatagram(in); err != nil {
		t.Fatalf("HandleDatagram: %v", err)
	}

	sender.mu.Lock()
	defer sender.mu.Unlock()
	if len(sender.datagrams) != 1 {
		t.Fatalf("datagram count: got %d want 1", len(sender.datagrams))
	}
	if len(sender.directed) != 0 {
		t.Fatalf("directed datagram count: got %d want 0", len(sender.directed))
	}
	got := sender.datagrams[0]
	if got.Destination != in.Destination {
		t.Fatalf("destination mismatch")
	}
	if got.Source.String() != "CLASSICSTACK" {
		t.Fatalf("source name: got %q want CLASSICSTACK", got.Source.String())
	}
	tx, err := unmarshalBrowserMailslotTransaction(got.Payload)
	if err != nil {
		t.Fatalf("unmarshalBrowserMailslotTransaction: %v", err)
	}
	frame, err := unmarshalRequestElectionFrame(tx.BrowserPayload)
	if err != nil {
		t.Fatalf("unmarshalRequestElectionFrame: %v", err)
	}
	if frame.Version != browserVersionElection {
		t.Fatalf("election version: got %d want %d", frame.Version, browserVersionElection)
	}
	if frame.Criteria != browserElectionCriteriaMasterMask {
		t.Fatalf("criteria mismatch: got %#x want %#x", frame.Criteria, uint32(browserElectionCriteriaMasterMask))
	}
	if frame.Uptime != 1 {
		t.Fatalf("uptime mismatch: got %d want 1", frame.Uptime)
	}
	if frame.Reserved != 0 {
		t.Fatalf("reserved election field: got %#x want 0", frame.Reserved)
	}
	if frame.ServerName != "CLASSICSTACK" {
		t.Fatalf("server name mismatch: got %q want CLASSICSTACK", frame.ServerName)
	}
}

func TestHandleDatagramElectionRequestWinsAfterFourTransmissions(t *testing.T) {
	sender := &fakeDatagramSender{}
	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, nil)
	svc.SetDatagramSender(sender)
	svc.electionDelay = func(browserRole) time.Duration { return 2 * time.Millisecond }
	defer svc.Stop()

	in := &netbiosproto.Datagram{
		Destination: netbiosproto.NewName("WORKGROUP", netbiosproto.NameTypeGroup),
		Source:      netbiosproto.NewName("LOWNODE", netbiosproto.NameTypeWorkstation),
		Payload:     makeElectionRequestPayloadWith(0x00000001, 1, "LOWNODE"),
	}
	if err := svc.HandleDatagram(in); err != nil {
		t.Fatalf("HandleDatagram: %v", err)
	}

	deadline := time.Now().Add(300 * time.Millisecond)
	for {
		sender.mu.Lock()
		count := len(sender.datagrams)
		sender.mu.Unlock()
		if count >= 5 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for election sequence; datagrams=%d", count)
		}
		time.Sleep(2 * time.Millisecond)
	}

	sender.mu.Lock()
	defer sender.mu.Unlock()
	if len(sender.datagrams) < 5 {
		t.Fatalf("datagram count: got %d want at least 5", len(sender.datagrams))
	}
	for i := 0; i < 4; i++ {
		tx, err := unmarshalBrowserMailslotTransaction(sender.datagrams[i].Payload)
		if err != nil {
			t.Fatalf("unmarshalBrowserMailslotTransaction[%d]: %v", i, err)
		}
		frame, err := unmarshalRequestElectionFrame(tx.BrowserPayload)
		if err != nil {
			t.Fatalf("unmarshalRequestElectionFrame[%d]: %v", i, err)
		}
		if frame.ServerName != "CLASSICSTACK" {
			t.Fatalf("election frame server name[%d]: got %q want CLASSICSTACK", i, frame.ServerName)
		}
	}
	lastTx, err := unmarshalBrowserMailslotTransaction(sender.datagrams[len(sender.datagrams)-1].Payload)
	if err != nil {
		t.Fatalf("unmarshalBrowserMailslotTransaction[last]: %v", err)
	}
	if len(lastTx.BrowserPayload) == 0 || lastTx.BrowserPayload[0] != browserCommandLocalMasterAnnounce {
		t.Fatalf("last browser frame command: got %#x want %#x", lastTx.BrowserPayload[0], browserCommandLocalMasterAnnounce)
	}
}

func TestHandleDatagramElectionRequestLoseStopsElection(t *testing.T) {
	sender := &fakeDatagramSender{}
	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, nil)
	svc.SetDatagramSender(sender)
	svc.electionDelay = func(browserRole) time.Duration { return 40 * time.Millisecond }
	defer svc.Stop()

	start := &netbiosproto.Datagram{
		Destination: netbiosproto.NewName("WORKGROUP", netbiosproto.NameTypeGroup),
		Source:      netbiosproto.NewName("LOWNODE", netbiosproto.NameTypeWorkstation),
		Payload:     makeElectionRequestPayloadWith(0x00000001, 1, "LOWNODE"),
	}
	if err := svc.HandleDatagram(start); err != nil {
		t.Fatalf("HandleDatagram start: %v", err)
	}

	lose := &netbiosproto.Datagram{
		Destination: netbiosproto.NewName("WORKGROUP", netbiosproto.NameTypeGroup),
		Source:      netbiosproto.NewName("WINNER", netbiosproto.NameTypeWorkstation),
		Payload:     makeElectionRequestPayloadWith(0x00000008, 2, "WINNER"),
	}
	if err := svc.HandleDatagram(lose); err != nil {
		t.Fatalf("HandleDatagram lose: %v", err)
	}

	time.Sleep(120 * time.Millisecond)

	sender.mu.Lock()
	defer sender.mu.Unlock()
	if len(sender.datagrams) != 1 {
		t.Fatalf("datagram count after losing election: got %d want 1", len(sender.datagrams))
	}
	tx, err := unmarshalBrowserMailslotTransaction(sender.datagrams[0].Payload)
	if err != nil {
		t.Fatalf("unmarshalBrowserMailslotTransaction: %v", err)
	}
	if len(tx.BrowserPayload) == 0 || tx.BrowserPayload[0] != browserCommandRequestElection {
		t.Fatalf("first browser frame command: got %#x want %#x", tx.BrowserPayload[0], browserCommandRequestElection)
	}
}

func TestHandleDatagramContextGetBackupListRequestSendsDirectedResponse(t *testing.T) {
	sender := &fakeDatagramSender{}
	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, nil)
	svc.SetDatagramSender(sender)
	svc.browserRole = browserRoleLocalMaster

	in := &netbiosproto.Datagram{
		Destination: netbiosproto.NewName("WORKGROUP", netbiosproto.NameTypeGroup),
		Source:      netbiosproto.NewName("W98CLIENT", netbiosproto.NameTypeWorkstation),
		Payload:     makeBrowseRequestPayload(0x11223344),
	}
	ctx := netbios.DatagramContext{
		Remote: netbios.DatagramEndpoint{
			Network: [4]byte{0, 0, 0, 0},
			Node:    [6]byte{0x08, 0x00, 0x27, 0x14, 0x74, 0x6D},
			Socket:  [2]byte{0x05, 0x53},
		},
	}
	if err := svc.HandleDatagramContext(in, ctx); err != nil {
		t.Fatalf("HandleDatagramContext: %v", err)
	}

	sender.mu.Lock()
	defer sender.mu.Unlock()
	if len(sender.datagrams) != 0 {
		t.Fatalf("broadcast datagram count: got %d want 0", len(sender.datagrams))
	}
	if len(sender.directed) != 1 {
		t.Fatalf("directed datagram count: got %d want 1", len(sender.directed))
	}
	got := sender.directed[0]
	if got.remote != ctx.Remote {
		t.Fatalf("remote endpoint mismatch")
	}
	if got.datagram.Source.String() != "CLASSICSTACK" || got.datagram.Source.Type() != netbiosproto.NameTypeFileServer {
		t.Fatalf("source name/type: got %q<%#x> want CLASSICSTACK<%#x>", got.datagram.Source.String(), got.datagram.Source.Type(), netbiosproto.NameTypeFileServer)
	}
	tx, err := unmarshalBrowserMailslotTransaction(got.datagram.Payload)
	if err != nil {
		t.Fatalf("unmarshalBrowserMailslotTransaction: %v", err)
	}
	frame, err := unmarshalGetBackupListResponseFrame(tx.BrowserPayload)
	if err != nil {
		t.Fatalf("unmarshalGetBackupListResponseFrame: %v", err)
	}
	if len(frame.BackupServers) != 1 || frame.BackupServers[0] != "CLASSICSTACK" {
		t.Fatalf("server name mismatch: got %v want [CLASSICSTACK]", frame.BackupServers)
	}
}

func TestHandleDatagramLegacyGetBackupListRequestPreamble(t *testing.T) {
	sender := &fakeDatagramSender{}
	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, nil)
	svc.SetDatagramSender(sender)
	svc.browserRole = browserRoleLocalMaster

	legacyPayload := append([]byte{0x01, 0x03}, getBackupListRequestFrame{RequestedCount: 2, Token: 0x11223344}.MarshalBinary()...)
	in := &netbiosproto.Datagram{
		Destination: netbiosproto.NewName("WORKGROUP", netbiosproto.NameTypeGroup),
		Source:      netbiosproto.NewName("W98CLIENT", netbiosproto.NameTypeWorkstation),
		Payload: browserMailslotTransaction{
			MailslotName:   browserMailslotBrowse,
			BrowserPayload: legacyPayload,
			Flags:          2,
			TimeoutMS:      1000,
			Priority:       0,
			Class:          2,
		}.MarshalBinary(),
	}
	if err := svc.HandleDatagram(in); err != nil {
		t.Fatalf("HandleDatagram: %v", err)
	}

	sender.mu.Lock()
	defer sender.mu.Unlock()
	if len(sender.datagrams) != 1 {
		t.Fatalf("datagram count: got %d want 1", len(sender.datagrams))
	}
	tx, err := unmarshalBrowserMailslotTransaction(sender.datagrams[0].Payload)
	if err != nil {
		t.Fatalf("unmarshalBrowserMailslotTransaction: %v", err)
	}
	frame, err := unmarshalGetBackupListResponseFrame(tx.BrowserPayload)
	if err != nil {
		t.Fatalf("unmarshalGetBackupListResponseFrame: %v", err)
	}
	if frame.Token != 0x11223344 {
		t.Fatalf("token mismatch: got %#x want %#x", frame.Token, uint32(0x11223344))
	}
}

func TestHandleDatagramGetBackupListIgnoredWhenNotLocalMaster(t *testing.T) {
	sender := &fakeDatagramSender{}
	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, nil)
	svc.SetDatagramSender(sender)
	svc.browserRole = browserRolePotential

	in := &netbiosproto.Datagram{
		Destination: netbiosproto.NewName("WORKGROUP", netbiosproto.NameTypeGroup),
		Source:      netbiosproto.NewName("W98CLIENT", netbiosproto.NameTypeWorkstation),
		Payload:     makeBrowseRequestPayload(0x11223344),
	}
	if err := svc.HandleDatagram(in); err != nil {
		t.Fatalf("HandleDatagram: %v", err)
	}

	sender.mu.Lock()
	defer sender.mu.Unlock()
	if len(sender.datagrams) != 0 || len(sender.directed) != 0 {
		t.Fatalf("expected no response while not local master; got datagrams=%d directed=%d", len(sender.datagrams), len(sender.directed))
	}
}

func TestHandleDatagramLegacyGetBackupListRequestPreambleWithPadding(t *testing.T) {
	sender := &fakeDatagramSender{}
	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, nil)
	svc.SetDatagramSender(sender)
	svc.browserRole = browserRoleLocalMaster

	padded := append(getBackupListRequestFrame{RequestedCount: 2, Token: 0xA1B2C3D4}.MarshalBinary(), 0x00)
	legacyPayload := append([]byte{0x01, 0x03}, padded...)
	in := &netbiosproto.Datagram{
		Destination: netbiosproto.NewName("WORKGROUP", netbiosproto.NameTypeGroup),
		Source:      netbiosproto.NewName("W98CLIENT", netbiosproto.NameTypeWorkstation),
		Payload: browserMailslotTransaction{
			MailslotName:   browserMailslotBrowse,
			BrowserPayload: legacyPayload,
			Flags:          2,
			TimeoutMS:      1000,
			Priority:       0,
			Class:          2,
		}.MarshalBinary(),
	}
	if err := svc.HandleDatagram(in); err != nil {
		t.Fatalf("HandleDatagram: %v", err)
	}

	sender.mu.Lock()
	defer sender.mu.Unlock()
	if len(sender.datagrams) != 1 {
		t.Fatalf("datagram count: got %d want 1", len(sender.datagrams))
	}
	tx, err := unmarshalBrowserMailslotTransaction(sender.datagrams[0].Payload)
	if err != nil {
		t.Fatalf("unmarshalBrowserMailslotTransaction: %v", err)
	}
	frame, err := unmarshalGetBackupListResponseFrame(tx.BrowserPayload)
	if err != nil {
		t.Fatalf("unmarshalGetBackupListResponseFrame: %v", err)
	}
	if frame.Token != 0xA1B2C3D4 {
		t.Fatalf("token mismatch: got %#x want %#x", frame.Token, uint32(0xA1B2C3D4))
	}
}

func TestHandleDatagramAnnouncementRequestSendsHostAnnouncement(t *testing.T) {
	sender := &fakeDatagramSender{}
	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, nil)
	svc.SetDatagramSender(sender)

	request := append([]byte{browserCommandAnnouncementReq, 0x00}, []byte("W98CLIENT\x00")...)
	in := &netbiosproto.Datagram{
		Destination: netbiosproto.NewName("WORKGROUP", netbiosproto.NameTypeGroup),
		Source:      netbiosproto.NewName("W98CLIENT", netbiosproto.NameTypeWorkstation),
		Payload: browserMailslotTransaction{
			MailslotName:   browserMailslotBrowse,
			BrowserPayload: request,
			Flags:          2,
			TimeoutMS:      1000,
			Priority:       0,
			Class:          2,
		}.MarshalBinary(),
	}
	if err := svc.HandleDatagram(in); err != nil {
		t.Fatalf("HandleDatagram: %v", err)
	}

	sender.mu.Lock()
	defer sender.mu.Unlock()
	if len(sender.datagrams) != 1 {
		t.Fatalf("datagram count: got %d want 1", len(sender.datagrams))
	}
	tx, err := unmarshalBrowserMailslotTransaction(sender.datagrams[0].Payload)
	if err != nil {
		t.Fatalf("unmarshalBrowserMailslotTransaction: %v", err)
	}
	frame, err := unmarshalHostAnnouncementFrame(tx.BrowserPayload)
	if err != nil {
		t.Fatalf("unmarshalHostAnnouncementFrame: %v", err)
	}
	if frame.ServerName != "CLASSICSTACK" {
		t.Fatalf("server name mismatch: got %q want CLASSICSTACK", frame.ServerName)
	}
}

func TestBrowserFrameRoundTrips(t *testing.T) {
	hostWire := hostAnnouncementFrame{
		UpdateCount:         0,
		PeriodicityMS:       120000,
		ServerName:          "ClassicStack",
		OSVersionMajor:      4,
		OSVersionMinor:      0,
		ServerType:          browserServerTypeWorkstationMask,
		BrowserVersionMajor: browserVersionMajor,
		BrowserVersionMinor: browserVersionMinor,
		Signature:           browserSignature,
	}.MarshalBinary()
	host, err := unmarshalHostAnnouncementFrame(hostWire)
	if err != nil {
		t.Fatalf("unmarshalHostAnnouncementFrame: %v", err)
	}
	if host.ServerName != "CLASSICSTACK" {
		t.Fatalf("host round-trip server: got %q want CLASSICSTACK", host.ServerName)
	}

	localWire := localMasterAnnouncementFrame{
		UpdateCount:               0,
		PeriodicityMS:             120000,
		ServerName:                "ClassicStack",
		OSVersionMajor:            4,
		OSVersionMinor:            0,
		ServerType:                browserServerTypeWorkstationMask | browserServerTypeMasterMask,
		BrowserConfigVersionMajor: browserVersionMajor,
		BrowserConfigVersionMinor: browserVersionMinor,
		Signature:                 browserSignature,
	}.MarshalBinary()
	local, err := unmarshalLocalMasterAnnouncementFrame(localWire)
	if err != nil {
		t.Fatalf("unmarshalLocalMasterAnnouncementFrame: %v", err)
	}
	if local.ServerName != "CLASSICSTACK" {
		t.Fatalf("local master round-trip server: got %q want CLASSICSTACK", local.ServerName)
	}

	electionWire := requestElectionFrame{
		Version:    browserVersionElection,
		Criteria:   browserElectionCriteriaMasterMask,
		Uptime:     1,
		Reserved:   0,
		ServerName: "ClassicStack",
	}.MarshalBinary()
	election, err := unmarshalRequestElectionFrame(electionWire)
	if err != nil {
		t.Fatalf("unmarshalRequestElectionFrame: %v", err)
	}
	if election.ServerName != "CLASSICSTACK" {
		t.Fatalf("election round-trip server: got %q want CLASSICSTACK", election.ServerName)
	}

	responseWire := getBackupListResponseFrame{
		Token:         0x11223344,
		BackupServers: []string{"ClassicStack"},
	}.MarshalBinary()
	response, err := unmarshalGetBackupListResponseFrame(responseWire)
	if err != nil {
		t.Fatalf("unmarshalGetBackupListResponseFrame: %v", err)
	}
	if len(response.BackupServers) != 1 || response.BackupServers[0] != "CLASSICSTACK" {
		t.Fatalf("backup response round-trip: got %v want [CLASSICSTACK]", response.BackupServers)
	}
}

func TestBrowserMailslotTransactionRoundTrip(t *testing.T) {
	txWire := browserMailslotTransaction{
		MailslotName:   browserMailslotBrowse,
		BrowserPayload: getBackupListRequestFrame{RequestedCount: 2, Token: 0x11223344}.MarshalBinary(),
		Flags:          2,
		TimeoutMS:      1000,
		Priority:       0,
		Class:          2,
	}.MarshalBinary()
	tx, err := unmarshalBrowserMailslotTransaction(txWire)
	if err != nil {
		t.Fatalf("unmarshalBrowserMailslotTransaction: %v", err)
	}
	if tx.MailslotName != browserMailslotBrowse {
		t.Fatalf("mailslot mismatch: got %q want %q", tx.MailslotName, browserMailslotBrowse)
	}
	request, err := unmarshalGetBackupListRequestFrame(tx.BrowserPayload)
	if err != nil {
		t.Fatalf("unmarshalGetBackupListRequestFrame: %v", err)
	}
	if request.Token != 0x11223344 || request.RequestedCount != 2 {
		t.Fatalf("request mismatch: got count=%d token=%#x", request.RequestedCount, request.Token)
	}
}

func TestHandleSessionContextLANMANTransactionReturnsResponse(t *testing.T) {
	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, nil)
	packet := &netbiosproto.SessionPacket{
		Type:    netbiosproto.SessionMessage,
		Payload: makeLANMANTransactionSessionPayload(),
	}
	resp, err := svc.HandleSessionContext(packet, netbios.SessionContext{})
	if err != nil {
		t.Fatalf("HandleSessionContext: %v", err)
	}
	if resp == nil {
		t.Fatalf("expected response packet")
	}
	if resp.Type != netbiosproto.SessionMessage {
		t.Fatalf("response type: got %#x want %#x", resp.Type, netbiosproto.SessionMessage)
	}
	if len(resp.Payload) < 33 || string(resp.Payload[0:4]) != "\xffSMB" {
		t.Fatalf("invalid SMB response payload")
	}
	if resp.Payload[4] != CommandTransaction {
		t.Fatalf("response command: got %#x want %#x", resp.Payload[4], CommandTransaction)
	}
}

func makeBrowseRequestPayload(token uint32) []byte {
	return browserMailslotTransaction{
		MailslotName:   browserMailslotBrowse,
		BrowserPayload: getBackupListRequestFrame{RequestedCount: 2, Token: token}.MarshalBinary(),
		Flags:          2,
		TimeoutMS:      1000,
		Priority:       0,
		Class:          2,
	}.MarshalBinary()
}

func makeElectionRequestPayload() []byte {
	return browserMailslotTransaction{
		MailslotName: browserMailslotBrowse,
		BrowserPayload: requestElectionFrame{
			Version:    browserVersionElection,
			Criteria:   0,
			Uptime:     0,
			Reserved:   0,
			ServerName: "W98CLIENT",
		}.MarshalBinary(),
		Flags:     2,
		TimeoutMS: 1000,
		Priority:  0,
		Class:     2,
	}.MarshalBinary()
}

func makeElectionRequestPayloadWith(criteria, uptime uint32, server string) []byte {
	return browserMailslotTransaction{
		MailslotName: browserMailslotBrowse,
		BrowserPayload: requestElectionFrame{
			Version:    browserVersionElection,
			Criteria:   criteria,
			Uptime:     uptime,
			Reserved:   0,
			ServerName: server,
		}.MarshalBinary(),
		Flags:     2,
		TimeoutMS: 1000,
		Priority:  0,
		Class:     2,
	}.MarshalBinary()
}

func makeLANMANTransactionSessionPayload() []byte {
	pipe := []byte("\\PIPE\\LANMAN\x00")
	out := make([]byte, 69+len(pipe))
	copy(out[0:4], []byte{0xff, 'S', 'M', 'B'})
	out[4] = CommandTransaction
	out[32] = 17
	binary.LittleEndian.PutUint16(out[67:69], uint16(len(pipe)))
	copy(out[69:], pipe)
	return out
}

func makeNegotiatePayload() []byte {
	dialects := []byte("\x02PC NETWORK PROGRAM 1.0\x00\x02LANMAN1.0\x00\x02NT LM 0.12\x00")
	out := make([]byte, smbHeaderLen+1+2+len(dialects))
	copy(out[0:4], []byte{0xff, 'S', 'M', 'B'})
	out[4] = CommandNegotiate
	// WCT = 0
	binary.LittleEndian.PutUint16(out[smbHeaderLen+1:smbHeaderLen+3], uint16(len(dialects)))
	copy(out[smbHeaderLen+3:], dialects)
	return out
}

func makeSessionSetupPayload() []byte {
	out := make([]byte, smbHeaderLen+1+2)
	copy(out[0:4], []byte{0xff, 'S', 'M', 'B'})
	out[4] = CommandSessionSetupAndX
	return out
}

func makeTreeConnectPayload() []byte {
	out := make([]byte, smbHeaderLen+1+2)
	copy(out[0:4], []byte{0xff, 'S', 'M', 'B'})
	out[4] = CommandTreeConnectAndX
	return out
}

func makeNetServerEnum2Payload() []byte {
	// bytes area: \PIPE\LANMAN\0 (13 bytes) + FunctionCode 0x0068 (2 bytes)
	bytesArea := append([]byte("\\PIPE\\LANMAN\x00"), 0x68, 0x00)
	out := make([]byte, 69+len(bytesArea))
	copy(out[0:4], []byte{0xff, 'S', 'M', 'B'})
	out[4] = CommandTransaction
	out[32] = 17
	binary.LittleEndian.PutUint16(out[67:69], uint16(len(bytesArea)))
	copy(out[69:], bytesArea)
	return out
}

func makeNetServerEnum2DomainEnumPayload() []byte {
	bytesArea := []byte("\\PIPE\\LANMAN\x00")
	bytesArea = append(bytesArea, 0x68, 0x00)                 // FunctionCode
	bytesArea = append(bytesArea, []byte("WrLehDz\x00")...)   // ParamDesc
	bytesArea = append(bytesArea, []byte("B16BBDz\x00")...)   // DataDesc
	bytesArea = append(bytesArea, 0xff, 0xff)                 // ReceiveBufferLength
	bytesArea = append(bytesArea, 0x00, 0x00, 0x00, 0x80)     // ServerType = SV_TYPE_DOMAIN_ENUM
	bytesArea = append(bytesArea, []byte("WORKGROUP\x00")...) // Domain

	out := make([]byte, 69+len(bytesArea))
	copy(out[0:4], []byte{0xff, 'S', 'M', 'B'})
	out[4] = CommandTransaction
	out[32] = 17
	binary.LittleEndian.PutUint16(out[67:69], uint16(len(bytesArea)))
	copy(out[69:], bytesArea)
	return out
}

func TestHandleSessionContextEchoCountZeroReturnsNoResponse(t *testing.T) {
	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, nil)
	packet := &netbiosproto.SessionPacket{
		Type:    netbiosproto.SessionMessage,
		Payload: makeEchoPayloadWith(0x0001, 0),
	}
	resp, err := svc.HandleSessionContext(packet, netbios.SessionContext{})
	if err != nil {
		t.Fatalf("HandleSessionContext: %v", err)
	}
	if resp != nil {
		t.Fatalf("expected nil response for EchoCount=0")
	}
}

func TestHandleSessionContextEchoInvalidTIDReturnsBadTID(t *testing.T) {
	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, nil)
	packet := &netbiosproto.SessionPacket{
		Type:    netbiosproto.SessionMessage,
		Payload: makeEchoPayloadWith(0x0002, 1),
	}
	resp, err := svc.HandleSessionContext(packet, netbios.SessionContext{})
	if err != nil {
		t.Fatalf("HandleSessionContext: %v", err)
	}
	if resp == nil || len(resp.Payload) < smbHeaderLen {
		t.Fatalf("expected SMB error response")
	}
	status := binary.LittleEndian.Uint32(resp.Payload[smbOffStatus : smbOffStatus+4])
	if status != smbStatusBadTID {
		t.Fatalf("status mismatch: got %#x want %#x", status, uint32(smbStatusBadTID))
	}
}

func makeEchoPayload(data []byte) []byte {
	out := make([]byte, smbHeaderLen+1+2+2+len(data))
	copy(out[0:4], []byte{0xff, 'S', 'M', 'B'})
	out[4] = CommandEcho
	binary.LittleEndian.PutUint16(out[smbOffTID:smbOffTID+2], 1)
	out[smbHeaderLen] = 1 // WCT
	binary.LittleEndian.PutUint16(out[smbHeaderLen+1:smbHeaderLen+3], 1)
	binary.LittleEndian.PutUint16(out[smbHeaderLen+3:smbHeaderLen+5], uint16(len(data)))
	copy(out[smbHeaderLen+5:], data)
	return out
}

func makeEchoPayloadWith(tid uint16, echoCount uint16) []byte {
	data := []byte("echo")
	out := make([]byte, smbHeaderLen+1+2+2+len(data))
	copy(out[0:4], []byte{0xff, 'S', 'M', 'B'})
	out[4] = CommandEcho
	binary.LittleEndian.PutUint16(out[smbOffTID:smbOffTID+2], tid)
	out[smbHeaderLen] = 1
	binary.LittleEndian.PutUint16(out[smbHeaderLen+1:smbHeaderLen+3], echoCount)
	binary.LittleEndian.PutUint16(out[smbHeaderLen+3:smbHeaderLen+5], uint16(len(data)))
	copy(out[smbHeaderLen+5:], data)
	return out
}

func makeTreeDisconnectPayload() []byte {
	out := make([]byte, smbHeaderLen+1+2)
	copy(out[0:4], []byte{0xff, 'S', 'M', 'B'})
	out[4] = CommandTreeDisconnect
	out[smbHeaderLen] = 0
	binary.LittleEndian.PutUint16(out[smbHeaderLen+1:smbHeaderLen+3], 0)
	return out
}

func TestHandleSessionContextNegotiateReturnsNTLMDialect(t *testing.T) {
	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, nil)
	packet := &netbiosproto.SessionPacket{
		Type:    netbiosproto.SessionMessage,
		Payload: makeNegotiatePayload(),
	}
	resp, err := svc.HandleSessionContext(packet, netbios.SessionContext{})
	if err != nil {
		t.Fatalf("HandleSessionContext: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Payload[4] != CommandNegotiate {
		t.Fatalf("cmd: got %#x want %#x", resp.Payload[4], CommandNegotiate)
	}
	if resp.Payload[smbHeaderLen] != 17 {
		t.Fatalf("WCT: got %d want 17", resp.Payload[smbHeaderLen])
	}
	if binary.LittleEndian.Uint32(resp.Payload[smbOffStatus:smbOffStatus+4]) != smbStatusSuccess {
		t.Fatalf("status: not success")
	}
	// Dialect index 2 = "NT LM 0.12" (third in the list)
	dialectIdx := binary.LittleEndian.Uint16(resp.Payload[smbHeaderLen+1 : smbHeaderLen+3])
	if dialectIdx != 2 {
		t.Fatalf("dialectIdx: got %d want 2", dialectIdx)
	}
}

func TestHandleSessionContextSessionSetupReturnsGuestLogon(t *testing.T) {
	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, nil)
	packet := &netbiosproto.SessionPacket{
		Type:    netbiosproto.SessionMessage,
		Payload: makeSessionSetupPayload(),
	}
	resp, err := svc.HandleSessionContext(packet, netbios.SessionContext{})
	if err != nil {
		t.Fatalf("HandleSessionContext: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Payload[4] != CommandSessionSetupAndX {
		t.Fatalf("cmd: got %#x want %#x", resp.Payload[4], CommandSessionSetupAndX)
	}
	if binary.LittleEndian.Uint32(resp.Payload[smbOffStatus:smbOffStatus+4]) != smbStatusSuccess {
		t.Fatalf("status: not success")
	}
	uid := binary.LittleEndian.Uint16(resp.Payload[smbOffUID : smbOffUID+2])
	if uid == 0 {
		t.Fatal("UID should be non-zero for guest session")
	}
	// Action word: smbHeaderLen+1 (WCT) + 4 bytes (AndXCommand/Rsv/Offset) = smbHeaderLen+5
	action := binary.LittleEndian.Uint16(resp.Payload[smbHeaderLen+5 : smbHeaderLen+7])
	if action&0x0001 == 0 {
		t.Fatal("expected guest logon action bit set")
	}
}

func TestHandleSessionContextTreeConnectReturnsIPC(t *testing.T) {
	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, nil)
	packet := &netbiosproto.SessionPacket{
		Type:    netbiosproto.SessionMessage,
		Payload: makeTreeConnectPayload(),
	}
	resp, err := svc.HandleSessionContext(packet, netbios.SessionContext{})
	if err != nil {
		t.Fatalf("HandleSessionContext: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Payload[4] != CommandTreeConnectAndX {
		t.Fatalf("cmd: got %#x want %#x", resp.Payload[4], CommandTreeConnectAndX)
	}
	if binary.LittleEndian.Uint32(resp.Payload[smbOffStatus:smbOffStatus+4]) != smbStatusSuccess {
		t.Fatalf("status: not success")
	}
	tid := binary.LittleEndian.Uint16(resp.Payload[smbOffTID : smbOffTID+2])
	if tid == 0 {
		t.Fatal("TID should be non-zero after tree connect")
	}
}

func TestHandleSessionContextNetServerEnum2ReturnsSelf(t *testing.T) {
	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, nil)
	svc.browserRole = browserRoleLocalMaster
	packet := &netbiosproto.SessionPacket{
		Type:    netbiosproto.SessionMessage,
		Payload: makeNetServerEnum2Payload(),
	}
	resp, err := svc.HandleSessionContext(packet, netbios.SessionContext{})
	if err != nil {
		t.Fatalf("HandleSessionContext: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Payload[4] != CommandTransaction {
		t.Fatalf("cmd: got %#x want %#x", resp.Payload[4], CommandTransaction)
	}
	if resp.Payload[smbHeaderLen] != 10 {
		t.Fatalf("WCT: got %d want 10", resp.Payload[smbHeaderLen])
	}
	// Extract ParameterOffset from word block.
	paramCount := int(binary.LittleEndian.Uint16(resp.Payload[smbHeaderLen+1+6 : smbHeaderLen+1+8]))
	paramOffset := int(binary.LittleEndian.Uint16(resp.Payload[smbHeaderLen+1+8 : smbHeaderLen+1+10]))
	if paramOffset+paramCount > len(resp.Payload) {
		t.Fatalf("param block out of bounds: count=%d offset=%d len=%d", paramCount, paramOffset, len(resp.Payload))
	}
	p := resp.Payload[paramOffset : paramOffset+paramCount]
	status := binary.LittleEndian.Uint16(p[0:2])
	if status != 0 {
		t.Fatalf("RAP status: got %d want 0", status)
	}
	entriesReturned := binary.LittleEndian.Uint16(p[4:6])
	if entriesReturned == 0 {
		t.Fatal("expected at least one server entry in NetServerEnum2 response")
	}
}

func TestHandleSessionContextEchoReturnsPayload(t *testing.T) {
	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, nil)
	payload := []byte("ping")
	packet := &netbiosproto.SessionPacket{
		Type:    netbiosproto.SessionMessage,
		Payload: makeEchoPayload(payload),
	}
	resp, err := svc.HandleSessionContext(packet, netbios.SessionContext{})
	if err != nil {
		t.Fatalf("HandleSessionContext: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Payload[4] != CommandEcho {
		t.Fatalf("cmd: got %#x want %#x", resp.Payload[4], CommandEcho)
	}
	if binary.LittleEndian.Uint32(resp.Payload[smbOffStatus:smbOffStatus+4]) != smbStatusSuccess {
		t.Fatalf("status: not success")
	}
	if resp.Payload[smbHeaderLen] != 1 {
		t.Fatalf("WCT: got %d want 1", resp.Payload[smbHeaderLen])
	}
	seq := binary.LittleEndian.Uint16(resp.Payload[smbHeaderLen+1 : smbHeaderLen+3])
	if seq != 1 {
		t.Fatalf("echo sequence: got %d want 1", seq)
	}
	bc := int(binary.LittleEndian.Uint16(resp.Payload[smbHeaderLen+3 : smbHeaderLen+5]))
	if bc != len(payload) {
		t.Fatalf("byte count: got %d want %d", bc, len(payload))
	}
	if string(resp.Payload[smbHeaderLen+5:smbHeaderLen+5+bc]) != string(payload) {
		t.Fatalf("echo payload mismatch")
	}
}

func TestHandleSessionContextNetServerEnum2DomainEnumReturnsWorkgroup(t *testing.T) {
	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, nil)
	svc.browserRole = browserRoleLocalMaster
	packet := &netbiosproto.SessionPacket{
		Type:    netbiosproto.SessionMessage,
		Payload: makeNetServerEnum2DomainEnumPayload(),
	}
	resp, err := svc.HandleSessionContext(packet, netbios.SessionContext{})
	if err != nil {
		t.Fatalf("HandleSessionContext: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Payload[4] != CommandTransaction {
		t.Fatalf("cmd: got %#x want %#x", resp.Payload[4], CommandTransaction)
	}
	if binary.LittleEndian.Uint32(resp.Payload[smbOffStatus:smbOffStatus+4]) != smbStatusSuccess {
		t.Fatalf("status: not success")
	}

	paramCount := int(binary.LittleEndian.Uint16(resp.Payload[smbHeaderLen+1+6 : smbHeaderLen+1+8]))
	paramOffset := int(binary.LittleEndian.Uint16(resp.Payload[smbHeaderLen+1+8 : smbHeaderLen+1+10]))
	dataCount := int(binary.LittleEndian.Uint16(resp.Payload[smbHeaderLen+1+12 : smbHeaderLen+1+14]))
	dataOffset := int(binary.LittleEndian.Uint16(resp.Payload[smbHeaderLen+1+14 : smbHeaderLen+1+16]))
	if paramOffset+paramCount > len(resp.Payload) || dataOffset+dataCount > len(resp.Payload) {
		t.Fatalf("response blocks out of bounds")
	}
	p := resp.Payload[paramOffset : paramOffset+paramCount]
	entriesReturned := binary.LittleEndian.Uint16(p[4:6])
	if entriesReturned != 1 {
		t.Fatalf("entries returned: got %d want 1", entriesReturned)
	}
	d := resp.Payload[dataOffset : dataOffset+dataCount]
	if string(d[0:9]) != "WORKGROUP" {
		t.Fatalf("domain entry name: got %q want %q", string(d[0:9]), "WORKGROUP")
	}
	serverType := binary.LittleEndian.Uint32(d[18:22])
	if serverType != browserServerTypeDomainEnumMask {
		t.Fatalf("domain entry type: got %#x want %#x", serverType, uint32(browserServerTypeDomainEnumMask))
	}
}

func TestHandleSessionContextNetServerEnum2PotentialBrowserReturnsReqNotAccepted(t *testing.T) {
	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, nil)
	// browserRole defaults to browserRolePotential — must refuse.
	packet := &netbiosproto.SessionPacket{
		Type:    netbiosproto.SessionMessage,
		Payload: makeNetServerEnum2Payload(),
	}
	resp, err := svc.HandleSessionContext(packet, netbios.SessionContext{})
	if err != nil {
		t.Fatalf("HandleSessionContext: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
	paramCount := int(binary.LittleEndian.Uint16(resp.Payload[smbHeaderLen+1+6 : smbHeaderLen+1+8]))
	paramOffset := int(binary.LittleEndian.Uint16(resp.Payload[smbHeaderLen+1+8 : smbHeaderLen+1+10]))
	if paramOffset+paramCount > len(resp.Payload) {
		t.Fatalf("param block out of bounds")
	}
	rapStatus := binary.LittleEndian.Uint16(resp.Payload[paramOffset : paramOffset+2])
	if rapStatus != uint16(rapStatusErrReqNotAccepted) {
		t.Fatalf("RAP status: got %d want %d (ERROR_REQ_NOT_ACCEP)", rapStatus, rapStatusErrReqNotAccepted)
	}
}

func TestHandleSessionContextNetServerEnum2DomainEnumPlusOtherBitsReturnsInvalidFunction(t *testing.T) {
	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, nil)
	svc.browserRole = browserRoleLocalMaster
	// SV_TYPE_DOMAIN_ENUM | SV_TYPE_WORKSTATION — invalid combination per spec §3.3.5.6.
	mixedType := uint32(browserServerTypeDomainEnumMask | 0x01)
	payload := makeNetServerEnum2PayloadWithServerType(mixedType)
	packet := &netbiosproto.SessionPacket{Type: netbiosproto.SessionMessage, Payload: payload}
	resp, err := svc.HandleSessionContext(packet, netbios.SessionContext{})
	if err != nil {
		t.Fatalf("HandleSessionContext: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
	paramCount := int(binary.LittleEndian.Uint16(resp.Payload[smbHeaderLen+1+6 : smbHeaderLen+1+8]))
	paramOffset := int(binary.LittleEndian.Uint16(resp.Payload[smbHeaderLen+1+8 : smbHeaderLen+1+10]))
	if paramOffset+paramCount > len(resp.Payload) {
		t.Fatalf("param block out of bounds")
	}
	rapStatus := binary.LittleEndian.Uint16(resp.Payload[paramOffset : paramOffset+2])
	if rapStatus != uint16(rapStatusErrInvalidFunction) {
		t.Fatalf("RAP status: got %d want %d (ERROR_INVALID_FUNCTION)", rapStatus, rapStatusErrInvalidFunction)
	}
}

func TestHandleSessionContextNetServerEnum2DomainEnumTracksObservedGroups(t *testing.T) {
	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, nil)
	svc.browserRole = browserRoleLocalMaster
	// Simulate having observed a DomainAnnouncement from another workgroup.
	svc.noteMachineGroup("OTHERGROUP", "OTHERMASTER")
	packet := &netbiosproto.SessionPacket{
		Type:    netbiosproto.SessionMessage,
		Payload: makeNetServerEnum2DomainEnumPayload(),
	}
	resp, err := svc.HandleSessionContext(packet, netbios.SessionContext{})
	if err != nil {
		t.Fatalf("HandleSessionContext: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
	paramCount := int(binary.LittleEndian.Uint16(resp.Payload[smbHeaderLen+1+6 : smbHeaderLen+1+8]))
	paramOffset := int(binary.LittleEndian.Uint16(resp.Payload[smbHeaderLen+1+8 : smbHeaderLen+1+10]))
	p := resp.Payload[paramOffset : paramOffset+paramCount]
	entriesReturned := int(binary.LittleEndian.Uint16(p[4:6]))
	if entriesReturned != 2 {
		t.Fatalf("entries returned: got %d want 2 (WORKGROUP + OTHERGROUP)", entriesReturned)
	}
}

func TestHandleSessionContextNetServerEnum2DomainFilterExcludesWrongDomain(t *testing.T) {
	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, nil)
	svc.browserRole = browserRoleLocalMaster
	// Request servers in "OTHERGROUP" — we don't serve that domain, expect empty success.
	payload := makeNetServerEnum2PayloadWithDomain(0x00000003, "OTHERGROUP")
	packet := &netbiosproto.SessionPacket{Type: netbiosproto.SessionMessage, Payload: payload}
	resp, err := svc.HandleSessionContext(packet, netbios.SessionContext{})
	if err != nil {
		t.Fatalf("HandleSessionContext: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
	paramCount := int(binary.LittleEndian.Uint16(resp.Payload[smbHeaderLen+1+6 : smbHeaderLen+1+8]))
	paramOffset := int(binary.LittleEndian.Uint16(resp.Payload[smbHeaderLen+1+8 : smbHeaderLen+1+10]))
	p := resp.Payload[paramOffset : paramOffset+paramCount]
	rapStatus := binary.LittleEndian.Uint16(p[0:2])
	if rapStatus != 0 {
		t.Fatalf("RAP status: got %d want 0 (empty success)", rapStatus)
	}
	entriesReturned := binary.LittleEndian.Uint16(p[4:6])
	if entriesReturned != 0 {
		t.Fatalf("entries returned: got %d want 0 (filtered out)", entriesReturned)
	}
}

func makeNetServerEnum2PayloadWithServerType(serverType uint32) []byte {
	bytesArea := []byte("\\PIPE\\LANMAN\x00")
	bytesArea = append(bytesArea, 0x68, 0x00)               // FunctionCode 0x0068
	bytesArea = append(bytesArea, []byte("WrLehDz\x00")...) // ParamDesc
	bytesArea = append(bytesArea, []byte("B16BBDz\x00")...) // DataDesc
	bytesArea = append(bytesArea, 0xff, 0xff)               // ReceiveBufferLength
	var stBytes [4]byte
	binary.LittleEndian.PutUint32(stBytes[:], serverType)
	bytesArea = append(bytesArea, stBytes[:]...)
	out := make([]byte, 69+len(bytesArea))
	copy(out[0:4], []byte{0xff, 'S', 'M', 'B'})
	out[4] = CommandTransaction
	out[32] = 17
	binary.LittleEndian.PutUint16(out[67:69], uint16(len(bytesArea)))
	copy(out[69:], bytesArea)
	return out
}

func makeNetServerEnum2PayloadWithDomain(serverType uint32, domain string) []byte {
	bytesArea := []byte("\\PIPE\\LANMAN\x00")
	bytesArea = append(bytesArea, 0x68, 0x00)               // FunctionCode
	bytesArea = append(bytesArea, []byte("WrLehDz\x00")...) // ParamDesc
	bytesArea = append(bytesArea, []byte("B16BBDz\x00")...) // DataDesc
	bytesArea = append(bytesArea, 0xff, 0xff)               // ReceiveBufferLength
	var stBytes [4]byte
	binary.LittleEndian.PutUint32(stBytes[:], serverType)
	bytesArea = append(bytesArea, stBytes[:]...)
	bytesArea = append(bytesArea, []byte(domain)...)
	bytesArea = append(bytesArea, 0x00)
	out := make([]byte, 69+len(bytesArea))
	copy(out[0:4], []byte{0xff, 'S', 'M', 'B'})
	out[4] = CommandTransaction
	out[32] = 17
	binary.LittleEndian.PutUint16(out[67:69], uint16(len(bytesArea)))
	copy(out[69:], bytesArea)
	return out
}

func TestHandleSessionContextTreeDisconnectReturnsSuccess(t *testing.T) {
	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, nil)
	packet := &netbiosproto.SessionPacket{
		Type:    netbiosproto.SessionMessage,
		Payload: makeTreeDisconnectPayload(),
	}
	resp, err := svc.HandleSessionContext(packet, netbios.SessionContext{})
	if err != nil {
		t.Fatalf("HandleSessionContext: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Payload[4] != CommandTreeDisconnect {
		t.Fatalf("cmd: got %#x want %#x", resp.Payload[4], CommandTreeDisconnect)
	}
	if binary.LittleEndian.Uint32(resp.Payload[smbOffStatus:smbOffStatus+4]) != smbStatusSuccess {
		t.Fatalf("status: not success")
	}
	if resp.Payload[smbHeaderLen] != 0 {
		t.Fatalf("WCT: got %d want 0", resp.Payload[smbHeaderLen])
	}
	if binary.LittleEndian.Uint16(resp.Payload[smbHeaderLen+1:smbHeaderLen+3]) != 0 {
		t.Fatalf("ByteCount: expected 0")
	}
}
