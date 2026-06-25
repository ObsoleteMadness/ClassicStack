package fs

import "testing"

// TestForkContainers_AppleDoubleReportsSidecar proves each AppleDouble-family adapter
// implements fs.ForkContainers and reports exactly its sidecar path (per layout).
func TestForkContainers_AppleDoubleReportsSidecar(t *testing.T) {
	cases := []struct {
		sidecar func(string) string
		want    string
	}{
		{netatalkSidecarPath, "dir/._file"},
		{osxZipSidecarPath, "__MACOSX/dir/._file"},
		{appleDoubleDirSidecarPath, "dir/.AppleDouble/file"},
	}
	for _, c := range cases {
		var eng ForkEngine = newAppleDoubleForkEngine(newMemFS(ShareSpec{}), c.sidecar)
		fc, ok := eng.(ForkContainers)
		if !ok {
			t.Fatalf("appledouble engine does not implement ForkContainers")
		}
		got := fc.MetadataPaths("dir/file")
		if len(got) != 1 || got[0] != c.want {
			t.Fatalf("MetadataPaths = %v, want [%q]", got, c.want)
		}
	}
}

// TestForkContainers_RideWithFileAdaptersReturnNil proves the adapters whose metadata
// rides with the data file expose no separate container: nofork implements
// ForkContainers returning nil OR does not implement it (both mean "no containers"),
// and ads/xattr likewise. shareFS.MetadataPaths must yield nil for them.
func TestForkContainers_RideWithFileAdaptersReturnNil(t *testing.T) {
	for _, name := range []string{"nofork", "ads", "xattr"} {
		eng, err := forkAdapterByName(name, ShareSpec{}, newMemFS(ShareSpec{}))
		if err != nil {
			t.Fatalf("forkAdapterByName(%q): %v", name, err)
		}
		if fc, ok := eng.(ForkContainers); ok {
			if got := fc.MetadataPaths("dir/file"); got != nil {
				t.Fatalf("%s MetadataPaths = %v, want nil (metadata rides with the file)", name, got)
			}
		}
	}
}

// TestShareFS_MetadataPathsForwards proves the assembled share stack forwards the
// optional ForkContainers capability to the fork adapter, and returns nil when the
// adapter does not provide it.
func TestShareFS_MetadataPathsForwards(t *testing.T) {
	// AppleDouble share: the sidecar path is reported through shareFS.
	ad, err := BuildShare(ShareSpec{FSType: "memfs", ForkBackend: ForkAppleDoubleOSXZip}, nil)
	if err != nil {
		t.Fatalf("BuildShare appledouble: %v", err)
	}
	fc, ok := ad.(ForkContainers)
	if !ok {
		t.Fatal("appledouble share does not expose ForkContainers")
	}
	if got := fc.MetadataPaths("dir/a"); len(got) != 1 || got[0] != "__MACOSX/dir/._a" {
		t.Fatalf("shareFS.MetadataPaths = %v, want [__MACOSX/dir/._a]", got)
	}

	// nofork share: no separate container.
	nf, err := BuildShare(ShareSpec{FSType: "memfs", ForkBackend: "nofork"}, nil)
	if err != nil {
		t.Fatalf("BuildShare nofork: %v", err)
	}
	if fc, ok := nf.(ForkContainers); ok {
		if got := fc.MetadataPaths("dir/a"); got != nil {
			t.Fatalf("nofork shareFS.MetadataPaths = %v, want nil", got)
		}
	}
}
