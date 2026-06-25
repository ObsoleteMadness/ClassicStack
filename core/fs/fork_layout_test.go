package fs

import (
	"bytes"
	"os"
	"testing"
)

// TestSidecarPath_PerLayout pins the store path each layout function computes for a data
// path, including the root case (no directory) — the only thing that varies between the
// AppleDouble-family adapters.
func TestSidecarPath_PerLayout(t *testing.T) {
	cases := []struct {
		name        string
		sidecar     func(string) string
		data        string
		wantSidecar string
	}{
		{"default", netatalkSidecarPath, "file.txt", "._file.txt"},
		{"default", netatalkSidecarPath, "dir/file.txt", "dir/._file.txt"},
		{"default", netatalkSidecarPath, "a/b/c.txt", "a/b/._c.txt"},
		{"osxzip", osxZipSidecarPath, "file.txt", "__MACOSX/._file.txt"},
		{"osxzip", osxZipSidecarPath, "dir/file.txt", "__MACOSX/dir/._file.txt"},
		{"dir", appleDoubleDirSidecarPath, "file.txt", ".AppleDouble/file.txt"},
		{"dir", appleDoubleDirSidecarPath, "dir/file.txt", "dir/.AppleDouble/file.txt"},
	}
	for _, c := range cases {
		if got := c.sidecar(c.data); got != c.wantSidecar {
			t.Errorf("%s sidecar(%q) = %q, want %q", c.name, c.data, got, c.wantSidecar)
		}
	}
}

// TestForkRegistry_AppleDoubleFamily proves each layout is its OWN registered adapter
// and that the plain/alias names resolve to the default "._name" layout.
func TestForkRegistry_AppleDoubleFamily(t *testing.T) {
	wants := map[string]string{
		ForkAppleDoubleDefault: "dir/._report",
		ForkAppleDoubleOSXZip:  "__MACOSX/dir/._report",
		ForkAppleDoubleDir:     "dir/.AppleDouble/report",
		"appledouble":          "dir/._report", // alias of default
		"auto":                 "dir/._report",
		"native":               "dir/._report",
	}
	for name, wantSidecar := range wants {
		base := newMemFS(ShareSpec{})
		eng, err := forkAdapterByName(name, ShareSpec{}, base)
		if err != nil {
			t.Fatalf("forkAdapterByName(%q): %v", name, err)
		}
		var fi [32]byte
		copy(fi[:], "TEXTttxt")
		if err := eng.WriteFinderInfo("dir/report", fi); err != nil {
			t.Fatalf("%s WriteFinderInfo: %v", name, err)
		}
		if _, err := base.Stat(wantSidecar); err != nil {
			t.Fatalf("%s: sidecar not at %q: %v", name, wantSidecar, err)
		}
	}
}

// TestAppleDoubleLayout_RoundTripPerLayout round-trips a resource fork + FinderInfo
// through the base engine under EACH layout function, asserting the sidecar lands at the
// layout's expected store path (not the default). Proves the payload codec is
// layout-independent — only the container location moves.
func TestAppleDoubleLayout_RoundTripPerLayout(t *testing.T) {
	cases := []struct {
		name        string
		sidecar     func(string) string
		dataPath    string
		wantSidecar string
	}{
		{"default", netatalkSidecarPath, "dir/report", "dir/._report"},
		{"osxzip", osxZipSidecarPath, "dir/report", "__MACOSX/dir/._report"},
		{"dir", appleDoubleDirSidecarPath, "dir/report", "dir/.AppleDouble/report"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			base := newMemFS(ShareSpec{})
			eng := newAppleDoubleForkEngine(base, c.sidecar)

			var fi [32]byte
			copy(fi[:], "TEXTttxt")
			if err := eng.WriteFinderInfo(c.dataPath, fi); err != nil {
				t.Fatalf("WriteFinderInfo: %v", err)
			}
			rf, err := eng.OpenFork(c.dataPath, ResourceFork, os.O_RDWR|os.O_CREATE)
			if err != nil {
				t.Fatalf("OpenFork(resource): %v", err)
			}
			payload := []byte("resource-fork-bytes")
			if _, err := rf.WriteAt(payload, 0); err != nil {
				t.Fatalf("resource WriteAt: %v", err)
			}
			if err := rf.Close(); err != nil {
				t.Fatalf("resource Close: %v", err)
			}

			if _, err := base.Stat(c.wantSidecar); err != nil {
				t.Fatalf("sidecar not at %q: %v", c.wantSidecar, err)
			}
			if c.name != "default" {
				if _, err := base.Stat("dir/._report"); err == nil {
					t.Fatalf("sidecar unexpectedly also at the default path")
				}
			}

			gotFI, ok, err := eng.ReadFinderInfo(c.dataPath)
			if err != nil || !ok || gotFI != fi {
				t.Fatalf("ReadFinderInfo = %v ok=%v err=%v, want %v", gotFI, ok, err, fi)
			}
			rr, err := eng.OpenFork(c.dataPath, ResourceFork, os.O_RDONLY)
			if err != nil {
				t.Fatalf("re-OpenFork: %v", err)
			}
			got := make([]byte, len(payload))
			if _, err := rr.ReadAt(got, 0); err != nil {
				t.Fatalf("resource ReadAt: %v", err)
			}
			rr.Close()
			if !bytes.Equal(got, payload) {
				t.Fatalf("resource round-trip = %q, want %q", got, payload)
			}
		})
	}
}

// TestAppleDoubleLayout_MoveAndDeleteFollowLayout proves MoveMetadata/DeleteMetadata
// operate on the configured layout's sidecar path, not the hardcoded default one.
func TestAppleDoubleLayout_MoveAndDeleteFollowLayout(t *testing.T) {
	base := newMemFS(ShareSpec{})
	eng := newAppleDoubleForkEngine(base, osxZipSidecarPath)

	var fi [32]byte
	copy(fi[:], "TEXTttxt")
	if err := eng.WriteFinderInfo("dir/a", fi); err != nil {
		t.Fatalf("WriteFinderInfo: %v", err)
	}
	if _, err := base.Stat("__MACOSX/dir/._a"); err != nil {
		t.Fatalf("sidecar not created at osxzip path: %v", err)
	}

	if err := eng.MoveMetadata("dir/a", "dir/b"); err != nil {
		t.Fatalf("MoveMetadata: %v", err)
	}
	if _, err := base.Stat("__MACOSX/dir/._a"); err == nil {
		t.Fatal("old sidecar still present after MoveMetadata")
	}
	if _, err := base.Stat("__MACOSX/dir/._b"); err != nil {
		t.Fatalf("sidecar not moved to new osxzip path: %v", err)
	}

	if err := eng.DeleteMetadata("dir/b"); err != nil {
		t.Fatalf("DeleteMetadata: %v", err)
	}
	if _, err := base.Stat("__MACOSX/dir/._b"); err == nil {
		t.Fatal("sidecar still present after DeleteMetadata")
	}
}

// TestBuildShare_SelectsAppleDoubleVariant proves a share built with a hyphenated
// AppleDouble adapter name uses that layout, and that "appledouble" stays the default.
func TestBuildShare_SelectsAppleDoubleVariant(t *testing.T) {
	ffs, err := BuildShare(ShareSpec{FSType: "memfs", ForkBackend: ForkAppleDoubleOSXZip}, nil)
	if err != nil {
		t.Fatalf("BuildShare(%s): %v", ForkAppleDoubleOSXZip, err)
	}
	var fi [32]byte
	copy(fi[:], "TEXTttxt")
	if err := ffs.WriteFinderInfo("dir/a", fi); err != nil {
		t.Fatalf("WriteFinderInfo: %v", err)
	}
	if _, err := ffs.Stat("__MACOSX/dir/._a"); err != nil {
		t.Fatalf("BuildShare did not apply osxzip layout: %v", err)
	}

	// Plain "appledouble" is the default "._name" layout.
	def, err := BuildShare(ShareSpec{FSType: "memfs", ForkBackend: "appledouble"}, nil)
	if err != nil {
		t.Fatalf("BuildShare(appledouble): %v", err)
	}
	if err := def.WriteFinderInfo("dir/a", fi); err != nil {
		t.Fatalf("default WriteFinderInfo: %v", err)
	}
	if _, err := def.Stat("dir/._a"); err != nil {
		t.Fatalf("appledouble alias is not the default layout: %v", err)
	}
}
