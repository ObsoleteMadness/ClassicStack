// Package e2e is the consolidated end-to-end gate for the ClassicStack client SDK: it
// stands up a REAL in-process server for every file protocol (AFP, SMB, NCP, EtherDFS)
// over every client transport that can run without a physical NIC or the Npcap/WinFsp
// kernel drivers (DDP, direct-IPX, NBIPX, NBF, TCP/NBT, raw-Ethernet-over-inmem), then
// drives the SAME file-operation battery through each, plus the WinFsp mount Adapter over
// one live server.
//
// It is deliberately a peer of the per-protocol e2e tests under client/*/e2e_test.go —
// those remain the focused per-transport gates. This package answers the different
// question the task poses: "create ONE server instance harness and exercise ALL client
// protocols + file operations through it," with a single shared operation battery so the
// coverage is identical across transports and lives in exactly one place.
//
// Live raw-Ethernet transports (EtherTalk/IPX/NBF on a real segment) and the WinFsp drive
// mount need a physical NIC, Npcap, two L2 stations, and (for the mount) the WinFsp kernel
// driver — none of which a unit test can provide. Those are covered by the manual runbook
// in README/mount docs; this harness proves the protocol engines + client stacks + file
// operations end to end in-process, which is what CI can guarantee.
//
// harness_test.go holds the shared, transport-agnostic pieces: the file-operation battery
// (exerciseFileOps) and the fork/metadata assertions. servers_test.go builds each
// protocol×transport server and returns a connected fs.ForkFS; e2e_test.go is the table.
package e2e

import (
	"bytes"
	"os"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/client/xfer"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// fileData / fileRsrc / fileType / fileCreator are the canonical payload the battery seeds
// on every transport: a data fork, a resource fork, and a Finder type/creator — the full
// AFP metadata surface every backend (native AFP forks or AppleDouble sidecars over
// SMB/NCP/EtherDFS) must round-trip.
var (
	fileData    = []byte("the quick brown fox jumps over the lazy dog")
	fileRsrc    = []byte("RESOURCE-FORK-CONTENTS-0123456789")
	fileType    = "TEXT"
	fileCreator = "ttxt"
)

// exerciseFileOps runs the full file-operation battery against a connected remote share:
// create-with-forks → list → copy remote→host → copy host→remote → rename → delete. It
// asserts bytes AND metadata (resource fork + type/creator) survive every hop, so a
// failure localises to the operation that broke rather than a single opaque round trip.
//
// names supplies the file names to use: some transports (NCP, EtherDFS) are DOS 8.3, so
// the caller passes 8.3-clean names; DDP/SMB callers pass long names.
func exerciseFileOps(t *testing.T, remote fs.ForkFS, names fileNames) {
	t.Helper()

	// 1. Create a file with data + resource fork + type/creator on the remote.
	writeRemoteFile(t, remote, names.seed, fileData, fileRsrc, fileType, fileCreator)

	// 2. List the root — the file must appear (with type/creator where the lister carries it).
	entries, err := xfer.List(remote, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !hasEntry(entries, names.seed) {
		t.Fatalf("%s not listed after create; entries=%+v", names.seed, entries)
	}

	// 3. Copy remote → a host directory (a local ForkFS), preserving forks + metadata.
	host := hostShare(t, t.TempDir())
	if err := xfer.Copy(remote, host, names.seed, names.seed); err != nil {
		t.Fatalf("Copy remote→host: %v", err)
	}
	assertForkFile(t, host, names.seed, fileData, fileRsrc, fileType, fileCreator)

	// 4. Copy host → remote under a new name, then read it back off the remote.
	if err := xfer.Copy(host, remote, names.seed, names.copy); err != nil {
		t.Fatalf("Copy host→remote: %v", err)
	}
	assertForkFile(t, remote, names.copy, fileData, fileRsrc, fileType, fileCreator)

	// 5. Rename the copy, confirm it moved, then delete it.
	if err := remote.Rename(names.copy, names.renamed); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if _, err := remote.Stat(names.renamed); err != nil {
		t.Fatalf("Stat after rename: %v", err)
	}
	if _, err := remote.Stat(names.copy); err == nil {
		t.Fatalf("%s still present after rename to %s", names.copy, names.renamed)
	}
	if err := xfer.Remove(remote, names.renamed); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := remote.Stat(names.renamed); err == nil {
		t.Fatalf("%s still present after Remove", names.renamed)
	}

	// 6. Directory create + nested file + directory delete, to exercise the dir path too.
	if err := remote.CreateDir(names.dir); err != nil {
		t.Fatalf("CreateDir %s: %v", names.dir, err)
	}
	nested := names.dir + "/" + names.seed
	writeRemoteFile(t, remote, nested, fileData, nil, "", "")
	if _, err := remote.Stat(nested); err != nil {
		t.Fatalf("Stat nested %s: %v", nested, err)
	}
	if err := xfer.Remove(remote, nested); err != nil {
		t.Fatalf("Remove nested: %v", err)
	}
	if err := remote.Remove(names.dir); err != nil {
		t.Fatalf("Remove dir %s: %v", names.dir, err)
	}
	if _, err := remote.Stat(names.dir); err == nil {
		t.Fatalf("%s still present after dir Remove", names.dir)
	}
}

// fileNames is the per-transport name set (long vs DOS 8.3).
type fileNames struct {
	seed    string
	copy    string
	renamed string
	dir     string
}

// longNames are used by the transports with a modern name space (DDP/SMB).
var longNames = fileNames{seed: "report.txt", copy: "copy.txt", renamed: "renamed.txt", dir: "subdir"}

// dosNames are the 8.3-clean set for the DOS-name transports (NCP, EtherDFS).
var dosNames = fileNames{seed: "REPORT.TXT", copy: "COPY.TXT", renamed: "RENAMED.TXT", dir: "SUBDIR"}

// writeRemoteFile seeds a file with a data fork, and (when rsrc is non-nil) a resource
// fork + Finder type/creator, exactly as the per-protocol e2e tests do.
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
	if rsrc == nil {
		return
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

// assertForkFile asserts a file's data fork, resource fork, and type/creator all match.
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

// hostShare builds a local_fs ForkFS over dir (the copy target/source), with the
// AppleDouble backend so sidecar metadata materialises the same way the SDK expects.
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
