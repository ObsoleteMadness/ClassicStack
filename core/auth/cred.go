package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
)

// Credential parameters. salt and hash are stored; the iteration count and key
// length are fixed by this build so a stored record is self-describing without a
// cost field. PBKDF2-HMAC-SHA256 is implemented here over crypto/hmac +
// crypto/sha256 so the package needs no golang.org/x/crypto dependency.
//
// core discipline (§1 / archtest): crypto/hmac, crypto/sha256, crypto/subtle,
// and crypto/rand are all fine in core — reflect itself builds and links under
// TinyGo (see core/csnet/random.go); the archtest gate bans specific generic
// reflection-based *serialization* packages (encoding/json, encoding/binary,
// database/sql), not reflect itself. So salt generation (NewCredential) lives
// here now rather than being the caller's job; hex coding (SaltHex/HashHex/
// ParseCredential) goes through core/binaryprimitives' shared codec.
const (
	SaltLen        = 16     // salt length in bytes NewCredential generates
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

// DeriveCredential derives a Credential for password under the supplied salt.
// Used to re-derive an existing user's credential (e.g. to re-verify against a
// stored salt) or by NewCredential for a fresh one; a caller decoding a stored
// record uses this directly with the salt from storage.
func DeriveCredential(password string, salt []byte) Credential {
	return Credential{
		Salt: salt,
		Hash: pbkdf2SHA256([]byte(password), salt, credIterations, credKeyLen),
	}
}

// NewCredential generates a fresh random SaltLen-byte salt and derives a
// Credential for password under it — the counterpart to DeriveCredential for
// creating a new user (SetUser/change-password) rather than re-verifying one
// already on disk.
func NewCredential(password string) (Credential, error) {
	salt := make([]byte, SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return Credential{}, err
	}
	return DeriveCredential(password, salt), nil
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
func (c Credential) SaltHex() string { return bp.EncodeHex(c.Salt) }
func (c Credential) HashHex() string { return bp.EncodeHex(c.Hash) }

// ParseCredential decodes a hex salt/hash pair back into a Credential, validating
// the lengths so a corrupt record fails loudly rather than silently never matching.
func ParseCredential(saltHex, hashHex string) (Credential, error) {
	salt, ok := bp.DecodeHex(saltHex)
	if !ok || len(salt) != SaltLen {
		return Credential{}, ErrBadCredentialRecord
	}
	hash, ok := bp.DecodeHex(hashHex)
	if !ok || len(hash) != credKeyLen {
		return Credential{}, ErrBadCredentialRecord
	}
	return Credential{Salt: salt, Hash: hash}, nil
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
