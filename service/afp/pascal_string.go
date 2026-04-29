//go:build afp || all

package afp

import "github.com/pgodw/omnitalk/encoding"

// ReadPascalString reads a length-prefixed MacRoman string at idx and returns UTF-8 text plus bytes consumed.
func ReadPascalString(data []byte, idx int) (string, int) {
	if idx >= len(data) {
		return "", 0
	}
	length := int(data[idx])
	if idx+1+length > len(data) {
		return "", 0
	}
	return encoding.MacRomanToUTF8(data[idx+1 : idx+1+length]), length + 1
}

// WritePascalString appends a UTF-8 string as a Pascal-style MacRoman string.
func WritePascalString(dst []byte, value string) []byte {
	encoded := encoding.UTF8ToMacRoman(value)
	if len(encoded) > 255 {
		encoded = encoded[:255]
	}
	dst = append(dst, byte(len(encoded)))
	return append(dst, encoded...)
}
