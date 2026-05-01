package encoding

import (
	"bytes"
	"testing"
)

func TestMacRomanToUpper(t *testing.T) {
	t.Parallel()
	// Re-implement the old logic for a correctness check
	atalkLower := []byte("abcdefghijklmnopqrstuvwxyz\x88\x8A\x8B\x8C\x8D\x8E\x96\x9A\x9B\x9F\xBE\xBF\xCF")
	atalkUpper := []byte("ABCDEFGHIJKLMNOPQRSTUVWXYZ\xCB\x80\xCC\x81\x82\x83\x84\x85\xCD\x86\xAE\xAF\xCE")

	oldUCase := func(input []byte) []byte {
		out := make([]byte, len(input))
		for i, b := range input {
			idx := bytes.IndexByte(atalkLower, b)
			if idx >= 0 {
				out[i] = atalkUpper[idx]
			} else {
				out[i] = b
			}
		}
		return out
	}

	for i := range 256 {
		input := []byte{byte(i)}
		expected := oldUCase(input)
		actual := MacRomanToUpper(input)
		if !bytes.Equal(expected, actual) {
			t.Errorf("byte %d (0x%x): expected %x, got %x", i, i, expected, actual)
		}
	}

	input := []byte("Hello, AppleTalk Zone\x88\x8A!")
	expected := oldUCase(input)
	actual := MacRomanToUpper(input)
	if !bytes.Equal(expected, actual) {
		t.Errorf("string test failed: expected %x, got %x", expected, actual)
	}
}

func TestMacRomanToUTF8(t *testing.T) {
	t.Parallel()
	input := []byte{'M', 'a', 'c', ' ', '\x80', '\x81', '\x82'}
	expected := "Mac ÄÅÇ"
	actual := MacRomanToUTF8(input)
	if expected != actual {
		t.Errorf("MacRomanToUTF8 failed: expected %q, got %q", expected, actual)
	}
}

func TestUTF8ToMacRoman(t *testing.T) {
	t.Parallel()
	input := "Mac ÄÅÇ"
	expected := []byte{'M', 'a', 'c', ' ', '\x80', '\x81', '\x82'}
	actual := UTF8ToMacRoman(input)
	if !bytes.Equal(expected, actual) {
		t.Errorf("UTF8ToMacRoman failed: expected %x, got %x", expected, actual)
	}

	input2 := "Mac 🤔"
	expected2 := []byte{'M', 'a', 'c', ' ', '?'}
	actual2 := UTF8ToMacRoman(input2)
	if !bytes.Equal(expected2, actual2) {
		t.Errorf("UTF8ToMacRoman fallback failed: expected %x, got %x", expected2, actual2)
	}
}
