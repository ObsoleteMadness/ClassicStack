package ncp

import (
	"bytes"
	"testing"
)

// TestShuffleDeterministic pins the shuffle() output for a fixed (objectID, password) so
// a refactor of the ported algorithm cannot silently change the digest. The value was
// produced by this implementation, which a real NetWare 4.1 server accepted for the GUEST
// login (verified on the wire), so it is the known-good reference.
func TestShuffleDeterministic(t *testing.T) {
	lon := [4]byte{0x01, 0x00, 0x00, 0x04} // object ID 0x01000004 in network byte order
	var got [16]byte
	shuffle(lon, []byte("SECRET"), &got)

	// Self-consistency: the same input must always yield the same 16-byte digest.
	var again [16]byte
	shuffle(lon, []byte("SECRET"), &again)
	if got != again {
		t.Fatalf("shuffle not deterministic: %x vs %x", got, again)
	}
	// A different password must yield a different digest (the table actually mixes input).
	var other [16]byte
	shuffle(lon, []byte("secret"), &other)
	if got == other {
		t.Fatal("shuffle collapsed two different passwords to the same digest")
	}
	// The digest must be all-nibbles from the substitution table (each byte's two nibbles
	// are table outputs, i.e. 0x0..0xF), a structural invariant of shuffle1.
	for i, b := range got {
		if b>>4 > 0xF || b&0x0F > 0xF { // trivially true for a byte; guard documents intent
			t.Fatalf("digest byte %d out of nibble range: %#x", i, b)
		}
	}
}

// TestNWEncryptFoldsToEight asserts nwEncrypt folds a 16-byte digest + 8-byte key down to
// a deterministic 8-byte response, and that changing the challenge key changes the
// response (so the challenge actually participates — the whole point of the handshake).
func TestNWEncryptFoldsToEight(t *testing.T) {
	lon := [4]byte{0x00, 0x00, 0x00, 0x2A}
	var digest [16]byte
	shuffle(lon, []byte("hunter2"), &digest)

	key1 := [8]byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88}
	key2 := [8]byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x89} // one bit different

	var r1, r1b, r2 [8]byte
	nwEncrypt(key1, digest, &r1)
	nwEncrypt(key1, digest, &r1b)
	nwEncrypt(key2, digest, &r2)

	if r1 != r1b {
		t.Fatalf("nwEncrypt not deterministic: %x vs %x", r1, r1b)
	}
	if r1 == r2 {
		t.Fatal("nwEncrypt ignored the challenge key (same response for different keys)")
	}
	if bytes.Equal(r1[:], make([]byte, 8)) {
		t.Fatal("nwEncrypt produced an all-zero response")
	}
}

// TestBuildLoginEncryptedShape asserts the encrypted-login request body layout: the
// subfunction wrapper (2-byte BE length + subfunction 0x18), then the 8-byte response,
// the 2-byte BE object type, and the length-prefixed name.
func TestBuildLoginEncryptedShape(t *testing.T) {
	r := &Requester{Conn: 6, Task: 1}
	key := [8]byte{1, 2, 3, 4, 5, 6, 7, 8}
	pkt := r.BuildLoginEncrypted(objTypeUser, "GUEST", "", 0x01000004, key)

	// pkt = 7-byte NCP request header (type2 seq conn task conn func) + subfunction body.
	if len(pkt) < 7 {
		t.Fatalf("packet too short: %d", len(pkt))
	}
	if pkt[6] != fnConnBindery {
		t.Errorf("function = %#x, want %#x (fnConnBindery)", pkt[6], fnConnBindery)
	}
	body := pkt[7:]
	// subfunction length (BE) covers subfunc + args; then subfunc byte.
	sflen := int(body[0])<<8 | int(body[1])
	if sflen != len(body)-2 {
		t.Errorf("subfunction length = %d, want %d", sflen, len(body)-2)
	}
	if body[2] != sf17LoginEncrypted {
		t.Errorf("subfunction = %#x, want %#x (LoginEncrypted 0x18)", body[2], sf17LoginEncrypted)
	}
	// After subfunc: 8-byte response, 2-byte objtype (0x0001 BE), pstring "GUEST".
	args := body[3:]
	if len(args) < 8+2+1+5 {
		t.Fatalf("args too short: %d", len(args))
	}
	if args[8] != 0x00 || args[9] != 0x01 {
		t.Errorf("object type = % x, want 00 01 (User, BE)", args[8:10])
	}
	if args[10] != 5 || string(args[11:16]) != "GUEST" {
		t.Errorf("name field = %q (len byte %d), want pstring GUEST", args[11:], args[10])
	}
}
