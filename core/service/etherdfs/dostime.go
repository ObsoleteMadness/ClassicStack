package etherdfs

import (
	"strings"
	"time"
)

// dosDateTime packs a time.Time into the DOS directory date/time double-word:
// the low 16 bits are the time (hours<<11 | minutes<<5 | seconds/2) and the high
// 16 bits are the date (years-since-1980<<9 | month<<5 | day). A zero time yields
// the DOS epoch (1980-01-01 00:00:00).
func dosDateTime(t time.Time) uint32 {
	if t.IsZero() {
		t = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	year := max(t.Year(), 1980)
	date := uint32(year-1980)<<9 | uint32(t.Month())<<5 | uint32(t.Day())
	tm := uint32(t.Hour())<<11 | uint32(t.Minute())<<5 | uint32(t.Second()/2)
	return date<<16 | tm
}

// matchWildcard reports whether an 8.3 short name matches a DOS wildcard mask.
// The mask is the 11-byte FCB form (already split into 8+3, space-padded), where
// '?' matches any single character and a space matches the padding. The name is
// the candidate's FCB form. This is the FCB-vs-FCB match the DOS FindNext uses:
// each of the 11 positions must be equal, or the mask position is '?'.
func matchFCB(mask, name [11]byte) bool {
	for i := range 11 {
		if mask[i] == '?' {
			continue
		}
		if mask[i] != name[i] {
			return false
		}
	}
	return true
}

// wildcardToFCBMask converts a DOS search pattern's final element (e.g. "*.TXT",
// "REPORT?.*") to its 11-byte FCB mask: '*' expands to '?' filling the rest of
// the base or extension, other characters map position-for-position, and unfilled
// positions become '?' so a bare "*.*" matches everything. The pattern is
// upper-cased.
func wildcardToFCBMask(pattern string) [11]byte {
	var mask [11]byte
	for i := range mask {
		mask[i] = ' '
	}
	pattern = strings.ToUpper(pattern)
	base, ext, _ := strings.Cut(pattern, ".")
	fillField(mask[0:8], base)
	fillField(mask[8:11], ext)
	return mask
}

// fillField copies a wildcard field into an FCB sub-field, expanding '*' to '?'
// for the remainder of the field and leaving unmatched trailing positions as '?'
// only when an explicit '*' was seen; otherwise trailing positions are spaces (so
// "AB" matches only "AB", but "AB*" matches "AB……").
func fillField(dst []byte, field string) {
	star := false
	i := 0
	for i < len(dst) && i < len(field) {
		c := field[i]
		if c == '*' {
			star = true
			break
		}
		dst[i] = c
		i++
	}
	if star {
		for ; i < len(dst); i++ {
			dst[i] = '?'
		}
	}
}
