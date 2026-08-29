package archive

import (
	"errors"
	"testing"
)

// TestSniff_ByExtension pins Sniff's extension-based shortcut: it recognises
// a supported wrapper by filename alone, before ever looking at content.
func TestSniff_ByExtension(t *testing.T) {
	for _, name := range []string{"a.zip", "A.ZIP", "b.sit", "c.hqx", "d.bin"} {
		if !Sniff(name, [32]byte{}, nil) {
			t.Errorf("Sniff(%s, no content) = false, want true", name)
		}
	}
	if Sniff("readme.txt", [32]byte{}, nil) {
		t.Error("Sniff(readme.txt, no content) = true, want false")
	}
}

// TestSniff_ByFinderType pins Sniff's Finder-type-code shortcut: a StuffIt
// type code (SIT!/SIT5/SITD) in the Mac Finder metadata is recognised even
// when the filename gives no hint and there is no content to sniff.
func TestSniff_ByFinderType(t *testing.T) {
	for _, sig := range []string{"SIT!", "SIT5", "SITD"} {
		var fi [32]byte
		copy(fi[:], sig)
		if !Sniff("Untitled", fi, nil) {
			t.Errorf("Sniff with Finder type %q = false, want true", sig)
		}
	}
	var fi [32]byte
	copy(fi[:], "TEXT")
	if Sniff("Untitled", fi, nil) {
		t.Error("Sniff with Finder type TEXT = true, want false")
	}
}

// TestSniff_ByContent pins Sniff's content-sniffing fallback: real archive
// bytes are recognised even under a name and Finder type that give no hint.
func TestSniff_ByContent(t *testing.T) {
	zipData := buildTestZip(t, map[string]string{"a.txt": "hi"})
	if !Sniff("download", [32]byte{}, zipData) {
		t.Error("Sniff(zip content, no hint from name) = false, want true")
	}
	macBinary := realSample(t, sampleMacBinary)
	if !Sniff("download", [32]byte{}, macBinary) {
		t.Error("Sniff(macbinary content, no hint from name) = false, want true")
	}
	binHex := realSample(t, sampleBinHex)
	if !Sniff("download", [32]byte{}, binHex) {
		t.Error("Sniff(binhex content, no hint from name) = false, want true")
	}
	stuffit := realSample(t, sampleSIT1Small)
	if !Sniff("download", [32]byte{}, stuffit) {
		t.Error("Sniff(stuffit content, no hint from name) = false, want true")
	}
}

// TestSniff_False checks Sniff declines plain, unrelated data with no
// extension/Finder-type/content signal at all.
func TestSniff_False(t *testing.T) {
	if Sniff("readme.txt", [32]byte{}, []byte("just some prose")) {
		t.Error("Sniff(plain text) = true, want false")
	}
}

// TestExpand_DispatchesByContent checks Expand routes each wrapper format to
// its own decoder and returns real content, not just a non-error result —
// exercising all four formats through the single public entry point rather
// than each format's own expandXxx function directly.
func TestExpand_DispatchesByContent(t *testing.T) {
	t.Run("zip", func(t *testing.T) {
		data := buildTestZip(t, map[string]string{"a.txt": "hello"})
		roots, err := Expand("a.zip", data, nil, [32]byte{})
		if err != nil {
			t.Fatalf("Expand: %v", err)
		}
		if got, ok := findNode(roots, "a.txt"); !ok || string(got.Data) != "hello" {
			t.Errorf("a.txt = %+v, want data %q", got, "hello")
		}
	})
	t.Run("macbinary", func(t *testing.T) {
		data := realSample(t, sampleMacBinary)
		roots, err := Expand("x.bin", data, nil, [32]byte{})
		if err != nil {
			t.Fatalf("Expand: %v", err)
		}
		if len(roots) != 1 || roots[0].Name != "MacTCP206.sit" {
			t.Errorf("roots = %+v, want one node named MacTCP206.sit", roots)
		}
	})
	t.Run("binhex", func(t *testing.T) {
		data := realSample(t, sampleBinHex)
		roots, err := Expand("x.hqx", data, nil, [32]byte{})
		if err != nil {
			t.Fatalf("Expand: %v", err)
		}
		if len(roots) != 1 || roots[0].Name != "eyeball.gif" {
			t.Errorf("roots = %+v, want one node named eyeball.gif", roots)
		}
	})
	t.Run("stuffit", func(t *testing.T) {
		data := realSample(t, sampleSIT1Small)
		roots, err := Expand("x.sit", data, nil, [32]byte{})
		if err != nil {
			t.Fatalf("Expand: %v", err)
		}
		if len(roots) != 6 {
			t.Errorf("got %d roots, want 6", len(roots))
		}
	})
}

// TestExpand_Unsupported checks Expand returns ErrUnsupportedFormat for data
// that matches none of the four formats.
func TestExpand_Unsupported(t *testing.T) {
	_, err := Expand("readme.txt", []byte("just some prose"), nil, [32]byte{})
	if !errors.Is(err, ErrUnsupportedFormat) {
		t.Errorf("error = %v, want ErrUnsupportedFormat", err)
	}
}

// TestExpand_CorruptZipPropagatesError checks that data which LOOKS like a
// zip (the "PK" signature) but fails to parse surfaces ErrCorrupt directly,
// rather than falling through to try the other three formats and eventually
// masking the real error as ErrUnsupportedFormat.
func TestExpand_CorruptZipPropagatesError(t *testing.T) {
	corrupt := []byte("PK\x03\x04this is not a valid zip central directory")
	_, err := Expand("broken.zip", corrupt, nil, [32]byte{})
	if !errors.Is(err, ErrCorrupt) {
		t.Errorf("error = %v, want ErrCorrupt", err)
	}
}
