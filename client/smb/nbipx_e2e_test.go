package smb_test

// nbipx_e2e_test.go is the end-to-end gate for the SMB-over-NBIPX (NWLink) client
// transport: it wires client/smb.DialNBIPX to a REAL server stack — a running
// core/service/smb.Service behind the core/service/netbios NBIPX session engine, on a
// core/router/ipx mini-router driven by a REAL core/port/ipx.Port — over an in-memory
// Ethernet link pair. So the client's SESSION_INITIALIZE, the server's session-accept,
// the sequenced DATA frames, and the reassembly all run over the genuine engine, not a
// stub. It reuses the fork/metadata assertions from e2e_test.go (same package).

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
	ipxport "github.com/ObsoleteMadness/ClassicStack/core/port/ipx"
	ipxrouter "github.com/ObsoleteMadness/ClassicStack/core/router/ipx"
	netbiossvc "github.com/ObsoleteMadness/ClassicStack/core/service/netbios"
	smbsvc "github.com/ObsoleteMadness/ClassicStack/core/service/smb"
)

// nbServerName is the file-server name the NetBIOS layer claims; the client's CALL /
// SESSION_INITIALIZE names it (SERVER<20>).
const nbServerName = "CLASSICSTACK"

// nbServerMAC is the server station's hardware address. On Ethernet the IPX node IS the
// MAC, so the router's identity node is set to it and the client's directed frames
// (after the broadcast SESSION_INITIALIZE) pass the router's addressed-to-us filter.
var nbServerMAC = [6]byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x5B}

// newNBIPXServer builds the whole server stack over an in-memory Ethernet pair and
// returns the client-side link end the transport dials. The stack is: SMB service (memfs
// "Share") → NetBIOS NBIPX engine (SMB set as its SessionConsumer via the bridge) →
// core/router/ipx (engine registered on the NB-IPX sockets) → real core/port/ipx.Port.
func newNBIPXServer(t *testing.T) link.FrameLink {
	t.Helper()
	serverEnd, clientEnd := inmem.Pair(64)

	// SMB service with one memfs share.
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

	// NetBIOS service claiming the server name, with SMB as its session consumer.
	nb := netbiossvc.NewService(nil, nbServerName)
	nb.SetSessionConsumer(nbSessionBridge{adapter: smbsvc.ConsumerAdapter{Service: sm}})

	// Real IPX port over the server end of the pair, wired to a mini-router.
	sec := &corenb.Section{SKey: ipxport.Name, IsEnabled: true}
	p, err := ipxport.NewInstanceFromOpener(sec, func() (link.FrameLink, error) {
		return serverEnd, nil
	}, nbServerMAC, log.New(ipxport.Name))
	if err != nil {
		t.Fatalf("ipx NewInstanceFromOpener: %v", err)
	}
	router := ipxrouter.NewRouter(nil)
	router.SetIdentity(ipxrouter.DefaultNetwork, nbServerMAC)
	router.AddPort(p.(ipxrouter.Port))

	// The NBIPX engine on the NB-IPX sockets.
	eng := nb.NewIPXEngine(router)
	for _, sock := range [][2]byte{
		netbiossvc.NBIPXSessionSocket, netbiossvc.NBIPXNameQuerySocket,
		netbiossvc.NBIPXDatagramSocket, netbiossvc.NBIPXNameSocket,
	} {
		if err := router.RegisterSocket(sock, eng); err != nil {
			t.Fatalf("RegisterSocket(%v): %v", sock, err)
		}
	}

	// Start the port (its read loop drives router.Inbound) and the NetBIOS service.
	if err := p.(interface {
		Start(context.Context) error
	}).Start(context.Background()); err != nil {
		t.Fatalf("ipx port Start: %v", err)
	}
	t.Cleanup(func() { _ = p.(interface{ Stop(context.Context) error }).Stop(context.Background()) })
	if err := nb.Start(context.Background()); err != nil {
		t.Fatalf("netbios Start: %v", err)
	}
	t.Cleanup(func() { _ = nb.Stop(context.Background()) })

	return clientEnd
}

// connectNBIPXClient dials the NBIPX transport over the client link end, runs the SMB
// session, and wraps the base FS with the same fork/meta stack client.Connect layers.
func connectNBIPXClient(t *testing.T, clientEnd link.FrameLink) fs.ForkFS {
	t.Helper()
	tr, err := clientsmb.DialNBIPX(clientEnd, clientsmb.RandomMAC(), nbServerName)
	if err != nil {
		t.Fatalf("DialNBIPX: %v", err)
	}
	sess, err := clientsmb.Open(tr, clientsmb.DialParams{ServerName: nbServerName, Share: "Share"})
	if err != nil {
		t.Fatalf("smb.Open over NBIPX: %v", err)
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

// TestNBIPX_InProcessE2E connects over the real NBIPX session engine, seeds a file with
// forks + type/creator, round-trips it through a host dir, then renames and deletes it —
// the same coverage as the direct-IPX SMB e2e, but exercising the SESSION_INITIALIZE /
// session-accept / sequenced-DATA path.
func TestNBIPX_InProcessE2E(t *testing.T) {
	clientEnd := newNBIPXServer(t)
	remote := connectNBIPXClient(t, clientEnd)

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

// nbSessionBridge adapts an smb.SessionConsumer to a netbios.SessionConsumer (the two
// interfaces are structurally identical but distinct types), mirroring compose's
// smbSessionBridge so the test wires SMB to the NetBIOS engine exactly as the runtime does.
type nbSessionBridge struct{ adapter smbsvc.SessionConsumer }

func (b nbSessionBridge) NewConn(client string) netbiossvc.SessionCircuit {
	return nbCircuitBridge{c: b.adapter.NewConn(client)}
}

type nbCircuitBridge struct{ c smbsvc.SessionCircuit }

func (b nbCircuitBridge) ServeMessage(req []byte) []byte { return b.c.ServeMessage(req) }
func (b nbCircuitBridge) SetPushWriter(w func([]byte))   { b.c.SetPushWriter(w) }
func (b nbCircuitBridge) Close()                         { b.c.Close() }
func (b nbCircuitBridge) SetNetBIOSName(name string) {
	if namer, ok := b.c.(netbiossvc.NetBIOSNamer); ok {
		namer.SetNetBIOSName(name)
	}
}
