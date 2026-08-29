package auth

import (
	"errors"
	"testing"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
)

// fixedSalt is a 16-byte salt used across the credential tests (core/auth does
// not generate randomness; the store adapter does).
var fixedSalt = []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}

// TestPBKDF2SHA256Vector pins the PRF against a published PBKDF2-HMAC-SHA256
// vector (password "passwd", salt "salt", 1 iteration, dkLen 64) so a refactor of
// the hand-rolled derivation cannot silently change the hash.
func TestPBKDF2SHA256Vector(t *testing.T) {
	got := bp.EncodeHex(pbkdf2SHA256([]byte("passwd"), []byte("salt"), 1, 64))
	want := "55ac046e56e3089fec1691c22544b605f94185216dde0465e68b9d57c20dacbc" +
		"49ca9cccf179b645991664b39d77ef317c71b845b1e30bd509112041d3a19783"
	if got != want {
		t.Fatalf("pbkdf2-sha256 mismatch:\n got %s\nwant %s", got, want)
	}
}

func TestCredentialRoundTrip(t *testing.T) {
	c := DeriveCredential("hunter2", fixedSalt)
	if len(c.Salt) != SaltLen || len(c.Hash) != credKeyLen {
		t.Fatalf("credential sizes salt=%d hash=%d", len(c.Salt), len(c.Hash))
	}
	if !c.Verify("hunter2") {
		t.Fatal("Verify rejected the correct password")
	}
	if c.Verify("hunter3") {
		t.Fatal("Verify accepted a wrong password")
	}
	if c.Verify("") {
		t.Fatal("Verify accepted an empty password")
	}
}

func TestNewCredential(t *testing.T) {
	c, err := NewCredential("hunter2")
	if err != nil {
		t.Fatalf("NewCredential: unexpected error: %v", err)
	}
	if len(c.Salt) != SaltLen || len(c.Hash) != credKeyLen {
		t.Fatalf("credential sizes salt=%d hash=%d", len(c.Salt), len(c.Hash))
	}
	if !c.Verify("hunter2") {
		t.Fatal("Verify rejected the correct password")
	}
	other, err := NewCredential("hunter2")
	if err != nil {
		t.Fatalf("NewCredential (second call): unexpected error: %v", err)
	}
	if string(c.Salt) == string(other.Salt) {
		t.Fatal("NewCredential produced the same salt twice in a row (rand.Read broken?)")
	}
}

func TestCredentialSaltMakesHashesDiffer(t *testing.T) {
	saltA := []byte("aaaaaaaaaaaaaaaa")
	saltB := []byte("bbbbbbbbbbbbbbbb")
	a := DeriveCredential("same", saltA)
	b := DeriveCredential("same", saltB)
	if a.HashHex() == b.HashHex() {
		t.Fatal("two credentials for the same password share a hash (salt not applied)")
	}
}

func TestParseCredential(t *testing.T) {
	c := DeriveCredential("pw", fixedSalt)
	got, err := ParseCredential(c.SaltHex(), c.HashHex())
	if err != nil {
		t.Fatal(err)
	}
	if !got.Verify("pw") {
		t.Fatal("parsed credential failed to verify")
	}

	for _, tc := range []struct{ salt, hash string }{
		{"zz", c.HashHex()},   // non-hex salt
		{"abcd", c.HashHex()}, // wrong-length salt
		{c.SaltHex(), "qq"},   // non-hex hash
		{c.SaltHex(), "ab"},   // wrong-length hash
	} {
		if _, err := ParseCredential(tc.salt, tc.hash); !errors.Is(err, ErrBadCredentialRecord) {
			t.Fatalf("ParseCredential(%q,%q) err=%v, want ErrBadCredentialRecord", tc.salt, tc.hash, err)
		}
	}

	var zero Credential
	if zero.Verify("anything") {
		t.Fatal("zero-value credential verified")
	}
}
