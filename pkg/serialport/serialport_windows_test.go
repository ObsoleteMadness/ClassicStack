//go:build windows

package serialport

import "testing"

func TestComNumber(t *testing.T) {
	cases := []struct {
		name string
		want int
		ok   bool
	}{
		{"COM3", 3, true},
		{"com10", 10, true},
		{"COM1", 1, true},
		{"/dev/ttyS0", 0, false},
		{"COMx", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		n, ok := comNumber(c.name)
		if ok != c.ok || (ok && n != c.want) {
			t.Errorf("comNumber(%q) = (%d, %v), want (%d, %v)", c.name, n, ok, c.want, c.ok)
		}
	}
}
