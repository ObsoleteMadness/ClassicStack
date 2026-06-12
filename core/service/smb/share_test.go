package smb

import (
	"errors"
	"testing"
	"unicode/utf16"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	protocol "github.com/ObsoleteMadness/ClassicStack/core/protocol/smb"
)

func newTestShare(t *testing.T) *Share {
	t.Helper()
	sh, err := NewShare(ShareSpec{
		Name: "PUBLIC",
		Share: fs.ShareSpec{
			Name:          "PUBLIC",
			FSType:        "memfs",
			ForkBackend:   "ads",
			FilenameCodec: "identity", // advertises every wire charset
		},
	})
	if err != nil {
		t.Fatalf("NewShare: %v", err)
	}
	return sh
}

// newReadOnlyShare builds a read-only memfs share (its FS Capabilities report
// ReadOnly), used to prove mutating FS commands are refused.
func newReadOnlyShare(t *testing.T) *Share {
	t.Helper()
	sh, err := NewShare(ShareSpec{
		Name: "RO",
		Share: fs.ShareSpec{
			Name:          "RO",
			FSType:        "memfs",
			ForkBackend:   "ads",
			FilenameCodec: "identity",
			ReadOnly:      true,
		},
	})
	if err != nil {
		t.Fatalf("NewShare read-only: %v", err)
	}
	return sh
}

// utf16Wire encodes an ASCII/Unicode path string to UTF-16LE wire bytes (the form
// an NT client sends when the FLAGS2 Unicode bit is set).
func utf16Wire(s string) []byte {
	u := utf16.Encode([]rune(s))
	b := make([]byte, len(u)*2)
	for i, c := range u {
		b[2*i] = byte(c)
		b[2*i+1] = byte(c >> 8)
	}
	return b
}

func TestShare_ResolvePath_BackslashToStore(t *testing.T) {
	sh := newTestShare(t)
	// NT client with the Unicode flag set: names are UTF-16LE on the wire,
	// including the backslash separators.
	store, err := sh.ResolvePath(utf16Wire("\\docs\\readme.txt"), protocol.Flags2Unicode)
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}
	if store != "docs/readme.txt" {
		t.Errorf("store path = %q, want %q", store, "docs/readme.txt")
	}
}

func TestShare_ResolvePath_ANSIBackslash(t *testing.T) {
	sh := newTestShare(t)
	// DOS/WfW client: single-byte ANSI path, backslash is one byte.
	store, err := sh.ResolvePath([]byte("\\DOCS\\FILE.TXT"), 0)
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}
	if store != "DOCS/FILE.TXT" {
		t.Errorf("store path = %q, want %q", store, "DOCS/FILE.TXT")
	}
}

func TestShare_ResolvePath_DotDotClimbs(t *testing.T) {
	sh := newTestShare(t)
	store, err := sh.ResolvePath(utf16Wire("a\\b\\..\\c"), protocol.Flags2Unicode)
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}
	if store != "a/c" {
		t.Errorf("store path = %q, want %q", store, "a/c")
	}
}

func TestShare_DialectThreadsWireCharset(t *testing.T) {
	sh := newTestShare(t)

	// NT (Unicode) and DOS (ANSI/CP437) clients send the same logical name in
	// different charsets; both must resolve to the same store path, proving the
	// wire charset is threaded per request from the FLAGS2 bit.
	utf16Store, err := sh.ResolvePath(utf16Wire("file"), protocol.Flags2Unicode)
	if err != nil {
		t.Fatalf("ResolvePath UTF-16: %v", err)
	}
	ansiStore, err := sh.ResolvePath([]byte("file"), 0) // no Unicode bit → ANSI/CP437
	if err != nil {
		t.Fatalf("ResolvePath ANSI: %v", err)
	}
	if utf16Store != ansiStore || utf16Store != "file" {
		t.Errorf("dialect threading mismatch: utf16=%q ansi=%q", utf16Store, ansiStore)
	}
}

func TestShare_EncodeName_RoundTrip(t *testing.T) {
	sh := newTestShare(t)
	store, err := sh.ResolvePath(utf16Wire("résumé.doc"), protocol.Flags2Unicode)
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}
	back, err := sh.EncodeName(store, protocol.Flags2Unicode)
	if err != nil {
		t.Fatalf("EncodeName: %v", err)
	}
	// Re-resolve the encoded UTF-16 element and confirm it maps to the same store.
	again, err := sh.ResolvePath(back, protocol.Flags2Unicode)
	if err != nil {
		t.Fatalf("re-resolve: %v", err)
	}
	if again != store {
		t.Errorf("UTF-16 round-trip mismatch: %q vs %q", again, store)
	}
}

func TestWireFor(t *testing.T) {
	if got := wireFor(protocol.Flags2Unicode); got != fs.WireUTF16 {
		t.Errorf("Unicode flag → %v, want WireUTF16", got)
	}
	if got := wireFor(0); got != fs.WireANSI {
		t.Errorf("no Unicode flag → %v, want WireANSI", got)
	}
}

func TestShare_ResolvePath_UnsupportedWireIsRejected(t *testing.T) {
	// A macroman-native share advertises only WireMacRoman, so a UTF-16 (Unicode)
	// request is unsupported and must fail loudly, not silently mangle the name.
	mr, err := NewShare(ShareSpec{
		Name: "HFS",
		Share: fs.ShareSpec{
			FSType:        "memfs",
			ForkBackend:   "appledouble",
			FilenameCodec: "macroman-native",
		},
	})
	if err != nil {
		t.Fatalf("NewShare macroman-native: %v", err)
	}
	_, err = mr.ResolvePath(utf16Wire("file"), protocol.Flags2Unicode)
	if !errors.Is(err, fs.ErrWireUnsupported) {
		t.Errorf("macroman-native UTF-16 request: err = %v, want ErrWireUnsupported", err)
	}
}
