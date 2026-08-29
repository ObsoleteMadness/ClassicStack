//go:build windows

package serial

import "testing"

// TestNormalizeDeviceName_Windows pins the COMn → \\.\COMn mapping the Windows
// serial API needs for ports above COM9, and that already-prefixed / non-COM names
// pass through unchanged (spec/08 §"Windows-Specific Notes").
func TestNormalizeDeviceName_Windows(t *testing.T) {
	cases := []struct{ in, want string }{
		{"COM3", `\\.\COM3`},
		{"com12", `\\.\COM12`},
		{`\\.\COM5`, `\\.\COM5`},         // already prefixed
		{"/dev/ttyUSB0", "/dev/ttyUSB0"}, // non-COM, unchanged
		{"COMX", "COMX"},                 // not a numbered COM port
	}
	for _, c := range cases {
		if got := normalizeDeviceName(c.in); got != c.want {
			t.Errorf("normalizeDeviceName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
