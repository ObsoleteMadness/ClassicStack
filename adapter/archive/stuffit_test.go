package archive

import (
	"os"
	"testing"
)

// realSample loads a real StuffIt sample archive checked into the
// classicstack-web submodule (src/fs/testdata and the welcome-volume
// Utilities folder — both already vetted as StuffIt fixtures by that
// project's own test suite). Skips, rather than fails, when the submodule
// hasn't been checked out (`git submodule update --init --recursive`).
func realSample(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("sample not available (submodule not checked out?): %v", err)
	}
	return data
}

const (
	sampleSIT1Small = "../../third_party/classicstack-web/src/fs/testdata/stuffit45.sit"
	sampleSIT5Mac   = "../../third_party/classicstack-web/src/fs/testdata/stuffit5-mac.sit"
	sampleSIT5Win   = "../../third_party/classicstack-web/src/fs/testdata/stuffit5-win.sit"
	sampleSIT1Real  = "../../third_party/classicstack-web/public/welcome/Utilities/Disk-Copy-42.sit"
)

// TestSniff_StuffItSamples pins Sniff's current (extension-based) detection
// of real StuffIt archives: it already returns true today purely from the
// ".sit" name, independent of expandStuffIt's stub body below.
func TestSniff_StuffItSamples(t *testing.T) {
	for _, path := range []string{sampleSIT1Small, sampleSIT5Mac, sampleSIT5Win, sampleSIT1Real} {
		data := realSample(t, path)
		if !Sniff(path, [32]byte{}, data) {
			t.Errorf("Sniff(%s) = false, want true", path)
		}
	}
}

// findNode looks up a child by name in a Node slice (top-level roots or a
// directory's Children), for asserting specific entries by name below.
func findNode(nodes []Node, name string) (Node, bool) {
	for _, n := range nodes {
		if n.Name == name {
			return n, true
		}
	}
	return Node{}, false
}

// TestExpand_StuffItClassicSmall extracts a real classic SIT! archive (flat,
// no subdirectories: mixed data-only, resource-only, and data+resource
// entries — see the `stuffit list` output this pins) and checks the tree
// Expand returns matches the archive's actual contents, not just that it
// no longer errors.
func TestExpand_StuffItClassicSmall(t *testing.T) {
	data := realSample(t, sampleSIT1Small)
	roots, err := Expand(sampleSIT1Small, data, nil, [32]byte{})
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if len(roots) != 6 {
		t.Fatalf("got %d root entries, want 6: %+v", len(roots), roots)
	}

	txt, ok := findNode(roots, "testfile.txt")
	if !ok {
		t.Fatal("testfile.txt not found")
	}
	if got := string(txt.Data); len(got) == 0 {
		t.Error("testfile.txt: empty data fork, want real content")
	}
	if len(txt.Resource) == 0 {
		t.Error("testfile.txt: empty resource fork, want real content (rsrc=332 per `stuffit list`)")
	}

	img, ok := findNode(roots, "Test Image")
	if !ok {
		t.Fatal("Test Image not found")
	}
	if len(img.Data) != 0 {
		t.Errorf("Test Image: data fork = %d bytes, want 0 (data-less entry, rsrc only)", len(img.Data))
	}
	if len(img.Resource) == 0 {
		t.Error("Test Image: empty resource fork, want real content (rsrc=9134 per `stuffit list`)")
	}
}

// TestExpand_StuffIt5Mac extracts a real StuffIt 5 (Arsenic-compressed)
// archive with the same flat 6-entry layout as the SIT1 sample above, so the
// tree shape should match even though the wire format and every fork's
// compression method differ.
func TestExpand_StuffIt5Mac(t *testing.T) {
	data := realSample(t, sampleSIT5Mac)
	roots, err := Expand(sampleSIT5Mac, data, nil, [32]byte{})
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if len(roots) != 6 {
		t.Fatalf("got %d root entries, want 6: %+v", len(roots), roots)
	}
	jpg, ok := findNode(roots, "testfile.jpg")
	if !ok {
		t.Fatal("testfile.jpg not found")
	}
	if len(jpg.Data) == 0 {
		t.Error("testfile.jpg: empty data fork, want real content (data=220 per `stuffit list`)")
	}
}

// TestExpand_StuffIt5Win extracts a real StuffIt 5 archive whose entries sit
// under one subdirectory ("sources/"), pinning that Expand reconstructs the
// nested tree (not just a flat file list) for the SIT5 format too.
func TestExpand_StuffIt5Win(t *testing.T) {
	data := realSample(t, sampleSIT5Win)
	roots, err := Expand(sampleSIT5Win, data, nil, [32]byte{})
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if len(roots) != 1 {
		t.Fatalf("got %d root entries, want 1 (the \"sources\" dir): %+v", len(roots), roots)
	}
	dir := roots[0]
	if dir.Name != "sources" || !dir.IsDir {
		t.Fatalf("root = %q (isDir=%v), want dir %q", dir.Name, dir.IsDir, "sources")
	}
	if len(dir.Children) != 3 {
		t.Fatalf("got %d children under sources/, want 3: %+v", len(dir.Children), dir.Children)
	}
	txt, ok := findNode(dir.Children, "testfile.txt")
	if !ok {
		t.Fatal("sources/testfile.txt not found")
	}
	if len(txt.Data) == 0 {
		t.Error("sources/testfile.txt: empty data fork, want real content")
	}
}

// TestExpand_StuffItClassicReal extracts a full-size, real-world classic
// SIT! archive (a StuffIt-compressed application, not a synthetic test
// fixture) with one subdirectory containing three entries — including an
// "Icon" entry (a data-less custom-icon marker file, resource fork only).
func TestExpand_StuffItClassicReal(t *testing.T) {
	data := realSample(t, sampleSIT1Real)
	roots, err := Expand(sampleSIT1Real, data, nil, [32]byte{})
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if len(roots) != 1 {
		t.Fatalf("got %d root entries, want 1: %+v", len(roots), roots)
	}
	dir := roots[0]
	if dir.Name != "Disk Copy 4.2" || !dir.IsDir {
		t.Fatalf("root = %q (isDir=%v), want dir %q", dir.Name, dir.IsDir, "Disk Copy 4.2")
	}
	if len(dir.Children) != 3 {
		t.Fatalf("got %d children, want 3: %+v", len(dir.Children), dir.Children)
	}
	// The classic custom-icon marker file is literally named "Icon\r" (Icon +
	// carriage return) — the convention Finder uses to hide it from listings.
	icon, ok := findNode(dir.Children, "Icon\r")
	if !ok {
		t.Fatal(`"Icon\r" not found`)
	}
	if len(icon.Data) != 0 {
		t.Errorf("Icon: data fork = %d bytes, want 0 (custom-icon marker, rsrc only)", len(icon.Data))
	}
	if len(icon.Resource) == 0 {
		t.Error("Icon: empty resource fork, want real content (rsrc=1902 per `stuffit list`)")
	}
}
