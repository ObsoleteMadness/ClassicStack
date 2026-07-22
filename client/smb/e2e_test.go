package smb_test

// e2e_test.go is the PRIMARY verification gate for the SMB client: it wires the whole
// client stack (client/smb fs adapter → session → client-direction codec) to a REAL
// running core/service/smb.Service over an in-process message bridge, with a memfs
// share. It drives operations through client.Connect + client/xfer and asserts bytes
// AND the AppleDouble-carried metadata (resource fork, Finder type/creator — stored as
// "._name" sidecars over the data fork) survive a round trip out to a host dir and
// back. Because SMB has no native fork, the whole fork story here is the AppleDouble
// backend reading/writing sidecar FILES over ordinary OPEN/READ/WRITE — the exact
// interop the client must get right.

import (
	"bytes"
	"context"
	"os"
	"testing"

	_ "github.com/ObsoleteMadness/ClassicStack/client/smb" // register the smb scheme
	clientsmb "github.com/ObsoleteMadness/ClassicStack/client/smb"
	"github.com/ObsoleteMadness/ClassicStack/client/xfer"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/metastore"
	smbsvc "github.com/ObsoleteMadness/ClassicStack/core/service/smb"
)

// bridge implements client/smb.Transport by driving one server-side SMB circuit: each
// Send hands the request straight to Conn.ServeMessage and returns its reply bytes.
// It models one SMB virtual circuit with no wire framing (the client sends whole SMB
// messages, the server returns whole reply messages), which is all a single
// client↔server session needs.
type bridge struct {
	conn smbsvc.SessionCircuit
}

func (b *bridge) Send(req []byte) ([]byte, error) { return b.conn.ServeMessage(req), nil }
func (b *bridge) MaxResponse() int                { return 1 << 20 } // in-process: no datagram limit
func (b *bridge) Close() error                    { b.conn.Close(); return nil }

// newServer builds a running SMB service with a single memfs "Share", wired to a fresh
// circuit exposed as a client/smb.Transport.
func newServer(t *testing.T) clientsmb.Transport {
	t.Helper()
	svc, err := smbsvc.NewWithShares(nil, smbsvc.ShareSpec{
		Name: "Share",
		Share: fs.ShareSpec{
			Name: "Share", FSType: "memfs",
			ForkBackend: "appledouble", FilenameCodec: "identity",
		},
	})
	if err != nil {
		t.Fatalf("NewWithShares: %v", err)
	}
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	conn := svc.NewConn("e2e")
	t.Cleanup(func() { conn.Close() })
	return &bridge{conn: conn}
}

// connectClient opens an SMB session over the bridge and wraps the base FS with the
// same fork/meta stack client.Connect layers (the "appledouble" fork backend, since SMB
// has no native fork). It exercises the public client/smb entry points (Open + New) plus
// the exact WrapBase the SDK uses, without a scheme registry round trip (which would
// need a transport-returning Opener the link package can't build without importing smb).
func connectClient(t *testing.T, tr clientsmb.Transport) fs.ForkFS {
	t.Helper()
	sess, err := clientsmb.Open(tr, clientsmb.DialParams{ServerName: "server", Share: "Share"})
	if err != nil {
		t.Fatalf("smb.Open: %v", err)
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

// TestSMB_InProcessE2E is the end-to-end gate: connect, seed a file with data + a
// resource fork + type/creator (all stored as AppleDouble sidecars over SMB data-fork
// I/O), copy it to a host dir and back, and assert bytes and metadata survive.
func TestSMB_InProcessE2E(t *testing.T) {
	tr := newServer(t)
	remote := connectClient(t, tr)

	// 1. Seed a file on the remote SMB share: data fork + resource fork + type/creator.
	data := []byte("the quick brown fox")
	rsrc := []byte("RESOURCE-FORK-CONTENTS")
	writeRemoteFile(t, remote, "report.txt", data, rsrc, "TEXT", "ttxt")

	// 2. List the share root — the file must appear.
	entries, err := xfer.List(remote, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !hasEntry(entries, "report.txt") {
		t.Fatalf("report.txt not listed; entries=%+v", entries)
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
