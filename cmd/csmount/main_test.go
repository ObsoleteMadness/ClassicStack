//go:build windows || darwin || linux

package main

import "testing"

func TestNativeForksUnix(t *testing.T) {
	on := []string{"", "passthrough", "native", "hfs", "ads", "xattr", "NATIVE"}
	for _, f := range on {
		if !nativeForksUnix(f) {
			t.Errorf("nativeForksUnix(%q) = false, want true", f)
		}
	}
	off := []string{"appledouble", "appledouble-dir", "derez", "applesingle", "macbinary", "nofork", "auto"}
	for _, f := range off {
		if nativeForksUnix(f) {
			t.Errorf("nativeForksUnix(%q) = true, want false", f)
		}
	}
}
