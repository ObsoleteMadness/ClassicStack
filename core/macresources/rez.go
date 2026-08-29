package macresources

// rez.go is the text side of the macresources codec: the Rez-like "rdump"/DeRez format
// that represents a resource fork as human-readable, version-controllable text. Ported
// from Elliot Nunn's macresources (make_rez_code / parse_rez_code):
// https://github.com/elliotnunn/macresources
//
// One resource renders as:
//
//	data 'TYPE' (id, "name", attrs) {
//		$"0011 2233 4455 6677 8899 AABB CCDD EEFF"  /* ........ */
//	};
//
// where the name and attribute clauses are omitted when empty/zero, the hex body is
// laid out 16 bytes per line with an ASCII comment, and a single quote inside a TYPE or
// a special char inside a name is escaped \0xHH. ParseRez is the inverse.

import (
	"errors"
	"strconv"
	"strings"
)

// FormatRez renders resources as Rez/rdump text (records → text).
func FormatRez(res []Resource) []byte {
	var b strings.Builder
	for i, r := range res {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("data ")
		b.WriteString(rezQuoteType(r.Type))
		b.WriteString(" (")
		b.WriteString(strconv.Itoa(int(r.ID)))
		if r.HasName {
			b.WriteString(", ")
			b.WriteString(rezQuoteString(r.Name))
		}
		if attrs := rezAttrs(r.Attribs); attrs != "" {
			b.WriteString(", ")
			b.WriteString(attrs)
		}
		b.WriteString(") {\n")
		writeHexBody(&b, r.Data)
		b.WriteString("};\n")
	}
	return []byte(b.String())
}

// rezAttrs renders the attribute byte as space-or-comma… actually as the named flags
// the reference recognises, joined by " | "; unknown bits fall back to a $HH literal.
func rezAttrs(a byte) string {
	if a == 0 {
		return ""
	}
	var parts []string
	named := []struct {
		bit  byte
		name string
	}{
		{AttrSysHeap, "sysheap"},
		{AttrPurgeable, "purgeable"},
		{AttrLocked, "locked"},
		{AttrProtected, "protected"},
		{AttrPreload, "preload"},
	}
	rest := a
	for _, n := range named {
		if a&n.bit != 0 {
			parts = append(parts, n.name)
			rest &^= n.bit
		}
	}
	if rest != 0 {
		parts = append(parts, "$"+twoHex(rest))
	}
	return strings.Join(parts, " | ")
}

// writeHexBody writes the data as $"...." lines of 16 bytes with an ASCII comment.
func writeHexBody(b *strings.Builder, data []byte) {
	for off := 0; off < len(data); off += 16 {
		end := off + 16
		if end > len(data) {
			end = len(data)
		}
		chunk := data[off:end]
		b.WriteString("\t$\"")
		for i, by := range chunk {
			if i > 0 && i%2 == 0 {
				b.WriteByte(' ')
			}
			b.WriteString(twoHex(by))
		}
		b.WriteString("\" /* ")
		for _, by := range chunk {
			if by >= 0x20 && by < 0x7f {
				b.WriteByte(by)
			} else {
				b.WriteByte('.')
			}
		}
		b.WriteString(" */\n")
	}
}

func rezQuoteType(t [4]byte) string {
	var b strings.Builder
	b.WriteByte('\'')
	for _, c := range t {
		if c == '\'' || c == '\\' || c < 0x20 || c >= 0x7f {
			b.WriteString("\\0x")
			b.WriteString(twoHex(c))
		} else {
			b.WriteByte(c)
		}
	}
	b.WriteByte('\'')
	return b.String()
}

func rezQuoteString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"' || c == '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		case c >= 0x20 && c < 0x7f:
			b.WriteByte(c)
		default:
			b.WriteString("\\0x")
			b.WriteString(twoHex(c))
		}
	}
	b.WriteByte('"')
	return b.String()
}

func twoHex(b byte) string {
	const hex = "0123456789ABCDEF"
	return string([]byte{hex[b>>4], hex[b&0x0f]})
}

// ErrBadRez is returned when the rdump text cannot be parsed.
var ErrBadRez = errors.New("macresources: malformed rez/rdump text")

// ParseRez parses Rez/rdump text into resources (text → records). It is tolerant of
// whitespace and the /* ... */ ASCII comments inside the hex body; it requires the
// `data 'TYPE' (id[, "name"][, attrs]) { $"..." } ;` shape.
func ParseRez(text []byte) ([]Resource, error) {
	s := string(text)
	var out []Resource
	i := 0
	for {
		// Find the next "data" keyword.
		j := indexWord(s, i, "data")
		if j < 0 {
			break
		}
		i = j + 4
		i = skipSpace(s, i)

		// Type: 'TYPE' (with possible \0xHH escapes).
		rtype, ni, err := parseQuotedType(s, i)
		if err != nil {
			return nil, err
		}
		i = skipSpace(s, ni)

		if i >= len(s) || s[i] != '(' {
			return nil, ErrBadRez
		}
		i++ // '('
		// ID: signed integer.
		id, ni2, err := parseInt(s, i)
		if err != nil {
			return nil, err
		}
		i = skipSpace(s, ni2)

		res := Resource{Type: rtype, ID: int16(id)}

		// Optional ", name" and ", attrs" clauses until ')'.
		for i < len(s) && s[i] == ',' {
			i = skipSpace(s, i+1)
			if i < len(s) && s[i] == '"' {
				name, ni3, err := parseQuotedString(s, i)
				if err != nil {
					return nil, err
				}
				res.Name = name
				res.HasName = true
				i = skipSpace(s, ni3)
			} else {
				attr, ni3, err := parseAttrs(s, i)
				if err != nil {
					return nil, err
				}
				res.Attribs |= attr
				i = skipSpace(s, ni3)
			}
		}
		if i >= len(s) || s[i] != ')' {
			return nil, ErrBadRez
		}
		i = skipSpace(s, i+1)
		if i >= len(s) || s[i] != '{' {
			return nil, ErrBadRez
		}
		i++ // '{'

		// Body: collect hex from every $"..." up to the closing '}'.
		data, ni4, err := parseHexBody(s, i)
		if err != nil {
			return nil, err
		}
		res.Data = data
		i = ni4

		// Expect '}' then optional ';'.
		if i >= len(s) || s[i] != '}' {
			return nil, ErrBadRez
		}
		i++
		i = skipSpace(s, i)
		if i < len(s) && s[i] == ';' {
			i++
		}
		out = append(out, res)
	}
	return out, nil
}

// --- small text-scan helpers ---

func skipSpace(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\r' || s[i] == '\n') {
		i++
	}
	return i
}

// indexWord finds the keyword starting at or after i, on a word boundary.
func indexWord(s string, i int, word string) int {
	for {
		k := strings.Index(s[i:], word)
		if k < 0 {
			return -1
		}
		pos := i + k
		before := pos == 0 || !isWordByte(s[pos-1])
		afterIdx := pos + len(word)
		after := afterIdx >= len(s) || !isWordByte(s[afterIdx])
		if before && after {
			return pos
		}
		i = pos + len(word)
	}
}

func isWordByte(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func parseQuotedType(s string, i int) ([4]byte, int, error) {
	var t [4]byte
	if i >= len(s) || s[i] != '\'' {
		return t, i, ErrBadRez
	}
	i++
	var bytes []byte
	for i < len(s) && s[i] != '\'' {
		if strings.HasPrefix(s[i:], "\\0x") && i+5 <= len(s) {
			v, err := strconv.ParseUint(s[i+3:i+5], 16, 8)
			if err != nil {
				return t, i, ErrBadRez
			}
			bytes = append(bytes, byte(v))
			i += 5
			continue
		}
		bytes = append(bytes, s[i])
		i++
	}
	if i >= len(s) {
		return t, i, ErrBadRez
	}
	i++ // closing quote
	if len(bytes) != 4 {
		return t, i, ErrBadRez
	}
	copy(t[:], bytes)
	return t, i, nil
}

func parseQuotedString(s string, i int) (string, int, error) {
	if i >= len(s) || s[i] != '"' {
		return "", i, ErrBadRez
	}
	i++
	var b []byte
	for i < len(s) && s[i] != '"' {
		if s[i] == '\\' && i+1 < len(s) {
			if strings.HasPrefix(s[i:], "\\0x") && i+5 <= len(s) {
				v, err := strconv.ParseUint(s[i+3:i+5], 16, 8)
				if err != nil {
					return "", i, ErrBadRez
				}
				b = append(b, byte(v))
				i += 5
				continue
			}
			b = append(b, s[i+1])
			i += 2
			continue
		}
		b = append(b, s[i])
		i++
	}
	if i >= len(s) {
		return "", i, ErrBadRez
	}
	i++ // closing quote
	return string(b), i, nil
}

func parseInt(s string, i int) (int, int, error) {
	i = skipSpace(s, i)
	start := i
	if i < len(s) && (s[i] == '-' || s[i] == '+') {
		i++
	}
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if start == i {
		return 0, i, ErrBadRez
	}
	v, err := strconv.Atoi(s[start:i])
	if err != nil {
		return 0, i, ErrBadRez
	}
	return v, i, nil
}

// parseAttrs reads one attribute token (name or $HH), possibly followed by more joined
// with '|'. It returns the OR of all the bits in this clause and the index after them.
func parseAttrs(s string, i int) (byte, int, error) {
	var acc byte
	for {
		i = skipSpace(s, i)
		if i < len(s) && s[i] == '$' {
			if i+3 > len(s) {
				return acc, i, ErrBadRez
			}
			v, err := strconv.ParseUint(s[i+1:i+3], 16, 8)
			if err != nil {
				return acc, i, ErrBadRez
			}
			acc |= byte(v)
			i += 3
		} else {
			start := i
			for i < len(s) && isWordByte(s[i]) {
				i++
			}
			if start == i {
				return acc, i, ErrBadRez
			}
			switch s[start:i] {
			case "sysheap":
				acc |= AttrSysHeap
			case "purgeable":
				acc |= AttrPurgeable
			case "locked":
				acc |= AttrLocked
			case "protected":
				acc |= AttrProtected
			case "preload":
				acc |= AttrPreload
			default:
				return acc, i, ErrBadRez
			}
		}
		j := skipSpace(s, i)
		if j < len(s) && s[j] == '|' {
			i = j + 1
			continue
		}
		return acc, i, nil
	}
}

// parseHexBody accumulates the bytes from every $"..." run up to the closing '}',
// skipping /* ... */ comments and whitespace. Returns the data and the index of '}'.
func parseHexBody(s string, i int) ([]byte, int, error) {
	var data []byte
	for i < len(s) {
		switch {
		case s[i] == '}':
			return data, i, nil
		case s[i] == '$' && i+1 < len(s) && s[i+1] == '"':
			i += 2
			var nib []byte
			for i < len(s) && s[i] != '"' {
				c := s[i]
				if isHex(c) {
					nib = append(nib, c)
				}
				i++
			}
			if i >= len(s) {
				return nil, i, ErrBadRez
			}
			i++ // closing quote
			if len(nib)%2 != 0 {
				return nil, i, ErrBadRez
			}
			for k := 0; k < len(nib); k += 2 {
				v, err := strconv.ParseUint(string(nib[k:k+2]), 16, 8)
				if err != nil {
					return nil, i, ErrBadRez
				}
				data = append(data, byte(v))
			}
		case strings.HasPrefix(s[i:], "/*"):
			end := strings.Index(s[i:], "*/")
			if end < 0 {
				return nil, i, ErrBadRez
			}
			i += end + 2
		default:
			i++
		}
	}
	return nil, i, ErrBadRez
}

func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}
