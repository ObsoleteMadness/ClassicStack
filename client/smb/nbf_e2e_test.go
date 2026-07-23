package smb_test

// nbf_e2e_test.go is the end-to-end gate for the SMB-over-NBF (NetBEUI) client
// transport: it wires client/smb.DialNBF to a REAL server stack — a running
// core/service/smb.Service behind the core/service/netbios NBF session engine, on a
// core/router/netbeui mini-router driven by a REAL core/port/netbeui.Port (which owns
// the LLC2 Type-2 responder: SABME→UA, I-frame extraction, RR acks) — over an in-memory
// Ethernet link pair. So the client's caller-side LLC2 (SABME, sequenced I-frames) and
// the NBF session handshake (NAME_QUERY → NAME_RECOGNIZED → SESSION_INITIALIZE →
// SESSION_CONFIRM → DATA) all run against the genuine responder. It reuses the fork /
// metadata assertions from e2e_test.go (same package).

import (
	"context"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/adapter/link/inmem"
	clientsmb "github.com/ObsoleteMadness/ClassicStack/client/smb"
	"github.com/ObsoleteMadness/ClassicStack/client/xfer"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/metastore"
	corenb "github.com/ObsoleteMadness/ClassicStack/core/port"
	nbfport "github.com/ObsoleteMadness/ClassicStack/core/port/netbeui"
	netbeuirouter "github.com/ObsoleteMadness/ClassicStack/core/router/netbeui"
	netbiossvc "github.com/ObsoleteMadness/ClassicStack/core/service/netbios"
	smbsvc "github.com/ObsoleteMadness/ClassicStack/core/service/smb"
)

// nbfServerMAC is the NBF server station's hardware address (the LLC2 peer MAC the
// client learns from the NAME_RECOGNIZED / UA).
var nbfServerMAC = [6]byte{0x02, 0x00, 0x00, 0x00, 0x00, 0xBF}

// newNBFServer builds the whole server stack over an in-memory Ethernet pair and returns
// the client-side link end the transport dials. The stack is: SMB service (memfs
// "Share") → NetBIOS NBF engine (SMB set as its SessionConsumer) → core/router/netbeui
// (engine registered as NameHandler per local name + SessionHandler + broadcast) → real
// core/port/netbeui.Port (LLC2 responder).
func newNBFServer(t *testing.T) link.FrameLink {
	t.Helper()
	serverEnd, clientEnd := inmem.Pair(64)

	sm, err := smbsvc.NewWithShares(nil, smbsvc.ShareSpec{
		Name: "Share",
		Share: fs.ShareSpec{
			Name: "Share", FSType: "memfs",
			ForkBackend: "appledouble", FilenameCodec: "identity",
		},
	})
	if err != nil {
		t.Fatalf("NewWithShares: %v", err)
	}
	if err := sm.Start(context.Background()); err != nil {
		t.Fatalf("smb Start: %v", err)
	}
	t.Cleanup(func() { _ = sm.Stop(context.Background()) })

	nb := netbiossvc.NewService(nil, nbServerName)
	nb.SetSessionConsumer(nbSessionBridge{adapter: smbsvc.ConsumerAdapter{Service: sm}})

	sec := &corenb.Section{SKey: nbfport.Name, IsEnabled: true}
	p, err := nbfport.NewInstanceFromOpener(sec, func() (link.FrameLink, error) {
		return serverEnd, nil
	}, nbfServerMAC, log.New(nbfport.Name))
	if err != nil {
		t.Fatalf("netbeui NewInstanceFromOpener: %v", err)
	}
	router := netbeuirouter.NewRouter(nil)
	router.AddPort(p.(netbeuirouter.Port))

	// The NBF engine as NameHandler for each local name + SessionHandler + broadcast.
	eng := nb.NewNBFEngine(router)
	for _, n := range nb.LocalNames() {
		if err := router.RegisterName(n, eng); err != nil {
			t.Fatalf("RegisterName(%v): %v", n, err)
		}
	}
	if err := router.RegisterSession(eng); err != nil {
		t.Fatalf("RegisterSession: %v", err)
	}
	if err := router.RegisterBroadcast(eng); err != nil {
		t.Fatalf("RegisterBroadcast: %v", err)
	}

	if err := p.(interface {
		Start(context.Context) error
	}).Start(context.Background()); err != nil {
		t.Fatalf("netbeui port Start: %v", err)
	}
	t.Cleanup(func() { _ = p.(interface{ Stop(context.Context) error }).Stop(context.Background()) })
	if err := nb.Start(context.Background()); err != nil {
		t.Fatalf("netbios Start: %v", err)
	}
	t.Cleanup(func() { _ = nb.Stop(context.Background()) })

	return clientEnd
}

// connectNBFClient dials the NBF transport over the client link end, runs the SMB
// session, and wraps the base FS with the same fork/meta stack client.Connect layers.
func connectNBFClient(t *testing.T, clientEnd link.FrameLink) fs.ForkFS {
	t.Helper()
	tr, err := clientsmb.DialNBF(clientEnd, clientsmb.RandomMAC(), nbServerName)
	if err != nil {
		t.Fatalf("DialNBF: %v", err)
	}
	sess, err := clientsmb.Open(tr, clientsmb.DialParams{ServerName: nbServerName, Share: "Share"})
	if err != nil {
		t.Fatalf("smb.Open over NBF: %v", err)
	}
	store, err := metastore.Open("mem", "")
	if err != nil {
		t.Fatalf("metastore.Open: %v", err)
	}
	remote, err := fs.WrapBase(clientsmb.New(sess), fs.ShareSpec{
		Name: "Share", ForkBackend: "appledouble", FilenameCodec: "identity",
	}, store)
	if err != nil {
		t.Fatalf("WrapBase: %v", err)
	}
	t.Cleanup(func() { _ = fs.CloseFS(remote) })
	return remote
}

// TestNBF_InProcessE2E connects over the real NBF (NetBEUI) session engine + LLC2
// responder, seeds a file with forks + type/creator, round-trips it through a host dir,
// then renames and deletes it — exercising the caller LLC2 (SABME/UA + I-frame
// sequencing) and the NBF session handshake end to end.
func TestNBF_InProcessE2E(t *testing.T) {
	clientEnd := newNBFServer(t)
	remote := connectNBFClient(t, clientEnd)

	data := []byte("the quick brown fox")
	rsrc := []byte("RESOURCE-FORK-CONTENTS")
	writeRemoteFile(t, remote, "report.txt", data, rsrc, "TEXT", "ttxt")

	entries, err := xfer.List(remote, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !hasEntry(entries, "report.txt") {
		t.Fatalf("report.txt not listed; entries=%+v", entries)
	}

	hostDir := t.TempDir()
	host := hostShare(t, hostDir)
	if err := xfer.Copy(remote, host, "report.txt", "report.txt"); err != nil {
		t.Fatalf("Copy remote→host: %v", err)
	}
	assertForkFile(t, host, "report.txt", data, rsrc, "TEXT", "ttxt")

	if err := xfer.Copy(host, remote, "report.txt", "copy.txt"); err != nil {
		t.Fatalf("Copy host→remote: %v", err)
	}
	assertForkFile(t, remote, "copy.txt", data, rsrc, "TEXT", "ttxt")

	if err := remote.Rename("copy.txt", "renamed.txt"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if _, err := remote.Stat("renamed.txt"); err != nil {
		t.Fatalf("Stat after rename: %v", err)
	}
	if err := xfer.Remove(remote, "renamed.txt"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := remote.Stat("renamed.txt"); err == nil {
		t.Fatalf("renamed.txt still present after Remove")
	}
}
