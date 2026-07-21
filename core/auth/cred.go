package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
)

// Credential parameters. salt and hash are stored; the iteration count and key
// length are fixed by this build so a stored record is self-describing without a
// cost field. PBKDF2-HMAC-SHA256 is implemented here over crypto/hmac +
// crypto/sha256 so the package needs no golang.org/x/crypto dependency.
//
// core discipline (§1 / archtest): this file imports only crypto/hmac,
// crypto/sha256 and crypto/subtle — all reflection-free. It deliberately does NOT
// import crypto/rand (which transitively pulls reflect) or encoding/hex (likewise):
// SALT GENERATION is the caller's job (a store adapter, which may use crypto/rand
// in the adapter ring), and hex coding is hand-rolled below. So the contract stays
// TinyGo-clean while the randomness lives where reflect is allowed.
const (
	SaltLen        = 16     // expected salt length in bytes (the adapter generates it)
	credIterations = 100000 // PBKDF2 iteration count
	credKeyLen     = 32     // derived key length (SHA-256 output size)
)

// ErrBadCredentialRecord is returned when a stored salt/hash record cannot be
// decoded (wrong length or non-hex) — a corrupt users file line.
var ErrBadCredentialRecord = errors.New("auth: malformed credential record")

// Credential is the stored secret for one user: the salt and the PBKDF2-SHA256
// derivation of the password under that salt. The plaintext password is never
// stored. Salt and Hash are raw bytes; a store serialises them via SaltHex/HashHex.
type Credential struct {
	Salt []byte
	Hash []byte
}

// DeriveCredential derives a Credential for password under the supplied salt. The
// caller (a store adapter) provides the salt — generated with crypto/rand for a
// new user, or decoded from storage when re-deriving. Keeping rand out of here is
// what lets core/auth stay reflection-free.
func DeriveCredential(password string, salt []byte) Credential {
	return Credential{
		Salt: salt,
		Hash: pbkdf2SHA256([]byte(password), salt, credIterations, credKeyLen),
	}
}

// Verify reports whether password matches the credential, in constant time. A
// zero-value (no Salt/Hash) credential never verifies.
func (c Credential) Verify(password string) bool {
	if len(c.Salt) == 0 || len(c.Hash) == 0 {
		return false
	}
	got := pbkdf2SHA256([]byte(password), c.Salt, credIterations, credKeyLen)
	return subtle.ConstantTimeCompare(got, c.Hash) == 1
}

// SaltHex / HashHex return the hex encodings a text store serialises.
func (c Credential) SaltHex() string { return encodeHex(c.Salt) }
func (c Credential) HashHex() string { return encodeHex(c.Hash) }

// ParseCredential decodes a hex salt/hash pair back into a Credential, validating
// the lengths so a corrupt record fails loudly rather than silently never matching.
func ParseCredential(saltHex, hashHex string) (Credential, error) {
	salt, ok := decodeHex(saltHex)
	if !ok || len(salt) != SaltLen {
		return Credential{}, ErrBadCredentialRecord
	}
	hash, ok := decodeHex(hashHex)
	if !ok || len(hash) != credKeyLen {
		return Credential{}, ErrBadCredentialRecord
	}
	return Credential{Salt: salt, Hash: hash}, nil
}

// TODO: Move this to are shared binaryprimitives
// --- hand-rolled hex (encoding/hex transitively imports reflect; §1). ---

const hexDigits = "0123456789abcdef"

func encodeHex(b []byte) string {
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = hexDigits[v>>4]
		out[i*2+1] = hexDigits[v&0x0f]
	}
	return string(out)
}

func decodeHex(s string) ([]byte, bool) {
	if len(s)%2 != 0 {
		return nil, false
	}
	out := make([]byte, len(s)/2)
	for i := 0; i < len(out); i++ {
		hi, ok1 := hexNibble(s[i*2])
		lo, ok2 := hexNibble(s[i*2+1])
		if !ok1 || !ok2 {
			return nil, false
		}
		out[i] = hi<<4 | lo
	}
	return out, true
}

func hexNibble(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// pbkdf2SHA256 is PBKDF2 (RFC 2898) with HMAC-SHA256 as the PRF. keyLen here is
// always ≤ the 32-byte HMAC-SHA256 output, so a single block (i=1) suffices; the
// loop is written for the general case regardless.
func pbkdf2SHA256(password, salt []byte, iter, keyLen int) []byte {
	prf := hmac.New(sha256.New, password)
	hLen := prf.Size()
	numBlocks := (keyLen + hLen - 1) / hLen
	out := make([]byte, 0, numBlocks*hLen)

	var blockIdx [4]byte
	u := make([]byte, 0, hLen)
	for block := 1; block <= numBlocks; block++ {
		blockIdx[0] = byte(block >> 24)
		blockIdx[1] = byte(block >> 16)
		blockIdx[2] = byte(block >> 8)
		blockIdx[3] = byte(block)

		prf.Reset()
		prf.Write(salt)
		prf.Write(blockIdx[:])
		u = prf.Sum(u[:0])

		t := make([]byte, hLen)
		copy(t, u)
		for n := 2; n <= iter; n++ {
			prf.Reset()
			prf.Write(u)
			u = prf.Sum(u[:0])
			for i := range t {
				t[i] ^= u[i]
			}
		}
		out = append(out, t...)
	}
	return out[:keyLen]
}
