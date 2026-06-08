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

	stored, err := c.Decode(wire)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	back, err := c.Encode(stored)
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}
	if string(back) != string(wire) {
		t.Fatalf("roundtrip = %q, want %q", string(back), string(wire))
	}

	_, err = c.Decode([]byte("bad/name"))
	if !errors.Is(err, ErrUnrepresentable) {
		t.Fatalf("Decode bad/name error = %v, want ErrUnrepresentable", err)
	}
}
