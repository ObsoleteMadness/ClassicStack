package smb

import (
	"bytes"
	"context"
	"encoding/binary"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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
	return makeTreeConnectSharePayload("\\\\SERVER\\IPC$")
}

// makeTreeConnectSharePayload builds a minimal SMB_COM_TREE_CONNECT_ANDX
// request whose bytes-area contains the given UNC share path followed by
// the service identifier "?????".
func makeTreeConnectSharePayload(uncPath string) []byte {
	path := append([]byte(uncPath), 0)
	service := []byte("?????\x00")
	byteCount := len(path) + len(service)
	// WCT=4: AndXCommand+AndXReserved+AndXOffset+Flags+PasswordLength (8 bytes)
	out := make([]byte, smbHeaderLen+1+8+2+byteCount)
	copy(out[0:4], []byte{0xff, 'S', 'M', 'B'})
	out[4] = CommandTreeConnectAndX
	out[smbHeaderLen] = 4 // WCT
	w := out[smbHeaderLen+1:]
	w[0] = 0xFF // AndXCommand = no chaining
	binary.LittleEndian.PutUint16(w[8:10], uint16(byteCount))
	copy(w[10:], path)
	copy(w[10+len(path):], service)
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
		t.Fatalf("status: not success (got %#x)", binary.LittleEndian.Uint32(resp.Payload[smbOffStatus:smbOffStatus+4]))
	}
	tid := binary.LittleEndian.Uint16(resp.Payload[smbOffTID : smbOffTID+2])
	if tid == 0 {
		t.Fatal("TID should be non-zero after tree connect")
	}
	// Service string should be "IPC" for IPC$ connections.
	wct := int(resp.Payload[smbHeaderLen])
	bytesOff := smbHeaderLen + 1 + wct*2
	if bytesOff+2 < len(resp.Payload) {
		bc := int(binary.LittleEndian.Uint16(resp.Payload[bytesOff : bytesOff+2]))
		if bytesOff+2+bc <= len(resp.Payload) {
			service := string(resp.Payload[bytesOff+2 : bytesOff+2+bc])
			if !strings.HasPrefix(service, "IPC") {
				t.Fatalf("service: got %q, want prefix \"IPC\"", service)
			}
		}
	}
}

func TestHandleSessionContextTreeConnectUnknownShareReturnsError(t *testing.T) {
	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, nil)
	packet := &netbiosproto.SessionPacket{
		Type:    netbiosproto.SessionMessage,
		Payload: makeTreeConnectSharePayload("\\\\SERVER\\NOEXIST"),
	}
	resp, err := svc.HandleSessionContext(packet, netbios.SessionContext{})
	if err != nil {
		t.Fatalf("HandleSessionContext: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
	if binary.LittleEndian.Uint32(resp.Payload[smbOffStatus:smbOffStatus+4]) != smbStatusErrInvNetName {
		t.Fatalf("status: got %#x, want ERRDOS/ERRinvnetname (%#x)",
			binary.LittleEndian.Uint32(resp.Payload[smbOffStatus:smbOffStatus+4]), smbStatusErrInvNetName)
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

func makeNetShareEnumPayload() []byte {
	// bytes area: \PIPE\LANMAN\0 (13 bytes) + FunctionCode 0x0000 (2 bytes)
	bytesArea := append([]byte("\\PIPE\\LANMAN\x00"), 0x00, 0x00)
	out := make([]byte, 69+len(bytesArea))
	copy(out[0:4], []byte{0xff, 'S', 'M', 'B'})
	out[4] = CommandTransaction
	out[32] = 17
	binary.LittleEndian.PutUint16(out[67:69], uint16(len(bytesArea)))
	copy(out[69:], bytesArea)
	return out
}

func TestHandleSessionContextNetShareEnumReturnsIPCWhenNoShares(t *testing.T) {
	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, nil)
	packet := &netbiosproto.SessionPacket{
		Type:    netbiosproto.SessionMessage,
		Payload: makeNetShareEnumPayload(),
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
	paramOffset := int(binary.LittleEndian.Uint16(resp.Payload[smbHeaderLen+1+8 : smbHeaderLen+1+10]))
	paramCount := int(binary.LittleEndian.Uint16(resp.Payload[smbHeaderLen+1+6 : smbHeaderLen+1+8]))
	if paramOffset+paramCount > len(resp.Payload) {
		t.Fatalf("param block out of bounds")
	}
	p := resp.Payload[paramOffset : paramOffset+paramCount]
	if status := binary.LittleEndian.Uint16(p[0:2]); status != 0 {
		t.Fatalf("RAP status: got %d want 0", status)
	}
	// IPC$ is always present even without configured shares
	entriesReturned := binary.LittleEndian.Uint16(p[4:6])
	if entriesReturned < 1 {
		t.Fatalf("expected at least IPC$ entry, got %d entries", entriesReturned)
	}
}

func TestHandleSessionContextNetShareEnumReturnsConfiguredShares(t *testing.T) {
	shares := []ShareConfig{
		{Name: "DOCS", Path: "/docs"},
		{Name: "MEDIA", Path: "/media"},
	}
	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, shares)
	packet := &netbiosproto.SessionPacket{
		Type:    netbiosproto.SessionMessage,
		Payload: makeNetShareEnumPayload(),
	}
	resp, err := svc.HandleSessionContext(packet, netbios.SessionContext{})
	if err != nil {
		t.Fatalf("HandleSessionContext: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
	paramOffset := int(binary.LittleEndian.Uint16(resp.Payload[smbHeaderLen+1+8 : smbHeaderLen+1+10]))
	paramCount := int(binary.LittleEndian.Uint16(resp.Payload[smbHeaderLen+1+6 : smbHeaderLen+1+8]))
	if paramOffset+paramCount > len(resp.Payload) {
		t.Fatalf("param block out of bounds")
	}
	p := resp.Payload[paramOffset : paramOffset+paramCount]
	// shares + IPC$
	got := int(binary.LittleEndian.Uint16(p[4:6]))
	want := len(shares) + 1 // +1 for IPC$
	if got != want {
		t.Fatalf("EntriesReturned: got %d want %d", got, want)
	}

	// Verify share names in the data block
	dataOffset := int(binary.LittleEndian.Uint16(resp.Payload[smbHeaderLen+1+14 : smbHeaderLen+1+16]))
	dataCount := int(binary.LittleEndian.Uint16(resp.Payload[smbHeaderLen+1+12 : smbHeaderLen+1+14]))
	if dataOffset+dataCount > len(resp.Payload) {
		t.Fatalf("data block out of bounds")
	}
	d := resp.Payload[dataOffset : dataOffset+dataCount]
	for i, sc := range shares {
		base := i * 20
		name := strings.TrimRight(string(d[base:base+12]), "\x00")
		if !strings.EqualFold(name, sc.Name) {
			t.Errorf("entry[%d] name: got %q want %q", i, name, sc.Name)
		}
	}
	// Last entry should be IPC$
	ipcBase := len(shares) * 20
	ipcName := strings.TrimRight(string(d[ipcBase:ipcBase+12]), "\x00")
	if !strings.EqualFold(ipcName, "IPC$") {
		t.Errorf("last entry: got %q want IPC$", ipcName)
	}
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

func TestHandleSessionContextIgnoresSMBResponses(t *testing.T) {
	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, nil)
	payload := makeNegotiatePayload()
	payload[smbOffFlags] |= 0x80 // mark packet as SMB response

	resp, err := svc.HandleSessionContext(&netbiosproto.SessionPacket{
		Type:    netbiosproto.SessionMessage,
		Payload: payload,
	}, netbios.SessionContext{})
	if err != nil {
		t.Fatalf("HandleSessionContext: %v", err)
	}
	if resp != nil {
		t.Fatalf("expected no response for inbound SMB response packet")
	}
}

type diskUsagePathProbeFS struct {
	diskUsagePath string
}

func (f *diskUsagePathProbeFS) ReadDir(string) ([]fs.DirEntry, error) {
	return nil, fs.ErrNotExist
}

func (f *diskUsagePathProbeFS) Stat(string) (fs.FileInfo, error) {
	return nil, fs.ErrNotExist
}

func (f *diskUsagePathProbeFS) DiskUsage(path string) (uint64, uint64, error) {
	f.diskUsagePath = path
	return 1024 * 1024, 512 * 1024, nil
}

func (f *diskUsagePathProbeFS) CreateDir(string) error {
	return fs.ErrPermission
}

func (f *diskUsagePathProbeFS) CreateFile(string) (vfs.File, error) {
	return nil, fs.ErrPermission
}

func (f *diskUsagePathProbeFS) OpenFile(string, int) (vfs.File, error) {
	return nil, fs.ErrPermission
}

func (f *diskUsagePathProbeFS) Remove(string) error {
	return fs.ErrPermission
}

func (f *diskUsagePathProbeFS) Rename(string, string) error {
	return fs.ErrPermission
}

func (f *diskUsagePathProbeFS) Capabilities() vfs.Capabilities {
	return vfs.Capabilities{}
}

func (f *diskUsagePathProbeFS) ShortName(path string) (string, error) {
	return "", fs.ErrNotExist
}

func TestBuildSMBErrorResponseUsesDOSStatusWithoutNTStatusFlag(t *testing.T) {
	req := make([]byte, smbHeaderLen)
	copy(req[0:4], []byte{0xff, 'S', 'M', 'B'})
	req[4] = CommandQueryInformationDisk

	resp := buildSMBErrorResponse(req, smbStatusNotSupported)
	if resp == nil {
		t.Fatal("expected error response")
	}

	got := binary.LittleEndian.Uint32(resp[smbOffStatus : smbOffStatus+4])
	if got != smbStatusErrBadFunc {
		t.Fatalf("status mismatch: got %#x want %#x", got, uint32(smbStatusErrBadFunc))
	}
}

func TestBuildSMBErrorResponseKeepsNTStatusWhenRequested(t *testing.T) {
	req := make([]byte, smbHeaderLen)
	copy(req[0:4], []byte{0xff, 'S', 'M', 'B'})
	req[4] = CommandQueryInformationDisk
	binary.LittleEndian.PutUint16(req[smbOffFlags2:smbOffFlags2+2], smbFlags2NTStatus)

	resp := buildSMBErrorResponse(req, smbStatusNotSupported)
	if resp == nil {
		t.Fatal("expected error response")
	}

	got := binary.LittleEndian.Uint32(resp[smbOffStatus : smbOffStatus+4])
	if got != smbStatusNotSupported {
		t.Fatalf("status mismatch: got %#x want %#x", got, uint32(smbStatusNotSupported))
	}
}

func TestHandleQueryInformationDiskUsesShareRootPath(t *testing.T) {
	probe := &diskUsagePathProbeFS{}
	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, []ShareConfig{{Name: "PUBLIC", Path: `C:\\PUBLIC`}})
	svc.shareFSes = map[int]vfs.FileSystem{0: probe}

	conn := &connState{tids: map[uint16]treeSlot{7: {shareIdx: 0}}}
	req := make([]byte, smbHeaderLen)
	copy(req[0:4], []byte{0xff, 'S', 'M', 'B'})
	req[4] = CommandQueryInformationDisk
	binary.LittleEndian.PutUint16(req[smbOffTID:smbOffTID+2], 7)

	resp := svc.handleQueryInformationDisk(req, conn)
	if resp == nil {
		t.Fatal("expected response")
	}
	if got := binary.LittleEndian.Uint32(resp[smbOffStatus : smbOffStatus+4]); got != smbStatusSuccess {
		t.Fatalf("status mismatch: got %#x want %#x", got, uint32(smbStatusSuccess))
	}
	if probe.diskUsagePath != `C:\\PUBLIC` {
		t.Fatalf("DiskUsage path mismatch: got %q want %q", probe.diskUsagePath, `C:\\PUBLIC`)
	}
}

func TestHandleQueryInformationMissingFileReturnsBadFile(t *testing.T) {
	probe := &diskUsagePathProbeFS{}
	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, []ShareConfig{{Name: "PUBLIC", Path: `C:\\PUBLIC`}})
	svc.shareFSes = map[int]vfs.FileSystem{0: probe}

	conn := &connState{tids: map[uint16]treeSlot{9: {shareIdx: 0}}}
	req := makeQueryInformationPayload(9, "\\DESKTOP.INI")

	resp := svc.handleQueryInformation(req, conn)
	if resp == nil {
		t.Fatal("expected response")
	}
	if got := binary.LittleEndian.Uint32(resp[smbOffStatus : smbOffStatus+4]); got != smbStatusErrBadFile {
		t.Fatalf("status mismatch: got %#x want %#x", got, uint32(smbStatusErrBadFile))
	}
}

func TestHandleQueryInformationExactLongDirectoryNameSucceeds(t *testing.T) {
	tmp := t.TempDir()
	if err := os.Mkdir(filepath.Join(tmp, "Volume 68k"), 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	fsys, err := vfs.New(vfs.LocalFSName, vfs.Params{Name: "PUBLIC", Path: tmp})
	if err != nil {
		t.Fatalf("vfs.New: %v", err)
	}
	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, []ShareConfig{{Name: "PUBLIC", Path: tmp}})
	svc.shareFSes = map[int]vfs.FileSystem{0: fsys}

	conn := &connState{tids: map[uint16]treeSlot{15: {shareIdx: 0}}}
	req := makeQueryInformationPayload(15, "\\Volume 68k")
	resp := svc.handleQueryInformation(req, conn)
	if resp == nil {
		t.Fatal("expected response")
	}
	if got := binary.LittleEndian.Uint32(resp[smbOffStatus : smbOffStatus+4]); got != smbStatusSuccess {
		t.Fatalf("status mismatch: got %#x want %#x", got, uint32(smbStatusSuccess))
	}
}

func TestHandleQueryInformationEmptyPathReturnsShareRoot(t *testing.T) {
	tmp := t.TempDir()
	fsys, err := vfs.New(vfs.LocalFSName, vfs.Params{Name: "PUBLIC", Path: tmp})
	if err != nil {
		t.Fatalf("vfs.New: %v", err)
	}
	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, []ShareConfig{{Name: "PUBLIC", Path: tmp}})
	svc.shareFSes = map[int]vfs.FileSystem{0: fsys}

	conn := &connState{tids: map[uint16]treeSlot{16: {shareIdx: 0}}}
	req := makeQueryInformationPayload(16, "")
	resp := svc.handleQueryInformation(req, conn)
	if resp == nil {
		t.Fatal("expected response")
	}
	if got := binary.LittleEndian.Uint32(resp[smbOffStatus : smbOffStatus+4]); got != smbStatusSuccess {
		t.Fatalf("status mismatch: got %#x want %#x", got, uint32(smbStatusSuccess))
	}
}

func TestHandleQueryInformationFallsBackToDOSLikeName(t *testing.T) {
	tmp := t.TempDir()
	if err := os.Mkdir(filepath.Join(tmp, "Volume 68k"), 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	fsys, err := vfs.New(vfs.LocalFSName, vfs.Params{Name: "PUBLIC", Path: tmp})
	if err != nil {
		t.Fatalf("vfs.New: %v", err)
	}
	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, []ShareConfig{{Name: "PUBLIC", Path: tmp}})
	svc.shareFSes = map[int]vfs.FileSystem{0: fsys}

	conn := &connState{tids: map[uint16]treeSlot{17: {shareIdx: 0}}}
	req := makeQueryInformationPayload(17, "\\VOLUME68K")
	resp := svc.handleQueryInformation(req, conn)
	if resp == nil {
		t.Fatal("expected response")
	}
	if got := binary.LittleEndian.Uint32(resp[smbOffStatus : smbOffStatus+4]); got != smbStatusSuccess {
		t.Fatalf("status mismatch: got %#x want %#x", got, uint32(smbStatusSuccess))
	}
}

func TestHandleTransaction2FindFirst2WildcardDoesNotReturnBadFunc(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "ONE.TXT"), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	fsys, err := vfs.New(vfs.LocalFSName, vfs.Params{Name: "PUBLIC", Path: tmp})
	if err != nil {
		t.Fatalf("vfs.New: %v", err)
	}
	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, []ShareConfig{{Name: "PUBLIC", Path: tmp}})
	svc.shareFSes = map[int]vfs.FileSystem{0: fsys}

	conn := &connState{tids: map[uint16]treeSlot{11: {shareIdx: 0}}}
	req := makeTrans2FindFirst2Payload(11, "\\*")

	resp := svc.handleTransaction2(req, conn)
	if resp == nil {
		t.Fatal("expected response")
	}
	if got := binary.LittleEndian.Uint32(resp[smbOffStatus : smbOffStatus+4]); got != smbStatusSuccess {
		t.Fatalf("status mismatch: got %#x want %#x", got, uint32(smbStatusSuccess))
	}
	if resp[4] != CommandTransaction2 {
		t.Fatalf("command mismatch: got %#x want %#x", resp[4], CommandTransaction2)
	}
	// Ensure returned payload includes at least one directory info record.
	if len(resp) <= smbHeaderLen+1+20+2+10 {
		t.Fatalf("expected transaction2 data payload")
	}
}

func TestHandleTransaction2FindFirst2ExactPatternDoesNotMatchSidecar(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "NICOLE CAMERA.JPG"), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "._NICOLE CAMERA.JPG"), []byte("y"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	fsys, err := vfs.New(vfs.LocalFSName, vfs.Params{Name: "PUBLIC", Path: tmp})
	if err != nil {
		t.Fatalf("vfs.New: %v", err)
	}
	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, []ShareConfig{{Name: "PUBLIC", Path: tmp}})
	svc.shareFSes = map[int]vfs.FileSystem{0: fsys}

	conn := &connState{tids: map[uint16]treeSlot{19: {shareIdx: 0}}, searches: map[uint16]*searchHandle{}}
	resp := svc.handleTransaction2(makeTrans2FindFirst2PayloadWithCount(19, "\\NICOLE CAMERA.JPG", 10), conn)
	if resp == nil {
		t.Fatal("expected response")
	}
	if got := binary.LittleEndian.Uint32(resp[smbOffStatus : smbOffStatus+4]); got != smbStatusSuccess {
		t.Fatalf("status mismatch: got %#x want %#x", got, uint32(smbStatusSuccess))
	}
	param := readTrans2ParamBlock(t, resp)
	if got := binary.LittleEndian.Uint16(param[2:4]); got != 1 {
		t.Fatalf("returned count mismatch: got %d want 1", got)
	}
	data := readTrans2DataBlock(t, resp)
	expectPrimary := encodeOEM("NICOLE CAMERA.JPG")
	expectSidecar := encodeOEM("._NICOLE CAMERA.JPG")
	if !bytes.Contains(bytes.ToUpper(data), expectPrimary) {
		t.Fatalf("expected primary file in response")
	}
	if bytes.Contains(bytes.ToUpper(data), expectSidecar) {
		t.Fatalf("unexpected sidecar match in exact-pattern response")
	}
}

func TestHandleTransaction2FindNext2ReturnsSecondPage(t *testing.T) {
	tmp := t.TempDir()
	for i := 0; i < 12; i++ {
		name := filepath.Join(tmp, "FILE"+string(rune('A'+i))+".TXT")
		if err := os.WriteFile(name, []byte("x"), 0o644); err != nil {
			t.Fatalf("WriteFile(%s): %v", name, err)
		}
	}

	fsys, err := vfs.New(vfs.LocalFSName, vfs.Params{Name: "PUBLIC", Path: tmp})
	if err != nil {
		t.Fatalf("vfs.New: %v", err)
	}
	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, []ShareConfig{{Name: "PUBLIC", Path: tmp}})
	svc.shareFSes = map[int]vfs.FileSystem{0: fsys}

	conn := &connState{tids: map[uint16]treeSlot{12: {shareIdx: 0}}, searches: map[uint16]*searchHandle{}}
	firstReq := makeTrans2FindFirst2PayloadWithCount(12, "\\*", 6)
	firstResp := svc.handleTransaction2(firstReq, conn)
	if firstResp == nil {
		t.Fatal("expected first response")
	}
	if got := binary.LittleEndian.Uint32(firstResp[smbOffStatus : smbOffStatus+4]); got != smbStatusSuccess {
		t.Fatalf("first status mismatch: got %#x want %#x", got, uint32(smbStatusSuccess))
	}
	firstParam := readTrans2ParamBlock(t, firstResp)
	if got := binary.LittleEndian.Uint16(firstParam[2:4]); got == 0 {
		t.Fatalf("expected first page entries")
	}
	sid := binary.LittleEndian.Uint16(firstParam[0:2])

	nextReq := makeTrans2FindNext2Payload(12, sid, 6)
	nextResp := svc.handleTransaction2(nextReq, conn)
	if nextResp == nil {
		t.Fatal("expected next response")
	}
	if got := binary.LittleEndian.Uint32(nextResp[smbOffStatus : smbOffStatus+4]); got != smbStatusSuccess {
		t.Fatalf("next status mismatch: got %#x want %#x", got, uint32(smbStatusSuccess))
	}
	nextParam := readTrans2ParamBlock(t, nextResp)
	if got := binary.LittleEndian.Uint16(nextParam[0:2]); got == 0 {
		t.Fatalf("expected second page entries")
	}
}

func TestHandleTransaction2FindNext2ResumeNameAdvancesPosition(t *testing.T) {
	tmp := t.TempDir()
	for _, n := range []string{"A.TXT", "B.TXT", "C.TXT", "D.TXT"} {
		if err := os.WriteFile(filepath.Join(tmp, n), []byte("x"), 0o644); err != nil {
			t.Fatalf("WriteFile(%s): %v", n, err)
		}
	}

	fsys, err := vfs.New(vfs.LocalFSName, vfs.Params{Name: "PUBLIC", Path: tmp})
	if err != nil {
		t.Fatalf("vfs.New: %v", err)
	}
	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, []ShareConfig{{Name: "PUBLIC", Path: tmp}})
	svc.shareFSes = map[int]vfs.FileSystem{0: fsys}

	conn := &connState{tids: map[uint16]treeSlot{13: {shareIdx: 0}}, searches: map[uint16]*searchHandle{}}
	firstReq := makeTrans2FindFirst2PayloadWithCount(13, "\\*", 2)
	firstResp := svc.handleTransaction2(firstReq, conn)
	if firstResp == nil {
		t.Fatal("expected first response")
	}
	firstParam := readTrans2ParamBlock(t, firstResp)
	sid := binary.LittleEndian.Uint16(firstParam[0:2])

	nextReq := makeTrans2FindNext2PayloadWithResume(13, sid, 1, "B.TXT", 0)
	nextResp := svc.handleTransaction2(nextReq, conn)
	if nextResp == nil {
		t.Fatal("expected next response")
	}
	if got := binary.LittleEndian.Uint32(nextResp[smbOffStatus : smbOffStatus+4]); got != smbStatusSuccess {
		t.Fatalf("next status mismatch: got %#x want %#x", got, uint32(smbStatusSuccess))
	}
	nextParam := readTrans2ParamBlock(t, nextResp)
	if got := binary.LittleEndian.Uint16(nextParam[0:2]); got == 0 {
		t.Fatalf("expected resumed entry")
	}
}

func TestHandleTransaction2FindNext2ReturnsNoMoreFiles(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "ONLY.TXT"), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	fsys, err := vfs.New(vfs.LocalFSName, vfs.Params{Name: "PUBLIC", Path: tmp})
	if err != nil {
		t.Fatalf("vfs.New: %v", err)
	}
	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, []ShareConfig{{Name: "PUBLIC", Path: tmp}})
	svc.shareFSes = map[int]vfs.FileSystem{0: fsys}

	conn := &connState{tids: map[uint16]treeSlot{14: {shareIdx: 0}}, searches: map[uint16]*searchHandle{}}
	firstReq := makeTrans2FindFirst2PayloadWithCount(14, "\\*", 1)
	firstResp := svc.handleTransaction2(firstReq, conn)
	if firstResp == nil {
		t.Fatal("expected first response")
	}
	firstParam := readTrans2ParamBlock(t, firstResp)
	sid := binary.LittleEndian.Uint16(firstParam[0:2])

	nextReq := makeTrans2FindNext2Payload(14, sid, 1)
	nextResp := svc.handleTransaction2(nextReq, conn)
	if nextResp == nil {
		t.Fatal("expected next response")
	}
	if got := binary.LittleEndian.Uint32(nextResp[smbOffStatus : smbOffStatus+4]); got != smbStatusErrNoFiles {
		t.Fatalf("status mismatch: got %#x want %#x", got, uint32(smbStatusErrNoFiles))
	}
}

func TestHandleFindClose2ReturnsSuccess(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "ONE.TXT"), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	fsys, err := vfs.New(vfs.LocalFSName, vfs.Params{Name: "PUBLIC", Path: tmp})
	if err != nil {
		t.Fatalf("vfs.New: %v", err)
	}
	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, []ShareConfig{{Name: "PUBLIC", Path: tmp}})
	svc.shareFSes = map[int]vfs.FileSystem{0: fsys}

	conn := &connState{tids: map[uint16]treeSlot{18: {shareIdx: 0}}, searches: map[uint16]*searchHandle{}}
	firstResp := svc.handleTransaction2(makeTrans2FindFirst2PayloadWithCount(18, "\\*", 1), conn)
	if firstResp == nil {
		t.Fatal("expected search response")
	}
	param := readTrans2ParamBlock(t, firstResp)
	sid := binary.LittleEndian.Uint16(param[0:2])

	resp := svc.handleFindClose2(makeFindClose2Payload(18, sid), conn)
	if resp == nil {
		t.Fatal("expected response")
	}
	if got := binary.LittleEndian.Uint32(resp[smbOffStatus : smbOffStatus+4]); got != smbStatusSuccess {
		t.Fatalf("status mismatch: got %#x want %#x", got, uint32(smbStatusSuccess))
	}
	conn.mu.Lock()
	_, exists := conn.searches[sid]
	conn.mu.Unlock()
	if exists {
		t.Fatalf("search handle %d was not removed", sid)
	}
}

func TestHandleLockingAndXProcessesChainedSubcommands(t *testing.T) {
	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, nil)
	conn := &connState{
		fids:       map[uint16]*fileHandle{11: {path: "HELLO.TXT"}},
		lockTables: map[string]*lockTable{},
	}

	lockReq := makeLockingAndXPayload(11, nil, []lockRange{{pid: 6245, start: 2147483559, length: 20}})
	lockResp := svc.handleLockingAndX(lockReq, conn)
	if lockResp == nil {
		t.Fatal("expected initial lock response")
	}
	if got := binary.LittleEndian.Uint32(lockResp[smbOffStatus : smbOffStatus+4]); got != smbStatusSuccess {
		t.Fatalf("initial lock status mismatch: got %#x want %#x", got, uint32(smbStatusSuccess))
	}

	rotateReq := makeChainedLockingAndXPayload(
		11,
		[]lockRange{{pid: 6245, start: 2147483559, length: 20}},
		nil,
		nil,
		[]lockRange{{pid: 6245, start: 2147483579, length: 20}},
	)
	rotateResp := svc.handleLockingAndX(rotateReq, conn)
	if rotateResp == nil {
		t.Fatal("expected rotated lock response")
	}
	if got := binary.LittleEndian.Uint32(rotateResp[smbOffStatus : smbOffStatus+4]); got != smbStatusSuccess {
		t.Fatalf("rotated lock status mismatch: got %#x want %#x", got, uint32(smbStatusSuccess))
	}

	unlockReq := makeLockingAndXPayload(11, []lockRange{{pid: 6245, start: 2147483579, length: 20}}, nil)
	unlockResp := svc.handleLockingAndX(unlockReq, conn)
	if unlockResp == nil {
		t.Fatal("expected unlock response")
	}
	if got := binary.LittleEndian.Uint32(unlockResp[smbOffStatus : smbOffStatus+4]); got != smbStatusSuccess {
		t.Fatalf("unlock status mismatch: got %#x want %#x", got, uint32(smbStatusSuccess))
	}
}

func TestHandleSeekFromEndReturnsFileSize(t *testing.T) {
	tmp := t.TempDir()
	hostPath := filepath.Join(tmp, "HELLO.TXT")
	if err := os.WriteFile(hostPath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	file, err := os.Open(hostPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer file.Close()

	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, nil)
	conn := &connState{fids: map[uint16]*fileHandle{15: {file: file, path: "HELLO.TXT"}}}

	resp := svc.handleSeek(makeSeekPayload(15, 2, 0), conn)
	if resp == nil {
		t.Fatal("expected response")
	}
	if got := binary.LittleEndian.Uint32(resp[smbOffStatus : smbOffStatus+4]); got != smbStatusSuccess {
		t.Fatalf("status mismatch: got %#x want %#x", got, uint32(smbStatusSuccess))
	}
	if resp[smbHeaderLen] != 2 {
		t.Fatalf("WCT mismatch: got %d want 2", resp[smbHeaderLen])
	}
	if got := binary.LittleEndian.Uint32(resp[smbHeaderLen+1 : smbHeaderLen+5]); got != 5 {
		t.Fatalf("offset mismatch: got %d want 5", got)
	}

	conn.mu.Lock()
	defer conn.mu.Unlock()
	if got := conn.fids[15].offset; got != 5 {
		t.Fatalf("stored offset mismatch: got %d want 5", got)
	}
}

func TestHandleOpenAndXCreatesFileUnderShareRoot(t *testing.T) {
	tmp := t.TempDir()
	localName := "SMB_ROOT_PATH_PROBE.TXT"
	_ = os.Remove(localName)
	t.Cleanup(func() {
		_ = os.Remove(localName)
	})

	fsys, err := vfs.New(vfs.LocalFSName, vfs.Params{Name: "PUBLIC", Path: tmp})
	if err != nil {
		t.Fatalf("vfs.New: %v", err)
	}

	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, []ShareConfig{{Name: "PUBLIC", Path: tmp}})
	svc.shareFSes = map[int]vfs.FileSystem{0: fsys}

	conn := &connState{
		tids: map[uint16]treeSlot{21: {shareIdx: 0}},
		fids: map[uint16]*fileHandle{},
	}

	resp := svc.handleOpenAndX(makeOpenAndXPayload(21, localName, 0), conn)
	if resp == nil {
		t.Fatal("expected response")
	}
	if got := binary.LittleEndian.Uint32(resp[smbOffStatus : smbOffStatus+4]); got != smbStatusSuccess {
		t.Fatalf("status mismatch: got %#x want %#x", got, uint32(smbStatusSuccess))
	}
	conn.mu.Lock()
	for _, h := range conn.fids {
		if h != nil && h.file != nil {
			_ = h.file.Close()
		}
	}
	conn.mu.Unlock()

	if _, err := os.Stat(filepath.Join(tmp, localName)); err != nil {
		t.Fatalf("expected file under share root: %v", err)
	}
}

func TestHandleOpenAndXOpenOnlyMissingReturnsNameNotFound(t *testing.T) {
	tmp := t.TempDir()
	fsys, err := vfs.New(vfs.LocalFSName, vfs.Params{Name: "PUBLIC", Path: tmp})
	if err != nil {
		t.Fatalf("vfs.New: %v", err)
	}

	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, []ShareConfig{{Name: "PUBLIC", Path: tmp}})
	svc.shareFSes = map[int]vfs.FileSystem{0: fsys}

	conn := &connState{
		tids: map[uint16]treeSlot{22: {shareIdx: 0}},
		fids: map[uint16]*fileHandle{},
	}

	resp := svc.handleOpenAndX(makeOpenAndXPayloadWithAccess(22, "MISSING.TXT", 0x0001, 0x00C2), conn)
	if resp == nil {
		t.Fatal("expected response")
	}
	if got := binary.LittleEndian.Uint32(resp[smbOffStatus : smbOffStatus+4]); got != smbStatusErrBadFile {
		t.Fatalf("status mismatch: got %#x want %#x", got, uint32(smbStatusErrBadFile))
	}
	if _, err := os.Stat(filepath.Join(tmp, "MISSING.TXT")); err == nil {
		t.Fatalf("unexpected file creation for open-only request")
	}
}

func TestHandleOpenAndXResponseIncludesGrantedAccess(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "HELLO WORLD.TXT"), []byte("hello world"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	fsys, err := vfs.New(vfs.LocalFSName, vfs.Params{Name: "PUBLIC", Path: tmp})
	if err != nil {
		t.Fatalf("vfs.New: %v", err)
	}

	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, []ShareConfig{{Name: "PUBLIC", Path: tmp}})
	svc.shareFSes = map[int]vfs.FileSystem{0: fsys}

	conn := &connState{
		tids: map[uint16]treeSlot{23: {shareIdx: 0}},
		fids: map[uint16]*fileHandle{},
	}

	resp := svc.handleOpenAndX(makeOpenAndXPayloadWithAccess(23, "hello world.txt", 0x0001, 0x00C2), conn)
	if resp == nil {
		t.Fatal("expected response")
	}
	if got := binary.LittleEndian.Uint32(resp[smbOffStatus : smbOffStatus+4]); got != smbStatusSuccess {
		t.Fatalf("status mismatch: got %#x want %#x", got, uint32(smbStatusSuccess))
	}
	if got := binary.LittleEndian.Uint16(resp[smbHeaderLen+1+16 : smbHeaderLen+1+18]); got != 0x00C2 {
		t.Fatalf("granted access mismatch: got %#x want %#x", got, uint16(0x00C2))
	}
	if got := binary.LittleEndian.Uint16(resp[smbHeaderLen+1+22 : smbHeaderLen+1+24]); got != 0x0001 {
		t.Fatalf("action mismatch: got %#x want %#x", got, uint16(0x0001))
	}
	if got := binary.LittleEndian.Uint32(resp[smbHeaderLen+1+12 : smbHeaderLen+1+16]); got == 0 {
		t.Fatalf("expected non-zero file size in OpenAndX response")
	}

	conn.mu.Lock()
	for _, h := range conn.fids {
		if h != nil && h.file != nil {
			_ = h.file.Close()
		}
	}
	conn.mu.Unlock()
}

// TestHandleReadMPXReturnsUseStandard asserts we reject ReadMPX with
// ERRSRV/ERRuseSTD so the client falls back to SMB_COM_READ. This mirrors
// Samba's reply_readbmpx and avoids a Win98-over-IPX retransmit loop where
// the client never advances past offset 0 of a multi-block read.
func TestHandleReadMPXReturnsUseStandard(t *testing.T) {
	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, nil)
	conn := &connState{fids: map[uint16]*fileHandle{}}

	resp := svc.handleReadMPX(makeReadMPXPayload(3, 0, 4), conn)
	if resp == nil {
		t.Fatal("expected response")
	}
	if got := binary.LittleEndian.Uint32(resp[smbOffStatus : smbOffStatus+4]); got != smbStatusUseStandard {
		t.Fatalf("status mismatch: got %#x want %#x", got, uint32(smbStatusUseStandard))
	}
	if got := resp[4]; got != CommandReadMPX {
		t.Fatalf("command mismatch: got %#x want %#x", got, byte(CommandReadMPX))
	}
}

func TestHandleReadReturnsData(t *testing.T) {
	tmp := t.TempDir()
	hostPath := filepath.Join(tmp, "READ.TXT")
	if err := os.WriteFile(hostPath, []byte("abcdef"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	file, err := os.Open(hostPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer file.Close()

	svc := NewService(ServerOptions{ServerName: "ClassicStack", Workgroup: "WORKGROUP"}, nil, nil)
	conn := &connState{fids: map[uint16]*fileHandle{5: {file: file, path: hostPath}}}

	resp := svc.handleRead(makeReadPayload(5, 2, 3), conn)
	if resp == nil {
		t.Fatal("expected response")
	}
	if got := binary.LittleEndian.Uint32(resp[smbOffStatus : smbOffStatus+4]); got != smbStatusSuccess {
		t.Fatalf("status mismatch: got %#x want %#x", got, uint32(smbStatusSuccess))
	}
	if got := resp[4]; got != CommandRead {
		t.Fatalf("command mismatch: got %#x want %#x", got, byte(CommandRead))
	}
	// WCT must be 5 per [MS-CIFS] 2.2.4.11.2
	if got := resp[smbHeaderLen]; got != 5 {
		t.Fatalf("WCT mismatch: got %d want 5", got)
	}
	// CountOfBytesReturned is Words[0]
	count := int(binary.LittleEndian.Uint16(resp[smbHeaderLen+1 : smbHeaderLen+3]))
	if count != 3 {
		t.Fatalf("CountOfBytesReturned mismatch: got %d want 3", count)
	}
	// SMB_Data starts after WCT(1) + Words(5*2=10) = offset 11 from smbHeaderLen
	// Bytes: BufferFormat(1)=0x01, CountOfBytesRead(2), data
	bytesOff := smbHeaderLen + 1 + 10 + 2 // skip WCT, Words, ByteCount
	if resp[bytesOff] != 0x01 {
		t.Fatalf("BufferFormat mismatch: got %#x want 0x01", resp[bytesOff])
	}
	dataLen := int(binary.LittleEndian.Uint16(resp[bytesOff+1 : bytesOff+3]))
	if dataLen != 3 {
		t.Fatalf("CountOfBytesRead mismatch: got %d want 3", dataLen)
	}
	if got := string(resp[bytesOff+3 : bytesOff+3+dataLen]); got != "cde" {
		t.Fatalf("data mismatch: got %q want %q", got, "cde")
	}
}


func makeQueryInformationPayload(tid uint16, path string) []byte {
	pathBytes := append([]byte(path), 0)
	byteCount := 1 + len(pathBytes)
	out := make([]byte, smbHeaderLen+1+2+byteCount)
	copy(out[0:4], []byte{0xff, 'S', 'M', 'B'})
	out[4] = CommandQueryInformation
	binary.LittleEndian.PutUint16(out[smbOffTID:smbOffTID+2], tid)
	out[smbHeaderLen] = 0 // WCT
	binary.LittleEndian.PutUint16(out[smbHeaderLen+1:smbHeaderLen+3], uint16(byteCount))
	out[smbHeaderLen+3] = 0x04
	copy(out[smbHeaderLen+4:], pathBytes)
	return out
}

func makeFindClose2Payload(tid, sid uint16) []byte {
	out := make([]byte, smbHeaderLen+1+2+2)
	copy(out[0:4], []byte{0xff, 'S', 'M', 'B'})
	out[4] = CommandFindClose2
	binary.LittleEndian.PutUint16(out[smbOffTID:smbOffTID+2], tid)
	out[smbHeaderLen] = 1
	binary.LittleEndian.PutUint16(out[smbHeaderLen+1:smbHeaderLen+3], sid)
	binary.LittleEndian.PutUint16(out[smbHeaderLen+3:smbHeaderLen+5], 0)
	return out
}

func makeSeekPayload(fid, mode uint16, offset int32) []byte {
	out := make([]byte, smbHeaderLen+1+8+2)
	copy(out[0:4], []byte{0xff, 'S', 'M', 'B'})
	out[4] = CommandSeek
	out[smbHeaderLen] = 4
	binary.LittleEndian.PutUint16(out[smbHeaderLen+1:smbHeaderLen+3], fid)
	binary.LittleEndian.PutUint16(out[smbHeaderLen+3:smbHeaderLen+5], mode)
	binary.LittleEndian.PutUint32(out[smbHeaderLen+5:smbHeaderLen+9], uint32(offset))
	binary.LittleEndian.PutUint16(out[smbHeaderLen+9:smbHeaderLen+11], 0)
	return out
}

func makeOpenAndXPayload(tid uint16, path string, openFunction uint16) []byte {
	return makeOpenAndXPayloadWithAccess(tid, path, openFunction, 0)
}

func makeOpenAndXPayloadWithAccess(tid uint16, path string, openFunction uint16, desiredAccess uint16) []byte {
	pathBytes := append([]byte(path), 0)
	byteCount := 1 + len(pathBytes)
	wct := 15
	wordBytes := wct * 2
	bytesOffset := smbHeaderLen + 1 + wordBytes
	out := make([]byte, bytesOffset+2+byteCount)

	copy(out[0:4], []byte{0xff, 'S', 'M', 'B'})
	out[4] = CommandOpenAndX
	binary.LittleEndian.PutUint16(out[smbOffTID:smbOffTID+2], tid)
	out[smbHeaderLen] = byte(wct)

	w := out[smbHeaderLen+1 : smbHeaderLen+1+wordBytes]
	w[0] = 0xFF                                  // AndXCommand
	w[1] = 0x00                                  // AndXReserved
	binary.LittleEndian.PutUint16(w[2:4], 0)     // AndXOffset
	binary.LittleEndian.PutUint16(w[4:6], 0)     // Flags
	binary.LittleEndian.PutUint16(w[6:8], desiredAccess)
	binary.LittleEndian.PutUint16(w[8:10], 0)    // SearchAttrs
	binary.LittleEndian.PutUint16(w[10:12], 0)   // FileAttrs
	binary.LittleEndian.PutUint32(w[12:16], 0)   // CreationTime
	binary.LittleEndian.PutUint16(w[16:18], openFunction)
	binary.LittleEndian.PutUint32(w[18:22], 0)   // AllocationSize
	binary.LittleEndian.PutUint32(w[22:26], 0)   // Timeout
	binary.LittleEndian.PutUint32(w[26:30], 0)   // Reserved

	binary.LittleEndian.PutUint16(out[bytesOffset:bytesOffset+2], uint16(byteCount))
	out[bytesOffset+2] = 0x04
	copy(out[bytesOffset+3:], pathBytes)
	return out
}

func makeReadMPXPayload(fid uint16, offset uint32, count uint16) []byte {
	out := make([]byte, smbHeaderLen+1+(8*2)+2)
	copy(out[0:4], []byte{0xff, 'S', 'M', 'B'})
	out[4] = CommandReadMPX
	out[smbHeaderLen] = 8
	w := out[smbHeaderLen+1 : smbHeaderLen+1+(8*2)]
	binary.LittleEndian.PutUint16(w[0:2], fid)
	binary.LittleEndian.PutUint32(w[2:6], offset)
	binary.LittleEndian.PutUint16(w[6:8], count)
	binary.LittleEndian.PutUint16(w[8:10], count)
	binary.LittleEndian.PutUint32(w[10:14], 0)
	binary.LittleEndian.PutUint16(w[14:16], 0)
	binary.LittleEndian.PutUint16(out[smbHeaderLen+1+(8*2):smbHeaderLen+1+(8*2)+2], 0)
	return out
}

func makeReadPayload(fid, offset, count uint16) []byte {
	out := make([]byte, smbHeaderLen+1+(5*2)+2)
	copy(out[0:4], []byte{0xff, 'S', 'M', 'B'})
	out[4] = CommandRead
	out[smbHeaderLen] = 5
	w := out[smbHeaderLen+1 : smbHeaderLen+1+(5*2)]
	binary.LittleEndian.PutUint16(w[0:2], fid)
	binary.LittleEndian.PutUint16(w[2:4], count)
	binary.LittleEndian.PutUint32(w[4:8], uint32(offset))
	binary.LittleEndian.PutUint16(w[8:10], 0)
	binary.LittleEndian.PutUint16(out[smbHeaderLen+1+(5*2):smbHeaderLen+1+(5*2)+2], 0)
	return out
}

func makeLockingAndXPayload(fid uint16, unlocks, locks []lockRange) []byte {
	cmd := marshalLockingAndXCommand(CommandNoAndXCommand, 0, fid, unlocks, locks)
	out := make([]byte, smbHeaderLen+len(cmd))
	copy(out[0:4], []byte{0xff, 'S', 'M', 'B'})
	out[4] = CommandLockingAndX
	copy(out[smbHeaderLen:], cmd)
	return out
}

func makeChainedLockingAndXPayload(fid uint16, firstUnlocks, firstLocks, secondUnlocks, secondLocks []lockRange) []byte {
	first := marshalLockingAndXCommand(CommandLockingAndX, uint16(smbHeaderLen), fid, firstUnlocks, firstLocks)
	secondOffset := uint16(smbHeaderLen + len(first))
	first = marshalLockingAndXCommand(CommandLockingAndX, secondOffset, fid, firstUnlocks, firstLocks)
	second := marshalLockingAndXCommand(CommandNoAndXCommand, 0, fid, secondUnlocks, secondLocks)
	out := make([]byte, smbHeaderLen+len(first)+len(second))
	copy(out[0:4], []byte{0xff, 'S', 'M', 'B'})
	out[4] = CommandLockingAndX
	copy(out[smbHeaderLen:], first)
	copy(out[smbHeaderLen+len(first):], second)
	return out
}

func marshalLockingAndXCommand(andxCommand byte, andxOffset, fid uint16, unlocks, locks []lockRange) []byte {
	byteCount := 10 * (len(unlocks) + len(locks))
	out := make([]byte, 1+16+2+byteCount)
	out[0] = 8
	w := out[1:17]
	w[0] = andxCommand
	w[1] = 0
	binary.LittleEndian.PutUint16(w[2:4], andxOffset)
	binary.LittleEndian.PutUint16(w[4:6], fid)
	w[6] = 0
	w[7] = 0
	binary.LittleEndian.PutUint32(w[8:12], 0)
	binary.LittleEndian.PutUint16(w[12:14], uint16(len(unlocks)))
	binary.LittleEndian.PutUint16(w[14:16], uint16(len(locks)))
	binary.LittleEndian.PutUint16(out[17:19], uint16(byteCount))
	off := 19
	for _, r := range unlocks {
		marshalLockRange(out[off:off+10], r)
		off += 10
	}
	for _, r := range locks {
		marshalLockRange(out[off:off+10], r)
		off += 10
	}
	return out
}

func marshalLockRange(dst []byte, r lockRange) {
	binary.LittleEndian.PutUint16(dst[0:2], r.pid)
	binary.LittleEndian.PutUint32(dst[2:6], uint32(r.start))
	binary.LittleEndian.PutUint32(dst[6:10], uint32(r.length))
}

func makeTrans2FindFirst2Payload(tid uint16, pattern string) []byte {
	return makeTrans2FindFirst2PayloadWithCount(tid, pattern, 1)
}

func makeTrans2FindFirst2PayloadWithCount(tid uint16, pattern string, count uint16) []byte {
	params := make([]byte, 12)
	binary.LittleEndian.PutUint16(params[0:2], 0x0016) // SearchAttributes
	binary.LittleEndian.PutUint16(params[2:4], count)  // SearchCount
	binary.LittleEndian.PutUint16(params[4:6], 0x0000) // Flags
	binary.LittleEndian.PutUint16(params[6:8], 0x0104) // SMB_FIND_FILE_BOTH_DIRECTORY_INFO
	binary.LittleEndian.PutUint32(params[8:12], 0x00000000)
	params = append(params, []byte(pattern)...)
	params = append(params, 0x00)

	const setupCount = 1
	const wct = 14 + setupCount
	wordBytes := wct * 2
	bytesOffset := smbHeaderLen + 1 + wordBytes
	paramOffset := bytesOffset + 2
	byteCount := len(params)
	out := make([]byte, paramOffset+byteCount)

	copy(out[0:4], []byte{0xff, 'S', 'M', 'B'})
	out[4] = CommandTransaction2
	binary.LittleEndian.PutUint16(out[smbOffTID:smbOffTID+2], tid)
	out[smbHeaderLen] = wct

	w := out[smbHeaderLen+1 : smbHeaderLen+1+wordBytes]
	binary.LittleEndian.PutUint16(w[0:2], uint16(len(params))) // TotalParameterCount
	binary.LittleEndian.PutUint16(w[2:4], 0)                   // TotalDataCount
	binary.LittleEndian.PutUint16(w[4:6], 10)                  // MaxParameterCount
	binary.LittleEndian.PutUint16(w[6:8], 4096)                // MaxDataCount
	w[8] = 0                                                   // MaxSetupCount
	binary.LittleEndian.PutUint16(w[10:12], 0)                 // Flags
	binary.LittleEndian.PutUint32(w[12:16], 0)                 // Timeout
	binary.LittleEndian.PutUint16(w[18:20], uint16(len(params)))
	binary.LittleEndian.PutUint16(w[20:22], uint16(paramOffset))
	binary.LittleEndian.PutUint16(w[22:24], 0) // DataCount
	binary.LittleEndian.PutUint16(w[24:26], 0) // DataOffset
	w[26] = setupCount
	binary.LittleEndian.PutUint16(w[28:30], trans2SubcommandFindFirst2)

	binary.LittleEndian.PutUint16(out[bytesOffset:bytesOffset+2], uint16(byteCount))
	copy(out[paramOffset:], params)
	return out
}

func makeTrans2FindNext2Payload(tid, sid, count uint16) []byte {
	return makeTrans2FindNext2PayloadWithResume(tid, sid, count, "", 0)
}

func makeTrans2FindNext2PayloadWithResume(tid, sid, count uint16, resumeName string, flags uint16) []byte {
	params := make([]byte, 12)
	binary.LittleEndian.PutUint16(params[0:2], sid)
	binary.LittleEndian.PutUint16(params[2:4], count)
	binary.LittleEndian.PutUint16(params[4:6], 0x0104) // SMB_FIND_FILE_BOTH_DIRECTORY_INFO
	binary.LittleEndian.PutUint32(params[6:10], 0x00000000)
	binary.LittleEndian.PutUint16(params[10:12], flags)
	if resumeName != "" {
		params = append(params, []byte(resumeName)...)
		params = append(params, 0)
	}

	const setupCount = 1
	const wct = 14 + setupCount
	wordBytes := wct * 2
	bytesOffset := smbHeaderLen + 1 + wordBytes
	paramOffset := bytesOffset + 2
	byteCount := len(params)
	out := make([]byte, paramOffset+byteCount)

	copy(out[0:4], []byte{0xff, 'S', 'M', 'B'})
	out[4] = CommandTransaction2
	binary.LittleEndian.PutUint16(out[smbOffTID:smbOffTID+2], tid)
	out[smbHeaderLen] = wct

	w := out[smbHeaderLen+1 : smbHeaderLen+1+wordBytes]
	binary.LittleEndian.PutUint16(w[0:2], uint16(len(params)))
	binary.LittleEndian.PutUint16(w[2:4], 0)
	binary.LittleEndian.PutUint16(w[4:6], 10)
	binary.LittleEndian.PutUint16(w[6:8], 4096)
	w[8] = 0
	binary.LittleEndian.PutUint16(w[10:12], 0)
	binary.LittleEndian.PutUint32(w[12:16], 0)
	binary.LittleEndian.PutUint16(w[18:20], uint16(len(params)))
	binary.LittleEndian.PutUint16(w[20:22], uint16(paramOffset))
	binary.LittleEndian.PutUint16(w[22:24], 0)
	binary.LittleEndian.PutUint16(w[24:26], 0)
	w[26] = setupCount
	binary.LittleEndian.PutUint16(w[28:30], trans2SubcommandFindNext2)

	binary.LittleEndian.PutUint16(out[bytesOffset:bytesOffset+2], uint16(byteCount))
	copy(out[paramOffset:], params)
	return out
}

func readTrans2ParamBlock(t *testing.T, resp []byte) []byte {
	t.Helper()
	if len(resp) < smbHeaderLen+1+20 {
		t.Fatalf("response too short")
	}
	paramCount := int(binary.LittleEndian.Uint16(resp[smbHeaderLen+1+6 : smbHeaderLen+1+8]))
	paramOffset := int(binary.LittleEndian.Uint16(resp[smbHeaderLen+1+8 : smbHeaderLen+1+10]))
	if paramOffset < 0 || paramCount < 0 || paramOffset+paramCount > len(resp) {
		t.Fatalf("param block out of bounds")
	}
	return resp[paramOffset : paramOffset+paramCount]
}

func readTrans2DataBlock(t *testing.T, resp []byte) []byte {
	t.Helper()
	if len(resp) < smbHeaderLen+1+20 {
		t.Fatalf("response too short")
	}
	dataCount := int(binary.LittleEndian.Uint16(resp[smbHeaderLen+1+12 : smbHeaderLen+1+14]))
	dataOffset := int(binary.LittleEndian.Uint16(resp[smbHeaderLen+1+14 : smbHeaderLen+1+16]))
	if dataOffset < 0 || dataCount < 0 || dataOffset+dataCount > len(resp) {
		t.Fatalf("data block out of bounds")
	}
	return resp[dataOffset : dataOffset+dataCount]
}
