// Package binaryprimitives provides the project's shared fixed-width big- and
// little-endian integer codecs: the hand-rolled byte-order helpers every core
// protocol and service needs, in one place instead of re-derived per package.
//
// Why this exists rather than encoding/binary: the core ring forbids
// encoding/binary because it transitively imports reflect, which breaks the
// no-reflection rule (TinyGo + allocation discipline; enforced by
// core/internal/archtest). Each core package therefore used to hand-roll its own
// be16/putLE32/etc., duplicating the same shifts a dozen times. This package is
// the single, reflection-free home for those primitives; depend on it instead of
// copying the helpers.
//
// Three call styles are provided for each width and byte order, because the
// codebase legitimately uses all three:
//
//   - Readers decode from the front of a slice: BE16(b), BE32(b), LE16(b), …
//     The caller guarantees the slice is long enough (these panic on a short
//     slice, like the stdlib's binary.BigEndian.Uint16, so a framing bug fails
//     loudly rather than silently truncating).
//   - Put* writers encode in place into a pre-sized slice: PutBE16(dst, v), …
//     They write exactly 2/4/8 bytes at dst[0:] and return nothing — the form a
//     packer that has already allocated its buffer uses.
//   - Append* writers grow and return a slice: AppendBE16(dst, v) []byte, …
//     The form a packer that builds its output incrementally uses.
//
// The package has no dependencies and pulls in nothing; it is safe for every ring.
package binaryprimitives
