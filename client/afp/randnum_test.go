package afp

import (
	"testing"
)

func TestRandnumEncrypt(t *testing.T) {
	key := afpPasswordKey("secret")
	plain := [8]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	out, err := randnumEncrypt(key, plain)
	if err != nil {
		t.Fatal(err)
	}
	if out == plain {
		t.Fatal("expected ciphertext != plaintext")
	}
	out2, err := randnumEncrypt(key, plain)
	if err != nil || out2 != out {
		t.Fatalf("encrypt not deterministic: %v vs %v", out, out2)
	}
}

func TestAfpPasswordKeyPads(t *testing.T) {
	key := afpPasswordKey("ab")
	if key[0] != 'a' || key[1] != 'b' {
		t.Fatalf("key prefix = %q", key[:2])
	}
	for i := 2; i < 8; i++ {
		if key[i] != 0 {
			t.Fatalf("key[%d] = %#x, want NUL pad", i, key[i])
		}
	}
}

func TestAfpPasswordKeyBlank(t *testing.T) {
	key := afpPasswordKey("")
	for i, b := range key {
		if b != 0 {
			t.Fatalf("blank password key[%d] = %#x, want 0", i, b)
		}
	}
}
