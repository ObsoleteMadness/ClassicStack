package encoding

import (
	"errors"
	"unicode/utf16"
	"unicode/utf8"
)

// ErrTruncatedUTF16 reports UTF-16 input whose length is not a whole number of
// 16-bit code units (an odd byte count, i.e. a truncated final unit).
var ErrTruncatedUTF16 = errors.New("encoding: truncated UTF-16 code unit")

// UTF16BOM is the byte-order mark used to flag UTF-16. SMB NT names are
// UTF-16LE on the wire; a leading BOM, when present, is stripped.
const (
	utf16BOMLE = 0xFEFF
	utf16BOMBE = 0xFFFE // a 0xFEFF code unit read as big-endian
)

// UTF16LEToUTF8 converts little-endian UTF-16 bytes (the SMB NT wire form) to a
// UTF-8 string. It strips an optional leading BOM and resolves surrogate pairs.
// Odd-length input (a truncated final unit) returns ErrTruncatedUTF16 rather
// than panicking or silently dropping the trailing byte.
func UTF16LEToUTF8(src []byte) (string, error) {
	if len(src)%2 != 0 {
		return "", ErrTruncatedUTF16
	}
	if len(src) == 0 {
		return "", nil
	}
	units := make([]uint16, 0, len(src)/2)
	for i := 0; i < len(src); i += 2 {
		units = append(units, uint16(src[i])|uint16(src[i+1])<<8)
	}
	// Strip a single leading BOM (either endianness flag). A 0xFFFE leading
	// unit means the producer wrote big-endian; we do not re-decode here
	// because the wire contract is LE — we only drop the marker.
	if units[0] == utf16BOMLE || units[0] == utf16BOMBE {
		units = units[1:]
	}
	return string(utf16.Decode(units)), nil
}

// UTF8ToUTF16LE converts a UTF-8 string to little-endian UTF-16 bytes (the SMB
// NT wire form) with no BOM. Lone surrogates in the input are emitted as the
// Unicode replacement character by the stdlib encoder, matching Windows.
func UTF8ToUTF16LE(s string) []byte {
	if !utf8.ValidString(s) {
		// utf16.Encode operates on runes; invalid UTF-8 would already have
		// been replaced when ranged. Keep the contract explicit.
		s = string([]rune(s))
	}
	units := utf16.Encode([]rune(s))
	out := make([]byte, 0, len(units)*2)
	for _, u := range units {
		out = append(out, byte(u), byte(u>>8))
	}
	return out
}
