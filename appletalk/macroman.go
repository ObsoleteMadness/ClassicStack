package appletalk

import "github.com/pgodw/omnitalk/go/encoding"

func MacRomanToUpper(b []byte) []byte { return encoding.MacRomanToUpper(b) }

func MacRomanToLower(b []byte) []byte { return encoding.MacRomanToLower(b) }

func MacRomanToUTF8(b []byte) string { return encoding.MacRomanToUTF8(b) }

func UTF8ToMacRoman(s string) []byte { return encoding.UTF8ToMacRoman(s) }
