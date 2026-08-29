package archive

import (
	"archive/zip"
	"bytes"
	"testing"
)

// buildTestZip writes entries (path -> content; a "" content with a
// trailing "/" path is a directory entry) into an in-memory zip.
func buildTestZip(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range entries {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatalf("Create(%s): %v", name, err)
		}
		if _, err := fw.Write([]byte(content)); err != nil {
			t.Fatalf("Write(%s): %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return buf.Bytes()
}

// TestExpandZip_NestedDirs pins expandZip's tree reconstruction (this
// session's refactor moved it onto the shared treeBuilder in tree.go): a
// file nested two directories deep, with no explicit parent-directory
// entries in the zip, still lands under a fully-nested tree.
func TestExpandZip_NestedDirs(t *testing.T) {
	data := buildTestZip(t, map[string]string{
		"a/b/c.txt": "hello",
		"top.txt":   "world",
	})
	roots, err := expandZip(data)
	if err != nil {
		t.Fatalf("expandZip: %v", err)
	}
	if len(roots) != 2 {
		t.Fatalf("got %d roots, want 2 (\"a\" dir, \"top.txt\"): %+v", len(roots), roots)
	}
	top, ok := findNode(roots, "top.txt")
	if !ok || string(top.Data) != "world" {
		t.Errorf("top.txt = %+v, want data %q", top, "world")
	}
	a, ok := findNode(roots, "a")
	if !ok || !a.IsDir {
		t.Fatalf("a = %+v, want a directory", a)
	}
	b, ok := findNode(a.Children, "b")
	if !ok || !b.IsDir {
		t.Fatalf("a/b = %+v, want a directory", b)
	}
	c, ok := findNode(b.Children, "c.txt")
	if !ok || string(c.Data) != "hello" {
		t.Errorf("a/b/c.txt = %+v, want data %q", c, "hello")
	}
}

// TestExpandZip_AppleDoubleSidecar pins the AppleDouble ._name merge: a
// "._foo" sidecar entry's resource fork + Finder info attach to "foo"'s
// Node rather than appearing as its own separate entry.
func TestExpandZip_AppleDoubleSidecar(t *testing.T) {
	var fi [32]byte
	copy(fi[:], "TEXTttxt")
	ad := buildAppleDouble(t, []byte("resource-fork-bytes"), fi)

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	mustWrite(t, w, "foo.txt", []byte("visible data"))
	mustWrite(t, w, "._foo.txt", ad)
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	data := buf.Bytes()

	roots, err := expandZip(data)
	if err != nil {
		t.Fatalf("expandZip: %v", err)
	}
	foo, ok := findNode(roots, "foo.txt")
	if !ok {
		t.Fatalf("foo.txt not found in %+v", roots)
	}
	if string(foo.Data) != "visible data" {
		t.Errorf("foo.txt data = %q, want %q", foo.Data, "visible data")
	}
	if string(foo.Resource) != "resource-fork-bytes" {
		t.Errorf("foo.txt resource = %q, want %q", foo.Resource, "resource-fork-bytes")
	}
	if foo.FinderInfo != fi {
		t.Errorf("foo.txt FinderInfo = %x, want %x", foo.FinderInfo, fi)
	}
	if _, ok := findNode(roots, "._foo.txt"); ok {
		t.Error("._foo.txt sidecar should be merged into foo.txt, not a separate root")
	}
}

func mustWrite(t *testing.T, w *zip.Writer, name string, content []byte) {
	t.Helper()
	fw, err := w.Create(name)
	if err != nil {
		t.Fatalf("Create(%s): %v", name, err)
	}
	if _, err := fw.Write(content); err != nil {
		t.Fatalf("Write(%s): %v", name, err)
	}
}

// buildAppleDouble constructs a minimal AppleDouble blob in the exact layout
// parseAppleDouble (zip.go) reads: magic (b[0:4]), entry count (b[4:8] — NOT
// the real AppleDouble spec's byte-24 uint16 count; this reader's own
// simplified convention), 18 bytes of ignored filler, then that many 12-byte
// (id, start, length) entry descriptors from offset 26 — here one resource
// fork (id 2) and one Finder info (id 9) entry, carrying resource and fi.
func buildAppleDouble(t *testing.T, resource []byte, fi [32]byte) []byte {
	t.Helper()
	const numEntries = 2
	header := 26 + numEntries*12
	rsrcOff := header
	fiOff := rsrcOff + len(resource)
	total := fiOff + 32

	b := make([]byte, total)
	putBE32(b[0:4], 0x00051607) // magic
	putBE32(b[4:8], numEntries) // entry count, per this reader's convention
	// entry 0: resource fork (id 2)
	putBE32(b[26:30], 2)
	putBE32(b[30:34], uint32(rsrcOff))
	putBE32(b[34:38], uint32(len(resource)))
	// entry 1: Finder info (id 9)
	putBE32(b[38:42], 9)
	putBE32(b[42:46], uint32(fiOff))
	putBE32(b[46:50], 32)

	copy(b[rsrcOff:], resource)
	copy(b[fiOff:], fi[:])
	return b
}

func putBE32(dst []byte, v uint32) {
	dst[0] = byte(v >> 24)
	dst[1] = byte(v >> 16)
	dst[2] = byte(v >> 8)
	dst[3] = byte(v)
}
