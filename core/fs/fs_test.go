package fs

import (
	"errors"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/metastore"
)

func TestPlaceholdersSatisfyInterfaces(t *testing.T) {
	var _ FileSystem = newMemFS(ShareSpec{})
	var _ ForkEngine = NewNullForkEngine()
	var _ NameEngine = NewPassthroughNameEngine()
	var _ FilenameCodec = NewIdentityFilenameCodec()

	share, err := BuildShare(ShareSpec{FSType: "memfs"}, nil)
	if err != nil {
		t.Fatalf("BuildShare(memfs) error: %v", err)
	}
	if share == nil {
		t.Fatal("BuildShare returned nil share")
	}
}

func TestBuildShare_ValidAndInvalidCombinations(t *testing.T) {
	// Register test-only factories to exercise validation rules.
	RegisterFS("hfs-image", func(spec ShareSpec, _ bus.Bus, _ metastore.Store) (FileSystem, error) {
		_ = spec
		return newMemFS(ShareSpec{}), nil
	})
	RegisterFS("zipfs", func(spec ShareSpec, _ bus.Bus, _ metastore.Store) (FileSystem, error) {
		return newMemFS(spec), nil
	})

	if _, err := BuildShare(ShareSpec{
		Name:          "ok",
		FSType:        "memfs",
		ForkBackend:   "appledouble",
		FilenameCodec: "identity",
		NameEngine:    "passthrough",
		Metastore:     "mem",
	}, nil); err != nil {
		t.Fatalf("valid share rejected: %v", err)
	}

	if _, err := BuildShare(ShareSpec{FSType: "hfs-image", FilenameCodec: "utf8"}, nil); err == nil {
		t.Fatal("expected hfs-image x utf8 to be rejected")
	}

	if _, err := BuildShare(ShareSpec{FSType: "zipfs", ReadOnly: true, ForkBackend: "native"}, nil); err == nil {
		t.Fatal("expected read-only zipfs x non-appledouble fork to be rejected")
	}
}

func TestFilenameCodecRoundTripAndUnrepresentable(t *testing.T) {
	c := NewIdentityFilenameCodec()
	wire := []byte("Report")

	stored, err := c.Decode(wire, WireUTF8)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	back, err := c.Encode(stored, WireUTF8)
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}
	if string(back) != string(wire) {
		t.Fatalf("roundtrip = %q, want %q", string(back), string(wire))
	}

	_, err = c.Decode([]byte("bad/name"), WireUTF8)
	if !errors.Is(err, ErrUnrepresentable) {
		t.Fatalf("Decode bad/name error = %v, want ErrUnrepresentable", err)
	}

	// Test macroman-utf8 codec
	mc, err := codecByName("macroman-utf8")
	if err != nil {
		t.Fatalf("failed to get macroman-utf8 codec: %v", err)
	}

	// 1. WireMacRoman roundtrip
	// In MacRoman, 0x8E is é. In UTF-8 it is 0xC3 0xA9.
	mrWire := []byte{0x8E, 't', 'e', 's', 't'}
	utf8Stored, err := mc.Decode(mrWire, WireMacRoman)
	if err != nil {
		t.Fatalf("macroman-utf8 Decode (MacRoman) error: %v", err)
	}
	if string(utf8Stored) != "étest" {
		t.Fatalf("macroman-utf8 stored = %q, want %q", string(utf8Stored), "étest")
	}
	mrBack, err := mc.Encode(utf8Stored, WireMacRoman)
	if err != nil {
		t.Fatalf("macroman-utf8 Encode (MacRoman) error: %v", err)
	}
	if string(mrBack) != string(mrWire) {
		t.Fatalf("macroman-utf8 MacRoman roundtrip = %v, want %v", mrBack, mrWire)
	}

	// 2. WireUTF8 roundtrip
	utf8Wire := []byte("Ätest")
	utf8Stored2, err := mc.Decode(utf8Wire, WireUTF8)
	if err != nil {
		t.Fatalf("macroman-utf8 Decode (UTF-8) error: %v", err)
	}
	if string(utf8Stored2) != "Ätest" {
		t.Fatalf("macroman-utf8 stored2 = %q, want %q", string(utf8Stored2), "Ätest")
	}
	utf8Back, err := mc.Encode(utf8Stored2, WireUTF8)
	if err != nil {
		t.Fatalf("macroman-utf8 Encode (UTF-8) error: %v", err)
	}
	if string(utf8Back) != string(utf8Wire) {
		t.Fatalf("macroman-utf8 UTF-8 roundtrip = %q, want %q", string(utf8Back), string(utf8Wire))
	}

	// 3. Unsupported WireEncoding
	_, err = mc.Decode([]byte("test"), WireANSI)
	if !errors.Is(err, ErrWireUnsupported) {
		t.Fatalf("expected ErrWireUnsupported, got %v", err)
	}
	_, err = mc.Encode(StoredName("test"), WireANSI)
	if !errors.Is(err, ErrWireUnsupported) {
		t.Fatalf("expected ErrWireUnsupported, got %v", err)
	}
}
