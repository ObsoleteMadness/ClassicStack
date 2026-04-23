package afp

import (
	"bytes"
	"fmt"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/pgodw/omnitalk/encoding"
)

// AFPOptions controls AFP filename/path translation behavior.
type AFPOptions struct {
	// DecomposedFilenames enables host-reserved character escaping using 0xNN tokens.
	DecomposedFilenames bool
	// CNIDBackend selects the CNID backend by name. The default is "sqlite".
	CNIDBackend string
	// CNIDStoreBackend overrides CNIDBackend with a concrete backend implementation.
	CNIDStoreBackend CNIDBackend
	// DesktopBackend selects the DesktopDB backend by name. The default is "sqlite".
	DesktopBackend string
	// DesktopStoreBackend overrides DesktopBackend with a concrete backend implementation.
	DesktopStoreBackend DesktopDBBackend
	// AppleDoubleMode controls AppleDouble layout for the default metadata backend.
	AppleDoubleMode AppleDoubleMode
	// ExtensionMap provides a netatalk-compatible file-extension to type/creator fallback.
	ExtensionMap *ExtensionMap
	// ForkMetadataBackend overrides AppleDoubleMode with a concrete backend.
	ForkMetadataBackend ForkMetadataBackend
	// PersistentVolumeIDs assigns stable volume IDs derived from volume names.
	PersistentVolumeIDs bool
}

func DefaultAFPOptions() AFPOptions {
	return AFPOptions{DecomposedFilenames: true, CNIDBackend: "sqlite", DesktopBackend: "sqlite", AppleDoubleMode: defaultAppleDoubleMode}
}

func (s *AFPService) afpPathElementToHost(raw string) string {
	decoded := encoding.MacRomanToUTF8([]byte(raw))
	if !s.options.DecomposedFilenames {
		return decoded
	}
	return encodeHostReservedChars(decoded)
}

func (s *AFPService) hostNameToAFPBytes(hostName string, volID uint16) []byte {
	name := hostName
	// In legacy AppleDouble mode the Icon\r file is stored on disk as "Icon_".
	// Before encoding back to AFP we need to restore the original Mac name.
	if m := s.metaFor(volID); m != nil && name == m.IconFileName() && name == "Icon_" {
		name = "Icon\r"
	}
	if s.options.DecomposedFilenames {
		name = decodeHostReservedTokens(name)
	}
	return encoding.UTF8ToMacRoman(name)
}

func (s *AFPService) writeAFPName(buf *bytes.Buffer, hostName string, volID uint16) {
	nameBytes := s.hostNameToAFPBytes(hostName, volID)
	if len(nameBytes) > 255 {
		nameBytes = nameBytes[:255]
	}
	buf.WriteByte(byte(len(nameBytes)))
	buf.Write(nameBytes)
}

func encodeHostReservedChars(name string) string {
	var b strings.Builder
	for _, r := range name {
		if isHostReservedRune(r) {
			b.WriteString(fmt.Sprintf("0x%02X", r))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func decodeHostReservedTokens(name string) string {
	var b strings.Builder
	for i := 0; i < len(name); {
		if i+4 <= len(name) && name[i] == '0' && name[i+1] == 'x' {
			h, okH := fromHex(name[i+2])
			l, okL := fromHex(name[i+3])
			if okH && okL {
				c := rune((h << 4) | l)
				if isHostReservedRune(c) {
					b.WriteRune(c)
					i += 4
					continue
				}
			}
		}
		r, size := utf8.DecodeRuneInString(name[i:])
		b.WriteRune(r)
		i += size
	}
	return b.String()
}

func hasHostReservedChar(name string) bool {
	for _, r := range name {
		if isHostReservedRune(r) {
			return true
		}
	}
	return false
}

func isHostReservedRune(r rune) bool {
	if r < 0x20 {
		return true
	}
	if r > 0xFF {
		return false
	}

	c := byte(r)
	if runtime.GOOS == "windows" {
		switch c {
		case '<', '>', ':', '"', '/', '\\', '|', '?', '*':
			return true
		}
		return false
	}

	return c == '/'
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
