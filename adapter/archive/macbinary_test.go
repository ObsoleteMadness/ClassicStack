package archive

import (
	"errors"
	"testing"
)

const sampleMacBinary = "../../third_party/classicstack-web/public/welcome/Utilities/mactcp206.sit_.bin"

// TestIsMacBinary_RealSample pins isMacBinary's detection of a real MacBinary
// header (the three reserved-zero bytes plus the 129 "old version" marker at
// offset 122 — MacBinary II's minimal self-identification, since it has no
// magic string).
func TestIsMacBinary_RealSample(t *testing.T) {
	data := realSample(t, sampleMacBinary)
	if !isMacBinary(data) {
		t.Error("isMacBinary = false, want true")
	}
}

// TestIsMacBinary_Rejects checks isMacBinary declines input too short to
// hold the 128-byte header, and input whose header bytes don't match.
func TestIsMacBinary_Rejects(t *testing.T) {
	if isMacBinary(nil) {
		t.Error("nil: isMacBinary = true, want false")
	}
	if isMacBinary(make([]byte, 127)) {
		t.Error("127 zero bytes (one short of the header): isMacBinary = true, want false")
	}
	notBinary := make([]byte, 128)
	copy(notBinary, "PK\x03\x04") // a zip signature, not MacBinary
	if isMacBinary(notBinary) {
		t.Error("zip-signature-prefixed 128 bytes: isMacBinary = true, want false")
	}
}

// TestExpandMacBinary_RealSample unwraps a real MacBinary-II file (itself a
// wrapped StuffIt archive — MacBinary is commonly used as the outer transfer
// wrapper for downloads) and checks the result matches the file's actual
// name, fork lengths, and Finder type/creator.
func TestExpandMacBinary_RealSample(t *testing.T) {
	data := realSample(t, sampleMacBinary)
	n, err := expandMacBinary(data)
	if err != nil {
		t.Fatalf("expandMacBinary: %v", err)
	}
	if n.Name != "MacTCP206.sit" {
		t.Errorf("Name = %q, want %q", n.Name, "MacTCP206.sit")
	}
	if len(n.Data) != 65147 {
		t.Errorf("len(Data) = %d, want 65147", len(n.Data))
	}
	if len(n.Resource) != 0 {
		t.Errorf("len(Resource) = %d, want 0", len(n.Resource))
	}
	// The unwrapped payload is itself a StuffIt archive: SIT! magic.
	if len(n.Data) < 4 || string(n.Data[:4]) != "SIT!" {
		t.Errorf("Data does not start with the SIT! magic: %x", n.Data[:4])
	}
	wantType, wantCreator := "SITD", "SIT!"
	if got := string(n.FinderInfo[0:4]); got != wantType {
		t.Errorf("FinderInfo type = %q, want %q", got, wantType)
	}
	if got := string(n.FinderInfo[4:8]); got != wantCreator {
		t.Errorf("FinderInfo creator = %q, want %q", got, wantCreator)
	}
}

// TestExpandMacBinary_Rejects checks expandMacBinary returns
// ErrUnsupportedFormat for non-MacBinary input.
func TestExpandMacBinary_Rejects(t *testing.T) {
	_, err := expandMacBinary([]byte("not a macbinary file"))
	if !errors.Is(err, ErrUnsupportedFormat) {
		t.Errorf("error = %v, want ErrUnsupportedFormat", err)
	}
}

// TestExpandMacBinary_TruncatedIsCorrupt checks a header that passes
// isMacBinary but whose declared fork lengths run past the actual data is
// reported as ErrCorrupt, not a silently truncated Node.
func TestExpandMacBinary_TruncatedIsCorrupt(t *testing.T) {
	data := realSample(t, sampleMacBinary)
	truncated := data[:200] // header (128) + a little data, nowhere near dataLen
	_, err := expandMacBinary(truncated)
	if !errors.Is(err, ErrCorrupt) {
		t.Errorf("error = %v, want ErrCorrupt", err)
	}
}
