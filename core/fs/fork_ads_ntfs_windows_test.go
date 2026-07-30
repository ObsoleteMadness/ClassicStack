//go:build windows

package fs

import (
	"bytes"
	"os"
	"testing"
)

// TestADSFactory_AcceptsNTFS confirms the ads factory's NTFS check PASSES over a real
// NTFS-backed local_fs (t.TempDir is on the NTFS system volume on the CI runners), so a
// correctly-configured share builds. If the runner's temp is ever not NTFS the volume
// probe would reject — skip rather than fail spuriously in that case.
func TestADSFactory_AcceptsNTFS(t *testing.T) {
	root := t.TempDir()
	if fsName, ok := volumeFilesystemName(root); !ok || fsName != "NTFS" {
		t.Skipf("temp volume is %q (ok=%v), not NTFS — skipping ads-accept check", fsName, ok)
	}
	base, err := newLocalFS(ShareSpec{Path: root}, nil)
	if err != nil {
		t.Fatalf("newLocalFS: %v", err)
	}
	eng, err := forkAdapterByName("ads", ShareSpec{Path: root}, base)
	if err != nil {
		t.Fatalf("forkAdapterByName(ads) over NTFS local_fs: %v", err)
	}
	if _, ok := eng.(*adsForkEngine); !ok {
		t.Fatalf("ads backend = %T, want *adsForkEngine", eng)
	}
}

// TestADSOverLocalFS_RealNTFS drives the ads fork engine over a real host directory
// (local_fs) so the :AFP_Resource / :AFP_AfpInfo / :Comments streams are REAL NTFS
// alternate data streams, and confirms create/read/move/delete all reach them.
func TestADSOverLocalFS_RealNTFS(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(root+`\doc`, []byte("data"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	base, err := newLocalFS(ShareSpec{Path: root}, nil)
	if err != nil {
		t.Fatalf("newLocalFS: %v", err)
	}
	e := newADSForkEngine(base)

	// Resource fork → real ADS.
	rf, err := e.OpenFork("doc", ResourceFork, os.O_RDWR|os.O_CREATE)
	if err != nil {
		t.Fatalf("OpenFork(resource): %v", err)
	}
	rsrc := []byte("RESOURCE-FORK")
	if _, err := rf.WriteAt(rsrc, 0); err != nil {
		t.Fatalf("write rsrc: %v", err)
	}
	_ = rf.Sync()
	_ = rf.Close()

	// It must be a real ADS on the host file, invisible to ReadDir.
	if _, err := os.Stat(root + `\doc:AFP_Resource`); err != nil {
		t.Fatalf("host ADS not present: %v", err)
	}
	ents, _ := os.ReadDir(root)
	if len(ents) != 1 || ents[0].Name() != "doc" {
		t.Fatalf("ADS leaked into ReadDir: %v", ents)
	}
	if n, _ := e.ForkLen("doc", ResourceFork); n != int64(len(rsrc)) {
		t.Fatalf("ForkLen(resource) = %d, want %d", n, len(rsrc))
	}

	// FinderInfo → AFP_AfpInfo ADS.
	var finder [32]byte
	copy(finder[:], "TEXTttxt")
	if err := e.WriteFinderInfo("doc", finder); err != nil {
		t.Fatalf("WriteFinderInfo: %v", err)
	}
	if got, ok, _ := e.ReadFinderInfo("doc"); !ok || got != finder {
		t.Fatalf("FinderInfo round-trip failed: ok=%v", ok)
	}

	// Comment → :Comments ADS.
	if err := e.WriteComment("doc", []byte("hello")); err != nil {
		t.Fatalf("WriteComment: %v", err)
	}
	if c, ok := e.ReadComment("doc"); !ok || !bytes.Equal(c, []byte("hello")) {
		t.Fatalf("comment round-trip failed: ok=%v got=%q", ok, c)
	}

	// Move the data file + its metadata streams.
	if err := base.Rename("doc", "doc2"); err != nil {
		t.Fatalf("rename data: %v", err)
	}
	if err := e.MoveMetadata("doc", "doc2"); err != nil {
		t.Fatalf("MoveMetadata: %v", err)
	}
	if c, ok := e.ReadComment("doc2"); !ok || !bytes.Equal(c, []byte("hello")) {
		t.Errorf("comment lost after move: ok=%v", ok)
	}
	if got, ok, _ := e.ReadFinderInfo("doc2"); !ok || got != finder {
		t.Errorf("FinderInfo lost after move: ok=%v", ok)
	}
	if n, _ := e.ForkLen("doc2", ResourceFork); n != int64(len(rsrc)) {
		t.Errorf("resource fork lost after move: len=%d", n)
	}

	// Delete metadata clears all three streams.
	if err := e.DeleteMetadata("doc2"); err != nil {
		t.Fatalf("DeleteMetadata: %v", err)
	}
	if _, ok := e.ReadComment("doc2"); ok {
		t.Error("comment survived DeleteMetadata")
	}
	if _, ok, _ := e.ReadFinderInfo("doc2"); ok {
		t.Error("FinderInfo survived DeleteMetadata")
	}
}
