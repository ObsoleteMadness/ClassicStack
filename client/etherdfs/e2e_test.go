package etherdfs_test

// e2e_test.go is the PRIMARY verification gate for the EtherDFS client: it wires the
// whole client stack (client/etherdfs fs adapter → session → client-direction codec →
// raw-Ethernet transport) to a REAL running core/service/etherdfs.Service over an
// in-memory Ethernet link pair, with a memfs drive. The server side is the genuine
// core/port/etherdfs.Port read loop driving the service dispatch and transmitting
// replies back over the same link — so the client's frame encoding, the sequence
// correlation, and the server-MAC discovery all run end to end. It then drives
// operations through client.Connect + client/xfer and asserts bytes AND the
// AppleDouble-carried metadata (resource fork, Finder type/creator — "._NAME" sidecars)
// survive a round trip out to a host dir and back.

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/adapter/link/inmem"
	clientetherdfs "github.com/ObsoleteMadness/ClassicStack/client/etherdfs"
	"github.com/ObsoleteMadness/ClassicStack/client/xfer"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/metastore"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
	etherport "github.com/ObsoleteMadness/ClassicStack/core/port/etherdfs"
	etherdfssvc "github.com/ObsoleteMadness/ClassicStack/core/service/etherdfs"
)

// serverMAC is the EtherDFS server station address the port stamps on replies; the
// client learns it from the first reply (its first request is broadcast).
var serverMAC = [6]byte{0x02, 0x00, 0x00, 0x00, 0x00, 0xED}

// newServer builds a running EtherDFS service over a real port whose link is one end of
// an in-memory pair, with a single memfs drive "C". It returns the client-side link end
// the transport dials.
func newServer(t *testing.T) link.FrameLink {
	t.Helper()
	serverEnd, clientEnd := inmem.Pair(64)

	sec := &port.Section{SKey: etherport.Name, IsEnabled: true}
	p, err := etherport.NewInstanceFromOpener(sec, func() (link.FrameLink, error) {
		return serverEnd, nil
	}, serverMAC, log.New(etherport.Name))
	if err != nil {
		t.Fatalf("NewInstanceFromOpener: %v", err)
	}
	svc := etherdfssvc.New(p, log.New(etherdfssvc.Name))
	if err := svc.ReconcileDrives([]etherdfssvc.DriveSpec{{
		Name: "C",
		Share: fs.ShareSpec{
			Name: "C", FSType: "memfs",
			ForkBackend: "appledouble", FilenameCodec: "identity",
		},
	}}); err != nil {
		t.Fatalf("ReconcileDrives: %v", err)
	}
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = svc.Stop(context.Background()) })
	return clientEnd
}

// connectClient opens an EtherDFS session over the client link end and wraps the base FS
// with the same fork/meta stack client.Connect layers (the "appledouble" fork backend,
// since EtherDFS has no native fork).
func connectClient(t *testing.T, clientEnd link.FrameLink) fs.ForkFS {
	t.Helper()
	tr := clientetherdfs.DialFrame(clientEnd, clientetherdfs.RandomMAC())
	sess, err := clientetherdfs.Open(tr, clientetherdfs.DialParams{Drive: "C"})
	if err != nil {
		t.Fatalf("etherdfs.Open: %v", err)
	}
	store, err := metastore.Open("mem", "")
	if err != nil {
		t.Fatalf("metastore.Open: %v", err)
	}
	remote, err := fs.WrapBase(clientetherdfs.New(sess), fs.ShareSpec{
		Name: "C", ForkBackend: "appledouble", FilenameCodec: "identity",
	}, store)
	if err != nil {
		t.Fatalf("WrapBase: %v", err)
	}
	t.Cleanup(func() { _ = fs.CloseFS(remote) })
	return remote
}

// TestEtherDFS_InProcessE2E is the end-to-end gate: connect, seed a file with data + a
// resource fork + type/creator (all stored as AppleDouble sidecars over EtherDFS
// data-fork I/O), copy it to a host dir and back, and assert bytes and metadata survive.
func TestEtherDFS_InProcessE2E(t *testing.T) {
	clientEnd := newServer(t)
	remote := connectClient(t, clientEnd)

	// DOS is 8.3, so use 8.3-clean names.
	data := []byte("the quick brown fox")
	rsrc := []byte("RESOURCE-FORK-CONTENTS")
	writeRemoteFile(t, remote, "REPORT.TXT", data, rsrc, "TEXT", "ttxt")

	entries, err := xfer.List(remote, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !hasEntry(entries, "REPORT.TXT") {
		t.Fatalf("REPORT.TXT not listed; entries=%+v", entries)
	}

	hostDir := t.TempDir()
	host := hostShare(t, hostDir)
	if err := xfer.Copy(remote, host, "REPORT.TXT", "REPORT.TXT"); err != nil {
		t.Fatalf("Copy remote→host: %v", err)
	}
	assertForkFile(t, host, "REPORT.TXT", data, rsrc, "TEXT", "ttxt")

	if err := xfer.Copy(host, remote, "REPORT.TXT", "COPY.TXT"); err != nil {
		t.Fatalf("Copy host→remote: %v", err)
	}
	assertForkFile(t, remote, "COPY.TXT", data, rsrc, "TEXT", "ttxt")

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
