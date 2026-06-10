package fs

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/ObsoleteMadness/ClassicStack/core/encoding"
)

// ReservedSet declares which runes a store backend cannot hold in a path
// element, so the codec escapes them reversibly as "0xNN" tokens instead of
// writing a path the host filesystem would reject or mis-split. The set is
// backend-declared (POSIX bytes vs NTFS vs FAT vs S3 url-safe), never derived
// from runtime.GOOS — a share served from one host can store names that are
// legal for the backend it actually writes to.
type ReservedSet struct {
	// Name identifies the set in diagnostics (e.g. "posix", "ntfs").
	Name string
	// reserved holds the reserved runes (in addition to the always-reserved
	// control chars < 0x20, which every backend escapes).
	reserved map[rune]struct{}
}

// reserved character sets, mirroring the host-reserved escaping that
// service/afp/path_codec.go applied per runtime.GOOS — now declared per
// backend so the choice is explicit and testable.
var (
	// ReservedPOSIX escapes NUL and '/' only — a POSIX filesystem path element
	// may contain any other byte.
	ReservedPOSIX = newReservedSet("posix", '/')
	// ReservedNTFS escapes the Win32 reserved set so a name stored on NTFS
	// round-trips. Matches the old isHostReservedRune Windows branch.
	ReservedNTFS = newReservedSet("ntfs", '<', '>', ':', '"', '/', '\\', '|', '?', '*')
)

func newReservedSet(name string, runes ...rune) ReservedSet {
	m := make(map[rune]struct{}, len(runes))
	for _, r := range runes {
		m[r] = struct{}{}
	}
	return ReservedSet{Name: name, reserved: m}
}

// isReserved reports whether r must be escaped for this backend. Control
// characters (< 0x20) are always reserved.
func (rs ReservedSet) isReserved(r rune) bool {
	if r < 0x20 {
		return true
	}
	if rs.reserved == nil {
		return false
	}
	_, ok := rs.reserved[r]
	return ok
}

// escape rewrites reserved runes in a UTF-8 store string as "0xNN" tokens.
// Ported from path_codec.go encodeHostReservedChars; the token form is the
// uppercase two-hex-digit code point so it round-trips through unescape.
func (rs ReservedSet) escape(s string) string {
	needs := false
	for _, r := range s {
		if rs.isReserved(r) {
			needs = true
			break
		}
	}
	if !needs {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if rs.isReserved(r) {
			fmt.Fprintf(&b, "0x%02X", r)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// unescape reverses escape: "0xNN" tokens whose code point is reserved for this
// backend become the original rune. Tokens for non-reserved code points are left
// literal, matching the old decodeHostReservedTokens behaviour.
func (rs ReservedSet) unescape(s string) string {
	if !strings.Contains(s, "0x") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if i+4 <= len(s) && s[i] == '0' && s[i+1] == 'x' {
			h, okH := fromHex(s[i+2])
			l, okL := fromHex(s[i+3])
			if okH && okL {
				c := rune((h << 4) | l)
				if rs.isReserved(c) {
					b.WriteRune(c)
					i += 4
					continue
				}
			}
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		b.WriteRune(r)
		i += size
	}
	return b.String()
}

func fromHex(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	default:
		return 0, false
	}
}

// transcodeCodec is the single FilenameCodec implementation. It threads three
// concerns the old service mixed together:
//   - wire transcode: client charset (MacRoman/UTF-8/ANSI/UTF-16) <-> a UTF-8
//     intermediate, picked per request by the service from the path-type byte.
//   - store charset: the intermediate UTF-8 is mapped to the backend's store
//     bytes (utf8, macroman, or posix-bytes identity).
//   - reserved-char escaping: reversible "0xNN" tokens for the backend's
//     declared ReservedSet.
type transcodeCodec struct {
	profile  FilenameProfile
	store    storeCharset
	escaping bool
	ansiCP   encoding.CodePage
}

type storeCharset uint8

const (
	storePOSIXBytes storeCharset = iota // identity: store == wire-derived UTF-8 bytes
	storeUTF8                           // store is UTF-8 text
	storeMacRoman                       // store is MacRoman bytes
)

func (c transcodeCodec) supports(w WireEncoding) bool {
	return slices.Contains(c.profile.Wire, w)
}

// wireToUTF8 decodes wire bytes in src charset to a UTF-8 intermediate string.
func (c transcodeCodec) wireToUTF8(wire []byte, src WireEncoding) (string, error) {
	switch src {
	case WireUTF8:
		return string(wire), nil
	case WireMacRoman:
		return encoding.MacRomanToUTF8(wire), nil
	case WireANSI:
		return encoding.ANSIToUTF8(wire, c.ansiCP)
	case WireUTF16:
		return encoding.UTF16LEToUTF8(wire)
	default:
		return "", ErrWireUnsupported
	}
}

// utf8ToWire encodes a UTF-8 intermediate string back to wire bytes in dst charset.
func (c transcodeCodec) utf8ToWire(s string, dst WireEncoding) ([]byte, error) {
	switch dst {
	case WireUTF8:
		return []byte(s), nil
	case WireMacRoman:
		b, err := encoding.UTF8ToMacRoman(s)
		if err != nil {
			return nil, ErrUnrepresentable
		}
		return b, nil
	case WireANSI:
		b, err := encoding.UTF8ToANSI(s, c.ansiCP)
		if err != nil {
			return nil, ErrUnrepresentable
		}
		return b, nil
	case WireUTF16:
		return encoding.UTF8ToUTF16LE(s), nil
	default:
		return nil, ErrWireUnsupported
	}
}

// utf8ToStore maps the UTF-8 intermediate to the backend store bytes.
func (c transcodeCodec) utf8ToStore(s string) (StoredName, error) {
	switch c.store {
	case storeUTF8, storePOSIXBytes:
		return StoredName(s), nil
	case storeMacRoman:
		b, err := encoding.UTF8ToMacRoman(s)
		if err != nil {
			return nil, ErrUnrepresentable
		}
		return StoredName(b), nil
	default:
		return nil, ErrUnrepresentable
	}
}

// storeToUTF8 reverses utf8ToStore.
func (c transcodeCodec) storeToUTF8(stored StoredName) string {
	switch c.store {
	case storeMacRoman:
		return encoding.MacRomanToUTF8(stored)
	default:
		return string(stored)
	}
}

func (c transcodeCodec) Decode(wire []byte, src WireEncoding) (StoredName, error) {
	if !c.supports(src) {
		return nil, ErrWireUnsupported
	}
	mid, err := c.wireToUTF8(wire, src)
	if err != nil {
		// A wire decode failure (e.g. truncated UTF-16) is an unrepresentable
		// name, not a wire-unsupported codec.
		if errors.Is(err, ErrWireUnsupported) {
			return nil, err
		}
		return nil, ErrUnrepresentable
	}
	if c.escaping {
		mid = c.profile.Reserved.escape(mid)
	}
	stored, err := c.utf8ToStore(mid)
	if err != nil {
		return nil, err
	}
	if c.profile.MaxElement > 0 && len(stored) > c.profile.MaxElement {
		return nil, ErrUnrepresentable
	}
	if c.profile.Validate != nil {
		if err := c.profile.Validate(stored); err != nil {
			return nil, ErrUnrepresentable
		}
	}
	return stored, nil
}

func (c transcodeCodec) Encode(stored StoredName, dst WireEncoding) ([]byte, error) {
	if !c.supports(dst) {
		return nil, ErrWireUnsupported
	}
	mid := c.storeToUTF8(stored)
	if c.escaping {
		mid = c.profile.Reserved.unescape(mid)
	}
	return c.utf8ToWire(mid, dst)
}

func (c transcodeCodec) Wire() []WireEncoding     { return c.profile.Wire }
func (c transcodeCodec) Profile() FilenameProfile { return c.profile }

// validatePOSIXElement rejects elements a POSIX store can never hold: an
// embedded NUL or '/'. With reserved-char escaping enabled these never survive
// to the store; the validator is the backstop for codecs that skip escaping.
func validatePOSIXElement(elem StoredName) error {
	for _, b := range elem {
		if b == 0 || b == '/' {
			return ErrUnrepresentable
		}
	}
	return nil
}

// NewIdentityFilenameCodec returns a codec that stores wire-derived UTF-8 bytes
// verbatim (no charset transcode), with POSIX reserved-char escaping. Used by
// the "utf8" / "identity" share codec names.
func NewIdentityFilenameCodec() FilenameCodec {
	return transcodeCodec{
		profile: FilenameProfile{
			Wire:         []WireEncoding{WireMacRoman, WireUTF8, WireANSI, WireUTF16},
			StoreCharset: "posix-bytes",
			Reserved:     ReservedPOSIX,
			Validate:     validatePOSIXElement,
		},
		store:    storePOSIXBytes,
		escaping: true,
		ansiCP:   encoding.CP437,
	}
}

// NewMacRomanUTF8FilenameCodec transcodes MacRoman/UTF-8 wire names to a UTF-8
// store (the macroman-utf8 default). This is the lifted service/afp path codec:
// MacRoman in, UTF-8 on disk, reversible reserved-char escaping.
func NewMacRomanUTF8FilenameCodec() FilenameCodec {
	return transcodeCodec{
		profile: FilenameProfile{
			Wire:         []WireEncoding{WireMacRoman, WireUTF8},
			StoreCharset: "utf8",
			Reserved:     ReservedPOSIX,
			Validate:     validatePOSIXElement,
		},
		store:    storeUTF8,
		escaping: true,
		ansiCP:   encoding.CP437,
	}
}

// NewMacRomanNativeFilenameCodec stores MacRoman bytes natively (no UTF-8
// transcode) for backends — e.g. an HFS image — whose on-disk charset is
// MacRoman. Only MacRoman wire names are representable.
func NewMacRomanNativeFilenameCodec() FilenameCodec {
	return transcodeCodec{
		profile: FilenameProfile{
			Wire:         []WireEncoding{WireMacRoman},
			StoreCharset: "macroman",
			Reserved:     ReservedPOSIX,
			Validate:     validatePOSIXElement,
		},
		store:    storeMacRoman,
		escaping: true,
		ansiCP:   encoding.CP437,
	}
}

func codecByName(name string) (FilenameCodec, error) {
	switch strings.ToLower(name) {
	case "identity", "utf8":
		return NewIdentityFilenameCodec(), nil
	case "macroman-utf8":
		return NewMacRomanUTF8FilenameCodec(), nil
	case "macroman-native":
		return NewMacRomanNativeFilenameCodec(), nil
	default:
		return nil, errors.New("fs: unknown filename codec")
	}
}
