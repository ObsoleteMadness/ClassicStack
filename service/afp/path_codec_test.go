//go:build afp || all

package afp

import (
	"bytes"
	"testing"
)

func TestWriteAFPName_EncodesToMacRoman(t *testing.T) {
	s := NewService("TestServer", nil, nil, nil)

	var buf bytes.Buffer
	s.writeAFPName(&buf, "tm™", 0)

	want := []byte{3, 't', 'm', 0xAA}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("writeAFPName bytes = %x, want %x", buf.Bytes(), want)
	}
}

func TestHostTokenRoundTrip_WhenEnabled(t *testing.T) {
	s := NewService("TestServer", nil, nil, nil, Options{DecomposedFilenames: true})

	host := s.afpPathElementToHost("Hello/World")
	if host != "Hello0x2FWorld" {
		t.Fatalf("afpPathElementToHost = %q, want %q", host, "Hello0x2FWorld")
	}

	encoded := s.hostNameToAFPBytes(host, 0)
	if !bytes.Equal(encoded, []byte("Hello/World")) {
		t.Fatalf("hostNameToAFPBytes = %x, want %x", encoded, []byte("Hello/World"))
	}
}
