package archive

import (
	"encoding/binary"
	"strings"
)

const (
	binHexRLE = 0x90
)

var binHexDecode = func() [128]int16 {
	const alphabet = "!\"#$%&'()*+,-012345689@ABCDEFGHIJKLMNPQRSTUVXYZ[`abcdefhijklmpqr"
	var t [128]int16
	for i := range t {
		t[i] = -1
	}
	for i := 0; i < len(alphabet); i++ {
		t[alphabet[i]] = int16(i)
	}
	return t
}()

func looksBinHex(data []byte) bool {
	n := len(data)
	if n > 32768 {
		n = 32768
	}
	i := 0
	for i < n && isBinHexWS(data[i]) {
		i++
	}
	if i < n && data[i] == ':' {
		return true
	}
	var s strings.Builder
	for j := 0; j < n; j++ {
		if data[j] > 127 {
			break
		}
		s.WriteByte(data[j])
	}
	return strings.Contains(strings.ToLower(s.String()), "binhex")
}

func isBinHexWS(b byte) bool {
	return b == '\r' || b == '\n' || b == '\t' || b == ' '
}

func expandBinHex(data []byte) (*Node, error) {
	if !looksBinHex(data) {
		return nil, ErrUnsupportedFormat
	}
	text := string(data)
	if idx := strings.Index(strings.ToLower(text), "converted with binhex"); idx >= 0 {
		text = text[idx:]
	}
	start := strings.Index(text, ":")
	if start < 0 {
		return nil, ErrCorrupt
	}
	end := strings.Index(text[start+1:], ":")
	if end < 0 {
		return nil, ErrCorrupt
	}
	payload := text[start+1 : start+1+end]
	packed := decodeBinHex6(payload)
	if packed == nil {
		return nil, ErrCorrupt
	}
	raw := decodeBinHexRLE(packed)
	if len(raw) < 22 {
		return nil, ErrCorrupt
	}
	nameLen := int(raw[0])
	if nameLen < 1 || nameLen > 63 {
		return nil, ErrCorrupt
	}
	nameEnd := 1 + nameLen
	if nameEnd+20 > len(raw) || raw[nameEnd] != 0 {
		return nil, ErrCorrupt
	}
	headerEnd := nameEnd + 1 + 4 + 4 + 2 + 4 + 4
	if headerEnd+2 > len(raw) {
		return nil, ErrCorrupt
	}
	typeOff := nameEnd + 1
	dataLen := int(binary.BigEndian.Uint32(raw[typeOff+10 : typeOff+14]))
	rsrcLen := int(binary.BigEndian.Uint32(raw[typeOff+14 : typeOff+18]))
	dataPart, next, ok := readBinHexFork(raw, headerEnd+2, dataLen)
	if !ok {
		return nil, ErrCorrupt
	}
	rsrcPart, _, ok := readBinHexFork(raw, next, rsrcLen)
	if !ok {
		return nil, ErrCorrupt
	}
	var fi [32]byte
	copy(fi[0:4], raw[typeOff:typeOff+4])
	copy(fi[4:8], raw[typeOff+4:typeOff+8])
	fi[8] = raw[typeOff+8]
	fi[9] = raw[typeOff+9]
	return &Node{
		Name:       string(raw[1:nameEnd]),
		Data:       dataPart,
		Resource:   rsrcPart,
		FinderInfo: fi,
	}, nil
}

func decodeBinHex6(payload string) []byte {
	var out []byte
	var acc uint
	var bits int
	for i := 0; i < len(payload); i++ {
		c := payload[i]
		if c > 127 || isBinHexWS(c) {
			if c > 127 {
				return nil
			}
			continue
		}
		v := binHexDecode[c]
		if v < 0 {
			return nil
		}
		acc = (acc << 6) | uint(v)
		bits += 6
		if bits >= 8 {
			bits -= 8
			out = append(out, byte(acc>>bits))
		}
	}
	return out
}

func decodeBinHexRLE(src []byte) []byte {
	var out []byte
	for i := 0; i < len(src); i++ {
		b := src[i]
		if b != binHexRLE {
			out = append(out, b)
			continue
		}
		i++
		if i >= len(src) {
			return nil
		}
		n := src[i]
		if n == 0 {
			out = append(out, binHexRLE)
			continue
		}
		if len(out) == 0 {
			return nil
		}
		prev := out[len(out)-1]
		for k := 1; k < int(n); k++ {
			out = append(out, prev)
		}
	}
	return out
}

func readBinHexFork(raw []byte, off, length int) ([]byte, int, bool) {
	if off+length+2 > len(raw) {
		return nil, 0, false
	}
	bytes := append([]byte(nil), raw[off:off+length]...)
	return bytes, off + length + 2, true
}
