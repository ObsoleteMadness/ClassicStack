package encoding

import "errors"

// ErrUnmappableANSI reports a UTF-8 rune that the selected OEM code page
// cannot represent.
var ErrUnmappableANSI = errors.New("encoding: rune not mappable to code page")

// CodePage identifies an 8-bit OEM/ANSI code page negotiated by an SMB client.
// SMB legacy ("DOS") names are single-byte in the negotiated OEM code page; the
// default for early DOS/Windows clients is CP437.
type CodePage uint16

const (
	// CP437 is the original IBM PC / DOS OEM code page. It is the default
	// chosen for SMB legacy filenames — see spec/ansi-codepage.md.
	CP437 CodePage = 437
)

// cp437ToRune maps the upper half (0x80..0xFF) of CP437 to Unicode runes. The
// lower half (0x00..0x7F) is ASCII-identity. Hand-written, reflection-free,
// matching the canonical IBM CP437 table.
var cp437ToRune = [128]rune{
	// 0x80
	0x00C7, 0x00FC, 0x00E9, 0x00E2, 0x00E4, 0x00E0, 0x00E5, 0x00E7,
	0x00EA, 0x00EB, 0x00E8, 0x00EF, 0x00EE, 0x00EC, 0x00C4, 0x00C5,
	// 0x90
	0x00C9, 0x00E6, 0x00C6, 0x00F4, 0x00F6, 0x00F2, 0x00FB, 0x00F9,
	0x00FF, 0x00D6, 0x00DC, 0x00A2, 0x00A3, 0x00A5, 0x20A7, 0x0192,
	// 0xA0
	0x00E1, 0x00ED, 0x00F3, 0x00FA, 0x00F1, 0x00D1, 0x00AA, 0x00BA,
	0x00BF, 0x2310, 0x00AC, 0x00BD, 0x00BC, 0x00A1, 0x00AB, 0x00BB,
	// 0xB0
	0x2591, 0x2592, 0x2593, 0x2502, 0x2524, 0x2561, 0x2562, 0x2556,
	0x2555, 0x2563, 0x2551, 0x2557, 0x255D, 0x255C, 0x255B, 0x2510,
	// 0xC0
	0x2514, 0x2534, 0x252C, 0x251C, 0x2500, 0x253C, 0x255E, 0x255F,
	0x255A, 0x2554, 0x2569, 0x2566, 0x2560, 0x2550, 0x256C, 0x2567,
	// 0xD0
	0x2568, 0x2564, 0x2565, 0x2559, 0x2558, 0x2552, 0x2553, 0x256B,
	0x256A, 0x2518, 0x250C, 0x2588, 0x2584, 0x258C, 0x2590, 0x2580,
	// 0xE0
	0x03B1, 0x00DF, 0x0393, 0x03C0, 0x03A3, 0x03C3, 0x00B5, 0x03C4,
	0x03A6, 0x0398, 0x03A9, 0x03B4, 0x221E, 0x03C6, 0x03B5, 0x2229,
	// 0xF0
	0x2261, 0x00B1, 0x2265, 0x2264, 0x2320, 0x2321, 0x00F7, 0x2248,
	0x00B0, 0x2219, 0x00B7, 0x221A, 0x207F, 0x00B2, 0x25A0, 0x00A0,
}

var runeToCP437 map[rune]byte

func init() {
	runeToCP437 = make(map[rune]byte, 256)
	for i := range 0x80 {
		runeToCP437[rune(i)] = byte(i)
	}
	for i, r := range cp437ToRune {
		b := byte(0x80 + i)
		if _, ok := runeToCP437[r]; !ok {
			runeToCP437[r] = b
		}
	}
}

// ANSIToUTF8 converts single-byte OEM/ANSI bytes in the given code page to a
// UTF-8 string. The low 7 bits are ASCII-identity for every supported page.
func ANSIToUTF8(src []byte, cp CodePage) (string, error) {
	table, err := cpTable(cp)
	if err != nil {
		return "", err
	}
	out := make([]rune, 0, len(src))
	for _, b := range src {
		if b < 0x80 {
			out = append(out, rune(b))
			continue
		}
		out = append(out, table[b-0x80])
	}
	return string(out), nil
}

// UTF8ToANSI converts a UTF-8 string to single-byte OEM/ANSI bytes in the given
// code page, failing with ErrUnmappableANSI on a rune the page cannot hold.
func UTF8ToANSI(s string, cp CodePage) ([]byte, error) {
	rev, err := cpReverse(cp)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(s))
	for _, r := range s {
		b, ok := rev[r]
		if !ok {
			return nil, ErrUnmappableANSI
		}
		out = append(out, b)
	}
	return out, nil
}

func cpTable(cp CodePage) (*[128]rune, error) {
	switch cp {
	case CP437, 0: // 0 = default OEM page
		return &cp437ToRune, nil
	default:
		return nil, ErrUnmappableANSI
	}
}

func cpReverse(cp CodePage) (map[rune]byte, error) {
	switch cp {
	case CP437, 0:
		return runeToCP437, nil
	default:
		return nil, ErrUnmappableANSI
	}
}
