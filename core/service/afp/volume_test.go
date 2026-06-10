package afp

import (
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

func newTestVolume(t *testing.T) *Volume {
	t.Helper()
	v, err := NewVolume(VolumeSpec{
		ID:   1,
		Name: "Test",
		Share: fs.ShareSpec{
			Name:          "Test",
			FSType:        "memfs",
			ForkBackend:   "appledouble",
			FilenameCodec: "macroman-utf8",
		},
	})
	if err != nil {
		t.Fatalf("NewVolume: %v", err)
	}
	return v
}

func TestVolume_RenamePath_CarriesMetadataAndRebindsCNID(t *testing.T) {
	v := newTestVolume(t)

	if _, err := v.FS().CreateFile("doc"); err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	if err := v.FS().WriteFinderInfo("doc", [32]byte{'F', 'I'}); err != nil {
		t.Fatalf("WriteFinderInfo: %v", err)
	}
	cnid := v.CNID("doc")

	if err := v.renamePath("doc", "moved"); err != nil {
		t.Fatalf("renamePath: %v", err)
	}
	// The CNID followed the rename (same node id now maps to the new path).
	if p, ok := v.PathForCNID(cnid); !ok || p != "moved" {
		t.Fatalf("CNID after rename maps to %q (ok=%v), want moved", p, ok)
	}
	// FinderInfo followed via the metadata-carrying FS rename.
	if info, ok, _ := v.FS().ReadFinderInfo("moved"); !ok || info[0] != 'F' {
		t.Fatalf("FinderInfo did not follow rename: ok=%v", ok)
	}

	if err := v.removePath("moved"); err != nil {
		t.Fatalf("removePath: %v", err)
	}
	if _, ok := v.PathForCNID(cnid); ok {
		t.Fatal("CNID survived removePath")
	}
	if _, ok, _ := v.FS().ReadFinderInfo("moved"); ok {
		t.Fatal("FinderInfo survived removePath")
	}
}

func TestNewVolume_InvalidTripleFailsLoudly(t *testing.T) {
	// hfs-image requires a macroman-native codec; pairing it with macroman-utf8
	// must be rejected at build time, not mangled at runtime.
	_, err := NewVolume(VolumeSpec{
		ID:   1,
		Name: "Bad",
		Share: fs.ShareSpec{
			FSType:        "hfs-image",
			FilenameCodec: "macroman-utf8",
		},
	})
	if err == nil {
		t.Fatal("expected build error for incompatible fs_type×codec triple")
	}
}

func TestVolume_ResolvePath_WireCharsetThreaded(t *testing.T) {
	v := newTestVolume(t)

	// MacRoman long-name path type: bytes are MacRoman on the wire. 0xBD is the
	// Greek capital Omega (Ω) in MacRoman, which must transcode to UTF-8 on the
	// store side, proving the wire charset is threaded from the path-type byte
	// rather than hard-wired.
	wire := []byte{0xBD}
	store, err := v.ResolvePath("", string(wire), PathTypeLongNames)
	if err != nil {
		t.Fatalf("ResolvePath MacRoman: %v", err)
	}
	if store == string(wire) {
		t.Fatalf("MacRoman name not transcoded: store == wire (%q)", store)
	}

	// Round-trip the stored name back to the wire charset.
	back, err := v.EncodeName(store, PathTypeLongNames)
	if err != nil {
		t.Fatalf("EncodeName: %v", err)
	}
	if string(back) != string(wire) {
		t.Errorf("round-trip mismatch: got %x, want %x", back, wire)
	}
}

func TestVolume_ResolvePath_UTF8PathType(t *testing.T) {
	v := newTestVolume(t)
	// A UTF-8 path type passes UTF-8 bytes straight to a UTF-8 store.
	name := "Café"
	store, err := v.ResolvePath("", name, PathTypeUTF8Names)
	if err != nil {
		t.Fatalf("ResolvePath UTF8: %v", err)
	}
	if store != name {
		t.Errorf("UTF-8 store = %q, want %q", store, name)
	}
}

func TestVolume_ResolvePath_ReservedCharEscaped(t *testing.T) {
	v := newTestVolume(t)
	// A '/' in a name element must be escaped reversibly (the POSIX reserved set),
	// never written as a path separator. The codec turns it into a 0xNN token.
	store, err := v.ResolvePath("", "a/b", PathTypeUTF8Names)
	if err != nil {
		t.Fatalf("ResolvePath reserved: %v", err)
	}
	if store == "a/b" {
		t.Fatal("'/' not escaped: would split into two path elements")
	}
	back, err := v.EncodeName(store, PathTypeUTF8Names)
	if err != nil {
		t.Fatalf("EncodeName: %v", err)
	}
	if string(back) != "a/b" {
		t.Errorf("reserved-char round-trip: got %q, want %q", back, "a/b")
	}
}

func TestVolume_ResolvePath_AscendsOnDoubleNull(t *testing.T) {
	v := newTestVolume(t)
	// "dir\x00sub\x00\x00file" : descend dir, descend sub, ascend one, descend file
	// → "dir/file".
	store, err := v.ResolvePath("", "dir\x00sub\x00\x00file", PathTypeUTF8Names)
	if err != nil {
		t.Fatalf("ResolvePath ascend: %v", err)
	}
	if store != "dir/file" {
		t.Errorf("ascend result = %q, want %q", store, "dir/file")
	}
}

func TestVolume_CNIDStableAndReversible(t *testing.T) {
	v := newTestVolume(t)
	if got := v.CNID(""); got != v.cnids.RootID() {
		t.Errorf("root CNID = %d, want %d", got, v.cnids.RootID())
	}
	a := v.CNID("dir/file")
	b := v.CNID("dir/file")
	if a != b {
		t.Errorf("CNID not stable: %d != %d", a, b)
	}
	if p, ok := v.PathForCNID(a); !ok || p != "dir/file" {
		t.Errorf("PathForCNID(%d) = %q,%v, want dir/file,true", a, p, ok)
	}
}
