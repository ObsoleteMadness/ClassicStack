//go:build !windows && !darwin && !linux

package fs

// On a platform with no host-native fork layout wired (e.g. a TinyGo/headless target),
// "native" has no target and is unavailable; the alias returns
// ErrNativeForkUnsupported. Such a share should name a portable engine explicitly
// (appledouble / applesingle / nofork).
const nativeForkTarget = ""
