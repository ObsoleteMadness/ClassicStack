package fs

import (
	"bytes"
	"os"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/macresources"
)

// buildResFork is a tiny helper producing a binary resource fork with one resource.
func buildResFork(t *testing.T, rtype string, id int16, data []byte) []byte {
	t.Helper()
	var ty [4]byte
	copy(ty[:], rtype)
	return macresources.BuildResourceFork([]macresources.Resource{
		{Type: ty, ID: id, Data: data},
	})
}

// TestDerez_WriteDeserialisesToRdump proves that writing the binary resource fork stores
// it as an .rdump TEXT sidecar (not a binary blob), and that FinderInfo lands in .idump.
func TestDerez_WriteDeserialisesToRdump(t *testing.T) {
	base := newMemFS(ShareSpec{})
	eng, err := forkAdapterByName("derez", ShareSpec{}, base)
	if err != nil {
		t.Fatalf("forkAdapterByName(derez): %v", err)
	}

	// Write a binary resource fork through the engine.
	bin := buildResFork(t, "STR ", 128, []byte("\x05Hello"))
	f, err := eng.OpenFork("greet", ResourceFork, os.O_RDWR|os.O_CREATE)
	if err != nil {
		t.Fatalf("OpenFork: %v", err)
	}
	if _, err := f.WriteAt(bin, 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// The on-disk sidecar is the human-readable rdump TEXT, not the binary fork.
	rdump := readWholeOK(t, base, "greet.rdump")
	if !bytes.Contains(rdump, []byte("data 'STR '")) || !bytes.Contains(rdump, []byte("(128")) {
		t.Fatalf("rdump sidecar is not DeRez text:\n%s", rdump)
	}
	if bytes.Equal(rdump, bin) {
		t.Fatal("rdump sidecar stored the binary fork verbatim (should be text)")
	}

	// FinderInfo round-trips through the idump sidecar (type/creator only).
	var fi [32]byte
	copy(fi[0:4], "TEXT")
	copy(fi[4:8], "ttxt")
	if err := eng.WriteFinderInfo("greet", fi); err != nil {
		t.Fatalf("WriteFinderInfo: %v", err)
	}
	idump := readWholeOK(t, base, "greet.idump")
	if len(idump) != 8 || string(idump[0:4]) != "TEXT" || string(idump[4:8]) != "ttxt" {
		t.Fatalf("idump = %q, want TEXTttxt (8 bytes)", idump)
	}
}

// TestDerez_ReadSerialisesToBinary proves that reading the resource fork serialises the
// rdump text BACK to the binary resource fork the client expects.
func TestDerez_ReadSerialisesToBinary(t *testing.T) {
	base := newMemFS(ShareSpec{})
	eng := newDerezForkEngine(base)

	// Seed an .rdump sidecar directly (as if committed in git).
	var ty [4]byte
	copy(ty[:], "CODE")
	rez := macresources.FormatRez([]macresources.Resource{
		{Type: ty, ID: 1, Attribs: macresources.AttrLocked, Data: []byte{0x4E, 0x75, 0x00, 0x00}},
	})
	w, _ := base.CreateFile("main.rdump")
	_, _ = w.WriteAt(rez, 0)
	_ = w.Close()

	// Reading the resource fork yields the binary form.
	f, err := eng.OpenFork("main", ResourceFork, os.O_RDONLY)
	if err != nil {
		t.Fatalf("OpenFork read: %v", err)
	}
	n, _ := eng.ForkLen("main", ResourceFork)
	got := make([]byte, n)
	if _, err := f.ReadAt(got, 0); err != nil {
		t.Fatalf("ReadAt: %v", err)
	}
	f.Close()

	// Decode what the client got back to confirm it is a valid binary resource fork.
	res, err := macresources.ParseResourceFork(got)
	if err != nil {
		t.Fatalf("client-visible fork is not valid binary: %v", err)
	}
	if len(res) != 1 || res[0].Type != ty || res[0].ID != 1 || res[0].Attribs != macresources.AttrLocked {
		t.Fatalf("decoded resource wrong: %+v", res)
	}
	if !bytes.Equal(res[0].Data, []byte{0x4E, 0x75, 0x00, 0x00}) {
		t.Fatalf("decoded data = %v", res[0].Data)
	}
}

// TestDerez_MetadataPathsAndMove proves both sidecars are reported and that
// MoveMetadata/DeleteMetadata follow them.
func TestDerez_MetadataPathsAndMove(t *testing.T) {
	base := newMemFS(ShareSpec{})
	eng := newDerezForkEngine(base)

	bin := buildResFork(t, "STR ", 1, []byte("\x02hi"))
	writeFork(t, eng, "a", ResourceFork, bin)
	var fi [32]byte
	copy(fi[0:8], "APPLMACS")
	_ = eng.WriteFinderInfo("a", fi)

	// MetadataPaths reports both sidecars.
	mp := eng.MetadataPaths("a")
	if len(mp) != 2 || mp[0] != "a.rdump" || mp[1] != "a.idump" {
		t.Fatalf("MetadataPaths = %v, want [a.rdump a.idump]", mp)
	}

	// Move follows both.
	if err := eng.MoveMetadata("a", "b"); err != nil {
		t.Fatalf("MoveMetadata: %v", err)
	}
	if _, err := base.Stat("a.rdump"); err == nil {
		t.Fatal("a.rdump still present after move")
	}
	if _, err := base.Stat("b.rdump"); err != nil {
		t.Fatalf("b.rdump missing after move: %v", err)
	}
	if _, err := base.Stat("b.idump"); err != nil {
		t.Fatalf("b.idump missing after move: %v", err)
	}

	// Delete drops both.
	if err := eng.DeleteMetadata("b"); err != nil {
		t.Fatalf("DeleteMetadata: %v", err)
	}
	if _, err := base.Stat("b.rdump"); err == nil {
		t.Fatal("b.rdump still present after delete")
	}
	if _, err := base.Stat("b.idump"); err == nil {
		t.Fatal("b.idump still present after delete")
	}
}

// TestDerez_ViaBuildShare proves a derez share assembles and its data fork is the plain
// file while the resource fork lives in the rdump sidecar.
func TestDerez_ViaBuildShare(t *testing.T) {
	ffs, err := BuildShare(ShareSpec{FSType: "memfs", ForkBackend: "derez"}, nil)
	if err != nil {
		t.Fatalf("BuildShare derez: %v", err)
	}
	if fc, ok := ffs.(ForkContainers); !ok {
		t.Fatal("derez share does not expose ForkContainers")
	} else if mp := fc.MetadataPaths("x"); len(mp) != 2 {
		t.Fatalf("MetadataPaths = %v, want two sidecars", mp)
	}
}

func readWholeOK(t *testing.T, base FileSystem, path string) []byte {
	t.Helper()
	b, err := readWhole(base, path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	return b
}
