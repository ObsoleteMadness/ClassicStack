package fs

import (
	"bytes"
	stdfs "io/fs"
	"os"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/appledouble"
	"github.com/ObsoleteMadness/ClassicStack/core/metastore"
)

// TestSidecarExport_DerezProjectsNativeForks proves the client-mount direction:
// a base with native forks + -fork derez synthesises .rdump/.idump in ReadDir and
// serves DeRez text / type-creator bytes through OpenFile — without those sidecars
// existing on the base FileSystem.
func TestSidecarExport_DerezProjectsNativeForks(t *testing.T) {
	base := newMemFS(ShareSpec{})
	if _, err := base.CreateFile("App"); err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	// Use AppleDouble as the "native" ForkEngine standing in for AFP passthrough.
	native := newAppleDoubleForkEngine(base, netatalkSidecarPath)
	rf, err := native.OpenFork("App", ResourceFork, os.O_RDWR|os.O_CREATE)
	if err != nil {
		t.Fatalf("OpenFork: %v", err)
	}
	bin := buildResFork(t, "CODE", 0, []byte{0x01, 0x02})
	if _, err := rf.WriteAt(bin, 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	if err := rf.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	var fi [32]byte
	copy(fi[0:4], "APPL")
	copy(fi[4:8], "ttxt")
	if err := native.WriteFinderInfo("App", fi); err != nil {
		t.Fatalf("WriteFinderInfo: %v", err)
	}

	export := newSidecarExportFS(base, native, "derez")

	// Base listing must NOT already contain .rdump (AppleDouble uses ._App).
	baseEnts, _ := base.ReadDir("")
	for _, e := range baseEnts {
		if e.Name() == "App.rdump" || e.Name() == "App.idump" {
			t.Fatalf("base unexpectedly has %q before export", e.Name())
		}
	}

	ents, err := export.ReadDir("")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	names := map[string]bool{}
	for _, e := range ents {
		names[e.Name()] = true
	}
	if !names["App"] {
		t.Fatal("missing data file App")
	}
	if !names["App.rdump"] {
		t.Fatal("missing projected App.rdump")
	}
	if !names["App.idump"] {
		t.Fatal("missing projected App.idump")
	}

	// Open the projected rdump and check it is DeRez text, not the binary fork.
	f, err := export.OpenFile("App.rdump", os.O_RDONLY)
	if err != nil {
		t.Fatalf("OpenFile rdump: %v", err)
	}
	defer f.Close()
	info, _ := f.Stat()
	buf := make([]byte, info.Size())
	n, _ := f.ReadAt(buf, 0)
	text := buf[:n]
	if !bytes.Contains(text, []byte("data 'CODE'")) {
		t.Fatalf("rdump is not DeRez text:\n%s", text)
	}
	if bytes.Equal(text, bin) {
		t.Fatal("rdump returned the binary fork verbatim")
	}

	id, err := export.OpenFile("App.idump", os.O_RDONLY)
	if err != nil {
		t.Fatalf("OpenFile idump: %v", err)
	}
	defer id.Close()
	ibuf := make([]byte, 8)
	_, _ = id.ReadAt(ibuf, 0)
	if string(ibuf[0:4]) != "APPL" || string(ibuf[4:8]) != "ttxt" {
		t.Fatalf("idump = %q, want APPL/ttxt", ibuf)
	}
}

// TestWrapBase_NativePlusDerezUsesExport verifies WrapBase keeps native OpenFork and
// projects sidecars when the base already implements ForkEngine.
func TestWrapBase_NativePlusDerezUsesExport(t *testing.T) {
	base := newMemFS(ShareSpec{})
	if _, err := base.CreateFile("X"); err != nil {
		t.Fatal(err)
	}
	native := newAppleDoubleForkEngine(base, netatalkSidecarPath)
	rf, err := native.OpenFork("X", ResourceFork, os.O_RDWR|os.O_CREATE)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = rf.WriteAt(buildResFork(t, "TEXT", 1, []byte("hi")), 0)
	_ = rf.Close()

	wrapped := &nativeForkBase{FileSystem: base, ForkEngine: native}
	store, err := metastore.Open("mem", "")
	if err != nil {
		t.Fatal(err)
	}
	share, err := WrapBase(wrapped, ShareSpec{ForkBackend: "derez", FilenameCodec: "identity"}, store)
	if err != nil {
		t.Fatalf("WrapBase: %v", err)
	}
	ents, err := share.ReadDir("")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	found := false
	for _, e := range ents {
		if e.Name() == "X.rdump" {
			found = true
		}
	}
	if !found {
		t.Fatal("WrapBase+derez did not project X.rdump")
	}
	// OpenFork must still hit the native engine (not try to read X.rdump from base).
	n, err := share.ForkLen("X", ResourceFork)
	if err != nil || n == 0 {
		t.Fatalf("ForkLen = %d, %v (native fork should still work)", n, err)
	}
}

// TestAppleDoubleExport_ListSizeFromRsrcLen proves the listing size for a projected
// ._name sidecar is Header+FinderInfo+rsrcLen from the enumerate fork length, without
// materialising the fork.
func TestAppleDoubleExport_ListSizeFromRsrcLen(t *testing.T) {
	a := appleDoubleExport{sidecar: netatalkSidecarPath}
	const rsrc = int64(256)
	got := a.listSize(exportAppleDouble, rsrc, true)
	want := int64(appledouble.ResourceForkStart) + rsrc
	if got != want {
		t.Fatalf("listSize = %d, want %d (ResourceForkStart=%d + rsrc)", got, want, appledouble.ResourceForkStart)
	}
	// Finder-only (empty resource entry still present in canonical Build).
	if got := a.listSize(exportAppleDouble, 0, true); got != int64(appledouble.ResourceForkStart) {
		t.Fatalf("finder-only listSize = %d, want %d", got, appledouble.ResourceForkStart)
	}
	if got := a.listSize(exportAppleDouble, 0, false); got != 0 {
		t.Fatalf("empty listSize = %d, want 0", got)
	}
}

// nativeForkBase is a FileSystem+ForkEngine pair for WrapBase export tests.
type nativeForkBase struct {
	FileSystem
	ForkEngine
}

// openForkCounter wraps a ForkEngine and counts OpenFork calls.
type openForkCounter struct {
	ForkEngine
	opens int
}

func (c *openForkCounter) OpenFork(path string, fork ForkType, flag int) (File, error) {
	c.opens++
	return c.ForkEngine.OpenFork(path, fork, flag)
}

// TestSidecarExport_ListingDoesNotOpenFork proves ReadDir and Stat on a projected
// sidecar use enumerate hints only; forks are read when the sidecar is opened.
func TestSidecarExport_ListingDoesNotOpenFork(t *testing.T) {
	base := newMemFS(ShareSpec{})
	if _, err := base.CreateFile("App"); err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	native := newAppleDoubleForkEngine(base, netatalkSidecarPath)
	rf, err := native.OpenFork("App", ResourceFork, os.O_RDWR|os.O_CREATE)
	if err != nil {
		t.Fatalf("OpenFork: %v", err)
	}
	payload := buildResFork(t, "TEXT", 1, []byte("hi"))
	if _, err := rf.WriteAt(payload, 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	_ = rf.Close()
	var fi [32]byte
	copy(fi[0:4], "APPL")
	copy(fi[4:8], "ttxt")
	if err := native.WriteFinderInfo("App", fi); err != nil {
		t.Fatalf("WriteFinderInfo: %v", err)
	}

	counter := &openForkCounter{ForkEngine: native}
	export := newSidecarExportFS(base, counter, "appledouble")

	if _, err := export.ReadDir(""); err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if counter.opens != 0 {
		t.Fatalf("ReadDir OpenFork calls = %d, want 0", counter.opens)
	}

	sfi, err := export.Stat("._App")
	if err != nil {
		t.Fatalf("Stat sidecar: %v", err)
	}
	want := int64(appledouble.ResourceForkStart) + int64(len(payload))
	if sfi.Size() != want {
		t.Fatalf("Stat size = %d, want %d", sfi.Size(), want)
	}
	if counter.opens != 0 {
		t.Fatalf("Stat OpenFork calls = %d, want 0", counter.opens)
	}

	f, err := export.OpenFile("._App", os.O_RDONLY)
	if err != nil {
		t.Fatalf("OpenFile sidecar: %v", err)
	}
	_ = f.Close()
	if counter.opens == 0 {
		t.Fatal("OpenFile sidecar did not OpenFork")
	}
}

func TestSidecarExport_SidecarInheritsHiddenAndTimes(t *testing.T) {
	base := newMemFS(ShareSpec{})
	if _, err := base.CreateFile("App"); err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	native := newAppleDoubleForkEngine(base, netatalkSidecarPath)
	rf, err := native.OpenFork("App", ResourceFork, os.O_RDWR|os.O_CREATE)
	if err != nil {
		t.Fatalf("OpenFork: %v", err)
	}
	if _, err := rf.WriteAt(buildResFork(t, "TEXT", 1, []byte("hi")), 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	_ = rf.Close()
	var finder [32]byte
	copy(finder[0:8], "APPLttxt")
	if err := native.WriteFinderInfo("App", finder); err != nil {
		t.Fatalf("WriteFinderInfo: %v", err)
	}

	srcMod := time.Date(2024, 7, 1, 2, 3, 4, 0, time.UTC)
	srcCreate := time.Date(2024, 6, 30, 20, 0, 0, 0, time.UTC)
	src := stubFileInfo{
		name:    "App",
		size:    10,
		modTime: srcMod,
		meta: stubMeta{
			attrs:  DOSReadOnly,
			create: srcCreate,
		},
	}
	export := &sidecarExportFS{
		FileSystem: statStubFS{info: src},
		native:     native,
		format:     appleDoubleExport{sidecar: netatalkSidecarPath},
	}

	fi, err := export.Stat("._App")
	if err != nil {
		t.Fatalf("Stat sidecar: %v", err)
	}
	if !fi.ModTime().Equal(srcMod) {
		t.Fatalf("ModTime = %v, want %v", fi.ModTime(), srcMod)
	}
	sys, ok := fi.Sys().(exportSidecarMeta)
	if !ok {
		t.Fatalf("Sys type = %T, want exportSidecarMeta", fi.Sys())
	}
	if !sys.create.Equal(srcCreate) {
		t.Fatalf("CreateTime = %v, want %v", sys.create, srcCreate)
	}
	if sys.attrs&DOSHidden == 0 {
		t.Fatalf("attrs %#x missing DOSHidden", sys.attrs)
	}
	if sys.attrs&DOSReadOnly == 0 {
		t.Fatalf("attrs %#x missing inherited DOSReadOnly", sys.attrs)
	}
}

type statStubFS struct {
	FileSystem
	info stdfs.FileInfo
}

func (s statStubFS) Stat(path string) (stdfs.FileInfo, error) {
	if path == "App" {
		return s.info, nil
	}
	return nil, stdfs.ErrNotExist
}

type stubMeta struct {
	attrs  uint16
	create time.Time
}

func (m stubMeta) DOSAttrs() uint16         { return m.attrs }
func (m stubMeta) DOSCreateTime() time.Time { return m.create }

type stubFileInfo struct {
	name    string
	size    int64
	modTime time.Time
	meta    stubMeta
}

func (fi stubFileInfo) Name() string         { return fi.name }
func (fi stubFileInfo) Size() int64          { return fi.size }
func (fi stubFileInfo) Mode() stdfs.FileMode { return 0o644 }
func (fi stubFileInfo) ModTime() time.Time   { return fi.modTime }
func (fi stubFileInfo) IsDir() bool          { return false }
func (fi stubFileInfo) Sys() any             { return fi.meta }
