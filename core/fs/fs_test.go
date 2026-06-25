package fs

import (
	"errors"
	"io/fs"
	"os"
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
	// Register test-only factories WITH validators to exercise the per-backend
	// constraint hook (the real hfs-image/zipfs backends declare the same rules from
	// their own packages — this mirrors them so core stays free of the plugin names).
	RegisterFSWithValidator("hfs-image",
		func(spec ShareSpec, _ bus.Bus, _ metastore.Store) (FileSystem, error) {
			_ = spec
			return newMemFS(ShareSpec{}), nil
		},
		func(c SpecConstraints) error {
			// An HFS image stores MacRoman bytes natively; a UTF-8 store charset would
			// double-encode names on disk.
			if c.CodecProfile.StoreCharset != "macroman" {
				return errors.New("fs: hfs-image requires a macroman-native filename codec")
			}
			return nil
		})
	RegisterFSWithValidator("zipfs",
		func(spec ShareSpec, _ bus.Bus, _ metastore.Store) (FileSystem, error) {
			return newMemFS(spec), nil
		},
		func(c SpecConstraints) error {
			if c.Spec.ReadOnly && c.ForkBackend != "appledouble" {
				return errors.New("fs: read-only zipfs requires appledouble fork backend")
			}
			return nil
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

	// hfs-image with the macroman-native codec is the valid pairing.
	if _, err := BuildShare(ShareSpec{FSType: "hfs-image", FilenameCodec: "macroman-native"}, nil); err != nil {
		t.Fatalf("hfs-image x macroman-native rejected: %v", err)
	}

	// macroman-native codec cannot pair with the xattr (Unicode EA) fork backend.
	if _, err := BuildShare(ShareSpec{FSType: "memfs", FilenameCodec: "macroman-native", ForkBackend: "xattr"}, nil); err == nil {
		t.Fatal("expected macroman-native x xattr to be rejected")
	}

	// An unknown codec name fails at validation, before any component builds.
	if _, err := BuildShare(ShareSpec{FSType: "memfs", FilenameCodec: "no-such-codec"}, nil); err == nil {
		t.Fatal("expected unknown codec to be rejected")
	}
}

// TestReadOnlyMemFSEnforcesAndPreservesCapabilities proves the read-only policy is
// now folded INTO memFS (no external wrapper): a read-only memfs rejects every
// mutation, reports the ReadOnly capability — AND still satisfies the optional
// CatSearcher it implements. The last assertion is the point of removing the wrapper:
// a hand-forwarding readOnlyFS could silently drop a capability it forgot to re-expose;
// the backend-internal policy cannot.
func TestReadOnlyMemFSEnforcesAndPreservesCapabilities(t *testing.T) {
	ro := newMemFS(ShareSpec{ReadOnly: true})

	if !ro.Capabilities().ReadOnly {
		t.Fatal("read-only memfs did not report ReadOnly capability")
	}
	if err := ro.CreateDir("d"); err != fs.ErrPermission {
		t.Fatalf("CreateDir on RO = %v, want ErrPermission", err)
	}
	if _, err := ro.CreateFile("f"); err != fs.ErrPermission {
		t.Fatalf("CreateFile on RO = %v, want ErrPermission", err)
	}
	if _, err := ro.OpenFile("f", os.O_RDWR); err != fs.ErrPermission {
		t.Fatalf("OpenFile(O_RDWR) on RO = %v, want ErrPermission", err)
	}
	if err := ro.Remove("f"); err != fs.ErrPermission {
		t.Fatalf("Remove on RO = %v, want ErrPermission", err)
	}
	if err := ro.Rename("a", "b"); err != fs.ErrPermission {
		t.Fatalf("Rename on RO = %v, want ErrPermission", err)
	}

	// Capability passthrough: the optional CatSearcher survives the read-only policy.
	if _, ok := ro.(CatSearcher); !ok {
		t.Fatal("read-only memfs lost the CatSearcher capability (wrapper bug class)")
	}
}

// closingMemFS is a memfs that also implements the optional FSCloser, recording how
// many times Close was called — used to prove the share stack forwards teardown.
type closingMemFS struct {
	FileSystem
	closes int
}

func (c *closingMemFS) Close() error {
	c.closes++
	return nil
}

// TestFSCloserSeam proves: (1) CloseFS no-ops on a backend that does not implement
// FSCloser, (2) it forwards to one that does, and (3) closing the assembled share
// stack (shareFS) reaches the base backend's Close.
func TestFSCloserSeam(t *testing.T) {
	// A plain backend (no Close) is a silent no-op, not an error.
	if err := CloseFS(newMemFS(ShareSpec{})); err != nil {
		t.Fatalf("CloseFS on non-closer = %v, want nil", err)
	}

	// A closing backend is reached directly…
	base := &closingMemFS{FileSystem: newMemFS(ShareSpec{})}
	if err := CloseFS(base); err != nil {
		t.Fatalf("CloseFS on closer = %v", err)
	}
	if base.closes != 1 {
		t.Fatalf("direct CloseFS: closes = %d, want 1", base.closes)
	}

	// …and through the assembled share stack: register a factory returning the closer,
	// build a share, and confirm shareFS.Close forwards to the base.
	RegisterFS("closing-test-fs", func(spec ShareSpec, _ bus.Bus, _ metastore.Store) (FileSystem, error) {
		return &closingMemFS{FileSystem: newMemFS(spec)}, nil
	})
	built, err := BuildShare(ShareSpec{FSType: "closing-test-fs"}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	// The built ForkFS exposes Close via shareFS (it always satisfies FSCloser).
	closer, ok := built.(FSCloser)
	if !ok {
		t.Fatal("built share does not satisfy FSCloser")
	}
	if err := closer.Close(); err != nil {
		t.Fatalf("shareFS.Close = %v", err)
	}
	// Reach into the base to confirm the call propagated.
	sf, ok := built.(*shareFS)
	if !ok {
		t.Fatalf("built share is %T, want *shareFS", built)
	}
	cm, ok := sf.FileSystem.(*closingMemFS)
	if !ok {
		t.Fatalf("base is %T, want *closingMemFS", sf.FileSystem)
	}
	if cm.closes != 1 {
		t.Fatalf("shareFS.Close did not forward: base closes = %d, want 1", cm.closes)
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

	// A reserved char ('/') is now representable: the codec escapes it
	// reversibly as a "0xNN" token rather than rejecting the name.
	escaped, err := c.Decode([]byte("bad/name"), WireUTF8)
	if err != nil {
		t.Fatalf("Decode bad/name error = %v, want nil (reserved char escaped)", err)
	}
	if string(escaped) != "bad0x2Fname" {
		t.Fatalf("escaped = %q, want %q", string(escaped), "bad0x2Fname")
	}
	unescaped, err := c.Encode(escaped, WireUTF8)
	if err != nil {
		t.Fatalf("Encode escaped error: %v", err)
	}
	if string(unescaped) != "bad/name" {
		t.Fatalf("reserved-char roundtrip = %q, want %q", string(unescaped), "bad/name")
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
