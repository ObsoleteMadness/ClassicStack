package afp_test

// e2e_test.go is the PRIMARY verification gate for the AFP client: it wires the whole
// client stack (client/afp fs adapter → client/asp session → client/atalk ATP requester)
// to a REAL running core/service/afp.Service over an in-process DDP bridge, with a memfs
// volume. It then drives the operations through client.Connect + client/xfer and asserts
// bytes AND metadata (resource fork, Finder type/creator) survive a round trip out to a
// host dir and back — the plan's in-process e2e (§Verify.2).

import (
	"bytes"
	"context"
	"os"
	"sync"
	"testing"
	"time"

	clientpkg "github.com/ObsoleteMadness/ClassicStack/client"
	_ "github.com/ObsoleteMadness/ClassicStack/client/afp" // register the afp scheme
	clientlink "github.com/ObsoleteMadness/ClassicStack/client/link"
	"github.com/ObsoleteMadness/ClassicStack/client/uri"
	"github.com/ObsoleteMadness/ClassicStack/client/xfer"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
	"github.com/ObsoleteMadness/ClassicStack/core/router"
	afpsvc "github.com/ObsoleteMadness/ClassicStack/core/service/afp"
)

// bridge is both the client's DDP DatagramLink and the server's ServiceRouter: a
// client WriteDatagram is delivered to svc.Inbound; the server's Reply/Route datagrams
// are queued back to the client's ReadDatagram. It models one point-to-point DDP link
// with no addressing/routing beyond src/dest echo, which is all a single client↔server
// session needs.
type bridge struct {
	svc      *afpsvc.Service
	clientRx chan ddp.Datagram
	mu       sync.Mutex
	closed   bool
}

func newBridge(svc *afpsvc.Service) *bridge {
	return &bridge{svc: svc, clientRx: make(chan ddp.Datagram, 64)}
}

// --- client side: link.DatagramLink ---

func (b *bridge) WriteDatagram(d ddp.Datagram) error {
	// The client sent a datagram; hand it straight to the server. The server dispatches
	// by DestSocket internally (it is the AFP socket for GetStatus/OpenSession, the
	// session socket afterwards), which Inbound handles.
	b.svc.Inbound(d, fakePort{})
	return nil
}

func (b *bridge) ReadDatagram() (ddp.Datagram, error) {
	d, ok := <-b.clientRx
	if !ok {
		return ddp.Datagram{}, errClosed
	}
	return d, nil
}

func (b *bridge) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.closed {
		b.closed = true
		close(b.clientRx)
	}
	return nil
}

// --- server side: router.ServiceRouter ---

func (b *bridge) Reply(d ddp.Datagram, _ router.RoutedPort, ddpType uint8, data []byte) {
	// Echo the src/dest swap the real router does, then queue to the client.
	b.deliver(ddp.Datagram{
		DestNetwork: d.SrcNetwork, SrcNetwork: d.DestNetwork,
		DestNode: d.SrcNode, SrcNode: d.DestNode,
		DestSocket: d.SrcSocket, SrcSocket: d.DestSocket,
		DDPType: ddpType, Data: append([]byte(nil), data...),
	})
}

func (b *bridge) Route(d ddp.Datagram, _ bool) error {
	// Server-initiated datagram (tickle / aspDataWrite / TRel) addressed to the client's
	// session socket: deliver as-is.
	d.Data = append([]byte(nil), d.Data...)
	b.deliver(d)
	return nil
}

func (b *bridge) deliver(d ddp.Datagram) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	select {
	case b.clientRx <- d:
	default:
	}
}

func (b *bridge) RoutingTable() *router.RoutingTable  { return nil }
func (b *bridge) Zones() *router.ZoneInformationTable { return nil }
func (b *bridge) Ports() []router.RoutedPort          { return nil }

type fakePort struct{ router.RoutedPort }

var errClosed = &closedErr{}

type closedErr struct{}

func (*closedErr) Error() string { return "bridge closed" }

// newServer builds a running AFP service with a single memfs "Share" volume, wired to
// the bridge as its router.
func newServer(t *testing.T) (*afpsvc.Service, *bridge) {
	t.Helper()
	svc, err := afpsvc.NewWithVolumes(nil, afpsvc.VolumeSpec{
		ID:   1,
		Name: "Share",
		Share: fs.ShareSpec{
			Name: "Share", FSType: "memfs",
			ForkBackend: "appledouble", FilenameCodec: "macroman-utf8",
		},
	})
	if err != nil {
		t.Fatalf("NewWithVolumes: %v", err)
	}
	br := newBridge(svc)
	svc.SetRouter(br)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return svc, br
}

// connectClient dials the server over the bridge via client.Connect (the public path),
// addressing the server by a literal net.node (no NBP needed — the bridge is one link).
func connectClient(t *testing.T, br *bridge) fs.ForkFS {
	t.Helper()
	return connectClientWithPopup(t, br, nil)
}

func connectClientWithPopup(t *testing.T, br *bridge, onMsg func(kind, from, text string)) fs.ForkFS {
	t.Helper()
	target, err := uri.Parse("afp://0.0/Share")
	if err != nil {
		t.Fatalf("uri.Parse: %v", err)
	}
	opener := clientlink.NewDatagramOpener(br)
	remote, err := clientpkg.Connect(context.Background(), target, clientpkg.Options{
		Opener:          opener,
		OnServerMessage: onMsg,
	})
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = fs.CloseFS(remote) })
	return remote
}

// TestAFP_InProcessE2E is the end-to-end gate: connect, seed a file with a resource fork
// + type/creator on the remote, copy it to a host dir and back, and assert bytes and
// metadata survive.
func TestAFP_InProcessE2E(t *testing.T) {
	_, br := newServer(t)
	remote := connectClient(t, br)

	// 1. Seed a file on the remote AFP volume: data fork + resource fork + type/creator.
	data := []byte("the quick brown fox")
	rsrc := []byte("RESOURCE-FORK-CONTENTS")
	writeRemoteFile(t, remote, "report.txt", data, rsrc, "TEXT", "ttxt")

	// 2. List the volume root — the file must appear with its type/creator.
	entries, err := xfer.List(remote, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !hasEntry(entries, "report.txt", "TEXT", "ttxt") {
		t.Fatalf("report.txt not listed with TEXT/ttxt; entries=%+v", entries)
	}

	// 3. Copy remote → host directory (a local ForkFS), preserving forks + metadata.
	hostDir := t.TempDir()
	host := hostShare(t, hostDir)
	if err := xfer.Copy(remote, host, "report.txt", "report.txt"); err != nil {
		t.Fatalf("Copy remote→host: %v", err)
	}
	assertForkFile(t, host, "report.txt", data, rsrc, "TEXT", "ttxt")

	// 4. Copy host → remote under a new name, then read it back off the remote.
	if err := xfer.Copy(host, remote, "report.txt", "copy.txt"); err != nil {
		t.Fatalf("Copy host→remote: %v", err)
	}
	assertForkFile(t, remote, "copy.txt", data, rsrc, "TEXT", "ttxt")

	// 5. Rename then delete on the remote.
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

func TestAFP_LoginMessageDelivered(t *testing.T) {
	svc, br := newServer(t)
	svc.SetLoginMessage("Welcome to ClassicStack.")
	got := make(chan string, 1)
	connectClientWithPopup(t, br, func(kind, _, text string) {
		got <- kind + "|" + text
	})
	select {
	case m := <-got:
		if m != "login|Welcome to ClassicStack." {
			t.Fatalf("popup = %q, want login greeting", m)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for login message")
	}
}

func TestAFP_AttentionMessageDelivered(t *testing.T) {
	svc, br := newServer(t)
	got := make(chan string, 1)
	connectClientWithPopup(t, br, func(kind, _, text string) {
		if kind == "server" {
			got <- text
		}
	})
	if err := svc.SendMessage(0, "hello there"); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	select {
	case m := <-got:
		if m != "hello there" {
			t.Fatalf("popup = %q, want hello there", m)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for attention message")
	}
}

func writeRemoteFile(t *testing.T, sh fs.ForkFS, path string, data, rsrc []byte, typ, creator string) {
	t.Helper()
	f, err := sh.CreateFile(path)
	if err != nil {
		t.Fatalf("CreateFile %s: %v", path, err)
	}
	if _, err := f.WriteAt(data, 0); err != nil {
		t.Fatalf("WriteAt data: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close data: %v", err)
	}
	rf, err := sh.OpenFork(path, fs.ResourceFork, os.O_RDWR|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		t.Fatalf("OpenFork rsrc: %v", err)
	}
	if _, err := rf.WriteAt(rsrc, 0); err != nil {
		t.Fatalf("WriteAt rsrc: %v", err)
	}
	if err := rf.Close(); err != nil {
		t.Fatalf("Close rsrc: %v", err)
	}
	var fi [32]byte
	copy(fi[0:4], typ)
	copy(fi[4:8], creator)
	if err := sh.WriteFinderInfo(path, fi); err != nil {
		t.Fatalf("WriteFinderInfo: %v", err)
	}
}

func assertForkFile(t *testing.T, sh fs.ForkFS, path string, data, rsrc []byte, typ, creator string) {
	t.Helper()
	got := readFullData(t, sh, path)
	if !bytes.Equal(got, data) {
		t.Errorf("%s data fork = %q, want %q", path, got, data)
	}
	gotRsrc := readFullFork(t, sh, path, fs.ResourceFork)
	if !bytes.Equal(gotRsrc, rsrc) {
		t.Errorf("%s resource fork = %q, want %q", path, gotRsrc, rsrc)
	}
	fi, ok, err := sh.ReadFinderInfo(path)
	if err != nil || !ok {
		t.Fatalf("%s ReadFinderInfo ok=%v err=%v", path, ok, err)
	}
	if string(fi[0:4]) != typ || string(fi[4:8]) != creator {
		t.Errorf("%s type/creator = %q/%q, want %q/%q", path, fi[0:4], fi[4:8], typ, creator)
	}
}

func readFullData(t *testing.T, sh fs.ForkFS, path string) []byte {
	t.Helper()
	f, err := sh.OpenFile(path, os.O_RDONLY)
	if err != nil {
		t.Fatalf("OpenFile %s: %v", path, err)
	}
	defer f.Close()
	return readAllFile(f)
}

func readFullFork(t *testing.T, sh fs.ForkFS, path string, fork fs.ForkType) []byte {
	t.Helper()
	f, err := sh.OpenFork(path, fork, os.O_RDONLY)
	if err != nil {
		t.Fatalf("OpenFork %s: %v", path, err)
	}
	defer f.Close()
	return readAllFile(f)
}

// readAllFile reads a whole fs.File via ReadAt until a short read.
func readAllFile(f fs.File) []byte {
	var out []byte
	buf := make([]byte, 512)
	var off int64
	for {
		n, err := f.ReadAt(buf, off)
		out = append(out, buf[:n]...)
		off += int64(n)
		if err != nil || n == 0 {
			break
		}
	}
	return out
}

func hostShare(t *testing.T, dir string) fs.ForkFS {
	t.Helper()
	sh, err := fs.BuildShare(fs.ShareSpec{
		FSType: "local_fs", Path: dir, ForkBackend: "appledouble",
	}, nil)
	if err != nil {
		t.Fatalf("host BuildShare: %v", err)
	}
	return sh
}

func hasEntry(entries []xfer.Entry, name, typ, creator string) bool {
	for _, e := range entries {
		if e.Name == name && e.Type == typ && e.Creator == creator {
			return true
		}
	}
	return false
}

var _ = time.Second // keep the time import if the file evolves
