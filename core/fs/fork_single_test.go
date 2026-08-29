package fs

import (
	"bytes"
	"io/fs"
	"os"
	"testing"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
)

// writeFork is a helper: open a fork for create+write and flush it.
func writeFork(t *testing.T, eng ForkEngine, path string, fork ForkType, data []byte) {
	t.Helper()
	f, err := eng.OpenFork(path, fork, os.O_RDWR|os.O_CREATE)
	if err != nil {
		t.Fatalf("OpenFork(%v): %v", fork, err)
	}
	if _, err := f.WriteAt(data, 0); err != nil {
		t.Fatalf("fork WriteAt: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("fork Close: %v", err)
	}
}

// readFork reads a whole fork back.
func readFork(t *testing.T, eng ForkEngine, path string, fork ForkType) []byte {
	t.Helper()
	f, err := eng.OpenFork(path, fork, os.O_RDONLY)
	if err != nil {
		t.Fatalf("OpenFork(%v) read: %v", fork, err)
	}
	defer f.Close()
	n, _ := eng.ForkLen(path, fork)
	buf := make([]byte, n)
	if n > 0 {
		if _, err := f.ReadAt(buf, 0); err != nil {
			t.Fatalf("fork ReadAt: %v", err)
		}
	}
	return buf
}

// TestAppleSingle_RoundTrip proves the single-container engine round-trips the data
// fork, resource fork, FinderInfo, and comment through one file.
func TestAppleSingle_RoundTrip(t *testing.T) {
	base := newMemFS(ShareSpec{})
	eng, err := forkAdapterByName("applesingle", ShareSpec{}, base)
	if err != nil {
		t.Fatalf("forkAdapterByName(applesingle): %v", err)
	}

	dataPayload := []byte("the data fork contents")
	rsrcPayload := []byte("RESOURCE-FORK")
	writeFork(t, eng, "doc", DataFork, dataPayload)
	writeFork(t, eng, "doc", ResourceFork, rsrcPayload)

	var fi [32]byte
	copy(fi[:], "TEXTttxt")
	if err := eng.WriteFinderInfo("doc", fi); err != nil {
		t.Fatalf("WriteFinderInfo: %v", err)
	}
	if err := eng.WriteComment("doc", []byte("hello")); err != nil {
		t.Fatalf("WriteComment: %v", err)
	}

	if got := readFork(t, eng, "doc", DataFork); !bytes.Equal(got, dataPayload) {
		t.Fatalf("data fork = %q, want %q", got, dataPayload)
	}
	if got := readFork(t, eng, "doc", ResourceFork); !bytes.Equal(got, rsrcPayload) {
		t.Fatalf("resource fork = %q, want %q", got, rsrcPayload)
	}
	if got, ok, _ := eng.ReadFinderInfo("doc"); !ok || got != fi {
		t.Fatalf("FinderInfo = %v ok=%v, want %v", got, ok, fi)
	}
	if got, ok := eng.ReadComment("doc"); !ok || string(got) != "hello" {
		t.Fatalf("comment = %q ok=%v, want hello", got, ok)
	}

	// Everything lives in ONE file: only "doc" exists in the base, no sidecar.
	ents, _ := base.ReadDir("")
	if len(ents) != 1 || ents[0].Name() != "doc" {
		t.Fatalf("base entries = %v, want exactly [doc] (single container)", names(ents))
	}
	// MetadataPaths is nil — nothing separate to coordinate.
	if mp := eng.(ForkContainers).MetadataPaths("doc"); mp != nil {
		t.Fatalf("applesingle MetadataPaths = %v, want nil", mp)
	}
}

// TestAppleSingle_ResourceForkIs4KAllocated proves the encoder honours Apple's 4K
// resource-fork allocation (a hole after the resource entry) and that the data fork is
// placed LAST so it can grow at EOF — per the AppleSingle writing recommendations.
func TestAppleSingle_ResourceForkIs4KAllocated(t *testing.T) {
	base := newMemFS(ShareSpec{})
	eng := newAppleSingleForkEngine(base)
	writeFork(t, eng, "doc", ResourceFork, []byte("small resource"))
	writeFork(t, eng, "doc", DataFork, []byte("DATA-AT-END"))

	raw, err := readWhole(base, "doc")
	if err != nil {
		t.Fatalf("readWhole: %v", err)
	}
	c, err := decodeAppleSingle(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(c.resource) != "small resource" || string(c.data) != "DATA-AT-END" {
		t.Fatalf("decoded forks wrong: rsrc=%q data=%q", c.resource, c.data)
	}

	// Find the resource and data entry offsets in the header; the data fork must start
	// at the resource entry's 4K-rounded boundary (proving the hole), and be the last
	// payload in the file.
	rOff, _ := entryOffLen(raw, asEntryResourceFork)
	dOff, dLen := entryOffLen(raw, asEntryDataFork)
	fOff, _ := entryOffLen(raw, asEntryFinderInfo)
	if rOff == 0 || dOff == 0 || fOff == 0 {
		t.Fatal("resource/data/finderinfo entries missing")
	}
	if dOff != rOff+asResourceChunk {
		t.Fatalf("data fork at %d, want resource(%d)+4K=%d (4K hole not honoured)", dOff, rOff, rOff+asResourceChunk)
	}
	if int(dOff)+int(dLen) != len(raw) {
		t.Fatalf("data fork not last: ends at %d, file len %d", int(dOff)+int(dLen), len(raw))
	}
	// FinderInfo (a frequently-read entry) sits closest to the header — its payload
	// starts immediately after the entry descriptors, before resource and data.
	if fOff >= rOff || fOff >= dOff {
		t.Fatalf("FinderInfo at %d not closest to header (resource %d, data %d)", fOff, rOff, dOff)
	}
}

// TestMacBinary_RoundTrip proves the MacBinary engine round-trips both forks and the
// type/creator through one 128-byte-header container file.
func TestMacBinary_RoundTrip(t *testing.T) {
	base := newMemFS(ShareSpec{})
	eng, err := forkAdapterByName("macbinary", ShareSpec{}, base)
	if err != nil {
		t.Fatalf("forkAdapterByName(macbinary): %v", err)
	}

	dataPayload := []byte("macbinary data fork")
	rsrcPayload := []byte("macbinary resource")
	writeFork(t, eng, "app", DataFork, dataPayload)
	writeFork(t, eng, "app", ResourceFork, rsrcPayload)

	var fi [32]byte
	copy(fi[0:4], "APPL")
	copy(fi[4:8], "MACS")
	if err := eng.WriteFinderInfo("app", fi); err != nil {
		t.Fatalf("WriteFinderInfo: %v", err)
	}

	if got := readFork(t, eng, "app", DataFork); !bytes.Equal(got, dataPayload) {
		t.Fatalf("data fork = %q, want %q", got, dataPayload)
	}
	if got := readFork(t, eng, "app", ResourceFork); !bytes.Equal(got, rsrcPayload) {
		t.Fatalf("resource fork = %q, want %q", got, rsrcPayload)
	}
	// MacBinary carries only type/creator/flags in FinderInfo[0:9].
	got, ok, _ := eng.ReadFinderInfo("app")
	if !ok || !bytes.Equal(got[0:8], fi[0:8]) {
		t.Fatalf("FinderInfo type/creator = %q ok=%v, want %q", got[0:8], ok, fi[0:8])
	}

	// Single container: only "app" exists.
	ents, _ := base.ReadDir("")
	if len(ents) != 1 || ents[0].Name() != "app" {
		t.Fatalf("base entries = %v, want exactly [app]", names(ents))
	}
	if mp := eng.(ForkContainers).MetadataPaths("app"); mp != nil {
		t.Fatalf("macbinary MetadataPaths = %v, want nil", mp)
	}
}

// TestMacBinary_RejectsNonMacBinary proves a plain (non-MacBinary) file is not silently
// decoded — the engine reports no forks rather than corrupting it.
func TestMacBinary_RejectsNonMacBinary(t *testing.T) {
	base := newMemFS(ShareSpec{})
	// Seed a plain file that is not a valid MacBinary container.
	f, _ := base.CreateFile("plain")
	_, _ = f.WriteAt([]byte("just some text, not macbinary at all....."), 0)
	_ = f.Close()

	eng := newMacBinaryForkEngine(base)
	if _, err := eng.OpenFork("plain", ResourceFork, os.O_RDONLY); err == nil {
		t.Fatal("OpenFork on a non-macbinary file: expected error, got nil")
	}
}

// --- small helpers ---

func names(ents []fs.DirEntry) []string {
	out := make([]string, len(ents))
	for i, e := range ents {
		out[i] = e.Name()
	}
	return out
}

// entryOffLen scans an AppleSingle header for the given entry ID, returning its offset
// and length (0,0 if absent).
func entryOffLen(b []byte, id uint32) (off, ln uint32) {
	if len(b) < asHeaderSize {
		return 0, 0
	}
	n := int(bp.BE16(b[24:26]))
	for i := 0; i < n; i++ {
		d := asHeaderSize + i*asEntrySize
		if d+asEntrySize > len(b) {
			return 0, 0
		}
		if bp.BE32(b[d:d+4]) == id {
			return bp.BE32(b[d+4 : d+8]), bp.BE32(b[d+8 : d+12])
		}
	}
	return 0, 0
}
