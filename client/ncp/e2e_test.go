package ncp_test

// e2e_test.go is the PRIMARY verification gate for the NCP client: it wires the whole
// client stack (client/ncp fs adapter → session → client-direction codec) to a REAL
// running core/service/ncp.Service over an in-process IPX-datagram bridge, with a memfs
// volume. It drives operations through client.Connect + client/xfer and asserts bytes
// AND the AppleDouble-carried metadata (resource fork, Finder type/creator — stored as
// "._NAME" sidecars over the data fork) survive a round trip out to a host dir and back.
// Because NCP has no native fork, the whole fork story here is the AppleDouble backend
// reading/writing sidecar FILES over ordinary Open/Read/Write — the exact interop the
// client must get right, over the DOS 8.3 name space.

import (
	"bytes"
	"context"
	"os"
	"sync"
	"testing"

	_ "github.com/ObsoleteMadness/ClassicStack/client/ncp" // register the ncp scheme
	clientncp "github.com/ObsoleteMadness/ClassicStack/client/ncp"
	"github.com/ObsoleteMadness/ClassicStack/client/xfer"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/metastore"
	ipxproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx"
	ncpsvc "github.com/ObsoleteMadness/ClassicStack/core/service/ncp"
)

// clientNode / serverNode are the two IPX station addresses the bridge uses. The bridge
// models one point-to-point IPX link: a client request is delivered to the server's
// OverIPX.HandleDatagram, whose reply the capturing sender returns.
var (
	clientNode = [6]byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}
	serverNode = [6]byte{0x02, 0x00, 0x00, 0x00, 0x00, 0xFE}
)

var ncpSock = [2]byte{0x04, 0x51}

// bridge implements client/ncp.Transport by driving one server-side NCP-over-IPX
// transport: each Send wraps the request in an IPX datagram, hands it to
// OverIPX.HandleDatagram, and returns the reply the capturing sender recorded. It
// models one connectionless NCP circuit with no wire framing.
type bridge struct {
	svc   *ncpsvc.Service
	over  *ncpsvc.OverIPX
	mu    sync.Mutex
	reply []byte
}

// Send delivers one NCP request to the server transport and returns the captured reply.
func (b *bridge) Send(req []byte) ([]byte, error) {
	b.mu.Lock()
	b.reply = nil
	b.mu.Unlock()
	b.over.HandleDatagram(&ipxproto.Datagram{
		Type:    0x11,
		SrcNode: clientNode,
		SrcSock: ncpSock,
		DstNode: serverNode,
		DstSock: ncpSock,
		Payload: req,
	})
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.reply, nil
}

func (b *bridge) MaxPayload() int { return 1024 }
func (b *bridge) Close() error    { return nil }

// captureSender records the reply datagram's payload as the bridge's pending reply, so
// Send returns it. It is the server's IPXSender.
type captureSender struct{ b *bridge }

func (s captureSender) Send(d *ipxproto.Datagram) error {
	s.b.mu.Lock()
	s.b.reply = append([]byte(nil), d.Payload...)
	s.b.mu.Unlock()
	return nil
}

// newServer builds a running NCP service with a single memfs "SYS" volume, wired to a
// fresh over-IPX transport exposed as a client/ncp.Transport.
func newServer(t *testing.T) clientncp.Transport {
	t.Helper()
	svc := ncpsvc.New(nil)
	if err := svc.AddShare(fs.ShareSpec{
		Name: "SYS", FSType: "memfs",
		ForkBackend: "appledouble", FilenameCodec: "identity",
	}); err != nil {
		t.Fatalf("AddShare: %v", err)
	}
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = svc.Stop(context.Background()) })
	b := &bridge{svc: svc}
	b.over = svc.NewOverIPX(captureSender{b})
	return b
}

// connectClient opens an NCP session over the bridge and wraps the base FS with the
// same fork/meta stack client.Connect layers (the "appledouble" fork backend, since NCP
// has no native fork). It exercises the public client/ncp entry points (Open + New) plus
// the exact WrapBase the SDK uses.
func connectClient(t *testing.T, tr clientncp.Transport) fs.ForkFS {
	t.Helper()
	sess, err := clientncp.Open(tr, clientncp.DialParams{Volume: "SYS"})
	if err != nil {
		t.Fatalf("ncp.Open: %v", err)
	}
	store, err := metastore.Open("mem", "")
	if err != nil {
		t.Fatalf("metastore.Open: %v", err)
	}
	remote, err := fs.WrapBase(clientncp.New(sess), fs.ShareSpec{
		Name: "SYS", ForkBackend: "appledouble", FilenameCodec: "identity",
	}, store)
	if err != nil {
		t.Fatalf("WrapBase: %v", err)
	}
	t.Cleanup(func() { _ = fs.CloseFS(remote) })
	return remote
}

// TestNCP_InProcessE2E is the end-to-end gate: connect, seed a file with data + a
// resource fork + type/creator (all stored as AppleDouble sidecars over NCP data-fork
// I/O), copy it to a host dir and back, and assert bytes and metadata survive.
func TestNCP_InProcessE2E(t *testing.T) {
	tr := newServer(t)
	remote := connectClient(t, tr)

	// 1. Seed a file on the remote NCP volume: data fork + resource fork + type/creator.
	// NetWare is 8.3, so use an 8.3-clean name.
	data := []byte("the quick brown fox")
	rsrc := []byte("RESOURCE-FORK-CONTENTS")
	writeRemoteFile(t, remote, "REPORT.TXT", data, rsrc, "TEXT", "ttxt")

	// 2. List the volume root — the file must appear.
	entries, err := xfer.List(remote, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !hasEntry(entries, "REPORT.TXT") {
		t.Fatalf("REPORT.TXT not listed; entries=%+v", entries)
	}

	// 3. Copy remote → host directory (a local ForkFS), preserving forks + metadata.
	hostDir := t.TempDir()
	host := hostShare(t, hostDir)
	if err := xfer.Copy(remote, host, "REPORT.TXT", "REPORT.TXT"); err != nil {
		t.Fatalf("Copy remote→host: %v", err)
	}
	assertForkFile(t, host, "REPORT.TXT", data, rsrc, "TEXT", "ttxt")

	// 4. Copy host → remote under a new name, then read it back off the remote.
	if err := xfer.Copy(host, remote, "REPORT.TXT", "COPY.TXT"); err != nil {
		t.Fatalf("Copy host→remote: %v", err)
	}
	assertForkFile(t, remote, "COPY.TXT", data, rsrc, "TEXT", "ttxt")

	// 5. Rename then delete on the remote.
	if err := remote.Rename("COPY.TXT", "RENAMED.TXT"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if _, err := remote.Stat("RENAMED.TXT"); err != nil {
		t.Fatalf("Stat after rename: %v", err)
	}
	if err := xfer.Remove(remote, "RENAMED.TXT"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := remote.Stat("RENAMED.TXT"); err == nil {
		t.Fatalf("RENAMED.TXT still present after Remove")
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

func hasEntry(entries []xfer.Entry, name string) bool {
	for _, e := range entries {
		if e.Name == name {
			return true
		}
	}
	return false
}
