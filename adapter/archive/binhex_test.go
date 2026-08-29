package archive

import (
	"errors"
	"testing"
)

const sampleBinHex = "../../third_party/classicstack-web/src/fs/testdata/eyeball.gif.hqx"

// TestLooksBinHex_RealSample pins looksBinHex's detection of a real BinHex 4.0
// wrapper: the classic "(This file must be converted with BinHex 4.0)" banner
// followed by the ':'-delimited 6-bit-encoded payload.
func TestLooksBinHex_RealSample(t *testing.T) {
	data := realSample(t, sampleBinHex)
	if !looksBinHex(data) {
		t.Error("looksBinHex = false, want true")
	}
}

// TestLooksBinHex_Rejects checks looksBinHex declines input with no BinHex
// banner and no leading ':' delimiter.
func TestLooksBinHex_Rejects(t *testing.T) {
	for name, data := range map[string][]byte{
		"empty":        nil,
		"plain text":   []byte("just some ordinary text, nothing to see here"),
		"random bin":   {0x00, 0x01, 0x02, 0x03, 0xFF, 0xFE},
		"almost colon": []byte("   not quite a colon here"),
	} {
		if looksBinHex(data) {
			t.Errorf("%s: looksBinHex = true, want false", name)
		}
	}
}

// TestExpandBinHex_RealSample decodes a real BinHex 4.0 file (a MacBinary-II
// header — name, type/creator, fork lengths — wrapped in the 6-bit ASCII
// encoding plus RLE) and checks the result matches the archive's actual
// contents, not just that it decodes without error.
func TestExpandBinHex_RealSample(t *testing.T) {
	data := realSample(t, sampleBinHex)
	n, err := expandBinHex(data)
	if err != nil {
		t.Fatalf("expandBinHex: %v", err)
	}
	if n.Name != "eyeball.gif" {
		t.Errorf("Name = %q, want %q", n.Name, "eyeball.gif")
	}
	if len(n.Data) != 2755 {
		t.Errorf("len(Data) = %d, want 2755 (the decoded GIF)", len(n.Data))
	}
	if len(n.Resource) != 0 {
		t.Errorf("len(Resource) = %d, want 0 (a GIF has no resource fork)", len(n.Resource))
	}
	// A real GIF data fork starts with the "GIF8" magic.
	if len(n.Data) < 4 || string(n.Data[:4]) != "GIF8" {
		t.Errorf("Data does not start with the GIF8 magic: %x", n.Data[:min(8, len(n.Data))])
	}
}

// TestExpandBinHex_Rejects checks expandBinHex returns ErrUnsupportedFormat
// for input looksBinHex itself declines, rather than panicking or silently
// returning a zero-value Node.
func TestExpandBinHex_Rejects(t *testing.T) {
	_, err := expandBinHex([]byte("just an ordinary text file, nothing encoded here"))
	if !errors.Is(err, ErrUnsupportedFormat) {
		t.Errorf("error = %v, want ErrUnsupportedFormat", err)
	}
}

// TestExpandBinHex_TruncatedIsCorrupt checks a BinHex wrapper that looks
// right (has the banner and both ':' delimiters) but whose payload is cut
// short is reported as ErrCorrupt rather than a successful partial decode.
func TestExpandBinHex_TruncatedIsCorrupt(t *testing.T) {
	data := realSample(t, sampleBinHex)
	truncated := data[:len(data)/2]
	_, err := expandBinHex(truncated)
	if !errors.Is(err, ErrCorrupt) {
		t.Errorf("error = %v, want ErrCorrupt", err)
	}
}
